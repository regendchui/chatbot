package admin_panel

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/survey"
)

var enrollmentBaselineInviteFunc func(participantPhone string) error

func SetEnrollmentBaselineInviteHandler(fn func(participantPhone string) error) {
	enrollmentBaselineInviteFunc = fn
}

func adminEnrollmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	msg := strings.TrimSpace(r.URL.Query().Get("msg"))
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	rows, err := adminLoadEnrollmentPreviewRows(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var b strings.Builder
	b.WriteString(adminPageHeader("Enrollment"))
	b.WriteString(`<h2>Enrollment</h2>`)
	b.WriteString(adminNav(r))
	if msg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<p>Add participant phone number into meta table without waiting for first inbound message.</p>`)
	b.WriteString(adminPhoneFilterForm("/admin/enrollment", phoneFilter))
	b.WriteString(`<form method="post" action="/admin/enrollment/add">`)
	b.WriteString(`<p><label>Participant phone (digits only, include country code)<br><input name="participant_phone" placeholder="85254036581" required></label></p>`)
	b.WriteString(`<p><button type="submit">Enroll Participant</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Recently Enrolled / Existing Meta Rows</h3>`)
	b.WriteString(adminTableOuterWrapOpen(len(rows)))
	b.WriteString(`<table border="1" cellpadding="6" cellspacing="0"><tr><th>Participant Phone</th><th>First Contact</th><th>Baseline Completed</th><th>Verified</th><th>Action</th></tr>`)
	for _, row := range rows {
		phone := strings.TrimSpace(row.Values["participant_phone"])
		b.WriteString("<tr>")
		b.WriteString("<td>" + html.EscapeString(phone) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Values["first_contact_ts"]) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Values["has_baseline_questionnaire"]) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.Values["verification"]) + "</td>")
		b.WriteString(`<td>`)
		b.WriteString(`<form method="post" action="/admin/enrollment/delete" onsubmit="return confirm('Delete participant ` + html.EscapeString(phone) + ` from meta, conversation history, survey responses, and auto-message rows?');">`)
		b.WriteString(`<input type="hidden" name="participant_phone" value="` + html.EscapeString(phone) + `">`)
		b.WriteString(`<button type="submit" style="background:#b91c1c;color:#fff;">Delete Participant</button>`)
		b.WriteString(`</form>`)
		b.WriteString(`</td>`)
		b.WriteString("</tr>")
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminEnrollmentAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/enrollment?msg=Invalid+form+data.", http.StatusSeeOther)
		return
	}
	phone := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant_phone")))
	if len(phone) < 8 || len(phone) > 15 {
		http.Redirect(w, r, "/admin/enrollment?msg=Phone+must+be+8-15+digits+(with+country+code).", http.StatusSeeOther)
		return
	}
	isNew, err := db.EnsureParticipantMeta(phone)
	if err != nil {
		http.Redirect(w, r, "/admin/enrollment?msg=Failed+to+enroll+participant.", http.StatusSeeOther)
		return
	}
	if isNew {
		if enrollmentBaselineInviteFunc != nil {
			if err := enrollmentBaselineInviteFunc(phone); err != nil {
				http.Redirect(w, r, "/admin/enrollment?msg=Participant+enrolled+but+failed+to+send+baseline+invitation.", http.StatusSeeOther)
				return
			}
		}
		http.Redirect(w, r, "/admin/enrollment?msg=Participant+enrolled+and+baseline+invitation+sent.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/enrollment?msg=Participant+already+exists+in+meta.", http.StatusSeeOther)
}

func adminEnrollmentDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/enrollment?msg=Invalid+form+data.", http.StatusSeeOther)
		return
	}
	phone := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant_phone")))
	if len(phone) < 8 || len(phone) > 15 {
		http.Redirect(w, r, "/admin/enrollment?msg=Phone+must+be+8-15+digits+(with+country+code).", http.StatusSeeOther)
		return
	}

	cfg := survey.GlobalSurveyConfig()
	if cfg == nil {
		http.Redirect(w, r, "/admin/enrollment?msg=Survey+config+is+not+loaded.", http.StatusSeeOther)
		return
	}

	var deletedSurveyRows int64
	n, err := adminDeleteSurveyRowsByPhone(cfg, phone)
	if err != nil {
		http.Redirect(w, r, "/admin/enrollment?msg=Failed+to+delete+survey+responses.", http.StatusSeeOther)
		return
	}
	deletedSurveyRows += n
	deletedConversationRows, err := db.DeleteConversationByParticipantPhoneDigits(phone)
	if err != nil {
		http.Redirect(w, r, "/admin/enrollment?msg=Failed+to+delete+participant+conversation+rows.", http.StatusSeeOther)
		return
	}
	deletedAutoMessageRows, err := db.DeleteAutoMessagesByParticipantPhoneDigits(phone)
	if err != nil {
		http.Redirect(w, r, "/admin/enrollment?msg=Failed+to+delete+participant+auto-message+rows.", http.StatusSeeOther)
		return
	}

	deletedMetaRows, err := db.DeleteParticipantMetaForPhoneDigits(phone)
	if err != nil {
		http.Redirect(w, r, "/admin/enrollment?msg=Failed+to+delete+participant+meta+row.", http.StatusSeeOther)
		return
	}
	if deletedMetaRows == 0 {
		http.Redirect(w, r, "/admin/enrollment?msg=Participant+not+found+in+meta.", http.StatusSeeOther)
		return
	}
	http.Redirect(
		w,
		r,
		"/admin/enrollment?msg="+urlQueryEscape(fmt.Sprintf("Participant deleted. Removed %d meta row(s), %d survey response row(s), %d conversation row(s), and %d auto-message row(s).", deletedMetaRows, deletedSurveyRows, deletedConversationRows, deletedAutoMessageRows)),
		http.StatusSeeOther,
	)
}

func adminDeleteSurveyRowsByPhone(cfg *survey.SurveyConfig, phone string) (int64, error) {
	if cfg == nil {
		return 0, fmt.Errorf("survey config is nil")
	}
	total := int64(0)
	baselineTable := strings.TrimSpace(cfg.Baseline.TableName)
	if baselineTable != "" {
		if err := common.ValidateSQLIdentifier(baselineTable, "baseline table name"); err != nil {
			return total, err
		}
		query := fmt.Sprintf(`DELETE FROM %s WHERE %s = $1`, baselineTable, survey.RespondentPhoneColumn)
		tag, err := db.DB.Exec(context.Background(), query, phone)
		if err != nil {
			return total, fmt.Errorf("delete baseline responses: %w", err)
		}
		total += tag.RowsAffected()
	}
	for _, fu := range cfg.Followups {
		table := strings.TrimSpace(fu.TableName)
		if table == "" {
			continue
		}
		if err := common.ValidateSQLIdentifier(table, "followup table name"); err != nil {
			return total, err
		}
		query := fmt.Sprintf(`DELETE FROM %s WHERE %s = $1`, table, survey.RespondentPhoneColumn)
		tag, err := db.DB.Exec(context.Background(), query, phone)
		if err != nil {
			return total, fmt.Errorf("delete followup responses from %s: %w", table, err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

func adminLoadEnrollmentPreviewRows(phoneFilter string) ([]adminMetaRow, error) {
	query := `
SELECT participant_phone, first_contact_ts, has_baseline_questionnaire, verification
FROM meta
ORDER BY first_contact_ts DESC, id DESC
LIMIT 200`
	rows, err := db.DB.Query(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("query enrollment preview rows: %w", err)
	}
	defer rows.Close()

	out := make([]adminMetaRow, 0, 64)
	for rows.Next() {
		var encPhone string
		var firstContactTS time.Time
		var baselineDone bool
		var verified bool
		if err := rows.Scan(&encPhone, &firstContactTS, &baselineDone, &verified); err != nil {
			return nil, fmt.Errorf("scan enrollment preview rows: %w", err)
		}
		plainPhone, err := common.DecryptPhone(encPhone)
		if err != nil {
			plainPhone = "[decrypt-error]"
		}
		out = append(out, adminMetaRow{
			Values: map[string]string{
				"participant_phone":          common.DigitsOnly(strings.TrimSpace(plainPhone)),
				"first_contact_ts":           adminFormatTime(firstContactTS),
				"has_baseline_questionnaire": boolToText(baselineDone),
				"verification":               boolToText(verified),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enrollment preview rows: %w", err)
	}
	if phoneFilter == "" {
		return out, nil
	}
	filtered := make([]adminMetaRow, 0, len(out))
	for _, row := range out {
		if strings.Contains(row.Values["participant_phone"], phoneFilter) {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

func boolToText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
