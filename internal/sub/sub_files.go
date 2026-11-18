package sub

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/openvpn"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// subEntrySource is a subscription entry with the inbound behind it, nil for an external link.
type subEntrySource struct {
	entry   service.SubEntry
	inbound *model.Inbound
}

// subEntrySources is subLinks split into one entry per link, each with its inbound's app
// location; found is false when no account owns subId.
func (s *SubService) subEntrySources(subId string) (sources []subEntrySource, emails []string, found bool, err error) {
	links, emails, _, _, _, err := s.subLinks(subId)
	if err != nil || links == nil {
		return nil, nil, false, err
	}
	app := &service.AppService{}
	locationOf, err := app.LocationOfInbounds()
	if err != nil {
		return nil, nil, false, err
	}
	places := map[int]model.AppLocation{}
	if len(locationOf) > 0 {
		all, err := app.Locations()
		if err != nil {
			return nil, nil, false, err
		}
		for _, place := range all {
			places[place.Id] = place
		}
	}
	sources = make([]subEntrySource, 0, len(links))
	for _, l := range links {
		for _, line := range splitLinkLines(l.text) {
			entry := service.SubEntry{Link: line}
			if l.inbound != nil {
				entry.InboundId = l.inbound.Id
				entry.Protocol = string(l.inbound.Protocol)
				if place, ok := places[locationOf[l.inbound.Id]]; ok {
					entry.LocationId = place.Id
					entry.Location = place.Name
					entry.CountryCode = place.CountryCode
					entry.City = place.City
					entry.SortOrder = place.SortOrder
				}
			}
			sources = append(sources, subEntrySource{entry: entry, inbound: l.inbound})
		}
	}
	return sources, emails, true, nil
}

func (p *LinkProvider) SubEntriesForSubId(host, subId string) ([]service.SubEntry, error) {
	sources, _, _, err := p.build(host).subEntrySources(subId)
	if err != nil {
		return nil, err
	}
	out := make([]service.SubEntry, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.entry)
	}
	return out, nil
}

// subFileKind is what one file page hands out: OpenVPN profiles or WireGuard configs.
type subFileKind struct {
	path        string
	scheme      string
	ext         string
	contentType string
}

var (
	openvpnFiles   = subFileKind{path: "openvpn", scheme: openvpn.ShareLinkScheme, ext: ".ovpn", contentType: "application/x-openvpn-profile"}
	wireguardFiles = subFileKind{path: "wireguard", scheme: "wireguard://", ext: ".conf", contentType: "text/plain; charset=utf-8"}
)

type subFile struct {
	N      int
	Name   string
	Title  string
	Flag   string
	City   string
	Detail string
	Body   string
	QR     template.URL
}

type subFileSet struct {
	Files    []subFile
	Username string
	Password string
}

var inlineOpenVPNLogin = regexp.MustCompile(`<auth-user-pass>\r?\n([^\r\n]*)\r?\n([^\r\n]*)\r?\n?</auth-user-pass>`)

// subFiles are a subscription's files of one kind, one per location in the locations'
// order, then those of inbounds in no location; ok is false once the request is answered.
func (a *SUBController) subFiles(c *gin.Context, kind subFileKind) (subFileSet, bool) {
	_, host, _, _ := a.subService.ResolveRequest(c)
	subReq := a.subService.ForRequest(host)
	subReq.subscriptionBody = false
	sources, emails, found, err := subReq.subEntrySources(c.Param("subid"))
	if err != nil || !found {
		writeSubError(c, err)
		return subFileSet{}, false
	}
	var picked []subEntrySource
	for _, source := range sources {
		if strings.HasPrefix(source.entry.Link, kind.scheme) {
			picked = append(picked, source)
		}
	}
	slices.SortStableFunc(picked, func(x, y subEntrySource) int {
		return compareLocations(x.entry, y.entry)
	})
	perLocation := map[string]int{}
	for _, source := range picked {
		perLocation[fileTitle(source, kind)]++
	}
	set := subFileSet{}
	if len(emails) > 0 {
		set.Username = emails[0]
	}
	used := map[string]bool{}
	for _, source := range picked {
		file := subFile{Title: fileTitle(source, kind), Flag: countryFlag(source.entry.CountryCode), City: source.entry.City}
		if perLocation[file.Title] > 1 && source.inbound != nil {
			file.Detail = source.inbound.Remark
		}
		var ok bool
		if kind == openvpnFiles {
			file.Body, ok = service.OpenVPNProfileFromLink(source.entry.Link)
		} else {
			file.Body, _, ok = service.WireguardConfFromLink(source.entry.Link)
		}
		if !ok {
			continue
		}
		file.Name = uniqueFileName(used, service.PackName(file.Title), kind.ext)
		file.N = len(set.Files) + 1
		set.Files = append(set.Files, file)
		if kind == openvpnFiles && set.Password == "" {
			set.Password = openvpnPassword(subReq, source, file.Body, set.Username)
		}
	}
	return set, true
}

func compareLocations(x, y service.SubEntry) int {
	if (x.LocationId == 0) != (y.LocationId == 0) {
		if x.LocationId == 0 {
			return 1
		}
		return -1
	}
	if x.SortOrder != y.SortOrder {
		return x.SortOrder - y.SortOrder
	}
	return x.LocationId - y.LocationId
}

func fileTitle(source subEntrySource, kind subFileKind) string {
	switch {
	case source.entry.Location != "":
		return source.entry.Location
	case source.inbound != nil && strings.TrimSpace(source.inbound.Remark) != "":
		return strings.TrimSpace(source.inbound.Remark)
	default:
		return kind.path
	}
}

func uniqueFileName(used map[string]bool, base, ext string) string {
	name := base + ext
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
	used[name] = true
	return name
}

// openvpnPassword reads the login the profile carries; a profile that asks for it
// (promptCredentials) leaves the account's own password to show.
func openvpnPassword(subReq *SubService, source subEntrySource, profile, username string) string {
	if m := inlineOpenVPNLogin.FindStringSubmatch(profile); m != nil {
		return strings.TrimSpace(m[2])
	}
	if source.inbound == nil {
		return ""
	}
	client, ok := subReq.clientForLink(source.inbound, username)
	if !ok {
		return ""
	}
	return client.Password
}

func countryFlag(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return string([]rune{0x1F1E6 + rune(code[0]-'A'), 0x1F1E6 + rune(code[1]-'A')})
}

func setFilePageHeaders(c *gin.Context) {
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Robots-Tag", "noindex, nofollow")
}

func (a *SUBController) openvpnPage(c *gin.Context)   { a.serveFilePage(c, openvpnFiles) }
func (a *SUBController) wireguardPage(c *gin.Context) { a.serveFilePage(c, wireguardFiles) }
func (a *SUBController) openvpnFile(c *gin.Context)   { a.serveSubFile(c, openvpnFiles) }
func (a *SUBController) wireguardFile(c *gin.Context) { a.serveSubFile(c, wireguardFiles) }

func (a *SUBController) serveSubFile(c *gin.Context, kind subFileKind) {
	setFilePageHeaders(c)
	set, ok := a.subFiles(c, kind)
	if !ok {
		return
	}
	which := c.Param("n")
	if kind == wireguardFiles && which == "all.zip" {
		archive, err := zipSubFiles(set.Files)
		if err != nil || len(set.Files) == 0 {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="wireguard.zip"`)
		c.Data(http.StatusOK, "application/zip", archive)
		return
	}
	n, err := strconv.Atoi(which)
	if err != nil || n < 1 || n > len(set.Files) {
		c.Status(http.StatusNotFound)
		return
	}
	file := set.Files[n-1]
	c.Header("Content-Disposition", `attachment; filename="`+file.Name+`"`)
	c.Data(http.StatusOK, kind.contentType, []byte(file.Body))
}

func zipSubFiles(files []subFile) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range files {
		w, err := zw.Create(file.Name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(file.Body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (a *SUBController) serveFilePage(c *gin.Context, kind subFileKind) {
	setFilePageHeaders(c)
	set, ok := a.subFiles(c, kind)
	if !ok {
		return
	}
	if kind == wireguardFiles {
		for i := range set.Files {
			png, err := qrcode.Encode(set.Files[i].Body, qrcode.Medium, 320)
			if err != nil {
				logger.Warning("wireguard page QR:", err)
				continue
			}
			set.Files[i].QR = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))
		}
	}
	lang := filePageLang(c)
	text := filePageTexts[lang]
	page := map[string]any{
		"Lang":      lang,
		"Dir":       map[string]string{"fa": "rtl", "en": "ltr"}[lang],
		"T":         text,
		"Title":     text[kind.path+"Title"],
		"Help":      text[kind.path+"Help"],
		"Kind":      kind.path,
		"OpenVPN":   kind == openvpnFiles,
		"WireGuard": kind == wireguardFiles,
		"Set":       set,
	}
	var out bytes.Buffer
	if err := filePageTemplate.Execute(&out, page); err != nil {
		logger.Warning("file page:", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", out.Bytes())
}

func filePageLang(c *gin.Context) string {
	switch strings.ToLower(c.Query("lang")) {
	case "en":
		return "en"
	case "fa":
		return "fa"
	}
	first, _, _ := strings.Cut(c.GetHeader("Accept-Language"), ",")
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(first)), "en") {
		return "en"
	}
	return "fa"
}

var filePageTexts = map[string]map[string]string{
	"fa": {
		"openvpnTitle":   "فایل\u200cهای OpenVPN",
		"wireguardTitle": "فایل\u200cهای WireGuard",
		"openvpnHelp":    "فایل لوکیشن دلخواه را دانلود کنید و در برنامه\u200cی OpenVPN Connect باز کنید.",
		"wireguardHelp":  "فایل لوکیشن دلخواه را دانلود کنید یا QR آن را در برنامه\u200cی WireGuard اسکن کنید.",
		"username":       "نام کاربری",
		"password":       "رمز عبور",
		"copy":           "کپی",
		"copied":         "کپی شد",
		"download":       "دانلود",
		"downloadAll":    "دانلود همه (ZIP)",
		"allHelp":        "برنامه\u200cی WireGuard فایل ZIP را یک\u200cجا وارد می\u200cکند.",
		"empty":          "این اشتراک فایلی از این نوع ندارد.",
	},
	"en": {
		"openvpnTitle":   "OpenVPN files",
		"wireguardTitle": "WireGuard files",
		"openvpnHelp":    "Download the location you want and open it in OpenVPN Connect.",
		"wireguardHelp":  "Download the location you want, or scan its QR code in the WireGuard app.",
		"username":       "Username",
		"password":       "Password",
		"copy":           "Copy",
		"copied":         "Copied",
		"download":       "Download",
		"downloadAll":    "Download all (ZIP)",
		"allHelp":        "The WireGuard app imports the ZIP in one go.",
		"empty":          "This subscription has no files of this kind.",
	},
}

var filePageTemplate = template.Must(template.New("files").Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>{{.Title}}</title>
<style>
:root{--bg:#0f1720;--card:#17212c;--line:#253445;--text:#e8eef4;--muted:#93a4b5;--accent:#14b8a6;--accent2:#0d9488}
@media (prefers-color-scheme: light){:root{--bg:#f4f7f9;--card:#fff;--line:#dde5ec;--text:#14202b;--muted:#5d6f80}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);font:16px/1.7 system-ui,-apple-system,"Segoe UI",Tahoma,sans-serif}
main{max-width:34rem;margin:0 auto;padding:1.25rem 1rem 3rem}
h1{font-size:1.35rem;margin:.25rem 0}
.help{color:var(--muted);margin:0 0 1rem}
.card{background:var(--card);border:1px solid var(--line);border-radius:14px;padding:1rem;margin:.75rem 0}
.login .row{display:flex;align-items:center;gap:.5rem;margin:.35rem 0}
.login .label{color:var(--muted);min-width:5.5rem}
code{direction:ltr;unicode-bidi:isolate;font:15px/1.5 ui-monospace,Menlo,Consolas,monospace;background:rgba(127,127,127,.14);padding:.2rem .45rem;border-radius:7px;word-break:break-all}
.place{display:flex;align-items:center;gap:.6rem}
.flag{font-size:1.7rem;line-height:1}
.name{font-weight:600;font-size:1.08rem}
.sub{color:var(--muted);font-size:.9rem}
.btn{display:block;text-align:center;text-decoration:none;background:var(--accent);color:#fff;font-weight:600;border-radius:11px;padding:.8rem 1rem;margin-top:.8rem;border:0;width:100%;font-size:1rem;cursor:pointer}
.btn:active{background:var(--accent2)}
.copy{background:transparent;color:var(--accent);border:1px solid var(--accent);border-radius:8px;padding:.15rem .6rem;font-size:.85rem;cursor:pointer}
.qr{display:block;width:100%;max-width:240px;margin:.8rem auto 0;border-radius:10px;background:#fff;padding:8px}
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p class="help">{{.Help}}</p>
{{if and .OpenVPN .Set.Username}}
<section class="card login">
<div class="row"><span class="label">{{index .T "username"}}</span><code id="u">{{.Set.Username}}</code><button class="copy" data-for="u">{{index .T "copy"}}</button></div>
{{if .Set.Password}}<div class="row"><span class="label">{{index .T "password"}}</span><code id="p">{{.Set.Password}}</code><button class="copy" data-for="p">{{index .T "copy"}}</button></div>{{end}}
</section>
{{end}}
{{if and .WireGuard .Set.Files}}
<section class="card"><a class="btn" href="wireguard/all.zip" download>{{index .T "downloadAll"}}</a><p class="sub">{{index .T "allHelp"}}</p></section>
{{end}}
{{range .Set.Files}}
<section class="card">
<div class="place">{{if .Flag}}<span class="flag">{{.Flag}}</span>{{end}}<div><div class="name">{{.Title}}</div>{{if or .City .Detail}}<div class="sub">{{.City}}{{if and .City .Detail}} · {{end}}{{.Detail}}</div>{{end}}</div></div>
{{if .QR}}<img class="qr" src="{{.QR}}" alt="QR {{.Title}}">{{end}}
<a class="btn" href="{{$.Kind}}/{{.N}}" download="{{.Name}}">{{index $.T "download"}} · {{.Name}}</a>
</section>
{{else}}
<p class="help">{{index .T "empty"}}</p>
{{end}}
</main>
<script>
document.querySelectorAll('.copy').forEach(function (b) {
  b.addEventListener('click', function () {
    var t = document.getElementById(b.dataset.for).textContent, done = function () { b.textContent = {{index .T "copied"}}; };
    if (navigator.clipboard) { navigator.clipboard.writeText(t).then(done); return; }
    var r = document.createRange(); r.selectNodeContents(document.getElementById(b.dataset.for));
    var s = getSelection(); s.removeAllRanges(); s.addRange(r); document.execCommand('copy'); done();
  });
});
</script>
</body>
</html>
`))
