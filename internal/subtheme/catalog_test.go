package subtheme

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// catalogServer serves a catalogue document and theme files from a map; a missing key is a 404.
func catalogServer(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const neonV1 = `{"themes":[{"name":"neon","version":"1.0.0","title":{"en":"Neon"},"files":["index.html","style.css"]}]}`

func TestInstallKeepsTheOldCopyUntilTheNewOneIsWhole(t *testing.T) {
	t.Setenv("XUI_DB_FOLDER", t.TempDir())
	files := map[string]string{
		"themes/catalog.json":     neonV1,
		"themes/neon/index.html":  `<h1>{{ t .lang "copy" }} v1</h1>`,
		"themes/neon/style.css":   `h1{}`,
		"themes/other/index.html": `unused`,
	}
	srv := catalogServer(t, files)
	ctx := context.Background()
	url := srv.URL + "/themes/catalog.json"

	got, err := Install(ctx, srv.Client(), url, "neon")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "installed:neon" || got.Version != "1.0.0" || got.Title != "Neon" {
		t.Fatalf("installed = %+v", got)
	}
	catalog, err := FetchCatalog(ctx, srv.Client(), url)
	if err != nil || len(catalog) != 1 || catalog[0].Installed != "1.0.0" {
		t.Fatalf("catalogue after install = %+v, %v; want neon marked 1.0.0", catalog, err)
	}

	files["themes/catalog.json"] = strings.Replace(neonV1, "1.0.0", "2.0.0", 1)
	files["themes/neon/index.html"] = `<h1>v2</h1>`
	delete(files, "themes/neon/style.css")
	if _, err := Install(ctx, srv.Client(), url, "neon"); err == nil {
		t.Fatal("an update missing a file installed")
	}
	page, err := os.ReadFile(filepath.Join(Root(), "neon", "index.html"))
	if err != nil || !strings.Contains(string(page), "v1") {
		t.Fatalf("a failed update replaced the working theme: %q, %v", page, err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(Root(), ".*")); len(leftovers) != 0 {
		t.Fatalf("a failed update left %v behind", leftovers)
	}

	fsys, err := Files("installed:neon")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Loader{}).Load(fsys, "installed:neon"); err != nil {
		t.Fatalf("the installed page does not parse with the theme helpers: %v", err)
	}
	if err := Remove("neon"); err != nil {
		t.Fatal(err)
	}
	if _, err := Files("installed:neon"); !errors.Is(err, ErrUnknownTheme) {
		t.Fatalf("removed theme still opens: %v", err)
	}
}

func TestInstallRefusesWhatIsNotATheme(t *testing.T) {
	t.Setenv("XUI_DB_FOLDER", t.TempDir())
	big := strings.Repeat("x", maxThemeBytes+1)
	srv := catalogServer(t, map[string]string{
		"catalog.json": `{"themes":[
			{"name":"escape","version":"1","files":["index.html","../../etc/x.css"]},
			{"name":"hidden","version":"1","files":["index.html",".env.css"]},
			{"name":"binary","version":"1","files":["index.html","run.sh"]},
			{"name":"nopage","version":"1","files":["style.css"]},
			{"name":"../up","version":"1","files":["index.html"]},
			{"name":"huge","version":"1","files":["index.html"]},
			{"name":"broken","version":"1","files":["index.html"]}
		]}`,
		"huge/index.html":   big,
		"broken/index.html": `{{ .sId `,
	})
	url := srv.URL + "/catalog.json"
	catalog, err := FetchCatalog(context.Background(), srv.Client(), url)
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, c := range catalog {
		listed = append(listed, c.Name)
	}
	if strings.Join(listed, ",") != "huge,broken" {
		t.Fatalf("catalogue kept %v, want only the entries whose files are a theme's", listed)
	}
	for _, name := range []string{"escape", "hidden", "binary", "nopage"} {
		if _, err := Install(context.Background(), srv.Client(), url, name); !errors.Is(err, ErrNotInCatalog) {
			t.Errorf("install %s = %v, want it refused", name, err)
		}
	}
	for _, name := range []string{"huge", "broken"} {
		if _, err := Install(context.Background(), srv.Client(), url, name); err == nil {
			t.Errorf("install %s succeeded", name)
		}
	}
	if entries, _ := os.ReadDir(Root()); len(entries) != 0 {
		t.Fatalf("refused installs left %v in the themes folder", entries)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(Root()), "etc")); err == nil {
		t.Fatal("a file escaped the themes folder")
	}
}
