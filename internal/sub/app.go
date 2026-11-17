package sub

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/link"
	"github.com/mhsanaei/3x-ui/v3/internal/util/loginlimit"
	"github.com/mhsanaei/3x-ui/v3/internal/web/clientip"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// AppAccount is the account a session belongs to, summed over its subscription.
// ExpiryTime is in ms (0 never, negative counts from first use); Total 0 is unlimited.
type AppAccount struct {
	Username    string `json:"username" example:"dana@example.com"`
	Enabled     bool   `json:"enabled" example:"true"`
	ExpiryTime  int64  `json:"expiryTime" example:"1767225600000"`
	Upload      int64  `json:"upload" example:"1048576"`
	Download    int64  `json:"download" example:"52428800"`
	Total       int64  `json:"total" example:"53687091200"`
	DeviceLimit int    `json:"deviceLimit" example:"2"`
	Devices     int    `json:"devices" example:"1"`
}

// AppConfig is one entry of the subscription. Outbound is set when Xray can run
// it; LocationID names its server and is absent on a client's external link.
type AppConfig struct {
	Protocol   string         `json:"protocol" example:"vless"`
	Name       string         `json:"name" example:"vless-de"`
	Link       string         `json:"link" example:"vless://11111111-2222-4333-8444-555555555555@de.example.com:8443?type=tcp#vless-de"`
	LocationID *int           `json:"locationId,omitempty" example:"0"`
	Outbound   map[string]any `json:"outbound,omitempty"`
}

// AppProfile is what GET .../app/v1/profile answers a signed-in device.
type AppProfile struct {
	Account    AppAccount          `json:"account"`
	Title      string              `json:"title,omitempty" example:"DataCraft"`
	SupportURL string              `json:"supportUrl,omitempty" example:"https://t.me/support"`
	Announce   string              `json:"announce,omitempty" example:"Maintenance on Friday"`
	Locations  []model.AppLocation `json:"locations"`
	Configs    []AppConfig         `json:"configs"`
}

type appLoginRequest struct {
	Username   string `json:"username"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	OS         string `json:"os"`
	OSVersion  string `json:"osVersion"`
	AppVersion string `json:"appVersion"`
}

const appRequestBodyLimit = 4 << 10

func newAppLoginLimiter() *loginlimit.Limiter {
	return loginlimit.New(5, 5*time.Minute, 15*time.Minute)
}

func (a *SUBController) registerAppRoutes(g *gin.RouterGroup) {
	g.POST(":subid/app/v1/login", a.appLogin)
	g.GET(":subid/app/v1/profile", a.appProfile)
	g.POST(":subid/app/v1/logout", a.appLogout)
}

func appError(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"error": code})
}

// appServiceError answers an AppService error; the codes are the app's contract.
func appServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAppDisabled):
		appError(c, http.StatusForbidden, "app_disabled")
	case errors.Is(err, service.ErrAppBadLogin):
		appError(c, http.StatusUnauthorized, "bad_login")
	case errors.Is(err, service.ErrAppBadDevice):
		appError(c, http.StatusBadRequest, "bad_device")
	case errors.Is(err, service.ErrAppDeviceLimit):
		appError(c, http.StatusForbidden, "device_limit")
	case errors.Is(err, service.ErrAppSession):
		appError(c, http.StatusUnauthorized, "bad_session")
	default:
		logger.Warning("app API:", err)
		appError(c, http.StatusInternalServerError, "server_error")
	}
}

func (a *SUBController) appLogin(c *gin.Context) {
	subID := c.Param("subid")
	ip := clientip.Of(c)
	if _, ok := a.appLimiter.Allow(ip, subID); !ok {
		appError(c, http.StatusTooManyRequests, "too_many_attempts")
		return
	}
	var req appLoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, appRequestBodyLimit)).Decode(&req); err != nil {
		a.appLimiter.RegisterFailure(ip, subID)
		appError(c, http.StatusBadRequest, "bad_request")
		return
	}
	token, err := a.appService.Login(subID, req.Username, service.AppDevice{
		ID: req.DeviceID, Name: req.DeviceName, OS: req.OS, OSVersion: req.OSVersion,
		AppVersion: req.AppVersion, UserAgent: c.GetHeader("User-Agent"),
	})
	// A refused device came with the right username: only a guess counts against the limit.
	if err == nil || errors.Is(err, service.ErrAppDeviceLimit) {
		a.appLimiter.RegisterSuccess(ip, subID)
	} else {
		a.appLimiter.RegisterFailure(ip, subID)
	}
	if err != nil {
		appServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

func appBearerToken(c *gin.Context) string {
	token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

func (a *SUBController) appSession(c *gin.Context) (model.AppSession, bool) {
	session, err := a.appService.Session(c.Param("subid"), appBearerToken(c), c.GetHeader("X-HWID"))
	if err != nil {
		appServiceError(c, err)
		return session, false
	}
	return session, true
}

func (a *SUBController) appProfile(c *gin.Context) {
	session, ok := a.appSession(c)
	if !ok {
		return
	}
	subID := c.Param("subid")
	_, host, _, _ := a.subService.ResolveRequest(c)
	subReq := a.subService.ForRequest(host)
	subReq.subscriptionBody = false
	links, _, _, _, traffic, err := subReq.subLinks(subID)
	if err != nil {
		appServiceError(c, err)
		return
	}
	configs := appConfigs(links)
	locations := []model.AppLocation{}
	if ids := appLocationIDs(configs); len(ids) > 0 {
		if locations, err = a.appService.Locations(ids...); err != nil {
			appServiceError(c, err)
			return
		}
	}
	devices, _, err := a.clientService.HwidSlotStatusForSubID(subID)
	if err != nil {
		appServiceError(c, err)
		return
	}
	metadata := a.metadataForSubRequest(func() *SubService { return subReq }, subID, "")
	c.JSON(http.StatusOK, AppProfile{
		Account: AppAccount{
			Username:    session.Email,
			Enabled:     traffic.Enable,
			ExpiryTime:  traffic.ExpiryTime,
			Upload:      traffic.Up,
			Download:    traffic.Down,
			Total:       traffic.Total,
			DeviceLimit: devices.Limit,
			Devices:     devices.Registered,
		},
		Title:      metadata.Title,
		SupportURL: metadata.SupportURL,
		Announce:   metadata.Announce,
		Locations:  locations,
		Configs:    configs,
	})
	if err := a.subService.RecordSubscriptionFetch(subID); err != nil {
		logger.Warning("Failed to record subscription fetch:", err)
	}
}

func (a *SUBController) appLogout(c *gin.Context) {
	if err := a.appService.Logout(c.Param("subid"), appBearerToken(c)); err != nil {
		appServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{})
}

// appConfigs splits the subscription's entries into one config per line, an
// entry rendering a line per endpoint, with the Xray outbound of each it can run.
func appConfigs(links []subLink) []AppConfig {
	configs := []AppConfig{}
	for _, entry := range links {
		for line := range strings.SplitSeq(entry.text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			cfg := AppConfig{Link: line, Name: linkDisplayName(line)}
			if entry.inbound != nil {
				nodeID := 0
				if entry.inbound.NodeID != nil {
					nodeID = *entry.inbound.NodeID
				}
				cfg.LocationID = &nodeID
				if cfg.Name == "" {
					cfg.Name = entry.inbound.Remark
				}
			}
			if res, err := link.ParseLink(line); err == nil && res.Outbound != nil {
				cfg.Outbound = map[string]any(res.Outbound)
				cfg.Protocol, _ = cfg.Outbound["protocol"].(string)
			} else if scheme, _, ok := strings.Cut(line, "://"); ok {
				cfg.Protocol = strings.ToLower(scheme)
			}
			configs = append(configs, cfg)
		}
	}
	return configs
}

func appLocationIDs(configs []AppConfig) []int {
	var ids []int
	for _, cfg := range configs {
		if cfg.LocationID != nil && !slices.Contains(ids, *cfg.LocationID) {
			ids = append(ids, *cfg.LocationID)
		}
	}
	slices.Sort(ids)
	return ids
}
