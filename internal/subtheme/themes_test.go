package subtheme

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type sampleConfig struct{ Name, Protocol, Link string }

func samplePage(lang string) map[string]any {
	return map[string]any{
		"lang": lang, "dir": Dir(lang), "assetBase": "s1/theme",
		"sId": "s1", "enabled": true, "isOnline": true, "status": "active",
		"used": "1.00GB", "total": "10.00GB", "remained": "9.00GB", "usedPercent": 10,
		"upload": "0.20GB", "download": "0.80GB", "totalByte": int64(10 << 30),
		"expire": int64(1893456000), "expireText": "1408/10/11", "daysLeft": 30,
		"subUrl": "https://sub.example.com/sub/s1", "subSingboxUrl": "https://sub.example.com/singbox/s1",
		"subClashUrl": "https://sub.example.com/clash/s1", "subJsonUrl": "",
		"subTitle": "", "subSupportUrl": "https://t.me/support", "announce": "<b>maintenance</b>",
		"configs": []sampleConfig{{Name: `<script>alert(1)</script>`, Protocol: "vless", Link: "vless://u@h:443#x"}},
	}
}

var externalLoad = regexp.MustCompile(`(?i)(<(script|img|iframe|source|video|audio)[^>]+src|<link[^>]+href)\s*=\s*"(https?:)?//`)

// themeTrees is every theme shipped to users: built into the panel, or in the repository's catalogue.
func themeTrees(t *testing.T) map[string]fs.FS {
	t.Helper()
	trees := map[string]fs.FS{}
	entries, _ := fs.ReadDir(builtinFS, "builtin")
	for _, e := range entries {
		sub, _ := fs.Sub(builtinFS, "builtin/"+e.Name())
		trees["builtin/"+e.Name()] = sub
	}
	root := filepath.Join("..", "..", "sub-themes")
	raw, err := os.ReadFile(filepath.Join(root, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc catalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("catalog.json: %v", err)
	}
	for _, entry := range doc.Themes {
		if err := checkEntry(entry); err != nil {
			t.Fatalf("catalogue entry %s: %v", entry.Name, err)
		}
		if entry.Version == "" || entry.Title["en"] == "" {
			t.Fatalf("catalogue entry %s needs a version and an English title", entry.Name)
		}
		dir := filepath.Join(root, entry.Name)
		for _, f := range entry.Files {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				t.Fatalf("catalogue entry %s lists %s, which is not in the repository", entry.Name, f)
			}
		}
		trees["catalog/"+entry.Name] = os.DirFS(dir)
	}
	return trees
}

// A theme that loads from another host shows a blank page before the VPN connects, one
// that leaks markup is an injection, and a defanged app link is a dead button.
func TestShippedThemesRenderSafely(t *testing.T) {
	for name, fsys := range themeTrees(t) {
		tmpl, err := Parse(fsys, pageName(fsys))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, lang := range Languages {
			var out bytes.Buffer
			if err := tmpl.Execute(&out, samplePage(lang)); err != nil {
				t.Fatalf("%s/%s: %v", name, lang, err)
			}
			page := out.String()
			for _, bad := range []string{"ZgotmplZ", "<no value>", "<script>alert", "<b>maintenance"} {
				if strings.Contains(page, bad) {
					t.Errorf("%s/%s renders %q", name, lang, bad)
				}
			}
			if m := externalLoad.FindString(page); m != "" {
				t.Errorf("%s/%s loads from another host: %s", name, lang, m)
			}
			for _, want := range []string{`dir="` + Dir(lang) + `"`, `href="happ://add/https://sub.example.com/sub/s1"`, "<svg", Phrase(lang, "copyLink")} {
				if !strings.Contains(page, want) {
					t.Errorf("%s/%s lacks %q", name, lang, want)
				}
			}
		}
		css, _, err := Asset(fsys, "style.css")
		if err != nil {
			t.Fatalf("%s: stylesheet: %v", name, err)
		}
		if strings.Contains(string(css), "@import") || strings.Contains(string(css), "url(http") {
			t.Errorf("%s: the stylesheet loads from another host", name)
		}
	}
}
