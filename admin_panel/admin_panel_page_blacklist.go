package admin_panel

import (
	"html"
	"net/http"
	"strings"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
)

func adminBlacklistHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	msg := strings.TrimSpace(r.URL.Query().Get("msg"))
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	rows, err := db.ListBlacklistedPhones()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var b strings.Builder
	b.WriteString(adminPageHeader("Blacklist"))
	b.WriteString(`<h2>Blacklist</h2>`)
	b.WriteString(adminNav(r))
	if msg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<p>Blacklisted participants are blocked: inbound messages are ignored (not saved, no AI reply), and cron tasks are suspended.</p>`)
	b.WriteString(adminPhoneFilterForm("/admin/blacklist", phoneFilter))
	b.WriteString(`<form method="post" action="/admin/blacklist/add">`)
	b.WriteString(`<p><label>Participant phone (digits only, include country code)<br><input name="participant_phone" placeholder="85254036581" required></label></p>`)
	b.WriteString(`<p><button type="submit" style="background:#b91c1c;color:#fff;">Blacklist Participant</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Current Blacklist</h3>`)
	blacklistVisible := 0
	for _, row := range rows {
		phone := strings.TrimSpace(row.Phone)
		if phoneFilter != "" && !strings.Contains(phone, phoneFilter) {
			continue
		}
		blacklistVisible++
	}
	b.WriteString(adminTableOuterWrapOpen(blacklistVisible))
	b.WriteString(`<table border="1" cellpadding="6" cellspacing="0"><tr><th>Phone</th><th>Blacklisted At</th><th>Action</th></tr>`)
	for _, row := range rows {
		phone := strings.TrimSpace(row.Phone)
		if phoneFilter != "" && !strings.Contains(phone, phoneFilter) {
			continue
		}
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + html.EscapeString(phone) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(adminFormatTime(row.Blacklisted)) + `</td>`)
		b.WriteString(`<td>`)
		b.WriteString(`<form method="post" action="/admin/blacklist/remove" onsubmit="return confirm('Unblacklist ` + html.EscapeString(phone) + `?');">`)
		b.WriteString(`<input type="hidden" name="participant_phone" value="` + html.EscapeString(phone) + `">`)
		b.WriteString(`<button type="submit">Unblacklist</button>`)
		b.WriteString(`</form>`)
		b.WriteString(`</td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())
	if len(rows) == 0 {
		b.WriteString(`<p>No blacklisted participant.</p>`)
	}
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminBlacklistAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/blacklist?msg=Invalid+form+data.", http.StatusSeeOther)
		return
	}
	phone := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant_phone")))
	if len(phone) < 8 || len(phone) > 15 {
		http.Redirect(w, r, "/admin/blacklist?msg=Phone+must+be+8-15+digits+(with+country+code).", http.StatusSeeOther)
		return
	}
	if err := db.AddBlacklistedPhone(phone); err != nil {
		http.Redirect(w, r, "/admin/blacklist?msg=Failed+to+blacklist+participant.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/blacklist?msg=Participant+blacklisted.", http.StatusSeeOther)
}

func adminBlacklistRemoveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/blacklist?msg=Invalid+form+data.", http.StatusSeeOther)
		return
	}
	phone := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant_phone")))
	if len(phone) < 8 || len(phone) > 15 {
		http.Redirect(w, r, "/admin/blacklist?msg=Phone+must+be+8-15+digits+(with+country+code).", http.StatusSeeOther)
		return
	}
	removed, err := db.RemoveBlacklistedPhone(phone)
	if err != nil {
		http.Redirect(w, r, "/admin/blacklist?msg=Failed+to+unblacklist+participant.", http.StatusSeeOther)
		return
	}
	if !removed {
		http.Redirect(w, r, "/admin/blacklist?msg=Phone+not+found+in+blacklist.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/blacklist?msg=Participant+unblacklisted.", http.StatusSeeOther)
}
