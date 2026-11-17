package backupstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// S3 stores backups in an S3-compatible bucket (AWS, Cloudflare R2, ArvanCloud, MinIO),
// addressed path-style: endpoint/bucket/key.
type S3 struct {
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
	Client    *http.Client
	now       func() time.Time
}

func (s *S3) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}

func (s *S3) region() string {
	if s.Region == "" {
		return "us-east-1"
	}
	return s.Region
}

func (s *S3) objectURL(key string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSuffix(s.Endpoint, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("s3 endpoint %q is not an http(s) address", s.Endpoint)
	}
	u.Path = "/" + s.Bucket
	if key != "" {
		u.Path += "/" + key
	}
	return u, nil
}

func (s *S3) key(name string) string {
	prefix := strings.Trim(s.Prefix, "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func (s *S3) do(ctx context.Context, method string, u *url.URL, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hash := emptySHA256
	if len(body) > 0 {
		sum := sha256.Sum256(body)
		hash = hex.EncodeToString(sum[:])
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	signV4(req, hash, s.AccessKey, s.SecretKey, s.region(), "s3", now())
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("s3 %s %s: %s: %s", method, u.Path, resp.Status, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

func (s *S3) Put(ctx context.Context, name string, data []byte) error {
	u, err := s.objectURL(s.key(name))
	if err != nil {
		return err
	}
	resp, err := s.do(ctx, http.MethodPut, u, data)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (s *S3) Delete(ctx context.Context, name string) error {
	u, err := s.objectURL(s.key(name))
	if err != nil {
		return err
	}
	resp, err := s.do(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

type listResult struct {
	Contents []struct {
		Key  string `xml:"Key"`
		Size int64  `xml:"Size"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// List names the objects under the prefix, without it.
func (s *S3) List(ctx context.Context) ([]File, error) {
	var out []File
	token := ""
	prefix := s.key("")
	for range 100 {
		u, err := s.objectURL("")
		if err != nil {
			return nil, err
		}
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		u.RawQuery = q.Encode()
		resp, err := s.do(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		var page listResult
		err = xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("s3 list: %w", err)
		}
		for _, c := range page.Contents {
			name := strings.TrimPrefix(c.Key, prefix)
			if name != "" && !strings.Contains(name, "/") {
				out = append(out, File{Name: name, Size: c.Size})
			}
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			return out, nil
		}
		token = page.NextContinuationToken
	}
	return nil, errors.New("s3 list: too many pages")
}

// signV4 adds AWS Signature Version 4 headers, signing the host and every header set on req.
func signV4(req *http.Request, payloadHash, accessKey, secretKey, region, service string, t time.Time) {
	t = t.UTC()
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	headers := map[string]string{"host": req.URL.Host}
	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "user-agent" {
			continue
		}
		headers[lower] = strings.TrimSpace(strings.Join(values, ","))
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, name := range names {
		canonHeaders.WriteString(name + ":" + headers[name] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		canonicalPath(req.URL),
		canonicalQuery(req.URL.Query()),
		canonHeaders.String(),
		signed,
		payloadHash,
	}, "\n")
	scope := day + "/" + region + "/" + service + "/aws4_request"
	sum := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])

	k := hmacSHA256([]byte("AWS4"+secretKey), day)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	k = hmacSHA256(k, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(k, toSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s,SignedHeaders=%s,Signature=%s",
		accessKey, scope, signed, signature))
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// awsEscape is RFC 3986 escaping, which S3 signs: everything but unreserved characters.
func awsEscape(s string, keepSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' || keepSlash && c == '/' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func canonicalPath(u *url.URL) string {
	p := u.Path
	if p == "" {
		return "/"
	}
	return awsEscape(p, true)
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		values := append([]string{}, q[k]...)
		sort.Strings(values)
		for _, v := range values {
			parts = append(parts, awsEscape(k, false)+"="+awsEscape(v, false))
		}
	}
	return strings.Join(parts, "&")
}
