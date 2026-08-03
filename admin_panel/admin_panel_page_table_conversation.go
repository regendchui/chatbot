package admin_panel

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
)

func adminConversationHandler(w http.ResponseWriter, r *http.Request) {
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	statusMsg := strings.TrimSpace(r.URL.Query().Get("msg"))
	rows, err := adminLoadConversationRows(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var b strings.Builder
	b.WriteString(adminPageHeader("Conversation History"))
	b.WriteString(`<h2>Conversation History</h2>`)
	b.WriteString(adminNav(r))
	if statusMsg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(statusMsg) + `</p>`)
	}
	b.WriteString(adminConversationPhoneFilterForm("/admin/table/conversation", phoneFilter))
	b.WriteString(`<form method="post" action="/admin/table/conversation/delete-one" onsubmit="return confirm('Delete this conversation row? This cannot be undone.');" style="display:none;"></form>`)
	b.WriteString(`<form method="post" action="/admin/table/conversation/delete-by-phone" onsubmit="return confirm('Delete all conversation history for this phone number? This cannot be undone.');" style="margin:12px 0;">`)
	b.WriteString(`<input type="hidden" name="phone_filter" value="` + html.EscapeString(phoneFilter) + `">`)
	b.WriteString(`<label>Delete all conversation by phone (digits only)<br><input name="participant_phone" value="` + html.EscapeString(phoneFilter) + `" placeholder="85254036581" required pattern="[0-9]{8,15}"></label> `)
	b.WriteString(`<button type="submit" style="background:#b91c1c;color:#fff;border-color:#991b1b;">Delete Conversation by Phone</button>`)
	b.WriteString(`</form>`)
	b.WriteString(`<p><a href="/admin/table/conversation/export?phone=` + html.EscapeString(phoneFilter) + `">Export current table as CSV</a></p>`)
	b.WriteString(adminTableOuterWrapOpen(len(rows)))
	b.WriteString(`<table><tr><th>ID</th><th>Phone</th><th>Sender</th><th>Receiver</th><th>Direction</th><th>Nature</th><th>Content</th><th>Created At</th><th>Actions</th></tr>`)
	for _, row := range rows {
		b.WriteString("<tr>")
		b.WriteString("<td>" + fmt.Sprintf("%d", row.ID) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Phone) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Sender) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Receiver) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Direction) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Nature) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Content) + "</td>")
		b.WriteString("<td>" + html.EscapeString(adminFormatTime(row.CreatedAt)) + "</td>")
		b.WriteString(`<td><form method="post" action="/admin/table/conversation/delete-one" style="margin:0;" onsubmit="return confirm('Delete conversation row id ` + html.EscapeString(fmt.Sprintf("%d", row.ID)) + `? This cannot be undone.');">`)
		b.WriteString(`<input type="hidden" name="conversation_id" value="` + html.EscapeString(fmt.Sprintf("%d", row.ID)) + `">`)
		b.WriteString(`<input type="hidden" name="phone_filter" value="` + html.EscapeString(phoneFilter) + `">`)
		b.WriteString(`<button type="submit" style="background:#b91c1c;color:#fff;border-color:#991b1b;">Delete</button></form></td>`)
		b.WriteString("</tr>")
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())

	engagementHTML, engagementErr := adminRenderEngagementTableHTML(phoneFilter)
	if engagementErr != nil {
		http.Error(w, engagementErr.Error(), http.StatusInternalServerError)
		return
	}
	b.WriteString(engagementHTML)

	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminConversationDeleteByPhoneHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/table/conversation?msg="+url.QueryEscape("Invalid form."), http.StatusSeeOther)
		return
	}
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.FormValue("phone_filter")))
	phone := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant_phone")))
	redir := "/admin/table/conversation"
	if phoneFilter != "" {
		redir += "?phone=" + url.QueryEscape(phoneFilter)
	}
	redirWithMsg := func(msg string) {
		sep := "?"
		if strings.Contains(redir, "?") {
			sep = "&"
		}
		http.Redirect(w, r, redir+sep+"msg="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if len(phone) < 8 || len(phone) > 15 {
		redirWithMsg("Phone must be 8-15 digits including country code.")
		return
	}
	deletedRows, err := db.DeleteConversationByParticipantPhoneDigits(phone)
	if err != nil {
		redirWithMsg("Delete failed: " + err.Error())
		return
	}
	redirWithMsg(fmt.Sprintf("Deleted %d conversation row(s) for %s.", deletedRows, phone))
}

func adminConversationDeleteOneHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/table/conversation?msg="+url.QueryEscape("Invalid form."), http.StatusSeeOther)
		return
	}
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.FormValue("phone_filter")))
	redir := "/admin/table/conversation"
	if phoneFilter != "" {
		redir += "?phone=" + url.QueryEscape(phoneFilter)
	}
	redirWithMsg := func(msg string) {
		sep := "?"
		if strings.Contains(redir, "?") {
			sep = "&"
		}
		http.Redirect(w, r, redir+sep+"msg="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	idStr := strings.TrimSpace(r.FormValue("conversation_id"))
	conversationID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || conversationID <= 0 {
		redirWithMsg("Invalid conversation id.")
		return
	}
	deleted, err := db.DeleteConversationByID(conversationID)
	if err != nil {
		redirWithMsg("Delete failed: " + err.Error())
		return
	}
	if !deleted {
		redirWithMsg("No row found for that id (already deleted?).")
		return
	}
	redirWithMsg(fmt.Sprintf("Conversation row %d deleted.", conversationID))
}

func adminLoadConversationRows(phoneFilter string) ([]adminConversationRow, error) {
	query := `
SELECT id, participant_phone, sender, receiver, direction, nature, content, created_at
FROM conversation
ORDER BY id DESC
LIMIT 500`
	rows, err := db.DB.Query(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("query conversation: %w", err)
	}
	defer rows.Close()
	out := []adminConversationRow{}
	for rows.Next() {
		var id int64
		var encParticipantPhone, encSender, encReceiver, direction, nature, content string
		var createdAt time.Time
		if err := rows.Scan(&id, &encParticipantPhone, &encSender, &encReceiver, &direction, &nature, &content, &createdAt); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		plainParticipantPhone, err := common.DecryptPhone(encParticipantPhone)
		if err != nil {
			plainParticipantPhone = "[decrypt-error]"
		}
		plainSender, err := common.DecryptPhone(encSender)
		if err != nil {
			plainSender = "[decrypt-error]"
		}
		plainReceiver, err := common.DecryptPhone(encReceiver)
		if err != nil {
			plainReceiver = "[decrypt-error]"
		}
		normalizedDirection := strings.TrimSpace(direction)
		normalizedNature := strings.TrimSpace(nature)
		normalizedPhone := common.DigitsOnly(strings.TrimSpace(plainParticipantPhone))
		if normalizedPhone == "" {
			normalizedPhone = strings.TrimSpace(plainParticipantPhone)
		}
		normalizedSender := common.DigitsOnly(strings.TrimSpace(plainSender))
		if normalizedSender == "" {
			normalizedSender = strings.TrimSpace(plainSender)
		}
		normalizedReceiver := common.DigitsOnly(strings.TrimSpace(plainReceiver))
		if normalizedReceiver == "" {
			normalizedReceiver = strings.TrimSpace(plainReceiver)
		}
		if phoneFilter != "" && normalizedSender != phoneFilter && normalizedReceiver != phoneFilter {
			continue
		}
		out = append(out, adminConversationRow{
			ID:        id,
			Phone:     normalizedPhone,
			Sender:    normalizedSender,
			Receiver:  normalizedReceiver,
			Direction: normalizedDirection,
			Nature:    normalizedNature,
			Content:   strings.TrimSpace(content),
			CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation: %w", err)
	}
	return out, nil
}
