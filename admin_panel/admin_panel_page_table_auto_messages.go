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

	"github.com/jackc/pgx/v5"
)

var autoMessageRetrySendFunc func(taskID int64) error

// SetAutoMessageRetrySendHandler wires admin “send missed cron” retries to the WhatsApp runtime (set from main).
func SetAutoMessageRetrySendHandler(fn func(taskID int64) error) {
	autoMessageRetrySendFunc = fn
}

func adminAutoMessagesHandler(w http.ResponseWriter, r *http.Request) {
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	statusMsg := strings.TrimSpace(r.URL.Query().Get("msg"))
	todayRows, err := adminLoadAutoMessageRowsForAdminLocalToday(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allRows, err := adminLoadAutoMessageRows(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	loc := db.AdminPanelDisplayLocation()
	todayAdminLabel := time.Now().In(loc).Format("2006-01-02")
	offsetHint := strings.TrimSpace(db.GetProjectSettingString("ADMIN_PANEL_UTC_OFFSET_HOURS", "0"))

	var b strings.Builder
	b.WriteString(adminPageHeader("Auto Messages"))
	b.WriteString(`<h2>Auto Message DB</h2>`)
	b.WriteString(adminNav(r))
	if statusMsg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(statusMsg) + `</p>`)
	}

	b.WriteString(`<div style="border:1px solid #cbd5e1;border-radius:10px;padding:14px;margin-bottom:18px;background:#f8fafc;">`)
	b.WriteString(`<h3 style="margin-top:0;">Add auto message row</h3>`)
	b.WriteString(`<p style="font-size:14px;color:#334155;margin-top:0;">Enter <strong>scheduled time</strong> and optional <strong>sent time</strong> as <em>local wall clock</em> using your admin panel offset (<code>ADMIN_PANEL_UTC_OFFSET_HOURS</code> = <strong>` + html.EscapeString(offsetHint) + `</strong> hours from UTC). Values are converted to <strong>UTC</strong> before insert.</p>`)
	b.WriteString(`<p style="font-size:13px;color:#64748b;">Accepted formats: <code>2006-01-02 15:04</code> or <code>2006-01-02T15:04</code> (optional seconds).</p>`)
	b.WriteString(`<form method="post" action="/admin/table/auto-messages/insert" style="display:flex;flex-direction:column;gap:10px;max-width:720px;">`)
	b.WriteString(`<input type="hidden" name="phone_filter" value="` + html.EscapeString(phoneFilter) + `">`)
	b.WriteString(`<label>Participant phone (digits only)<br><input name="participant_phone" required pattern="[0-9]{8,15}" style="width:100%;max-width:420px;"></label>`)
	b.WriteString(`<label>Scheduled time (local to admin offset)<br><input name="scheduled_local" required placeholder="2026-04-24 15:30" style="width:100%;max-width:420px;"></label>`)
	b.WriteString(`<label><input type="checkbox" name="is_sent" value="true"> Already sent</label>`)
	b.WriteString(`<label>Sent at (local, optional — used if “Already sent” is checked)<br><input name="sent_at_local" placeholder="leave blank to use current UTC time" style="width:100%;max-width:420px;"></label>`)
	b.WriteString(`<label>Nature<br><select name="nature" required><option value="AI message">AI message</option><option value="follow-up prompt">follow-up prompt</option><option value="manual message">manual message</option></select></label>`)
	b.WriteString(`<label>Follow-up survey id (required only for follow-up prompt; otherwise leave empty)<br><input name="followup_survey_id" style="width:100%;max-width:420px;"></label>`)
	b.WriteString(`<label>Message content (required for manual message; optional otherwise)<br><textarea name="message_content" rows="2" style="width:100%;max-width:520px;"></textarea></label>`)
	b.WriteString(`<div><button type="submit">Insert row</button></div>`)
	b.WriteString(`</form></div>`)

	b.WriteString(adminPhoneFilterForm("/admin/table/auto-messages", phoneFilter))
	b.WriteString(`<form method="post" action="/admin/table/auto-messages/delete-by-phone" onsubmit="return confirm('Delete all cron/auto-message rows for this phone number? This cannot be undone.');" style="margin:12px 0;">`)
	b.WriteString(`<input type="hidden" name="phone_filter" value="` + html.EscapeString(phoneFilter) + `">`)
	b.WriteString(`<label>Delete all cron messages by phone (digits only)<br><input name="participant_phone" value="` + html.EscapeString(phoneFilter) + `" placeholder="85254036581" required pattern="[0-9]{8,15}"></label> `)
	b.WriteString(`<button type="submit" style="background:#b91c1c;border-color:#991b1b;">Delete Cron Messages by Phone</button>`)
	b.WriteString(`</form>`)
	b.WriteString(`<p><a href="/admin/table/auto-messages/export?phone=` + html.EscapeString(phoneFilter) + `">Export current table as CSV</a></p>`)

	b.WriteString(`<h3>Today’s cron tasks (admin local date ` + html.EscapeString(todayAdminLabel) + `)</h3>`)
	b.WriteString(`<p style="font-size:14px;color:#475569;">Rows whose <code>scheduled_time</code> falls on this calendar day in your admin panel offset (<code>ADMIN_PANEL_UTC_OFFSET_HOURS</code>). On each automated run (daily at survey <code>cron_task_time</code> in UTC and once at startup), every <strong>unsent</strong> row in that day is sent, even if its wall-clock time is later in the day. Table times use the same offset for display.</p>`)
	b.WriteString(adminRenderAutoMessageTable(todayRows, phoneFilter))

	b.WriteString(`<h3 style="margin-top:28px;">All scheduled tasks (furthest future first → oldest past)</h3>`)
	b.WriteString(`<p style="font-size:14px;color:#475569;">Ordered by <code>scheduled_time</code> descending. “Send missed” appears when the row is not sent and the scheduled instant has already passed (server clock), so you can recover same-day misses after the scheduled time.</p>`)
	b.WriteString(adminRenderAutoMessageTable(allRows, phoneFilter))

	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminRenderAutoMessageTable(rows []adminAutoMessageRow, phoneFilter string) string {
	if len(rows) == 0 {
		return `<p><em>No rows.</em></p>`
	}
	var b strings.Builder
	b.WriteString(adminTableOuterWrapOpen(len(rows)))
	b.WriteString(`<table><tr><th>ID</th><th>Phone</th><th>Scheduled At</th><th>Sent</th><th>Sent At</th><th>Nature</th><th>Follow-up Survey</th><th>Content</th><th>Actions</th></tr>`)
	for _, row := range rows {
		b.WriteString("<tr>")
		b.WriteString("<td>" + fmt.Sprintf("%d", row.ID) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Phone) + "</td>")
		b.WriteString("<td>" + html.EscapeString(adminFormatTime(row.ScheduledAt)) + "</td>")
		b.WriteString("<td>" + fmt.Sprintf("%t", row.IsSent) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.SentAt) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Nature) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.FollowupID) + "</td>")
		b.WriteString(`<td style="max-width:360px;white-space:normal;word-break:break-word;">` + html.EscapeString(row.Content) + `</td>`)
		b.WriteString(`<td><div style="display:flex;flex-direction:column;gap:6px;align-items:flex-start;">`)
		if adminAutoMessageRetryEligible(row.ScheduledAt, row.IsSent) && autoMessageRetrySendFunc != nil {
			b.WriteString(`<form method="post" action="/admin/table/auto-messages/retry-send" style="margin:0;" onsubmit="return confirm('Send this missed scheduled message now? It will be marked as sent.');">`)
			b.WriteString(`<input type="hidden" name="task_id" value="` + html.EscapeString(fmt.Sprintf("%d", row.ID)) + `">`)
			b.WriteString(`<input type="hidden" name="phone_filter" value="` + html.EscapeString(phoneFilter) + `">`)
			b.WriteString(`<button type="submit">Send missed</button></form>`)
		} else if adminAutoMessageRetryEligible(row.ScheduledAt, row.IsSent) && autoMessageRetrySendFunc == nil {
			b.WriteString(`<span style="color:#64748b;font-size:13px;">Send missed unavailable</span>`)
		} else {
			b.WriteString(`<span style="color:#94a3b8;font-size:13px;">—</span>`)
		}
		b.WriteString(`<form method="post" action="/admin/table/auto-messages/delete" style="margin:0;" onsubmit="return confirm('Delete this cron task (id ` + html.EscapeString(fmt.Sprintf("%d", row.ID)) + `)? This cannot be undone.');">`)
		b.WriteString(`<input type="hidden" name="task_id" value="` + html.EscapeString(fmt.Sprintf("%d", row.ID)) + `">`)
		b.WriteString(`<input type="hidden" name="phone_filter" value="` + html.EscapeString(phoneFilter) + `">`)
		b.WriteString(`<button type="submit" style="background:#b91c1c;border-color:#991b1b;">Delete</button></form>`)
		b.WriteString(`</div></td>`)
		b.WriteString("</tr>")
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())
	return b.String()
}

func adminAutoMessageRetryEligible(scheduledAt time.Time, isSent bool) bool {
	if isSent {
		return false
	}
	return scheduledAt.Before(time.Now())
}

func adminAutoMessageInsertHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/table/auto-messages?msg="+url.QueryEscape("Invalid form."), http.StatusSeeOther)
		return
	}
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.FormValue("phone_filter")))
	redirBase := "/admin/table/auto-messages"
	if phoneFilter != "" {
		redirBase += "?phone=" + url.QueryEscape(phoneFilter)
	}
	redirMsg := func(msg string) {
		sep := "?"
		if strings.Contains(redirBase, "?") {
			sep = "&"
		}
		http.Redirect(w, r, redirBase+sep+"msg="+url.QueryEscape(msg), http.StatusSeeOther)
	}

	participant := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant_phone")))
	schedLocal := strings.TrimSpace(r.FormValue("scheduled_local"))
	isSent := strings.TrimSpace(r.FormValue("is_sent")) == "true"
	sentLocal := strings.TrimSpace(r.FormValue("sent_at_local"))
	nature := strings.TrimSpace(r.FormValue("nature"))
	followupID := strings.TrimSpace(r.FormValue("followup_survey_id"))
	msgContent := strings.TrimSpace(r.FormValue("message_content"))

	if participant == "" {
		redirMsg("Participant phone is required.")
		return
	}
	schedUTC, err := adminParseLocalWallTime(schedLocal)
	if err != nil {
		redirMsg("Scheduled time: " + err.Error())
		return
	}
	var sentUTC *time.Time
	if isSent {
		if sentLocal != "" {
			t, err := adminParseLocalWallTime(sentLocal)
			if err != nil {
				redirMsg("Sent at: " + err.Error())
				return
			}
			sentUTC = &t
		}
	}
	if err := db.InsertAutoMessageManualRow(participant, schedUTC, isSent, sentUTC, nature, followupID, msgContent); err != nil {
		redirMsg("Insert failed: " + err.Error())
		return
	}
	redirMsg("Row inserted.")
}

func adminAutoMessageRetrySendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/table/auto-messages?msg="+url.QueryEscape("Invalid form."), http.StatusSeeOther)
		return
	}
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.FormValue("phone_filter")))
	redir := "/admin/table/auto-messages"
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
	idStr := strings.TrimSpace(r.FormValue("task_id"))
	taskID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || taskID <= 0 {
		redirWithMsg("Invalid task id.")
		return
	}
	if autoMessageRetrySendFunc == nil {
		redirWithMsg("Send missed is not available (WhatsApp client not wired).")
		return
	}
	if err := autoMessageRetrySendFunc(taskID); err != nil {
		redirWithMsg("Send missed failed: " + err.Error())
		return
	}
	redirWithMsg("Missed message sent and marked as sent.")
}

func adminAutoMessageDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/table/auto-messages?msg="+url.QueryEscape("Invalid form."), http.StatusSeeOther)
		return
	}
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.FormValue("phone_filter")))
	redir := "/admin/table/auto-messages"
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
	idStr := strings.TrimSpace(r.FormValue("task_id"))
	taskID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || taskID <= 0 {
		redirWithMsg("Invalid task id.")
		return
	}
	deleted, err := db.DeleteAutoMessageTaskByID(taskID)
	if err != nil {
		redirWithMsg("Delete failed: " + err.Error())
		return
	}
	if !deleted {
		redirWithMsg("No row found for that id (already deleted?).")
		return
	}
	redirWithMsg("Cron task deleted.")
}

func adminAutoMessageDeleteByPhoneHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/table/auto-messages?msg="+url.QueryEscape("Invalid form."), http.StatusSeeOther)
		return
	}
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.FormValue("phone_filter")))
	phone := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant_phone")))
	redir := "/admin/table/auto-messages"
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
	deletedRows, err := db.DeleteAutoMessagesByParticipantPhoneDigits(phone)
	if err != nil {
		redirWithMsg("Delete failed: " + err.Error())
		return
	}
	redirWithMsg(fmt.Sprintf("Deleted %d auto-message row(s) for %s.", deletedRows, phone))
}

func adminLoadAutoMessageRowsForAdminLocalToday(phoneFilter string) ([]adminAutoMessageRow, error) {
	startUTC, endUTC := db.AdminLocalDayRangeUTC(time.Now())
	query := `
SELECT id, participant_phone, scheduled_time, is_sent, sent_timestamp, nature, followup_survey_id, message_content
FROM auto_message_db
WHERE scheduled_time >= $1 AND scheduled_time < $2
ORDER BY id DESC
LIMIT 500`
	rows, err := db.DB.Query(context.Background(), query, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("query auto_message_db today: %w", err)
	}
	return adminScanAutoMessageRows(rows, phoneFilter)
}

func adminLoadAutoMessageRows(phoneFilter string) ([]adminAutoMessageRow, error) {
	query := `
SELECT id, participant_phone, scheduled_time, is_sent, sent_timestamp, nature, followup_survey_id, message_content
FROM auto_message_db
ORDER BY id DESC
LIMIT 800`
	rows, err := db.DB.Query(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("query auto_message_db: %w", err)
	}
	return adminScanAutoMessageRows(rows, phoneFilter)
}

func adminScanAutoMessageRows(rows pgx.Rows, phoneFilter string) ([]adminAutoMessageRow, error) {
	defer rows.Close()
	out := []adminAutoMessageRow{}
	for rows.Next() {
		var row adminAutoMessageRow
		var encPhone string
		var sentAt *time.Time
		if err := rows.Scan(&row.ID, &encPhone, &row.ScheduledAt, &row.IsSent, &sentAt, &row.Nature, &row.FollowupID, &row.Content); err != nil {
			return nil, fmt.Errorf("scan auto_message_db: %w", err)
		}
		plainPhone, err := common.DecryptPhone(encPhone)
		if err != nil {
			row.Phone = "[decrypt-error]"
		} else {
			row.Phone = common.DigitsOnly(strings.TrimSpace(plainPhone))
		}
		if phoneFilter != "" && row.Phone != phoneFilter {
			continue
		}
		if sentAt != nil {
			row.SentAt = adminFormatTime(*sentAt)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auto_message_db: %w", err)
	}
	return out, nil
}
