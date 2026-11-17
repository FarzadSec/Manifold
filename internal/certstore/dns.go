package certstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider writes and removes the TXT records a DNS-01 challenge asks for.
type Provider interface {
	Present(ctx context.Context, fqdn, value string) (id string, err error)
	CleanUp(ctx context.Context, fqdn, id string) error
}

const (
	CloudflareAPI = "https://api.cloudflare.com/client/v4"
	ArvanCloudAPI = "https://napi.arvancloud.ir/cdn/4.0"
	challengeTTL  = 120
)

var defaultHTTP = &http.Client{Timeout: 30 * time.Second}

// zoneCandidates are the names a zone for fqdn can have, longest first, without the
// first label (the record itself) and without a bare top-level domain.
func zoneCandidates(fqdn string) []string {
	labels := strings.Split(strings.TrimSuffix(fqdn, "."), ".")
	var out []string
	for i := 1; i < len(labels)-1; i++ {
		out = append(out, strings.Join(labels[i:], "."))
	}
	return out
}

func call(ctx context.Context, client *http.Client, method, url string, header http.Header, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if client == nil {
		client = defaultHTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil && resp.StatusCode/100 == 2 {
			return resp.StatusCode, fmt.Errorf("decode %s: %w", url, err)
		}
	}
	return resp.StatusCode, nil
}

// Cloudflare uses an API token with Zone.DNS edit on the zone.
type Cloudflare struct {
	Token   string
	BaseURL string
	Client  *http.Client
}

type cfAnswer struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}

func (a cfAnswer) err() error {
	if a.Success {
		return nil
	}
	if len(a.Errors) > 0 {
		return fmt.Errorf("cloudflare: %s (%d)", a.Errors[0].Message, a.Errors[0].Code)
	}
	return errors.New("cloudflare refused the request")
}

func (c *Cloudflare) do(ctx context.Context, method, path string, body any) (cfAnswer, error) {
	base := c.BaseURL
	if base == "" {
		base = CloudflareAPI
	}
	var ans cfAnswer
	_, err := call(ctx, c.Client, method, strings.TrimSuffix(base, "/")+path,
		http.Header{"Authorization": {"Bearer " + c.Token}}, body, &ans)
	if err != nil {
		return ans, err
	}
	return ans, ans.err()
}

func (c *Cloudflare) zone(ctx context.Context, fqdn string) (string, error) {
	for _, name := range zoneCandidates(fqdn) {
		ans, err := c.do(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(name), nil)
		if err != nil {
			return "", err
		}
		var zones []struct{ ID string }
		if json.Unmarshal(ans.Result, &zones) == nil && len(zones) > 0 {
			return zones[0].ID, nil
		}
	}
	return "", fmt.Errorf("cloudflare: no zone this token can reach holds %s", fqdn)
}

func (c *Cloudflare) Present(ctx context.Context, fqdn, value string) (string, error) {
	zone, err := c.zone(ctx, fqdn)
	if err != nil {
		return "", err
	}
	ans, err := c.do(ctx, http.MethodPost, "/zones/"+zone+"/dns_records", map[string]any{
		"type": "TXT", "name": fqdn, "content": value, "ttl": challengeTTL,
	})
	if err != nil {
		return "", err
	}
	var rec struct{ ID string }
	if err := json.Unmarshal(ans.Result, &rec); err != nil || rec.ID == "" {
		return "", errors.New("cloudflare: the record was made without an id")
	}
	return zone + "/" + rec.ID, nil
}

func (c *Cloudflare) CleanUp(ctx context.Context, _, id string) error {
	zone, rec, ok := strings.Cut(id, "/")
	if !ok {
		return errors.New("cloudflare: bad record id")
	}
	_, err := c.do(ctx, http.MethodDelete, "/zones/"+zone+"/dns_records/"+rec, nil)
	return err
}

// ArvanCloud uses an API key from the ArvanCloud panel ("Apikey …").
type ArvanCloud struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

func (a *ArvanCloud) do(ctx context.Context, method, path string, body, out any) (int, error) {
	base := a.BaseURL
	if base == "" {
		base = ArvanCloudAPI
	}
	key := strings.TrimSpace(a.APIKey)
	if !strings.HasPrefix(strings.ToLower(key), "apikey ") {
		key = "Apikey " + key
	}
	var problem struct {
		Message string `json:"message"`
	}
	target := out
	if target == nil {
		target = &problem
	}
	code, err := call(ctx, a.Client, method, strings.TrimSuffix(base, "/")+path,
		http.Header{"Authorization": {key}, "Accept": {"application/json"}}, body, target)
	if err != nil {
		return code, err
	}
	if code/100 != 2 {
		return code, fmt.Errorf("arvancloud: %s answered %d %s", path, code, problem.Message)
	}
	return code, nil
}

func (a *ArvanCloud) zone(ctx context.Context, fqdn string) (string, error) {
	for _, name := range zoneCandidates(fqdn) {
		code, err := a.do(ctx, http.MethodGet, "/domains/"+url.PathEscape(name), nil, &struct{}{})
		if err == nil {
			return name, nil
		}
		if code != http.StatusNotFound {
			return "", err
		}
	}
	return "", fmt.Errorf("arvancloud: no domain on this account holds %s", fqdn)
}

func (a *ArvanCloud) Present(ctx context.Context, fqdn, value string) (string, error) {
	zone, err := a.zone(ctx, fqdn)
	if err != nil {
		return "", err
	}
	name := strings.TrimSuffix(strings.TrimSuffix(fqdn, "."), "."+zone)
	var ans struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if _, err := a.do(ctx, http.MethodPost, "/domains/"+url.PathEscape(zone)+"/dns-records", map[string]any{
		"type": "txt", "name": name, "value": map[string]string{"text": value}, "ttl": challengeTTL, "cloud": false,
	}, &ans); err != nil {
		return "", err
	}
	if ans.Data.ID == "" {
		return "", errors.New("arvancloud: the record was made without an id")
	}
	return zone + "/" + ans.Data.ID, nil
}

func (a *ArvanCloud) CleanUp(ctx context.Context, _, id string) error {
	zone, rec, ok := strings.Cut(id, "/")
	if !ok {
		return errors.New("arvancloud: bad record id")
	}
	_, err := a.do(ctx, http.MethodDelete, "/domains/"+url.PathEscape(zone)+"/dns-records/"+url.PathEscape(rec), nil, nil)
	return err
}
