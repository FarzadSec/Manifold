package subtheme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DefaultCatalogURL lists the themes kept in the Manifold repository.
const DefaultCatalogURL = "https://raw.githubusercontent.com/FarzadSec/Manifold/main/sub-themes/catalog.json"

const maxCatalogBytes = 256 << 10

var ErrNotInCatalog = errors.New("the catalogue has no such theme")

// CatalogTheme is one theme a catalogue offers. Files sit beside the catalogue in a
// directory named after the theme, or in Path when it says otherwise.
type CatalogTheme struct {
	Name        string            `json:"name" example:"neon"`
	Version     string            `json:"version" example:"1.0.0"`
	Title       map[string]string `json:"title"`
	Description map[string]string `json:"description"`
	Author      string            `json:"author,omitempty" example:"FarzadSec"`
	Files       []string          `json:"files"`
	Path        string            `json:"path,omitempty"`
	Preview     string            `json:"preview,omitempty"`
	Installed   string            `json:"installed,omitempty" example:"0.9.0"`
}

type catalogDoc struct {
	Themes []CatalogTheme `json:"themes"`
}

func catalogURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return DefaultCatalogURL
	}
	return strings.TrimSpace(raw)
}

// FetchCatalog reads the catalogue at raw ("" for the default) and marks what is installed.
func FetchCatalog(ctx context.Context, client *http.Client, raw string) ([]CatalogTheme, error) {
	themes, _, err := fetchCatalog(ctx, client, catalogURL(raw))
	if err != nil {
		return nil, err
	}
	installed := Installed()
	for i := range themes {
		themes[i].Installed = installed[themes[i].Name]
	}
	return themes, nil
}

func fetchCatalog(ctx context.Context, client *http.Client, raw string) ([]CatalogTheme, *url.URL, error) {
	base, err := url.Parse(raw)
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" {
		return nil, nil, fmt.Errorf("catalogue address %q is not an http(s) address", raw)
	}
	body, err := get(ctx, client, base.String(), maxCatalogBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("catalogue: %w", err)
	}
	var doc catalogDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, nil, fmt.Errorf("catalogue: %w", err)
	}
	out := make([]CatalogTheme, 0, len(doc.Themes))
	for _, t := range doc.Themes {
		if checkEntry(t) == nil {
			out = append(out, t)
		}
	}
	return out, base, nil
}

func checkEntry(t CatalogTheme) error {
	if !ValidName(t.Name) {
		return ErrBadName
	}
	if len(t.Files) == 0 || len(t.Files) > maxThemeFiles {
		return ErrBadFile
	}
	page := false
	for _, f := range t.Files {
		if !validFileName(f) {
			return fmt.Errorf("%w: %q", ErrBadFile, f)
		}
		page = page || f == "index.html" || f == "sub.html"
	}
	if !page {
		return ErrNoPage
	}
	return nil
}

func get(ctx context.Context, client *http.Client, target string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", target, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", target, limit)
	}
	return body, nil
}

// Install downloads a catalogue theme into Root, replacing an older copy only once
// every file has arrived and the page parses.
func Install(ctx context.Context, client *http.Client, raw, name string) (Theme, error) {
	themes, base, err := fetchCatalog(ctx, client, catalogURL(raw))
	if err != nil {
		return Theme{}, err
	}
	var entry *CatalogTheme
	for i := range themes {
		if themes[i].Name == name {
			entry = &themes[i]
		}
	}
	if entry == nil {
		return Theme{}, ErrNotInCatalog
	}
	dirRef := entry.Path
	if dirRef == "" {
		dirRef = entry.Name
	}
	dir, err := base.Parse(strings.TrimSuffix(dirRef, "/") + "/")
	if err != nil {
		return Theme{}, err
	}
	files := map[string][]byte{}
	budget := int64(maxThemeBytes)
	for _, f := range entry.Files {
		body, err := get(ctx, client, dir.JoinPath(f).String(), budget)
		if err != nil {
			return Theme{}, fmt.Errorf("theme %s: %w", name, err)
		}
		budget -= int64(len(body))
		files[f] = body
	}
	if err := os.MkdirAll(Root(), 0o750); err != nil {
		return Theme{}, err
	}
	tmp, err := os.MkdirTemp(Root(), ".install-"+name+"-")
	if err != nil {
		return Theme{}, err
	}
	defer os.RemoveAll(tmp)
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(tmp, f), body, 0o640); err != nil {
			return Theme{}, err
		}
	}
	title := entry.Title["en"]
	meta, _ := json.Marshal(manifest{Name: name, Version: entry.Version, Title: title})
	if err := os.WriteFile(filepath.Join(tmp, manifestName), meta, 0o640); err != nil {
		return Theme{}, err
	}
	fsys := os.DirFS(tmp)
	if _, err := Parse(fsys, pageName(fsys)); err != nil {
		return Theme{}, err
	}
	final := filepath.Join(Root(), name)
	old := filepath.Join(Root(), ".old-"+name)
	_ = os.RemoveAll(old)
	if _, err := os.Stat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			return Theme{}, err
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Rename(old, final)
		return Theme{}, err
	}
	_ = os.RemoveAll(old)
	return describe(os.DirFS(final), InstalledPrefix, name, false), nil
}
