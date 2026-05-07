package admin_panel

import (
	"encoding/base64"
	"html"
	"net/http"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

type WhatsAppStatusSnapshot struct {
	Connected     bool
	Authenticated bool
	DeviceID      string
	LastEvent     string
	LastError     string
	LatestQRCode  string
	UpdatedAt     time.Time
}

var (
	whatsAppStatusProvider func() WhatsAppStatusSnapshot
	whatsAppQRRefreshFunc  func() error
	whatsAppLogoutFunc     func() error
)

func SetWhatsAppStatusProvider(provider func() WhatsAppStatusSnapshot) {
	whatsAppStatusProvider = provider
}

func SetWhatsAppQRRefreshHandler(refresh func() error) {
	whatsAppQRRefreshFunc = refresh
}

func SetWhatsAppLogoutHandler(logout func() error) {
	whatsAppLogoutFunc = logout
}

func adminWhatsAppHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	msg := ""
	shouldRefresh := strings.TrimSpace(r.URL.Query().Get("refresh")) == "1"
	if shouldRefresh && whatsAppQRRefreshFunc == nil {
		msg = "QR refresh is not configured."
	} else if shouldRefresh && whatsAppQRRefreshFunc != nil {
		if err := whatsAppQRRefreshFunc(); err != nil {
			msg = "Failed to refresh QR code: " + err.Error()
		} else {
			waitForLatestQRCode(2500 * time.Millisecond)
			msg = "QR code refreshed."
		}
	}

	var status WhatsAppStatusSnapshot
	if whatsAppStatusProvider != nil {
		status = whatsAppStatusProvider()
	}

	var b strings.Builder
	b.WriteString(adminPageHeader("WhatsApp"))
	b.WriteString(`<h2>WhatsApp</h2>`)
	b.WriteString(adminNav(r))
	if msg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<p><strong>Connection:</strong> ` + boolLabel(status.Connected, "Connected", "Disconnected") + `</p>`)
	b.WriteString(`<p><strong>Authenticated:</strong> ` + boolLabel(status.Authenticated, "Yes", "No") + `</p>`)
	if strings.TrimSpace(status.DeviceID) != "" {
		b.WriteString(`<p><strong>Device ID:</strong> <code>` + html.EscapeString(status.DeviceID) + `</code></p>`)
	}
	if strings.TrimSpace(status.LastEvent) != "" {
		b.WriteString(`<p><strong>Last event:</strong> ` + html.EscapeString(status.LastEvent) + `</p>`)
	}
	if strings.TrimSpace(status.LastError) != "" {
		b.WriteString(`<p style="color:#b91c1c;"><strong>Last error:</strong> ` + html.EscapeString(status.LastError) + `</p>`)
	}
	if !status.UpdatedAt.IsZero() {
		b.WriteString(`<p><strong>Updated at:</strong> ` + html.EscapeString(adminFormatTime(status.UpdatedAt)) + `</p>`)
	}

	if !status.Authenticated {
		if dataURI, ok := qrCodeDataURI(status.LatestQRCode); ok {
			b.WriteString(`<p><strong>Scan this QR code in WhatsApp:</strong></p>`)
			b.WriteString(`<p><img alt="WhatsApp QR Code" src="` + dataURI + `" width="280" height="280"></p>`)
		} else {
			b.WriteString(`<p>QR code is not available yet. Click the refresh page button to request the latest QR code.</p>`)
		}
		b.WriteString(`<p><a href="/admin/whatsapp?refresh=1"><button type="button">Refresh Page</button></a></p>`)
	} else {
		b.WriteString(`<form method="post" action="/admin/whatsapp/logout" onsubmit="return confirm('This will log out WhatsApp on this server and require scanning a new QR code to reconnect. Continue?');">`)
		b.WriteString(`<button type="submit" style="background:#b91c1c;color:#fff;border:none;padding:8px 12px;border-radius:4px;cursor:pointer;">Logout WhatsApp</button>`)
		b.WriteString(`</form>`)
	}

	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func qrCodeDataURI(code string) (string, bool) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return "", false
	}
	png, err := qrcode.Encode(trimmed, qrcode.Medium, 280)
	if err != nil {
		return "", false
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), true
}

func boolLabel(v bool, yes string, no string) string {
	if v {
		return yes
	}
	return no
}

func waitForLatestQRCode(timeout time.Duration) {
	if whatsAppStatusProvider == nil {
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := whatsAppStatusProvider()
		if strings.TrimSpace(s.LatestQRCode) != "" || s.Authenticated {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func adminWhatsAppLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if whatsAppLogoutFunc == nil {
		http.Redirect(w, r, "/admin/whatsapp", http.StatusSeeOther)
		return
	}
	if err := whatsAppLogoutFunc(); err != nil {
		http.Redirect(w, r, "/admin/whatsapp", http.StatusSeeOther)
		return
	}
	waitForLatestQRCode(2500 * time.Millisecond)
	http.Redirect(w, r, "/admin/whatsapp", http.StatusSeeOther)
}
