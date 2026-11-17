// Package subtheme holds the subscription page themes: the ones built into the
// panel, the ones installed from a catalogue, and the files and helpers a theme uses.
package subtheme

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
)

//go:embed all:builtin
var builtinFS embed.FS

const (
	BuiltinPrefix   = "builtin:"
	InstalledPrefix = "installed:"
	manifestName    = "theme.json"
	maxThemeBytes   = 2 << 20
	maxThemeFiles   = 32
)

var (
	ErrUnknownTheme = errors.New("no such subscription theme")
	ErrBadName      = errors.New("a theme name is letters, digits, '.', '_' and '-', up to 64 characters")
	ErrBadFile      = errors.New("a theme holds only page, style, script, image and font files")
	ErrNoPage       = errors.New("a theme needs an index.html or a sub.html")

	nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

	// The page itself is a template, never served as a file.
	assetTypes = map[string]string{
		".css":   "text/css; charset=utf-8",
		".js":    "text/javascript; charset=utf-8",
		".json":  "application/json",
		".svg":   "image/svg+xml",
		".png":   "image/png",
		".jpg":   "image/jpeg",
		".jpeg":  "image/jpeg",
		".webp":  "image/webp",
		".gif":   "image/gif",
		".ico":   "image/x-icon",
		".woff":  "font/woff",
		".woff2": "font/woff2",
		".ttf":   "font/ttf",
	}
)

// Root is where installed themes live, one directory each.
func Root() string {
	return filepath.Join(config.GetDBFolderPath(), "sub-themes")
}

// Theme is one entry of the panel's theme picker.
type Theme struct {
	ID      string `json:"id" example:"builtin:aurora"`
	Name    string `json:"name" example:"aurora"`
	Title   string `json:"title" example:"Aurora"`
	Version string `json:"version,omitempty" example:"1.0.0"`
	Builtin bool   `json:"builtin" example:"true"`
}

type manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Title   string `json:"title"`
}

func ValidName(name string) bool { return nameRE.MatchString(name) }

// ValidID accepts what the theme setting may hold: nothing, or a namespaced theme name.
func ValidID(id string) bool {
	if id == "" {
		return true
	}
	for _, prefix := range []string{BuiltinPrefix, InstalledPrefix} {
		if name, ok := strings.CutPrefix(id, prefix); ok {
			return ValidName(name)
		}
	}
	return false
}

// validFileName is one file of a theme: a plain name, no directories, no hidden files.
func validFileName(name string) bool {
	if name == "index.html" || name == "sub.html" {
		return true
	}
	if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) || len(name) > 128 {
		return false
	}
	_, ok := assetTypes[strings.ToLower(path.Ext(name))]
	return ok
}

// Files is the tree a theme id names.
func Files(id string) (fs.FS, error) {
	if name, ok := strings.CutPrefix(id, BuiltinPrefix); ok && ValidName(name) {
		sub, err := fs.Sub(builtinFS, "builtin/"+name)
		if err != nil {
			return nil, err
		}
		if _, err := fs.Stat(sub, "index.html"); err != nil {
			return nil, ErrUnknownTheme
		}
		return sub, nil
	}
	if name, ok := strings.CutPrefix(id, InstalledPrefix); ok && ValidName(name) {
		dir := filepath.Join(Root(), name)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return nil, ErrUnknownTheme
		}
		return os.DirFS(dir), nil
	}
	return nil, ErrUnknownTheme
}

// List is every theme the picker offers: the built-in ones, then the installed ones.
func List() []Theme {
	out := []Theme{}
	entries, _ := fs.ReadDir(builtinFS, "builtin")
	for _, e := range entries {
		if e.IsDir() {
			sub, _ := fs.Sub(builtinFS, "builtin/"+e.Name())
			out = append(out, describe(sub, BuiltinPrefix, e.Name(), true))
		}
	}
	dirs, _ := os.ReadDir(Root())
	for _, e := range dirs {
		if !e.IsDir() || !ValidName(e.Name()) {
			continue
		}
		fsys := os.DirFS(filepath.Join(Root(), e.Name()))
		if pageName(fsys) == "" {
			continue
		}
		out = append(out, describe(fsys, InstalledPrefix, e.Name(), false))
	}
	return out
}

func describe(fsys fs.FS, prefix, name string, builtin bool) Theme {
	t := Theme{ID: prefix + name, Name: name, Title: name, Builtin: builtin}
	if raw, err := fs.ReadFile(fsys, manifestName); err == nil {
		var m manifest
		if json.Unmarshal(raw, &m) == nil {
			t.Version = m.Version
			if m.Title != "" {
				t.Title = m.Title
			}
		}
	}
	return t
}

func pageName(fsys fs.FS) string {
	for _, name := range []string{"sub.html", "index.html"} {
		if info, err := fs.Stat(fsys, name); err == nil && info.Mode().IsRegular() {
			return name
		}
	}
	return ""
}

// Asset is a theme file the page links to, with its content type.
func Asset(fsys fs.FS, name string) ([]byte, string, error) {
	ctype, ok := assetTypes[strings.ToLower(path.Ext(name))]
	if !ok || !fs.ValidPath(name) {
		return nil, "", fs.ErrNotExist
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return nil, "", fs.ErrNotExist
		}
	}
	info, err := fs.Stat(fsys, name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxThemeBytes {
		return nil, "", fs.ErrNotExist
	}
	data, err := fs.ReadFile(fsys, name)
	return data, ctype, err
}

// Loader parses theme pages once and again only when the file changes.
type Loader struct {
	mu    sync.Mutex
	cache map[string]loaded
}

type loaded struct {
	tmpl    *template.Template
	modTime time.Time
	size    int64
}

// Load parses the page of the theme in fsys; key tells themes apart in the cache.
func (l *Loader) Load(fsys fs.FS, key string) (*template.Template, error) {
	name := pageName(fsys)
	if name == "" {
		return nil, ErrNoPage
	}
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return nil, err
	}
	key += "\x00" + name
	l.mu.Lock()
	defer l.mu.Unlock()
	if c, ok := l.cache[key]; ok && c.modTime.Equal(info.ModTime()) && c.size == info.Size() {
		return c.tmpl, nil
	}
	tmpl, err := Parse(fsys, name)
	if err != nil {
		return nil, err
	}
	if l.cache == nil {
		l.cache = map[string]loaded{}
	}
	l.cache[key] = loaded{tmpl: tmpl, modTime: info.ModTime(), size: info.Size()}
	return tmpl, nil
}

// Parse reads a theme page as an html/template with the theme helpers.
func Parse(fsys fs.FS, name string) (*template.Template, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(name).Funcs(Funcs()).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("theme page %s: %w", name, err)
	}
	return tmpl, nil
}

// Installed reports the version of each installed theme, by name.
func Installed() map[string]string {
	out := map[string]string{}
	for _, t := range List() {
		if !t.Builtin {
			out[t.Name] = t.Version
		}
	}
	return out
}

// Remove deletes an installed theme.
func Remove(name string) error {
	if !ValidName(name) {
		return ErrBadName
	}
	dir := filepath.Join(Root(), name)
	if _, err := os.Stat(dir); err != nil {
		return ErrUnknownTheme
	}
	return os.RemoveAll(dir)
}
