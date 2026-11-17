package subtheme

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"strings"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// Languages are the ones the built-in phrases carry; the first is the fallback.
var Languages = []string{"en", "fa", "ru", "zh"}

// Funcs are the helpers every theme page may call.
func Funcs() template.FuncMap {
	return template.FuncMap{
		"bytes":   func(n int64) string { return common.FormatTraffic(n) },
		"qr":      QR,
		"b64":     func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
		"t":       Phrase,
		"dataURI": func(mime, content string) template.URL { return dataURI(mime, content) },
		"default": func(def, v string) string {
			if strings.TrimSpace(v) == "" {
				return def
			}
			return v
		},
		"percent": func(used, total int64) int {
			if total <= 0 {
				return 0
			}
			return int(min(100, max(0, used*100/total)))
		},
	}
}

func dataURI(mime, content string) template.URL {
	if !strings.Contains(mime, "/") || strings.ContainsAny(mime, ";,\"'<> ") {
		mime = "application/octet-stream"
	}
	return template.URL("data:" + mime + ";base64," + base64.StdEncoding.EncodeToString([]byte(content)))
}

// QR draws text as an inline SVG QR code, or nothing for text too long for one.
func QR(text string) template.HTML {
	if text == "" {
		return ""
	}
	code, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return ""
	}
	bits := code.Bitmap()
	var d strings.Builder
	for y, row := range bits {
		for x, dark := range row {
			if dark {
				fmt.Fprintf(&d, "M%d %dh1v1h-1z", x, y)
			}
		}
	}
	n := len(bits)
	// The path holds only numbers and fixed letters, so it is safe to emit unescaped.
	return template.HTML(fmt.Sprintf(
		`<svg class="qr" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`,
		n, n, n, n, d.String()))
}

// Lang picks the page language from ?lang=, then the browser, then English.
func Lang(query, acceptLanguage string) string {
	if known(query) {
		return query
	}
	for _, part := range strings.Split(acceptLanguage, ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		primary, _, _ := strings.Cut(strings.ToLower(tag), "-")
		if known(primary) {
			return primary
		}
	}
	return Languages[0]
}

func known(lang string) bool {
	for _, l := range Languages {
		if l == lang {
			return true
		}
	}
	return false
}

// Dir is the writing direction of a page language.
func Dir(lang string) string {
	if lang == "fa" {
		return "rtl"
	}
	return "ltr"
}

// Phrase is a built-in phrase in lang, English when lang lacks it, the key when neither has it.
func Phrase(lang, key string) string {
	if p, ok := phrases[key]; ok {
		if s := p[lang]; s != "" {
			return s
		}
		return p["en"]
	}
	return key
}

var phrases = map[string]map[string]string{
	"subscription": {"en": "Subscription", "fa": "اشتراک", "ru": "Подписка", "zh": "订阅"},
	"active":       {"en": "Active", "fa": "فعال", "ru": "Активна", "zh": "有效"},
	"disabled":     {"en": "Disabled", "fa": "غیرفعال", "ru": "Отключена", "zh": "已停用"},
	"expired":      {"en": "Expired", "fa": "منقضی", "ru": "Истекла", "zh": "已过期"},
	"limited":      {"en": "Out of traffic", "fa": "حجم تمام شده", "ru": "Трафик исчерпан", "zh": "流量已用完"},
	"online":       {"en": "Online", "fa": "آنلاین", "ru": "В сети", "zh": "在线"},
	"offline":      {"en": "Offline", "fa": "آفلاین", "ru": "Не в сети", "zh": "离线"},
	"used":         {"en": "Used", "fa": "مصرف\u200cشده", "ru": "Использовано", "zh": "已用"},
	"total":        {"en": "Total", "fa": "کل", "ru": "Всего", "zh": "总量"},
	"remaining":    {"en": "Remaining", "fa": "باقی\u200cمانده", "ru": "Осталось", "zh": "剩余"},
	"upload":       {"en": "Upload", "fa": "آپلود", "ru": "Отдача", "zh": "上传"},
	"download":     {"en": "Download", "fa": "دانلود", "ru": "Загрузка", "zh": "下载"},
	"unlimited":    {"en": "Unlimited", "fa": "نامحدود", "ru": "Без лимита", "zh": "不限"},
	"expires":      {"en": "Expires", "fa": "انقضا", "ru": "Действует до", "zh": "到期"},
	"never":        {"en": "Never", "fa": "بدون انقضا", "ru": "Бессрочно", "zh": "永不"},
	"daysLeft":     {"en": "days left", "fa": "روز مانده", "ru": "дн. осталось", "zh": "天剩余"},
	"copy":         {"en": "Copy", "fa": "کپی", "ru": "Копировать", "zh": "复制"},
	"copied":       {"en": "Copied", "fa": "کپی شد", "ru": "Скопировано", "zh": "已复制"},
	"copyLink":     {"en": "Copy subscription link", "fa": "کپی لینک اشتراک", "ru": "Скопировать ссылку", "zh": "复制订阅链接"},
	"addToApp":     {"en": "Add to an app", "fa": "افزودن به برنامه", "ru": "Добавить в приложение", "zh": "添加到应用"},
	"configs":      {"en": "Configurations", "fa": "کانفیگ\u200cها", "ru": "Конфигурации", "zh": "配置"},
	"formats":      {"en": "Other formats", "fa": "فرمت\u200cهای دیگر", "ru": "Другие форматы", "zh": "其他格式"},
	"support":      {"en": "Support", "fa": "پشتیبانی", "ru": "Поддержка", "zh": "支持"},
	"scan":         {"en": "Scan in your app", "fa": "در برنامه اسکن کنید", "ru": "Отсканируйте в приложении", "zh": "在应用中扫描"},
}
