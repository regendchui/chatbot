package admin_panel

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"whatsapp-bot/db"
)

type adminRolePermissionOption struct {
	Path  string
	Label string
}

func adminRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	roles, err := db.ListRoleUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	msg := strings.TrimSpace(r.URL.Query().Get("msg"))
	options := adminRolePermissionOptions()

	var b strings.Builder
	b.WriteString(adminPageHeader("Role"))
	b.WriteString(`<h2>Role</h2>`)
	b.WriteString(adminNav(r))
	if msg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<p>Create role users and define which admin paths they can access.</p>`)

	b.WriteString(`<form method="post" action="/admin/role/add">`)
	b.WriteString(`<p><label>Username<br><input name="username" required></label></p>`)
	b.WriteString(`<p><label>Password<br><input type="password" name="password" required></label></p>`)
	b.WriteString(`<p><label>Confirm password<br><input type="password" name="confirm_password" required></label></p>`)
	b.WriteString(`<p><strong>Permitted pages</strong><br>`)
	for _, opt := range options {
		b.WriteString(`<label style="display:block;margin:4px 0;">`)
		b.WriteString(`<input type="checkbox" name="permitted_pages" value="` + html.EscapeString(opt.Path) + `"> `)
		b.WriteString(html.EscapeString(opt.Label) + ` <code>` + html.EscapeString(opt.Path) + `</code>`)
		b.WriteString(`</label>`)
	}
	b.WriteString(`</p>`)
	b.WriteString(`<p><button type="submit">Create Role User</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Existing Role Users</h3>`)
	b.WriteString(adminTableOuterWrapOpen(len(roles)))
	b.WriteString(`<table border="1" cellpadding="6" cellspacing="0"><tr><th>Username</th><th>Permitted pages</th><th>Manage permissions</th><th>Reset password</th><th>Action</th></tr>`)
	for _, role := range roles {
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + html.EscapeString(role.Username) + `</td>`)
		b.WriteString(`<td>`)
		if len(role.PermittedPages) == 0 {
			b.WriteString(`<span style="color:#6b7280;">No permitted pages</span>`)
		} else {
			for i, page := range role.PermittedPages {
				if i > 0 {
					b.WriteString(`<br>`)
				}
				b.WriteString(`<code>` + html.EscapeString(page) + `</code>`)
			}
		}
		b.WriteString(`</td>`)
		b.WriteString(`<td>`)
		rolePageSet := map[string]struct{}{}
		for _, p := range role.PermittedPages {
			rolePageSet[p] = struct{}{}
		}
		b.WriteString(`<form method="post" action="/admin/role/permissions/update">`)
		b.WriteString(`<input type="hidden" name="username" value="` + html.EscapeString(role.Username) + `">`)
		for _, opt := range options {
			checked := ""
			if _, ok := rolePageSet[opt.Path]; ok {
				checked = " checked"
			}
			b.WriteString(`<label style="display:block;margin:4px 0;">`)
			b.WriteString(`<input type="checkbox" name="permitted_pages" value="` + html.EscapeString(opt.Path) + `"` + checked + `> `)
			b.WriteString(html.EscapeString(opt.Label) + ` <code>` + html.EscapeString(opt.Path) + `</code>`)
			b.WriteString(`</label>`)
		}
		b.WriteString(`<button type="submit">Save Permissions</button>`)
		b.WriteString(`</form>`)
		b.WriteString(`</td>`)
		b.WriteString(`<td>`)
		b.WriteString(`<form method="post" action="/admin/role/password/reset">`)
		b.WriteString(`<input type="hidden" name="username" value="` + html.EscapeString(role.Username) + `">`)
		b.WriteString(`<label>New password<br><input type="password" name="new_password" required></label><br>`)
		b.WriteString(`<label>Confirm password<br><input type="password" name="confirm_password" required></label><br>`)
		b.WriteString(`<button type="submit">Reset Password</button>`)
		b.WriteString(`</form>`)
		b.WriteString(`</td>`)
		b.WriteString(`<td>`)
		b.WriteString(`<form method="post" action="/admin/role/delete" onsubmit="return confirm('Remove role user ` + html.EscapeString(role.Username) + `?');">`)
		b.WriteString(`<input type="hidden" name="username" value="` + html.EscapeString(role.Username) + `">`)
		b.WriteString(`<button type="submit" style="background:#b91c1c;color:#fff;">Remove</button>`)
		b.WriteString(`</form>`)
		b.WriteString(`</td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminRoleAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRoleRedirect(w, r, "Invalid form data.")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))
	confirmPassword := strings.TrimSpace(r.FormValue("confirm_password"))
	if username == "" || password == "" || confirmPassword == "" {
		adminRoleRedirect(w, r, "Username and password are required.")
		return
	}
	if password != confirmPassword {
		adminRoleRedirect(w, r, "Password and confirm password must match.")
		return
	}
	adminUsername := strings.TrimSpace(db.GetProjectSettingString("ADMIN_PANEL_USERNAME", ""))
	if adminUsername != "" && strings.EqualFold(username, adminUsername) {
		adminRoleRedirect(w, r, "Username conflicts with primary admin username.")
		return
	}
	permittedPages := make([]string, 0, 32)
	permittedPages = append(permittedPages, r.Form["permitted_pages"]...)
	permittedPages = db.NormalizeRolePermittedPages(permittedPages)
	if len(permittedPages) == 0 {
		adminRoleRedirect(w, r, "At least one permitted page is required.")
		return
	}
	if err := db.CreateRoleUser(username, password, permittedPages); err != nil {
		adminRoleRedirect(w, r, "Failed to create role user: "+err.Error())
		return
	}
	adminRoleRedirect(w, r, fmt.Sprintf("Role user %q created.", username))
}

func adminRolePermissionOptions() []adminRolePermissionOption {
	return []adminRolePermissionOption{
		{Path: "/admin/home", Label: "Home"},
		{Path: "/admin/table/conversation", Label: "Conversation History"},
		{Path: "/admin/client-info", Label: "Client Information"},
		{Path: "/admin/enrollment", Label: "Enrollment"},
		{Path: "/admin/blacklist", Label: "Blacklist"},
		{Path: "/admin/survey-responses", Label: "Survey Responses"},
		{Path: "/admin/table/meta", Label: "Meta"},
		{Path: "/admin/verification", Label: "Verification"},
		{Path: "/admin/table/auto-messages", Label: "Auto Messages"},
		{Path: "/admin/table/db-tables", Label: "DB Tables"},
		{Path: "/admin/table/project-setting", Label: "Project Setting (Raw)"},
		{Path: "/admin/whatsapp", Label: "WhatsApp"},
		{Path: "/admin/configuration", Label: "Configuration"},
		{Path: "/admin/log", Label: "Log"},
		{Path: "/admin/role", Label: "Role"},
		{Path: "/admin/rag", Label: "RAG"},
		{Path: "/admin/table/embedding", Label: "Embedding Table"},
	}
}

func adminParseCustomPermittedPages(raw string) []string {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	text = strings.ReplaceAll(text, ",", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func adminRoleRedirect(w http.ResponseWriter, r *http.Request, msg string) {
	target := "/admin/role"
	if strings.TrimSpace(msg) != "" {
		target = fmt.Sprintf("/admin/role?msg=%s", urlQueryEscape(msg))
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func adminRoleDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRoleRedirect(w, r, "Invalid form data.")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if username == "" {
		adminRoleRedirect(w, r, "Username is required.")
		return
	}
	deleted, err := db.DeleteRoleUser(username)
	if err != nil {
		adminRoleRedirect(w, r, "Failed to remove role user: "+err.Error())
		return
	}
	if !deleted {
		adminRoleRedirect(w, r, "Role user not found.")
		return
	}
	adminRoleRedirect(w, r, fmt.Sprintf("Role user %q removed.", username))
}

func adminRoleUpdatePermissionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRoleRedirect(w, r, "Invalid form data.")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if username == "" {
		adminRoleRedirect(w, r, "Username is required.")
		return
	}
	permittedPages := db.NormalizeRolePermittedPages(
		r.Form["permitted_pages"],
	)
	updated, err := db.UpdateRoleUserPermittedPages(username, permittedPages)
	if err != nil {
		adminRoleRedirect(w, r, "Failed to update permissions: "+err.Error())
		return
	}
	if !updated {
		adminRoleRedirect(w, r, "Role user not found.")
		return
	}
	adminRoleRedirect(w, r, fmt.Sprintf("Permissions updated for %q.", username))
}

func adminRoleResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRoleRedirect(w, r, "Invalid form data.")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	newPassword := strings.TrimSpace(r.FormValue("new_password"))
	confirmPassword := strings.TrimSpace(r.FormValue("confirm_password"))
	if username == "" {
		adminRoleRedirect(w, r, "Username is required.")
		return
	}
	if newPassword == "" || confirmPassword == "" {
		adminRoleRedirect(w, r, "Both password fields are required.")
		return
	}
	if newPassword != confirmPassword {
		adminRoleRedirect(w, r, "New password and confirmation do not match.")
		return
	}
	updated, err := db.UpdateRoleUserPassword(username, newPassword)
	if err != nil {
		adminRoleRedirect(w, r, "Failed to reset password: "+err.Error())
		return
	}
	if !updated {
		adminRoleRedirect(w, r, "Role user not found.")
		return
	}
	adminRoleRedirect(w, r, fmt.Sprintf("Password reset for %q.", username))
}
