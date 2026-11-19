package sub

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
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

// AppConfig is one entry of the subscription. Outbound is set when Xray can run it;
// LocationID is absent when its inbound serves no location, as on an external link.
type AppConfig struct {
	Protocol   string         `json:"protocol" example:"vless"`
	Name       string         `json:"name" example:"vless-de"`
	Link       string         `json:"link" example:"vless://11111111-2222-4333-8444-555555555555@de.example.com:8443?type=tcp#vless-de"`
	LocationID *int           `json:"locationId,omitempty" example:"1"`
	Outbound   map[string]any `json:"outbound,omitempty"`
}

// AppPlace is a location as the app sees it, without the panel's inbound ids.
type AppPlace struct {
	ID          int    `json:"id" example:"1"`
	Name        string `json:"name" example:"Germany"`
	CountryCode string `json:"countryCode" example:"DE"`
	City        string `json:"city" example:"Frankfurt"`
	Timezone    string `json:"timezone" example:"Europe/Berlin"`
}

// AppProfile is what GET .../app/v1/profile answers a signed-in device. Notices
// are the panel's published notices, newest first.
type AppProfile struct {
	Account    AppAccount        `json:"account"`
	Title      string            `json:"title,omitempty" example:"DataCraft"`
	SupportURL string            `json:"supportUrl,omitempty" example:"https://t.me/support"`
	Announce   string            `json:"announce,omitempty" example:"Maintenance on Friday"`
	Locations  []AppPlace        `json:"locations"`
	Configs    []AppConfig       `json:"configs"`
	Notices    []model.AppNotice `json:"notices"`
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

// A report's log is capped at 8 MB (service.FileReport); its JSON comes gzipped, so the cap
// holds on what the stream expands to as well as on what crosses the wire.
const (
	appReportBodyLimit = 3 << 20
	appReportJSONLimit = 9 << 20
)

type appReportRequest struct {
	Message    string `json:"message"`
	Log        string `json:"log"`
	DeviceName string `json:"deviceName"`
	OS         string `json:"os"`
	OSVersion  string `json:"osVersion"`
	AppVersion string `json:"appVersion"`
}

// The newest notices only: the panel keeps every notice until an admin deletes it.
const maxAppProfileNotices = 50

func newAppLoginLimiter() *loginlimit.Limiter {
	return loginlimit.New(5, 5*time.Minute, 15*time.Minute)
}

func (a *SUBController) registerAppRoutes(g *gin.RouterGroup) {
	g.POST(":subid/app/v1/login", a.appLogin)
	g.GET(":subid/app/v1/profile", a.appProfile)
	g.POST(":subid/app/v1/logout", a.appLogout)
	g.POST(":subid/app/v1/report", a.appReport)
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
	case errors.Is(err, service.ErrAppReportTooLarge):
		appError(c, http.StatusRequestEntityTooLarge, "too_large")
	case errors.Is(err, service.ErrAppTooManyReports):
		appError(c, http.StatusTooManyRequests, "too_many_reports")
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
	subReq.signedInApp = true
	links, _, _, _, traffic, err := subReq.subLinks(subID)
	if err != nil {
		appServiceError(c, err)
		return
	}
	locationOf, err := a.appService.LocationOfInbounds()
	if err != nil {
		appServiceError(c, err)
		return
	}
	configs := appConfigs(links, locationOf)
	locations := []AppPlace{}
	if ids := appLocationIDs(configs); len(ids) > 0 {
		all, err := a.appService.Locations()
		if err != nil {
			appServiceError(c, err)
			return
		}
		for _, loc := range all {
			if slices.Contains(ids, loc.Id) {
				locations = append(locations, AppPlace{ID: loc.Id, Name: loc.Name, CountryCode: loc.CountryCode, City: loc.City, Timezone: loc.Timezone})
			}
		}
	}
	devices, _, err := a.clientService.HwidSlotStatusForSubID(subID)
	if err != nil {
		appServiceError(c, err)
		return
	}
	notices, err := a.appService.Notices()
	if err != nil {
		appServiceError(c, err)
		return
	}
	notices = notices[:min(len(notices), maxAppProfileNotices)]
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
		Notices:    notices,
	})
	if err := a.subService.RecordSubscriptionFetch(subID); err != nil {
		logger.Warning("Failed to record subscription fetch:", err)
	}
}

// appReport files a support report: the user's message and the app's log, under the account
// and device of the session; it answers the report's number for the user to quote.
func (a *SUBController) appReport(c *gin.Context) {
	session, ok := a.appSession(c)
	if !ok {
		return
	}
	var body io.Reader = http.MaxBytesReader(c.Writer, c.Request.Body, appReportBodyLimit)
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("Content-Encoding")), "gzip") {
		zr, err := gzip.NewReader(body)
		if err != nil {
			appError(c, http.StatusBadRequest, "bad_request")
			return
		}
		defer zr.Close()
		body = zr
	}
	expanded := &io.LimitedReader{R: body, N: appReportJSONLimit + 1}
	var req appReportRequest
	if err := json.NewDecoder(expanded).Decode(&req); err != nil {
		var wire *http.MaxBytesError
		if errors.As(err, &wire) || expanded.N <= 0 {
			appError(c, http.StatusRequestEntityTooLarge, "too_large")
			return
		}
		appError(c, http.StatusBadRequest, "bad_request")
		return
	}
	report, err := a.appService.FileReport(session, service.AppReportInput{
		Message: req.Message, Log: req.Log, DeviceName: req.DeviceName, OS: req.OS, OSVersion: req.OSVersion, AppVersion: req.AppVersion,
	})
	if err != nil {
		appServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": report.Id})
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
func appConfigs(links []subLink, locationOf map[int]int) []AppConfig {
	configs := []AppConfig{}
	for _, entry := range links {
		for line := range strings.SplitSeq(entry.text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			cfg := AppConfig{Link: line, Name: linkDisplayName(line)}
			if entry.inbound != nil {
				if id, ok := locationOf[entry.inbound.Id]; ok {
					cfg.LocationID = &id
				}
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
