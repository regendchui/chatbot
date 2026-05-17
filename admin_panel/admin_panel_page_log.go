package admin_panel

import (
	"html"
	"net/http"
	"strconv"
	"strings"

	"whatsapp-bot/db"
)

func adminLogHandler(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	loginRows, err := db.ListRecentLoginHistory(limit)
	if err != nil {
		http.Error(w, "failed to load login history: "+err.Error(), http.StatusInternalServerError)
		return
	}
	configRows, err := db.ListRecentConfigUpdateHistory(limit)
	if err != nil {
		http.Error(w, "failed to load configuration update history: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var b strings.Builder
	b.WriteString(adminPageHeader("Log"))
	b.WriteString(`<h2>Log</h2>`)
	b.WriteString(adminNav(r))
	b.WriteString(`<p>Showing latest ` + strconv.Itoa(limit) + ` rows for each table.</p>`)

	b.WriteString(`<h3>Login History (Admin and Role Users)</h3>`)
	b.WriteString(adminTableOuterWrapOpen(len(loginRows)))
	b.WriteString(`<table><thead><tr><th>ID</th><th>Timestamp</th><th>Username</th><th>User Type</th><th>Result</th><th>Failure Type</th><th>Client IP</th></tr></thead><tbody>`)
	for _, row := range loginRows {
		result := "fail"
		if row.Success {
			result = "success"
		}
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + strconv.FormatInt(row.ID, 10) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(adminFormatTime(row.CreatedAt)) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(strings.TrimSpace(row.Username)) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(strings.TrimSpace(row.UserType)) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(result) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(strings.TrimSpace(row.FailureType)) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(strings.TrimSpace(row.ClientIP)) + `</td>`)
		b.WriteString(`</tr>`)
	}
	if len(loginRows) == 0 {
		b.WriteString(`<tr><td colspan="7">No login history yet.</td></tr>`)
	}
	b.WriteString(`</tbody></table>`)
	b.WriteString(adminTableOuterWrapClose())

	b.WriteString(`<h3>Configuration Update History</h3>`)
	b.WriteString(adminTableOuterWrapOpen(len(configRows)))
	b.WriteString(`<table><thead><tr><th>ID</th><th>Timestamp</th><th>Actor</th><th>Action</th><th>Description</th></tr></thead><tbody>`)
	for _, row := range configRows {
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + strconv.FormatInt(row.ID, 10) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(adminFormatTime(row.CreatedAt)) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(strings.TrimSpace(row.Actor)) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(strings.TrimSpace(row.Action)) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(strings.TrimSpace(row.Description)) + `</td>`)
		b.WriteString(`</tr>`)
	}
	if len(configRows) == 0 {
		b.WriteString(`<tr><td colspan="5">No configuration update history yet.</td></tr>`)
	}
	b.WriteString(`</tbody></table>`)
	b.WriteString(adminTableOuterWrapClose())

	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}
