package admin_panel

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/survey"
)

type adminClientMetaSummary struct {
	ParticipantPhone string
	ParticipantName  string
	FirstContactAt   string
	BaselineDone     bool
	BaselineAt       string
	Verified         bool
	EndMessageSent   bool
	MessageInterval  string
	Intervention     string
	Followups        []string
}

type adminClientListItem struct {
	Phone        string
	BaselineDone bool
	Verified     bool
}

type adminConversationEntry struct {
	Time      time.Time
	Sender    string
	Receiver  string
	Direction string
	Nature    string
	Content   string
}

var clientInfoSendMessageFunc func(participantPhone string, text string) error

func SetClientInfoSendMessageHandler(fn func(participantPhone string, text string) error) {
	clientInfoSendMessageFunc = fn
}

func adminClientInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	search := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	selected := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("participant")))
	baselineFilter := strings.TrimSpace(r.URL.Query().Get("baseline"))
	if baselineFilter != "done" && baselineFilter != "not_done" {
		baselineFilter = ""
	}
	verifiedFilter := strings.TrimSpace(r.URL.Query().Get("verified"))
	if verifiedFilter != "yes" && verifiedFilter != "no" {
		verifiedFilter = ""
	}
	msg := strings.TrimSpace(r.URL.Query().Get("msg"))

	participants, err := adminLoadClientParticipants(search, baselineFilter, verifiedFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	selectedOK := false
	for _, p := range participants {
		if p.Phone == selected {
			selectedOK = true
			break
		}
	}
	if !selectedOK {
		selected = ""
	}
	if selected == "" && len(participants) > 0 {
		selected = participants[0].Phone
	}

	summary, _ := adminLoadClientMetaSummary(selected)
	history, _ := adminLoadClientConversation(selected)
	requireVerification := db.GetProjectSettingBool("REQUIRE_VERIFICATION", false)

	var b strings.Builder
	b.WriteString(adminPageHeader("Client Information"))
	b.WriteString(`<h2>Client Information</h2>`)
	b.WriteString(adminNav(r))
	if msg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<div style="display:flex;flex-direction:column;gap:14px;">`)
	b.WriteString(`<div style="border:1px solid #c9c9c9;padding:14px;">`)
	b.WriteString(adminRenderClientMetaSummary(summary))
	b.WriteString(`</div>`)
	b.WriteString(`<div style="display:flex;gap:14px;align-items:stretch;">`)
	b.WriteString(`<div style="border:1px solid #c9c9c9;padding:12px;width:280px;height:560px;display:flex;flex-direction:column;">`)
	b.WriteString(`<form method="get" action="/admin/client-info" style="margin-bottom:10px;display:flex;flex-direction:column;gap:10px;">`)
	b.WriteString(`<label>Filter by phone<br><input name="phone" value="` + html.EscapeString(search) + `" placeholder="digits only"></label>`)
	b.WriteString(`<label>Baseline<br><select name="baseline" style="width:100%;">`)
	b.WriteString(adminClientInfoOption("", baselineFilter == "", "All"))
	b.WriteString(adminClientInfoOption("done", baselineFilter == "done", "Completed"))
	b.WriteString(adminClientInfoOption("not_done", baselineFilter == "not_done", "Not completed"))
	b.WriteString(`</select></label>`)
	b.WriteString(`<label>Verified<br><select name="verified" style="width:100%;">`)
	b.WriteString(adminClientInfoOption("", verifiedFilter == "", "All"))
	b.WriteString(adminClientInfoOption("yes", verifiedFilter == "yes", "Verified"))
	b.WriteString(adminClientInfoOption("no", verifiedFilter == "no", "Not verified"))
	b.WriteString(`</select></label>`)
	if selected != "" {
		b.WriteString(`<input type="hidden" name="participant" value="` + html.EscapeString(selected) + `">`)
	}
	b.WriteString(`<div><button type="submit">Apply filters</button></div></form>`)
	b.WriteString(`<div><strong>Select Participant in meta</strong></div>`)
	b.WriteString(`<div style="margin-top:10px;height:100%;overflow-y:scroll;overflow-x:auto;">`)
	if len(participants) == 0 {
		b.WriteString(`<p>No participant found.</p>`)
	} else {
		for _, item := range participants {
			rawHref := "/admin/client-info?" + adminClientInfoListQuery(search, baselineFilter, verifiedFilter, item.Phone).Encode()
			line2 := `Baseline: ` + adminClientInfoStatusEmoji(item.BaselineDone)
			if requireVerification {
				line2 += ` | Verified: ` + adminClientInfoStatusEmoji(item.Verified)
			}
			sel := selected != "" && item.Phone == selected
			box := `display:block;padding:8px 10px;margin:6px 0;border:1px solid #e2e8f0;border-radius:8px;text-decoration:none;color:#0f172a;background:#fff;`
			if sel {
				box += `border-color:#2563eb;background:#eff6ff;`
			}
			b.WriteString(`<a href="` + html.EscapeString(rawHref) + `" style="` + box + `">`)
			b.WriteString(`<div style="font-weight:600;line-height:1.3;">` + html.EscapeString(item.Phone) + `</div>`)
			b.WriteString(`<div style="margin-top:4px;font-size:13px;color:#475569;line-height:1.35;">` + line2 + `</div>`)
			b.WriteString(`</a>`)
		}
	}
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)

	b.WriteString(`<div style="border:1px solid #c9c9c9;padding:12px;flex:1;display:flex;flex-direction:column;height:560px;">`)
	b.WriteString(`<div><strong>Participant Chat History</strong></div>`)
	b.WriteString(`<div style="margin-top:10px;flex:1;overflow-y:scroll;overflow-x:auto;border:1px solid #e1e1e1;padding:10px;background:#efeae2;">`)
	if selected == "" {
		b.WriteString(`<p>Select a participant to view chat history.</p>`)
	} else if len(history) == 0 {
		b.WriteString(`<p>No conversation found for this participant.</p>`)
	} else {
		for _, row := range history {
			align := "flex-start"
			bg := "#ffffff"
			if strings.EqualFold(strings.TrimSpace(row.Direction), "outbound") {
				align = "flex-end"
				bg = "#d9fdd3"
			}
			messageText := html.EscapeString(row.Content)
			messageText = strings.ReplaceAll(messageText, "\n", "<br>")
			b.WriteString(`<div style="display:flex;justify-content:` + align + `;margin:7px 0;">`)
			b.WriteString(`<div style="max-width:78%;background:` + bg + `;border:1px solid #d6d6d6;border-radius:10px;padding:8px 10px;box-shadow:0 1px 1px rgba(0,0,0,0.06);">`)
			b.WriteString(`<div style="white-space:normal;word-break:break-word;">` + messageText + `</div>`)
			b.WriteString(`<div style="margin-top:4px;font-size:12px;color:#5f6368;text-align:right;">` + html.EscapeString(adminFormatTime(row.Time)) + `</div>`)
			b.WriteString(`</div>`)
			b.WriteString(`</div>`)
		}
	}
	b.WriteString(`</div>`)
	b.WriteString(`<div style="margin-top:10px;border:1px solid #e1e1e1;padding:10px;">`)
	b.WriteString(`<form method="post" action="/admin/client-info/send">`)
	b.WriteString(`<input type="hidden" name="phone_filter" value="` + html.EscapeString(search) + `">`)
	b.WriteString(`<input type="hidden" name="baseline_filter" value="` + html.EscapeString(baselineFilter) + `">`)
	b.WriteString(`<input type="hidden" name="verified_filter" value="` + html.EscapeString(verifiedFilter) + `">`)
	b.WriteString(`<input type="hidden" name="participant" value="` + html.EscapeString(selected) + `">`)
	b.WriteString(`<label>Chatbox for sending manual message<br><textarea name="message" rows="3" style="width:100%;" placeholder="Type message"></textarea></label>`)
	b.WriteString(`<div style="margin-top:8px;"><button type="submit"` + disabledWhen(selected == "") + `>Send</button></div>`)
	b.WriteString(`</form>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminClientInfoSendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/client-info?msg="+url.QueryEscape("Invalid form data."), http.StatusSeeOther)
		return
	}
	search := common.DigitsOnly(strings.TrimSpace(r.FormValue("phone_filter")))
	participant := common.DigitsOnly(strings.TrimSpace(r.FormValue("participant")))
	baselineF := strings.TrimSpace(r.FormValue("baseline_filter"))
	if baselineF != "done" && baselineF != "not_done" {
		baselineF = ""
	}
	verifiedF := strings.TrimSpace(r.FormValue("verified_filter"))
	if verifiedF != "yes" && verifiedF != "no" {
		verifiedF = ""
	}
	message := strings.TrimSpace(r.FormValue("message"))
	redirBase := adminClientInfoRedirectQuery(search, baselineF, verifiedF, participant, "")
	if participant == "" {
		v := adminClientInfoListQuery(search, baselineF, verifiedF, "")
		v.Set("msg", "Select a participant first.")
		http.Redirect(w, r, "/admin/client-info?"+v.Encode(), http.StatusSeeOther)
		return
	}
	if message == "" {
		redirBase.Set("msg", "Message cannot be empty.")
		http.Redirect(w, r, "/admin/client-info?"+redirBase.Encode(), http.StatusSeeOther)
		return
	}
	if clientInfoSendMessageFunc == nil {
		redirBase.Set("msg", "Send handler is not configured.")
		http.Redirect(w, r, "/admin/client-info?"+redirBase.Encode(), http.StatusSeeOther)
		return
	}
	if err := clientInfoSendMessageFunc(participant, message); err != nil {
		redirBase.Set("msg", "Failed to send message.")
		http.Redirect(w, r, "/admin/client-info?"+redirBase.Encode(), http.StatusSeeOther)
		return
	}
	redirBase.Set("msg", "Message sent.")
	http.Redirect(w, r, "/admin/client-info?"+redirBase.Encode(), http.StatusSeeOther)
}

func adminClientInfoOption(value string, selected bool, label string) string {
	sel := ""
	if selected {
		sel = ` selected`
	}
	return `<option value="` + html.EscapeString(value) + `"` + sel + `>` + html.EscapeString(label) + `</option>`
}

// adminClientInfoListQuery builds GET query for participant links (digits-only phone values).
func adminClientInfoListQuery(phoneFilter, baselineFilter, verifiedFilter, participantPhone string) url.Values {
	v := url.Values{}
	if phoneFilter != "" {
		v.Set("phone", phoneFilter)
	}
	if baselineFilter != "" {
		v.Set("baseline", baselineFilter)
	}
	if verifiedFilter != "" {
		v.Set("verified", verifiedFilter)
	}
	if participantPhone != "" {
		v.Set("participant", participantPhone)
	}
	return v
}

func adminClientInfoRedirectQuery(phoneFilter, baselineFilter, verifiedFilter, participantPhone, msg string) url.Values {
	v := adminClientInfoListQuery(phoneFilter, baselineFilter, verifiedFilter, participantPhone)
	if strings.TrimSpace(msg) != "" {
		v.Set("msg", strings.TrimSpace(msg))
	}
	return v
}

func adminLoadClientParticipants(phoneFilter, baselineFilter, verifiedFilter string) ([]adminClientListItem, error) {
	rows, err := db.DB.Query(context.Background(), `SELECT participant_phone, has_baseline_questionnaire, verification FROM meta ORDER BY id DESC LIMIT 2000`)
	if err != nil {
		return nil, fmt.Errorf("query participants from meta: %w", err)
	}
	defer rows.Close()
	seen := map[string]adminClientListItem{}
	for rows.Next() {
		var encPhone string
		var baselineDone bool
		var verified bool
		if err := rows.Scan(&encPhone, &baselineDone, &verified); err != nil {
			return nil, fmt.Errorf("scan participants from meta: %w", err)
		}
		plain, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		phone := common.DigitsOnly(strings.TrimSpace(plain))
		if phone == "" {
			continue
		}
		if phoneFilter != "" && !strings.Contains(phone, phoneFilter) {
			continue
		}
		if existing, ok := seen[phone]; ok {
			existing.BaselineDone = existing.BaselineDone || baselineDone
			existing.Verified = existing.Verified || verified
			seen[phone] = existing
			continue
		}
		seen[phone] = adminClientListItem{
			Phone:        phone,
			BaselineDone: baselineDone,
			Verified:     verified,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate participants from meta: %w", err)
	}
	out := make([]adminClientListItem, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Phone < out[j].Phone })
	filtered := make([]adminClientListItem, 0, len(out))
	for _, item := range out {
		if baselineFilter == "done" && !item.BaselineDone {
			continue
		}
		if baselineFilter == "not_done" && item.BaselineDone {
			continue
		}
		if verifiedFilter == "yes" && !item.Verified {
			continue
		}
		if verifiedFilter == "no" && item.Verified {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func adminLoadClientMetaSummary(participantPhone string) (adminClientMetaSummary, error) {
	out := adminClientMetaSummary{ParticipantPhone: participantPhone}
	phone := common.DigitsOnly(strings.TrimSpace(participantPhone))
	if phone == "" {
		return out, nil
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return out, fmt.Errorf("encrypt selected participant phone: %w", err)
	}
	rows, err := db.DB.Query(context.Background(), `SELECT * FROM meta WHERE participant_phone = $1 ORDER BY id DESC LIMIT 1`, encryptedPhone)
	if err != nil {
		return out, fmt.Errorf("query selected participant meta: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return out, nil
	}
	fieldDescriptions := rows.FieldDescriptions()
	values, err := rows.Values()
	if err != nil {
		return out, fmt.Errorf("meta values for selected participant: %w", err)
	}
	metaMap := map[string]string{}
	for i, fd := range fieldDescriptions {
		key := string(fd.Name)
		if i >= len(values) || values[i] == nil {
			metaMap[key] = ""
			continue
		}
		switch x := values[i].(type) {
		case time.Time:
			metaMap[key] = adminFormatTime(x)
		case *time.Time:
			if x == nil {
				metaMap[key] = ""
			} else {
				metaMap[key] = adminFormatTime(*x)
			}
		default:
			metaMap[key] = strings.TrimSpace(fmt.Sprintf("%v", values[i]))
		}
	}

	out.FirstContactAt = adminFormatTimestampString(metaMap["first_contact_ts"])
	out.ParticipantName = strings.TrimSpace(metaMap["participant_name"])
	out.BaselineDone = strings.EqualFold(metaMap["has_baseline_questionnaire"], "true")
	out.BaselineAt = adminFormatTimestampString(metaMap["baseline_completed_ts"])
	out.Verified = strings.EqualFold(metaMap["verification"], "true")
	out.EndMessageSent = strings.EqualFold(metaMap["end_message"], "true")
	out.MessageInterval = metaMap["message_interval"]
	out.Intervention = adminInterventionStatus(out.BaselineAt)

	for col, val := range metaMap {
		if !strings.HasPrefix(col, "fu_") {
			continue
		}
		if strings.HasSuffix(col, "_completed") {
			name := strings.TrimSuffix(strings.TrimPrefix(col, "fu_"), "_completed")
			out.Followups = append(out.Followups, name+": completed="+val)
		}
		if strings.HasSuffix(col, "_timestamp") && strings.TrimSpace(val) != "" {
			name := strings.TrimSuffix(strings.TrimPrefix(col, "fu_"), "_timestamp")
			out.Followups = append(out.Followups, name+": timestamp="+adminFormatTimestampString(val))
		}
	}
	sort.Strings(out.Followups)
	return out, nil
}

func adminInterventionStatus(baselineCompletedAt string) string {
	trimmed := strings.TrimSpace(baselineCompletedAt)
	if trimmed == "" {
		return "not started"
	}
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil || cfg.Project.InterventionPeriod <= 0 {
		return "unknown"
	}
	t, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		t, err = time.Parse(time.RFC3339, trimmed)
	}
	if err != nil {
		return "unknown"
	}
	end := t.AddDate(0, 0, cfg.Project.InterventionPeriod)
	remaining := int(end.Sub(time.Now()).Hours() / 24)
	if remaining < 0 {
		return "ended"
	}
	return strconv.Itoa(remaining) + " day(s) left"
}

func adminLoadClientConversation(participantPhone string) ([]adminConversationEntry, error) {
	phone := common.DigitsOnly(strings.TrimSpace(participantPhone))
	if phone == "" {
		return []adminConversationEntry{}, nil
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return nil, fmt.Errorf("encrypt participant phone for conversation query: %w", err)
	}
	rows, err := db.DB.Query(context.Background(), `
SELECT sender, receiver, direction, nature, content, created_at
FROM conversation
WHERE participant_phone = $1
ORDER BY created_at ASC
LIMIT 500`, encryptedPhone)
	if err != nil {
		return nil, fmt.Errorf("query participant conversation: %w", err)
	}
	defer rows.Close()

	out := make([]adminConversationEntry, 0, 64)
	for rows.Next() {
		var encSender, encReceiver, direction, nature, content string
		var createdAt time.Time
		if err := rows.Scan(&encSender, &encReceiver, &direction, &nature, &content, &createdAt); err != nil {
			return nil, fmt.Errorf("scan participant conversation: %w", err)
		}
		plainSender, err := common.DecryptPhone(encSender)
		if err != nil {
			plainSender = "[decrypt-error]"
		}
		plainReceiver, err := common.DecryptPhone(encReceiver)
		if err != nil {
			plainReceiver = "[decrypt-error]"
		}
		out = append(out, adminConversationEntry{
			Time:      createdAt,
			Sender:    normalizePhoneDisplay(plainSender),
			Receiver:  normalizePhoneDisplay(plainReceiver),
			Direction: strings.TrimSpace(direction),
			Nature:    strings.TrimSpace(nature),
			Content:   strings.TrimSpace(content),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate participant conversation: %w", err)
	}
	return out, nil
}

func normalizePhoneDisplay(s string) string {
	digits := common.DigitsOnly(strings.TrimSpace(s))
	if digits != "" {
		return digits
	}
	return strings.TrimSpace(s)
}

func adminClientInfoStatusEmoji(done bool) string {
	if done {
		return "✅"
	}
	return "❌"
}

func adminRenderClientMetaSummary(summary adminClientMetaSummary) string {
	if strings.TrimSpace(summary.ParticipantPhone) == "" {
		return `<p>Select a participant to view details.</p>`
	}
	var b strings.Builder
	b.WriteString(`<p><strong>Participant:</strong> ` + html.EscapeString(summary.ParticipantPhone) + `</p>`)
	b.WriteString(`<p><strong>Name:</strong> ` + html.EscapeString(nonEmpty(summary.ParticipantName, "-")) + `</p>`)
	b.WriteString(`<p><strong>First contact:</strong> ` + html.EscapeString(nonEmpty(summary.FirstContactAt, "-")) + `</p>`)
	b.WriteString(`<p><strong>Baseline completed:</strong> ` + boolLabel(summary.BaselineDone, "yes", "no") + `</p>`)
	b.WriteString(`<p><strong>Baseline completion timestamp:</strong> ` + html.EscapeString(nonEmpty(summary.BaselineAt, "-")) + `</p>`)
	b.WriteString(`<p><strong>Verification status:</strong> ` + boolLabel(summary.Verified, "verified", "not verified") + `</p>`)
	b.WriteString(`<p><strong>Intervention status:</strong> ` + html.EscapeString(summary.Intervention) + `</p>`)
	b.WriteString(`<p><strong>End message sent:</strong> ` + boolLabel(summary.EndMessageSent, "yes", "no") + `</p>`)
	b.WriteString(`<p><strong>Message interval:</strong> ` + html.EscapeString(nonEmpty(summary.MessageInterval, "-")) + `</p>`)
	if len(summary.Followups) > 0 {
		b.WriteString(`<p><strong>Follow-up status:</strong></p><ul>`)
		for _, line := range summary.Followups {
			b.WriteString(`<li>` + html.EscapeString(line) + `</li>`)
		}
		b.WriteString(`</ul>`)
	}
	return b.String()
}

func nonEmpty(v string, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func disabledWhen(v bool) string {
	if v {
		return ` disabled`
	}
	return ""
}
