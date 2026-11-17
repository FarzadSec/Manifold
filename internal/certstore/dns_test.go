package certstore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type apiCall struct {
	method, path, auth string
	body               map[string]any
}

func recordingAPI(t *testing.T, answer func(c apiCall) (int, string)) (*httptest.Server, *[]apiCall) {
	t.Helper()
	var calls []apiCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := apiCall{method: r.Method, path: r.URL.RequestURI(), auth: r.Header.Get("Authorization")}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &c.body)
		calls = append(calls, c)
		code, body := answer(c)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestCloudflareFindsTheZoneAndRemovesItsRecord(t *testing.T) {
	srv, calls := recordingAPI(t, func(c apiCall) (int, string) {
		switch {
		case c.path == "/zones?name=example.com":
			return 200, `{"success":true,"result":[{"id":"z1","name":"example.com"}]}`
		case strings.HasPrefix(c.path, "/zones?name="):
			return 200, `{"success":true,"result":[]}`
		case c.method == "POST" && c.path == "/zones/z1/dns_records":
			return 200, `{"success":true,"result":{"id":"r9"}}`
		case c.method == "DELETE" && c.path == "/zones/z1/dns_records/r9":
			return 200, `{"success":true,"result":{"id":"r9"}}`
		}
		return 400, `{"success":false,"errors":[{"code":1003,"message":"Invalid or missing zone id."}]}`
	})
	cf := &Cloudflare{Token: "tok", BaseURL: srv.URL}
	id, err := cf.Present(context.Background(), "_acme-challenge.a.b.example.com", "v4lue")
	if err != nil {
		t.Fatal(err)
	}
	if err := cf.CleanUp(context.Background(), "", id); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range *calls {
		paths = append(paths, c.method+" "+c.path)
		if c.auth != "Bearer tok" {
			t.Fatalf("%s sent Authorization %q", c.path, c.auth)
		}
	}
	want := "GET /zones?name=a.b.example.com,GET /zones?name=b.example.com,GET /zones?name=example.com,POST /zones/z1/dns_records,DELETE /zones/z1/dns_records/r9"
	if strings.Join(paths, ",") != want {
		t.Fatalf("calls:\n%s\nwant\n%s", strings.Join(paths, ","), want)
	}
	if b := (*calls)[3].body; b["type"] != "TXT" || b["name"] != "_acme-challenge.a.b.example.com" || b["content"] != "v4lue" {
		t.Fatalf("record body %v", b)
	}
	if got := cf.CleanUp(context.Background(), "", "z2/r1"); got == nil || !strings.Contains(got.Error(), "Invalid or missing zone id") {
		t.Fatalf("a refused call gave %v", got)
	}
}

func TestArvanCloudFindsTheDomainAndWritesARelativeName(t *testing.T) {
	srv, calls := recordingAPI(t, func(c apiCall) (int, string) {
		switch {
		case c.path == "/domains/example.ir":
			return 200, `{"data":{"domain":"example.ir"}}`
		case c.method == "GET":
			return 404, `{"message":"Domain not found"}`
		case c.method == "POST" && c.path == "/domains/example.ir/dns-records":
			return 201, `{"data":{"id":"rec-1"}}`
		case c.method == "DELETE" && c.path == "/domains/example.ir/dns-records/rec-1":
			return 200, `{"message":"deleted"}`
		}
		return 422, `{"message":"bad"}`
	})
	ar := &ArvanCloud{APIKey: "k3y", BaseURL: srv.URL}
	id, err := ar.Present(context.Background(), "_acme-challenge.vpn.example.ir", "v4lue")
	if err != nil {
		t.Fatal(err)
	}
	if err := ar.CleanUp(context.Background(), "", id); err != nil {
		t.Fatal(err)
	}
	post := (*calls)[2]
	if post.auth != "Apikey k3y" {
		t.Fatalf("Authorization %q, want the key with its Apikey prefix", post.auth)
	}
	value, _ := post.body["value"].(map[string]any)
	if post.body["type"] != "txt" || post.body["name"] != "_acme-challenge.vpn" || value["text"] != "v4lue" {
		t.Fatalf("record body %v", post.body)
	}
	if len(*calls) != 4 || (*calls)[3].method != "DELETE" {
		t.Fatalf("calls %v", *calls)
	}
	if _, err := (&ArvanCloud{APIKey: "k3y", BaseURL: srv.URL}).Present(context.Background(), "_acme-challenge.other.com", "v"); err == nil || !strings.Contains(err.Error(), "no domain on this account") {
		t.Fatalf("an unknown domain gave %v", err)
	}
}
