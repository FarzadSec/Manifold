package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const pageSize = 500

var remoteClient = &http.Client{Timeout: 60 * time.Second}

func getJSON(ctx context.Context, client *http.Client, target, bearer string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s answered %s: %s", target, resp.Status, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	return json.Unmarshal(raw, out)
}

// Remote reads users from a panel's API instead of its database file.
type Remote struct {
	URL      string
	Username string
	Password string
	Token    string
	Client   *http.Client
}

func (r Remote) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return remoteClient
}

func (r Remote) base() (string, error) {
	u, err := url.Parse(strings.TrimSpace(r.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("the panel address must be an http(s) URL")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

// ReadMarzbanAPI reads a Marzban or PasarGuard panel through its admin API, which works
// whatever database it keeps. Its subscription secret is not in the API; secret, when
// given, lets old token links keep working.
func (r Remote) ReadMarzbanAPI(ctx context.Context, secret string) (*Result, error) {
	base, err := r.base()
	if err != nil {
		return nil, err
	}
	token := r.Token
	if token == "" {
		form := url.Values{"username": {r.Username}, "password": {r.Password}, "grant_type": {"password"}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/admin/token", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := r.client().Do(req)
		if err != nil {
			return nil, err
		}
		var ans struct {
			AccessToken string `json:"access_token"`
			Detail      any    `json:"detail"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ans)
		resp.Body.Close()
		if ans.AccessToken == "" {
			return nil, fmt.Errorf("the panel refused the login (%s)", resp.Status)
		}
		token = ans.AccessToken
	}
	res := &Result{Source: "marzban", TokenSecret: strings.TrimSpace(secret)}
	for offset := 0; ; offset += pageSize {
		var page struct {
			Users []struct {
				Username      string                     `json:"username"`
				Status        string                     `json:"status"`
				DataLimit     *int64                     `json:"data_limit"`
				UsedTraffic   int64                      `json:"used_traffic"`
				Expire        any                        `json:"expire"`
				Note          string                     `json:"note"`
				Reset         string                     `json:"data_limit_reset_strategy"`
				Proxies       map[string]json.RawMessage `json:"proxies"`
				ProxySettings map[string]json.RawMessage `json:"proxy_settings"`
				SubURL        string                     `json:"subscription_url"`
			} `json:"users"`
			Total int `json:"total"`
		}
		q := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(pageSize)}}
		if err := getJSON(ctx, r.client(), base+"/api/users?"+q.Encode(), token, &page); err != nil {
			return nil, err
		}
		for _, u := range page.Users {
			creds := u.Proxies
			if len(creds) == 0 {
				creds = u.ProxySettings
				res.Source = "pasarguard"
			}
			user := User{
				Username: u.Username, Enabled: u.Status == "active" || u.Status == "on_hold",
				Down: u.UsedTraffic, Expire: moment(u.Expire), Note: u.Note, Reset: resetName(u.Reset),
				Creds: marzbanCreds(creds), TokenName: u.Username,
			}
			if u.DataLimit != nil {
				user.DataLimit = *u.DataLimit
			}
			if i := strings.LastIndex(strings.TrimSuffix(u.SubURL, "/"), "/sub/"); i >= 0 {
				user.Aliases = append(user.Aliases, strings.Trim(u.SubURL[i+len("/sub/"):], "/"))
			}
			res.Users = append(res.Users, user)
		}
		if len(page.Users) < pageSize || len(res.Users) >= page.Total {
			break
		}
	}
	if res.TokenSecret == "" {
		res.Warnings = append(res.Warnings, "without the subscription secret only each user's current link keeps working; older links with other tokens do not")
	}
	return res, nil
}

// ReadRemnawave reads a Remnawave panel through its API with an API token.
func (r Remote) ReadRemnawave(ctx context.Context) (*Result, error) {
	base, err := r.base()
	if err != nil {
		return nil, err
	}
	res := &Result{Source: "remnawave"}
	for start := 0; ; start += pageSize {
		var page struct {
			Response struct {
				Users []struct {
					ShortUUID        string `json:"shortUuid"`
					Username         string `json:"username"`
					Status           string `json:"status"`
					UsedTrafficBytes any    `json:"usedTrafficBytes"`
					UserTraffic      *struct {
						UsedTrafficBytes any `json:"usedTrafficBytes"`
					} `json:"userTraffic"`
					TrafficLimitBytes    any    `json:"trafficLimitBytes"`
					TrafficLimitStrategy string `json:"trafficLimitStrategy"`
					ExpireAt             string `json:"expireAt"`
					VlessUUID            string `json:"vlessUuid"`
					TrojanPassword       string `json:"trojanPassword"`
					SSPassword           string `json:"ssPassword"`
					Description          string `json:"description"`
					TelegramID           any    `json:"telegramId"`
				} `json:"users"`
				Total int `json:"total"`
			} `json:"response"`
		}
		q := url.Values{"start": {strconv.Itoa(start)}, "size": {strconv.Itoa(pageSize)}}
		if err := getJSON(ctx, r.client(), base+"/api/users?"+q.Encode(), r.Token, &page); err != nil {
			return nil, err
		}
		for _, u := range page.Response.Users {
			used := u.UsedTrafficBytes
			if u.UserTraffic != nil {
				used = u.UserTraffic.UsedTrafficBytes
			}
			creds := map[string]Cred{}
			if u.VlessUUID != "" {
				creds[VLESS] = Cred{ID: u.VlessUUID}
			}
			if u.TrojanPassword != "" {
				creds[Trojan] = Cred{Password: u.TrojanPassword}
			}
			if u.SSPassword != "" {
				creds[Shadowsocks] = Cred{Password: u.SSPassword, Method: "chacha20-ietf-poly1305"}
			}
			res.Users = append(res.Users, User{
				Username: u.Username, Enabled: u.Status == "ACTIVE", DataLimit: num(u.TrafficLimitBytes),
				Down: num(used), Expire: moment(u.ExpireAt), Note: u.Description, TgID: num(u.TelegramID),
				Reset: resetName(u.TrafficLimitStrategy), Creds: creds, SubID: u.ShortUUID,
			})
		}
		if len(page.Response.Users) < pageSize || len(res.Users) >= page.Response.Total {
			break
		}
	}
	return res, nil
}

// ReadHiddify reads the JSON backup Hiddify Manager downloads (Backup → Download).
func ReadHiddify(raw []byte) (*Result, error) {
	var backup struct {
		Users []struct {
			UUID           string  `json:"uuid"`
			Name           string  `json:"name"`
			UsageLimitGB   float64 `json:"usage_limit_GB"`
			CurrentUsageGB float64 `json:"current_usage_GB"`
			PackageDays    int     `json:"package_days"`
			StartDate      string  `json:"start_date"`
			Mode           string  `json:"mode"`
			Enable         *bool   `json:"enable"`
			Comment        string  `json:"comment"`
			TelegramID     any     `json:"telegram_id"`
		} `json:"users"`
	}
	if err := json.Unmarshal(raw, &backup); err != nil || backup.Users == nil {
		return nil, errors.New("this is not a Hiddify backup (no users list)")
	}
	res := &Result{Source: "hiddify"}
	const gib = 1 << 30
	for _, u := range backup.Users {
		user := User{
			Username: u.Name, Enabled: u.Enable == nil || *u.Enable, DataLimit: int64(u.UsageLimitGB * gib),
			Down: int64(u.CurrentUsageGB * gib), Note: u.Comment, TgID: num(u.TelegramID), SubID: u.UUID,
			Reset: resetName(map[string]string{"daily": "daily", "weekly": "weekly", "monthly": "monthly"}[u.Mode]),
			Creds: map[string]Cred{VLESS: {ID: u.UUID}, VMess: {ID: u.UUID}, Trojan: {Password: u.UUID}},
		}
		if start := moment(u.StartDate); start > 0 && u.PackageDays > 0 {
			user.Expire = start + int64(u.PackageDays)*24*3600*1000
		} else if u.PackageDays > 0 {
			user.Expire = time.Now().Add(time.Duration(u.PackageDays) * 24 * time.Hour).UnixMilli()
			res.Warnings = append(res.Warnings, u.Name+" had not started its package in Hiddify; its days count from now")
		}
		res.Users = append(res.Users, user)
	}
	return res, nil
}
