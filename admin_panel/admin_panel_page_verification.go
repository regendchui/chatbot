package admin_panel

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
)

type adminVerificationCard struct {
	MetaID               int64
	ParticipantPhone     string
	BaselineCompletedAt  string
	HasBaselineCompleted bool
	Verified             bool
}

var verificationApprovedFunc func(participantPhone string) error

func SetVerificationApprovedHandler(fn func(participantPhone string) error) {
	verificationApprovedFunc = fn
}

func adminVerificationHandler(w http.ResponseWriter, r *http.Request) {
	required := db.GetProjectSettingBool("REQUIRE_VERIFICATION", false)
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	statusMsg := strings.TrimSpace(r.URL.Query().Get("msg"))

	var b strings.Builder
	b.WriteString(adminPageHeader("Verification"))
	b.WriteString(`<h2>Verification</h2>`)
	b.WriteString(adminNav(r))
	if statusMsg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(statusMsg) + `</p>`)
	}

	if !required {
		b.WriteString(`<p>Verification is not enabled.</p>`)
		b.WriteString(adminPageFooter())
		adminWriteHTML(w, b.String())
		return
	}

	b.WriteString(adminPhoneFilterForm("/admin/verification", phoneFilter))

	cards, err := adminLoadPendingVerificationCards(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b.WriteString(`<h3 style="margin-top:20px;">Pending verification</h3>`)
	if len(cards) == 0 {
		b.WriteString(`<p>No participants are waiting for verification.</p>`)
	} else {
		for _, card := range cards {
			b.WriteString(`<div style="border:1px solid #d1d5db;border-radius:8px;padding:12px;margin:12px 0;max-width:600px;">`)
			b.WriteString(`<p><strong>Participant:</strong> ` + html.EscapeString(card.ParticipantPhone) + `</p>`)
			b.WriteString(`<p><strong>Baseline completed:</strong> ` + boolLabel(card.HasBaselineCompleted, "Yes", "No") + `</p>`)
			if card.BaselineCompletedAt != "" {
				b.WriteString(`<p><strong>Baseline completed at:</strong> ` + html.EscapeString(card.BaselineCompletedAt) + `</p>`)
			}
			b.WriteString(`<p><strong>Verified:</strong> ` + boolLabel(card.Verified, "Yes", "No") + `</p>`)
			b.WriteString(`<div style="display:flex;gap:8px;flex-wrap:wrap;">`)
			b.WriteString(`<form method="post" action="/admin/verification/approve" onsubmit="return confirm('Verify this participant and allow AI chat?');">`)
			b.WriteString(`<input type="hidden" name="participant_phone" value="` + html.EscapeString(card.ParticipantPhone) + `">`)
			b.WriteString(`<button type="submit">Verify + Send AI Message</button>`)
			b.WriteString(`</form>`)
			b.WriteString(`<form method="post" action="/admin/verification/approve-no-ai" onsubmit="return confirm('Verify this participant without sending AI initiation message?');">`)
			b.WriteString(`<input type="hidden" name="participant_phone" value="` + html.EscapeString(card.ParticipantPhone) + `">`)
			b.WriteString(`<button type="submit" style="background:#475569;border-color:#334155;">Verify Only (No AI Message)</button>`)
			b.WriteString(`</form>`)
			b.WriteString(`</div>`)
			b.WriteString(`</div>`)
		}
	}

	verifiedCards, err := adminLoadVerifiedVerificationCards(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b.WriteString(`<h3 style="margin-top:28px;">Verified participants</h3>`)
	b.WriteString(`<p style="font-size:14px;color:#475569;">Revoke verification so the participant must be verified again before AI chat (when verification is required).</p>`)
	if len(verifiedCards) == 0 {
		b.WriteString(`<p><em>No verified participants match the current filter.</em></p>`)
	} else {
		for _, card := range verifiedCards {
			b.WriteString(`<div style="border:1px solid #cbd5e1;border-radius:8px;padding:12px;margin:12px 0;max-width:600px;background:#f8fafc;">`)
			b.WriteString(`<p><strong>Participant:</strong> ` + html.EscapeString(card.ParticipantPhone) + `</p>`)
			b.WriteString(`<p><strong>Baseline completed:</strong> ` + boolLabel(card.HasBaselineCompleted, "Yes", "No") + `</p>`)
			if card.BaselineCompletedAt != "" {
				b.WriteString(`<p><strong>Baseline completed at:</strong> ` + html.EscapeString(card.BaselineCompletedAt) + `</p>`)
			}
			b.WriteString(`<form method="post" action="/admin/verification/unverify" onsubmit="return confirm('Unverify this participant? They will be blocked from AI chat until verified again.');">`)
			b.WriteString(`<input type="hidden" name="participant_phone" value="` + html.EscapeString(card.ParticipantPhone) + `">`)
			b.WriteString(`<button type="submit" style="background:#b45309;border-color:#92400e;">Unverify participant</button>`)
			b.WriteString(`</form>`)
			b.WriteString(`</div>`)
		}
	}

	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminVerificationApproveHandler(w http.ResponseWriter, r *http.Request) {
	adminVerificationApproveInternal(w, r, true)
}

func adminVerificationApproveNoAIHandler(w http.ResponseWriter, r *http.Request) {
	adminVerificationApproveInternal(w, r, false)
}

func adminVerificationApproveInternal(w http.ResponseWriter, r *http.Request, shouldSendAI bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !db.GetProjectSettingBool("REQUIRE_VERIFICATION", false) {
		http.Redirect(w, r, "/admin/verification?msg=Verification+is+not+enabled.", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/verification?msg=Invalid+form+data.", http.StatusSeeOther)
		return
	}
	phone := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant_phone")))
	if phone == "" {
		http.Redirect(w, r, "/admin/verification?msg=Participant+phone+is+required.", http.StatusSeeOther)
		return
	}
	updatedRows, err := db.MarkParticipantVerifiedForPhoneDigits(phone)
	if err != nil {
		http.Redirect(w, r, "/admin/verification?msg=Failed+to+verify+participant.", http.StatusSeeOther)
		return
	}
	if shouldSendAI && updatedRows > 0 && verificationApprovedFunc != nil {
		if err := verificationApprovedFunc(phone); err != nil {
			http.Redirect(w, r, "/admin/verification?msg=Participant+verified+but+AI+initiation+message+failed.", http.StatusSeeOther)
			return
		}
	}
	if shouldSendAI {
		http.Redirect(w, r, "/admin/verification?msg=Participant+verified.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/verification?msg=Participant+verified+without+sending+AI+message.", http.StatusSeeOther)
}

func adminVerificationUnverifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !db.GetProjectSettingBool("REQUIRE_VERIFICATION", false) {
		http.Redirect(w, r, "/admin/verification?msg="+url.QueryEscape("Verification is not enabled."), http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/verification?msg="+url.QueryEscape("Invalid form data."), http.StatusSeeOther)
		return
	}
	phone := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant_phone")))
	if phone == "" {
		http.Redirect(w, r, "/admin/verification?msg="+url.QueryEscape("Participant phone is required."), http.StatusSeeOther)
		return
	}
	n, err := db.MarkParticipantUnverifiedForPhoneDigits(phone)
	if err != nil {
		http.Redirect(w, r, "/admin/verification?msg="+url.QueryEscape("Failed to unverify: "+err.Error()), http.StatusSeeOther)
		return
	}
	if n == 0 {
		http.Redirect(w, r, "/admin/verification?msg="+url.QueryEscape("No verified meta row was updated (already unverified?)."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/verification?msg="+url.QueryEscape("Participant unverified."), http.StatusSeeOther)
}

func adminLoadPendingVerificationCards(phoneFilter string) ([]adminVerificationCard, error) {
	rows, err := db.DB.Query(context.Background(), `
SELECT id, participant_phone, baseline_completed_ts, has_baseline_questionnaire, verification
FROM meta
WHERE verification = FALSE
  AND (has_baseline_questionnaire = TRUE OR baseline_completed_ts IS NOT NULL)
ORDER BY baseline_completed_ts DESC NULLS LAST, id DESC
LIMIT 500`)
	if err != nil {
		return nil, fmt.Errorf("query pending verification participants: %w", err)
	}
	defer rows.Close()

	out := make([]adminVerificationCard, 0, 64)
	for rows.Next() {
		var id int64
		var encPhone string
		var baselineCompletedAtTS *time.Time
		var hasBaseline bool
		var verified bool
		if err := rows.Scan(&id, &encPhone, &baselineCompletedAtTS, &hasBaseline, &verified); err != nil {
			return nil, fmt.Errorf("scan pending verification participants: %w", err)
		}
		plainPhone, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		digits := common.DigitsOnly(plainPhone)
		if phoneFilter != "" && digits != phoneFilter {
			continue
		}
		baselineCompletedAtText := ""
		if baselineCompletedAtTS != nil {
			baselineCompletedAtText = adminFormatTime(*baselineCompletedAtTS)
		}
		out = append(out, adminVerificationCard{
			MetaID:               id,
			ParticipantPhone:     digits,
			BaselineCompletedAt:  baselineCompletedAtText,
			HasBaselineCompleted: hasBaseline || baselineCompletedAtTS != nil,
			Verified:             verified,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending verification participants: %w", err)
	}
	return out, nil
}

func adminLoadVerifiedVerificationCards(phoneFilter string) ([]adminVerificationCard, error) {
	rows, err := db.DB.Query(context.Background(), `
SELECT id, participant_phone, baseline_completed_ts, has_baseline_questionnaire, verification
FROM meta
WHERE verification = TRUE
  AND (has_baseline_questionnaire = TRUE OR baseline_completed_ts IS NOT NULL)
ORDER BY baseline_completed_ts DESC NULLS LAST, id DESC
LIMIT 500`)
	if err != nil {
		return nil, fmt.Errorf("query verified participants: %w", err)
	}
	defer rows.Close()

	out := make([]adminVerificationCard, 0, 64)
	for rows.Next() {
		var id int64
		var encPhone string
		var baselineCompletedAtTS *time.Time
		var hasBaseline bool
		var verified bool
		if err := rows.Scan(&id, &encPhone, &baselineCompletedAtTS, &hasBaseline, &verified); err != nil {
			return nil, fmt.Errorf("scan verified participants: %w", err)
		}
		plainPhone, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		digits := common.DigitsOnly(plainPhone)
		if phoneFilter != "" && digits != phoneFilter {
			continue
		}
		baselineCompletedAtText := ""
		if baselineCompletedAtTS != nil {
			baselineCompletedAtText = adminFormatTime(*baselineCompletedAtTS)
		}
		out = append(out, adminVerificationCard{
			MetaID:               id,
			ParticipantPhone:     digits,
			BaselineCompletedAt:  baselineCompletedAtText,
			HasBaselineCompleted: hasBaseline || baselineCompletedAtTS != nil,
			Verified:             verified,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate verified participants: %w", err)
	}
	return out, nil
}
