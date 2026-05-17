package admin_panel

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"
)

const adminSessionTimeout = 3 * time.Hour

func adminSessionTimeoutDuration() time.Duration {
	return adminSessionTimeout
}

func adminSessionExpiryFromNow(now time.Time) time.Time {
	return now.Add(adminSessionTimeoutDuration())
}

func adminSessionCountdownHTML(r *http.Request) string {
	session, ok := adminSessionFromRequest(r)
	if !ok || session.ExpiresAt.IsZero() {
		return ""
	}

	remaining := int64(time.Until(session.ExpiresAt).Seconds())
	if remaining < 0 {
		remaining = 0
	}

	expiresUnix := session.ExpiresAt.Unix()
	var b strings.Builder
	b.WriteString(`<div class="session-timeout-panel">`)
	b.WriteString(`<strong>Auto logout in:</strong> `)
	b.WriteString(`<span id="admin-session-remaining" data-remaining-seconds="` + fmt.Sprintf("%d", remaining) + `" data-expires-unix="` + fmt.Sprintf("%d", expiresUnix) + `">--:--:--</span>`)
	b.WriteString(`</div>`)
	b.WriteString(`<script>(function(){`)
	b.WriteString(`var el=document.getElementById("admin-session-remaining");if(!el){return;}`)
	b.WriteString(`var expiresUnix=parseInt(el.getAttribute("data-expires-unix")||"0",10);`)
	b.WriteString(`if(!Number.isFinite(expiresUnix)||expiresUnix<=0){el.textContent="00:00:00";return;}`)
	b.WriteString(`function pad(n){return n<10?"0"+n:String(n);}`)
	b.WriteString(`function render(){`)
	b.WriteString(`var now=Math.floor(Date.now()/1000);var sec=expiresUnix-now;if(sec<0){sec=0;}`)
	b.WriteString(`var h=Math.floor(sec/3600);var m=Math.floor((sec%3600)/60);var s=sec%60;`)
	b.WriteString(`el.textContent=pad(h)+":"+pad(m)+":"+pad(s);`)
	b.WriteString(`if(sec<=0){window.location.href="/admin/logout";return false;}return true;}`)
	b.WriteString(`if(!render()){return;}var t=setInterval(function(){if(!render()){clearInterval(t);}},1000);`)
	b.WriteString(`})();</script>`)
	return b.String()
}

func adminSessionTimeoutLabel() string {
	return html.EscapeString(adminSessionTimeoutDuration().String())
}
