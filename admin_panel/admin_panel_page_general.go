package admin_panel

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"whatsapp-bot/db"
)

type adminConversationRow struct {
	ID        int64
	Phone     string
	Sender    string
	Receiver  string
	Direction string
	Nature    string
	Content   string
	CreatedAt time.Time
}

type adminMetaRow struct {
	Values map[string]string
}

type adminAutoMessageRow struct {
	ID          int64
	Phone       string
	ScheduledAt time.Time
	IsSent      bool
	SentAt      string
	Nature      string
	FollowupID  string
	Content     string
}

type adminTableColumnRow struct {
	TableName  string
	ColumnName string
	DataType   string
	IsNullable string
}

type adminSurveyResponseSection struct {
	Heading    string
	TableName  string
	SurveyID   string
	IsBaseline bool
	SurveyURL  string
	Headers    []string
	Rows       []adminMetaRow
}

func adminHomeHandler(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString(adminPageHeader("Admin Panel"))
	b.WriteString(`<h2>Admin Panel</h2>`)
	b.WriteString(adminNav(r))
	b.WriteString(`<p>Welcome to the admin panel. Use the navigation above to access the different pages.</p>`)
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminRenderLoginPage(w http.ResponseWriter, errMsg string) {
	var b strings.Builder
	b.WriteString(adminPageHeader("Admin Login"))
	b.WriteString(`<div class="panel">`)
	b.WriteString(`<h2>Admin Login</h2>`)
	if strings.TrimSpace(errMsg) != "" {
		b.WriteString(`<p class="status status-error">` + html.EscapeString(errMsg) + `</p>`)
	}
	b.WriteString(`<form method="post" action="/admin/login">`)
	b.WriteString(`<p><label>Username<br><input name="username" required></label></p>`)
	b.WriteString(`<p><label>Password<br><input type="password" name="password" required></label></p>`)
	b.WriteString(`<p><button type="submit">Login</button></p>`)
	b.WriteString(`</form>`)
	b.WriteString(`</div>`)
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminWriteHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func adminPageHeader(title string) string {
	return `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>` + html.EscapeString(title) + `</title><style>
body{font-family:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",Arial,sans-serif;background:#f8fafc;color:#0f172a;margin:0;padding:20px;line-height:1.5;}
h1,h2,h3{margin:0 0 12px 0;}
p{margin:8px 0;}
a{color:#0f5fd8;text-decoration:none;}
a:hover{text-decoration:underline;}
.panel{background:#fff;border:1px solid #e2e8f0;border-radius:10px;padding:16px;box-shadow:0 1px 2px rgba(15,23,42,0.05);margin-bottom:14px;}
.top-nav{display:flex;flex-wrap:wrap;gap:8px;margin:12px 0 16px 0;}
.top-nav a{display:inline-block;padding:6px 10px;border:1px solid #d6dee8;border-radius:999px;background:#fff;color:#1e293b;font-size:13px;}
.top-nav a:hover{background:#f1f5f9;text-decoration:none;}
.session-timeout-panel{margin:0 0 12px 0;padding:8px 10px;border:1px solid #bfdbfe;background:#eff6ff;color:#1e3a8a;border-radius:8px;font-size:13px;}
table{width:100%;border-collapse:collapse;margin-top:12px;background:#fff;border:1px solid #e2e8f0;border-radius:10px;overflow:hidden;}
th,td{vertical-align:top;border:1px solid #e2e8f0;padding:8px 10px;text-align:left;font-size:14px;}
th{background:#f1f5f9;font-weight:600;}
input,textarea,select{border:1px solid #cbd5e1;border-radius:8px;padding:8px 10px;font-size:14px;max-width:100%;box-sizing:border-box;background:#fff;}
textarea{width:100%;max-width:100%;}
button{border:1px solid #0f5fd8;background:#0f5fd8;color:#fff;border-radius:8px;padding:7px 12px;font-weight:600;cursor:pointer;}
button:hover{background:#0b4eb6;}
pre{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px;}
.status{padding:8px 10px;border-radius:8px;}
.status-error{background:#fef2f2;color:#991b1b;border:1px solid #fecaca;}
.status-success{background:#ecfdf3;color:#166534;border:1px solid #bbf7d0;}
code{background:#f1f5f9;padding:1px 5px;border-radius:6px;}
.admin-table-scroll-wrap{overflow-x:auto;margin-top:8px;}
.admin-table-scroll-wrap.tall{max-height:30rem;overflow-y:auto;border:1px solid #e2e8f0;border-radius:8px;}
.admin-table-scroll-wrap table{margin-top:0;}
</style></head><body>`
}

func adminPageFooter() string {
	return `</body></html>`
}

// adminTableOuterWrapOpen wraps a table: always horizontal scroll; when dataRowCount > 10,
// also bounds height with vertical scroll (header row not included in the count).
func adminTableOuterWrapOpen(dataRowCount int) string {
	class := "admin-table-scroll-wrap"
	if dataRowCount > 10 {
		class += " tall"
	}
	return `<div class="` + class + `">`
}

func adminTableOuterWrapClose() string {
	return `</div>`
}

func adminNav(r *http.Request) string {
	type navItem struct {
		Path  string
		Label string
	}
	items := []navItem{
		{Path: "/admin/home/", Label: "Home"},
		{Path: "/admin/table/conversation", Label: "Conversation History"},
		{Path: "/admin/client-info", Label: "Client Information"},
		{Path: "/admin/enrollment", Label: "Enrollment"},
		{Path: "/admin/blacklist", Label: "Blacklist"},
		{Path: "/admin/survey-responses", Label: "Survey Responses"},
		{Path: "/admin/table/meta", Label: "Meta"},
		{Path: "/admin/verification", Label: "Verification"},
		{Path: "/admin/table/auto-messages", Label: "Auto Messages"},
		{Path: "/admin/rag", Label: "RAG"},
		{Path: "/admin/table/embedding", Label: "Embedding Table"},
		{Path: "/admin/table/db-tables", Label: "DB Tables"},
		{Path: "/admin/table/project-setting", Label: "Project Setting (Raw)"},
		{Path: "/admin/whatsapp", Label: "WhatsApp"},
		{Path: "/admin/configuration", Label: "Configuration"},
		{Path: "/admin/log", Label: "Log"},
		{Path: "/admin/role", Label: "Role"},
	}

	session, sessionOK := adminSessionFromRequest(r)
	permittedPages := []string{}
	if sessionOK && !session.IsRoot {
		if pages, err := db.RolePermittedPages(session.Username); err == nil {
			permittedPages = pages
		}
	}

	parts := make([]string, 0, len(items)+1)
	for _, item := range items {
		allowed := false
		if sessionOK && session.IsRoot {
			allowed = true
		} else if sessionOK {
			allowed = db.RoleAllowsPath(permittedPages, item.Path)
		}
		if !allowed {
			continue
		}
		parts = append(parts, `<a href="`+html.EscapeString(item.Path)+`">`+html.EscapeString(item.Label)+`</a>`)
	}
	parts = append(parts, `<a href="/admin/logout">Logout</a>`)
	timeoutHTML := adminSessionCountdownHTML(r)
	return timeoutHTML + `<div class="top-nav">` + strings.Join(parts, ``) + `</div>`
}

func adminPhoneFilterForm(action string, phone string) string {
	return `<form method="get" action="` + html.EscapeString(action) + `"><label>Filter by phone (digits): <input name="phone" value="` + html.EscapeString(phone) + `"></label> <button type="submit">Search</button></form>`
}

func adminConversationPhoneFilterForm(action string, phone string) string {
	return `<form method="get" action="` + html.EscapeString(action) + `"><label>Filter by phone (sender/receiver digits): <input name="phone" value="` + html.EscapeString(phone) + `"></label> <button type="submit">Search</button></form>`
}

func adminDisplayLocation() *time.Location {
	return db.AdminPanelDisplayLocation()
}

// adminParseLocalWallTime parses a civil date/time using ADMIN_PANEL_UTC_OFFSET_HOURS as the zone and returns UTC for DB storage.
func adminParseLocalWallTime(input string) (time.Time, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return time.Time{}, fmt.Errorf("datetime is empty")
	}
	loc := adminDisplayLocation()
	layouts := []string{
		"2006-01-02 15:04",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid datetime (use admin offset local wall time, e.g. 2026-04-24 15:30)")
}

func adminFormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(adminDisplayLocation()).Format(time.RFC3339)
}

func adminFormatTimestampString(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999-07",
		"2006-01-02 15:04:05.999999-07",
		"2006-01-02 15:04:05-07",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return adminFormatTime(parsed)
		}
	}
	return raw
}

func adminShouldFormatTimestampColumn(column string) bool {
	c := strings.ToLower(strings.TrimSpace(column))
	if c == "" {
		return false
	}
	switch c {
	case "created_at", "updated_at", "submitted_at", "scheduled_time", "sent_timestamp", "timestamp", "first_contact_ts", "baseline_completed_ts":
		return true
	}
	return strings.HasSuffix(c, "_timestamp") || strings.HasSuffix(c, "_ts")
}

func adminFormatValueByColumn(column string, value string) string {
	if adminShouldFormatTimestampColumn(column) {
		return adminFormatTimestampString(value)
	}
	return value
}
