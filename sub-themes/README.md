# Subscription page themes

A theme is what a user sees when they open their subscription link in a browser.
Panels list the themes in `catalog.json` under **Settings → Subscription → Page theme →
Catalogue** and install them with one click.

## What a theme is

A directory with one page and the files it links to:

```
neon/
├── index.html   the page (or sub.html)
└── style.css
```

- The page is a Go [`html/template`](https://pkg.go.dev/html/template). Every value is
  escaped for the place it lands, so a user's remark cannot add markup.
- Other files are served at `{{ .assetBase }}/<name>`. Allowed: `.css .js .json .svg .png
  .jpg .jpeg .webp .gif .ico .woff .woff2 .ttf`, at most 32 files and 2 MB in all. Names
  starting with a dot are never served, and neither is the page source.
- A theme that fails to parse or render falls back to the panel's own page.

## The data a page receives

| Key | |
| --- | --- |
| `.lang`, `.dir` | `en`, `fa`, `ru` or `zh` (from `?lang=`, then the browser), and `ltr` or `rtl` |
| `.status` | `active`, `disabled`, `expired` or `limited` |
| `.enabled`, `.isOnline` | |
| `.used`, `.total`, `.remained`, `.upload`, `.download` | Written for people, such as `2.00GB` |
| `.usedPercent`, `.totalByte`, `.uploadByte`, `.downloadByte` | Numbers; `.totalByte` is 0 with no limit |
| `.expire`, `.expireText`, `.daysLeft` | Unix seconds (0 for never), the date in the panel's calendar, whole days left |
| `.subUrl`, `.subJsonUrl`, `.subClashUrl`, `.subSingboxUrl` | The subscription and its formats; a format that is off is empty |
| `.configs` | Each connection: `.Name`, `.Protocol`, `.Link` |
| `.links`, `.emails` | The share links as a plain list, and the accounts behind them |
| `.subTitle`, `.subSupportUrl`, `.announce` | From the subscription settings |
| `.lastOnline`, `.datepicker`, `.subUpdates`, `.sId` | |

## Helpers

| Helper | |
| --- | --- |
| `t` | `{{ t .lang "copy" }}`: a built-in phrase in the page language |
| `qr` | `{{ $c := qr .subUrl }}{{ if $c }}{{ $c }}{{ end }}`: an inline SVG QR code, empty when the text is too long |
| `bytes` | `{{ bytes .totalByte }}` |
| `b64`, `dataURI` | `href="{{ dataURI "application/octet-stream" .Content }}"` for a download |
| `default`, `percent` | `{{ default "Subscription" .subTitle }}`, `{{ percent used total }}` |

App buttons: `happ://add/{{ .subUrl }}`, `v2rayng://install-sub?url={{ .subUrl }}`,
`hiddify://import/{{ .subUrl }}`, `streisand://import/{{ .subUrl }}`,
`sing-box://import-remote-profile?url={{ .subSingboxUrl }}`,
`clash://install-config?url={{ .subClashUrl }}`. Write the scheme in the template: a link
whose scheme comes from a value is replaced by `#ZgotmplZ`.

## Rules

- Load nothing from another host. Users open this page before their VPN connects, often
  where font and script CDNs are blocked.
- Read nothing but the page's own data. The page may share an origin with the panel.
- Use logical CSS properties (`margin-inline-start`, `text-align: start`) so one stylesheet
  serves Persian too.

`go test ./internal/subtheme/` renders every theme listed here and every built-in one in
all four languages and fails on a remote load, a defanged link or unescaped markup.

## Adding a theme

1. Copy a theme directory, such as `neon/`, to a new name.
2. Add an entry to `catalog.json`: `name`, `version`, `title` and `description` in `en`,
   `fa`, `ru` and `zh`, `author`, and `files`.
3. Raise `version` on every change; that is how panels learn there is an update.
