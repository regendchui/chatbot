package admin_panel

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
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
	Phone           string
	BaselineDone    bool
	Verified        bool
	ParticipantName string // from meta; set at baseline completion
	BaselineAt      time.Time
	LastMessageAt   time.Time
}

type adminConversationEntry struct {
	ID        int64
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
	interventionFilter := strings.TrimSpace(r.URL.Query().Get("intervention"))
	if interventionFilter != "in" && interventionFilter != "not_in" {
		interventionFilter = ""
	}
	msg := strings.TrimSpace(r.URL.Query().Get("msg"))

	participants, err := adminLoadClientParticipants(search, baselineFilter, verifiedFilter, interventionFilter)
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
	b.WriteString(`<div style="display:flex;gap:14px;align-items:stretch;min-height:78vh;">`)
	b.WriteString(`<div style="border:1px solid #c9c9c9;padding:12px;width:300px;min-height:78vh;height:78vh;display:flex;flex-direction:column;">`)
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
	b.WriteString(`<label>Intervention period<br><select name="intervention" style="width:100%;">`)
	b.WriteString(adminClientInfoOption("", interventionFilter == "", "All"))
	b.WriteString(adminClientInfoOption("in", interventionFilter == "in", "In intervention"))
	b.WriteString(adminClientInfoOption("not_in", interventionFilter == "not_in", "Not in intervention"))
	b.WriteString(`</select></label>`)
	if selected != "" {
		b.WriteString(`<input type="hidden" name="participant" value="` + html.EscapeString(selected) + `">`)
	}
	b.WriteString(`<div><button type="submit">Apply filters</button></div></form>`)
	b.WriteString(`<div><strong>Select Participant in meta</strong></div>`)
	b.WriteString(`<div style="margin-top:10px;flex:1;min-height:0;overflow-y:scroll;overflow-x:auto;">`)
	if len(participants) == 0 {
		b.WriteString(`<p>No participant found.</p>`)
	} else {
		for _, item := range participants {
			rawHref := "/admin/client-info?" + adminClientInfoListQuery(search, baselineFilter, verifiedFilter, interventionFilter, item.Phone).Encode()
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
			b.WriteString(`<div style="font-weight:600;line-height:1.3;">` + html.EscapeString(item.Phone) + ` ` + html.EscapeString(adminClientInfoParticipantParen(item)) + `</div>`)
			b.WriteString(`<div style="margin-top:4px;font-size:13px;color:#475569;line-height:1.35;">` + line2 + `</div>`)
			b.WriteString(`</a>`)
		}
	}
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)

	b.WriteString(`<div style="border:1px solid #c9c9c9;padding:12px;flex:1;display:flex;flex-direction:column;min-height:78vh;height:78vh;">`)
	b.WriteString(`<div><strong>` + html.EscapeString(adminClientInfoChatHistoryHeading(summary)) + `</strong></div>`)
	lastMsgID := int64(0)
	for _, row := range history {
		if row.ID > lastMsgID {
			lastMsgID = row.ID
		}
	}
	b.WriteString(fmt.Sprintf(
		`<div id="client-chat-log" data-participant="%s" data-last-id="%d" style="margin-top:10px;flex:1;min-height:0;overflow-y:scroll;overflow-x:auto;border:1px solid #e1e1e1;padding:10px;background:#efeae2;">`,
		html.EscapeString(selected), lastMsgID,
	))
	if selected == "" {
		b.WriteString(`<p id="client-chat-empty">Select a participant to view chat history.</p>`)
	} else if len(history) == 0 {
		b.WriteString(`<p id="client-chat-empty">No conversation found for this participant.</p>`)
	} else {
		for _, row := range history {
			b.WriteString(adminClientInfoRenderBubbleHTML(row))
		}
	}
	b.WriteString(`</div>`)
	b.WriteString(`<div style="margin-top:10px;border:1px solid #e1e1e1;padding:10px;flex-shrink:0;">`)
	b.WriteString(`<form id="client-chat-send-form" method="post" action="/admin/client-info/send">`)
	b.WriteString(`<input type="hidden" name="phone_filter" value="` + html.EscapeString(search) + `">`)
	b.WriteString(`<input type="hidden" name="baseline_filter" value="` + html.EscapeString(baselineFilter) + `">`)
	b.WriteString(`<input type="hidden" name="verified_filter" value="` + html.EscapeString(verifiedFilter) + `">`)
	b.WriteString(`<input type="hidden" name="intervention_filter" value="` + html.EscapeString(interventionFilter) + `">`)
	b.WriteString(`<input type="hidden" name="participant" value="` + html.EscapeString(selected) + `">`)
	b.WriteString(`<label>Chatbox for sending manual message<br><textarea id="client-chat-message" name="message" rows="3" style="width:100%;" placeholder="Type message"` + disabledWhen(selected == "") + `></textarea></label>`)
	b.WriteString(`<div style="margin-top:8px;display:flex;align-items:center;gap:10px;">`)
	b.WriteString(`<button id="client-chat-send-btn" type="submit"` + disabledWhen(selected == "") + `>Send</button>`)
	b.WriteString(`<span id="client-chat-status" style="font-size:13px;color:#475569;"></span>`)
	b.WriteString(`</div>`)
	b.WriteString(`</form>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	if selected != "" {
		b.WriteString(adminClientInfoLiveChatScript(selected, lastMsgID))
	}
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminClientInfoSendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminClientInfoSendRespond(w, r, false, "Invalid form data.", "", "")
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
	interventionF := strings.TrimSpace(r.FormValue("intervention_filter"))
	if interventionF != "in" && interventionF != "not_in" {
		interventionF = ""
	}
	message := strings.TrimSpace(r.FormValue("message"))
	redirBase := adminClientInfoRedirectQuery(search, baselineF, verifiedF, interventionF, participant, "")
	if participant == "" {
		adminClientInfoSendRespond(w, r, false, "Select a participant first.", search, "")
		return
	}
	if message == "" {
		adminClientInfoSendRespond(w, r, false, "Message cannot be empty.", "", redirBase.Encode())
		return
	}
	if clientInfoSendMessageFunc == nil {
		adminClientInfoSendRespond(w, r, false, "Send handler is not configured.", "", redirBase.Encode())
		return
	}

	// Queue WhatsApp send in background so the admin UI is not blocked.
	go func(phone, text string) {
		if err := clientInfoSendMessageFunc(phone, text); err != nil {
			log.Printf("admin client-info send (phone=%s): %v", phone, err)
		}
	}(participant, message)

	adminClientInfoSendRespond(w, r, true, "Message queued. It will appear in the chat when delivered.", "", redirBase.Encode())
}

func adminClientInfoWantsJSON(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("X-Requested-With"), "XMLHttpRequest") {
		return true
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "application/json")
}

func adminClientInfoSendRespond(w http.ResponseWriter, r *http.Request, ok bool, msg, phoneFilter, redirQuery string) {
	if adminClientInfoWantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		status := http.StatusOK
		if !ok {
			status = http.StatusBadRequest
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      ok,
			"message": msg,
		})
		return
	}
	if redirQuery != "" {
		v, _ := url.ParseQuery(redirQuery)
		v.Set("msg", msg)
		http.Redirect(w, r, "/admin/client-info?"+v.Encode(), http.StatusSeeOther)
		return
	}
	if phoneFilter != "" {
		v := url.Values{}
		v.Set("phone", phoneFilter)
		v.Set("msg", msg)
		http.Redirect(w, r, "/admin/client-info?"+v.Encode(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/client-info?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func adminClientInfoMessagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	participant := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("participant")))
	afterID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("after_id")), 10, 64)
	if afterID < 0 {
		afterID = 0
	}
	if participant == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "participant is required"})
		return
	}
	rows, err := adminLoadClientConversationAfter(participant, afterID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	type msgDTO struct {
		ID        int64  `json:"id"`
		Direction string `json:"direction"`
		Nature    string `json:"nature"`
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
		TimeLabel string `json:"time_label"`
		IsVoice   bool   `json:"is_voice"`
	}
	out := make([]msgDTO, 0, len(rows))
	lastID := afterID
	for _, row := range rows {
		if row.ID > lastID {
			lastID = row.ID
		}
		out = append(out, msgDTO{
			ID:        row.ID,
			Direction: row.Direction,
			Nature:    row.Nature,
			Content:   row.Content,
			CreatedAt: adminFormatTime(row.Time),
			TimeLabel: adminFormatTime(row.Time),
			IsVoice:   strings.EqualFold(strings.TrimSpace(row.Nature), common.MessageNatureVoiceMessage),
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"messages": out,
		"last_id":  lastID,
	})
}

func adminClientInfoOption(value string, selected bool, label string) string {
	sel := ""
	if selected {
		sel = ` selected`
	}
	return `<option value="` + html.EscapeString(value) + `"` + sel + `>` + html.EscapeString(label) + `</option>`
}

// adminClientInfoListQuery builds GET query for participant links (digits-only phone values).
func adminClientInfoListQuery(phoneFilter, baselineFilter, verifiedFilter, interventionFilter, participantPhone string) url.Values {
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
	if interventionFilter != "" {
		v.Set("intervention", interventionFilter)
	}
	if participantPhone != "" {
		v.Set("participant", participantPhone)
	}
	return v
}

func adminClientInfoRedirectQuery(phoneFilter, baselineFilter, verifiedFilter, interventionFilter, participantPhone, msg string) url.Values {
	v := adminClientInfoListQuery(phoneFilter, baselineFilter, verifiedFilter, interventionFilter, participantPhone)
	if strings.TrimSpace(msg) != "" {
		v.Set("msg", strings.TrimSpace(msg))
	}
	return v
}

func adminClientInfoParticipantParen(item adminClientListItem) string {
	if !item.BaselineDone {
		return "(N/A)"
	}
	n := strings.TrimSpace(item.ParticipantName)
	if n == "" {
		return "(N/A)"
	}
	return "(" + n + ")"
}

func adminClientInfoChatHistoryHeading(summary adminClientMetaSummary) string {
	if !summary.BaselineDone {
		return "N/A"
	}
	n := strings.TrimSpace(summary.ParticipantName)
	if n == "" {
		return "N/A"
	}
	return n
}

func adminLoadClientParticipants(phoneFilter, baselineFilter, verifiedFilter, interventionFilter string) ([]adminClientListItem, error) {
	rows, err := db.DB.Query(context.Background(), `
SELECT participant_phone, has_baseline_questionnaire, verification, participant_name, baseline_completed_ts
FROM meta
ORDER BY id DESC
LIMIT 2000`)
	if err != nil {
		return nil, fmt.Errorf("query participants from meta: %w", err)
	}
	defer rows.Close()
	seen := map[string]adminClientListItem{}
	for rows.Next() {
		var encPhone string
		var baselineDone bool
		var verified bool
		var participantName *string
		var baselineAt *time.Time
		if err := rows.Scan(&encPhone, &baselineDone, &verified, &participantName, &baselineAt); err != nil {
			return nil, fmt.Errorf("scan participants from meta: %w", err)
		}
		nameStr := ""
		if participantName != nil {
			nameStr = strings.TrimSpace(*participantName)
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
		baselineTS := time.Time{}
		if baselineAt != nil {
			baselineTS = baselineAt.UTC()
		}
		if existing, ok := seen[phone]; ok {
			existing.BaselineDone = existing.BaselineDone || baselineDone || !baselineTS.IsZero()
			existing.Verified = existing.Verified || verified
			if strings.TrimSpace(existing.ParticipantName) == "" && nameStr != "" {
				existing.ParticipantName = nameStr
			}
			if existing.BaselineAt.IsZero() && !baselineTS.IsZero() {
				existing.BaselineAt = baselineTS
			}
			seen[phone] = existing
			continue
		}
		seen[phone] = adminClientListItem{
			Phone:           phone,
			BaselineDone:    baselineDone || !baselineTS.IsZero(),
			Verified:        verified,
			ParticipantName: nameStr,
			BaselineAt:      baselineTS,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate participants from meta: %w", err)
	}

	lastMsgByPhone, err := adminLoadLastMessageTimesByPhone()
	if err != nil {
		return nil, err
	}

	out := make([]adminClientListItem, 0, len(seen))
	for _, item := range seen {
		if lastAt, ok := lastMsgByPhone[item.Phone]; ok {
			item.LastMessageAt = lastAt
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].LastMessageAt, out[j].LastMessageAt
		if ai.IsZero() && aj.IsZero() {
			return out[i].Phone < out[j].Phone
		}
		if ai.IsZero() {
			return false
		}
		if aj.IsZero() {
			return true
		}
		if ai.Equal(aj) {
			return out[i].Phone < out[j].Phone
		}
		return ai.After(aj)
	})

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
		inIntervention := adminParticipantCurrentlyInIntervention(item.BaselineAt)
		if interventionFilter == "in" && !inIntervention {
			continue
		}
		if interventionFilter == "not_in" && inIntervention {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func adminLoadLastMessageTimesByPhone() (map[string]time.Time, error) {
	rows, err := db.DB.Query(context.Background(), `
SELECT participant_phone, MAX(created_at) AS last_at
FROM conversation
GROUP BY participant_phone`)
	if err != nil {
		return nil, fmt.Errorf("query last message timestamps: %w", err)
	}
	defer rows.Close()

	out := map[string]time.Time{}
	for rows.Next() {
		var encPhone string
		var lastAt time.Time
		if err := rows.Scan(&encPhone, &lastAt); err != nil {
			return nil, fmt.Errorf("scan last message timestamps: %w", err)
		}
		plain, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		phone := common.DigitsOnly(strings.TrimSpace(plain))
		if phone == "" {
			continue
		}
		if existing, ok := out[phone]; !ok || lastAt.After(existing) {
			out[phone] = lastAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate last message timestamps: %w", err)
	}
	return out, nil
}

func adminParticipantCurrentlyInIntervention(baselineAt time.Time) bool {
	if baselineAt.IsZero() {
		return false
	}
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil || cfg.Project.InterventionPeriod <= 0 {
		return false
	}
	end := baselineAt.UTC().AddDate(0, 0, cfg.Project.InterventionPeriod)
	return time.Now().UTC().Before(end)
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
	return adminLoadClientConversationAfter(participantPhone, 0)
}

func adminLoadClientConversationAfter(participantPhone string, afterID int64) ([]adminConversationEntry, error) {
	phone := common.DigitsOnly(strings.TrimSpace(participantPhone))
	if phone == "" {
		return []adminConversationEntry{}, nil
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return nil, fmt.Errorf("encrypt participant phone for conversation query: %w", err)
	}

	var (
		query string
		args  []interface{}
	)
	if afterID > 0 {
		query = `
SELECT id, sender, receiver, direction, nature, content, created_at
FROM conversation
WHERE participant_phone = $1 AND id > $2
ORDER BY id ASC
LIMIT 200`
		args = []interface{}{encryptedPhone, afterID}
	} else {
		query = `
SELECT id, sender, receiver, direction, nature, content, created_at
FROM conversation
WHERE participant_phone = $1
ORDER BY created_at ASC
LIMIT 500`
		args = []interface{}{encryptedPhone}
	}

	rows, err := db.DB.Query(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("query participant conversation: %w", err)
	}
	defer rows.Close()

	out := make([]adminConversationEntry, 0, 64)
	for rows.Next() {
		var id int64
		var encSender, encReceiver, direction, nature, content string
		var createdAt time.Time
		if err := rows.Scan(&id, &encSender, &encReceiver, &direction, &nature, &content, &createdAt); err != nil {
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
			ID:        id,
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

func adminClientInfoRenderBubbleHTML(row adminConversationEntry) string {
	align := "flex-start"
	bg := "#ffffff"
	if strings.EqualFold(strings.TrimSpace(row.Direction), "outbound") {
		align = "flex-end"
		bg = "#d9fdd3"
	}
	messageText := html.EscapeString(row.Content)
	messageText = strings.ReplaceAll(messageText, "\n", "<br>")
	var b strings.Builder
	b.WriteString(fmt.Sprintf(
		`<div class="client-chat-msg" data-msg-id="%d" style="display:flex;justify-content:%s;margin:7px 0;">`,
		row.ID, align,
	))
	b.WriteString(`<div style="max-width:78%;background:` + bg + `;border:1px solid #d6d6d6;border-radius:10px;padding:8px 10px;box-shadow:0 1px 1px rgba(0,0,0,0.06);">`)
	b.WriteString(`<div style="white-space:normal;word-break:break-word;">` + messageText + `</div>`)
	b.WriteString(`<div style="margin-top:4px;font-size:12px;color:#5f6368;text-align:right;">` + adminClientInfoBubbleTimestamp(row) + `</div>`)
	b.WriteString(`</div></div>`)
	return b.String()
}

func adminClientInfoLiveChatScript(participant string, lastID int64) string {
	p := strings.TrimSpace(participant)
	if p == "" {
		return ""
	}
	return fmt.Sprintf(`<script>
(function() {
  var participant = %s;
  var lastId = %d;
  var log = document.getElementById('client-chat-log');
  var form = document.getElementById('client-chat-send-form');
  var textarea = document.getElementById('client-chat-message');
  var statusEl = document.getElementById('client-chat-status');
  var sendBtn = document.getElementById('client-chat-send-btn');
  if (!log || !form || !textarea) return;

  function setStatus(text, isError) {
    if (!statusEl) return;
    statusEl.textContent = text || '';
    statusEl.style.color = isError ? '#b91c1c' : '#475569';
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function appendMessage(msg) {
    if (!msg || !msg.id) return;
    if (log.querySelector('[data-msg-id="' + msg.id + '"]')) return;
    var empty = document.getElementById('client-chat-empty');
    if (empty) empty.remove();
    var outbound = String(msg.direction || '').toLowerCase() === 'outbound';
    var align = outbound ? 'flex-end' : 'flex-start';
    var bg = outbound ? '#d9fdd3' : '#ffffff';
    var content = escapeHtml(msg.content || '').replace(/\n/g, '<br>');
    var timeLabel = escapeHtml(msg.time_label || msg.created_at || '');
    if (msg.is_voice) {
      timeLabel += ' <span style="font-weight:600;">voice</span>';
    }
    var wrap = document.createElement('div');
    wrap.className = 'client-chat-msg';
    wrap.setAttribute('data-msg-id', String(msg.id));
    wrap.style.cssText = 'display:flex;justify-content:' + align + ';margin:7px 0;';
    wrap.innerHTML = '<div style="max-width:78%%;background:' + bg + ';border:1px solid #d6d6d6;border-radius:10px;padding:8px 10px;box-shadow:0 1px 1px rgba(0,0,0,0.06);">' +
      '<div style="white-space:normal;word-break:break-word;">' + content + '</div>' +
      '<div style="margin-top:4px;font-size:12px;color:#5f6368;text-align:right;">' + timeLabel + '</div>' +
      '</div>';
    var nearBottom = (log.scrollHeight - log.scrollTop - log.clientHeight) < 80;
    log.appendChild(wrap);
    if (nearBottom) {
      log.scrollTop = log.scrollHeight;
    }
    if (msg.id > lastId) lastId = msg.id;
    log.setAttribute('data-last-id', String(lastId));
  }

  function pollMessages() {
    if (document.visibilityState === 'hidden') return;
    fetch('/admin/client-info/messages?participant=' + encodeURIComponent(participant) + '&after_id=' + encodeURIComponent(String(lastId)), {
      headers: { 'Accept': 'application/json', 'X-Requested-With': 'XMLHttpRequest' },
      credentials: 'same-origin'
    }).then(function(res) { return res.json(); }).then(function(data) {
      if (!data || !data.ok || !Array.isArray(data.messages)) return;
      data.messages.forEach(appendMessage);
      if (typeof data.last_id === 'number' && data.last_id > lastId) {
        lastId = data.last_id;
        log.setAttribute('data-last-id', String(lastId));
      }
    }).catch(function() { /* ignore transient poll errors */ });
  }

  form.addEventListener('submit', function(ev) {
    ev.preventDefault();
    var text = (textarea.value || '').trim();
    if (!text) {
      setStatus('Message cannot be empty.', true);
      return;
    }
    if (sendBtn) sendBtn.disabled = true;
    setStatus('Sending…', false);
    var body = new URLSearchParams(new FormData(form));
    fetch('/admin/client-info/send', {
      method: 'POST',
      headers: {
        'Accept': 'application/json',
        'X-Requested-With': 'XMLHttpRequest',
        'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8'
      },
      credentials: 'same-origin',
      body: body.toString()
    }).then(function(res) { return res.json().then(function(data) { return { okHttp: res.ok, data: data }; }); })
      .then(function(result) {
        if (!result.data || !result.data.ok) {
          setStatus((result.data && result.data.message) || 'Failed to send message.', true);
          return;
        }
        textarea.value = '';
        setStatus(result.data.message || 'Queued. Waiting for delivery…', false);
        pollMessages();
        setTimeout(pollMessages, 1200);
        setTimeout(pollMessages, 3000);
      }).catch(function() {
        setStatus('Failed to send message.', true);
      }).finally(function() {
        if (sendBtn) sendBtn.disabled = false;
        textarea.focus();
      });
  });

  setInterval(pollMessages, 2500);
  pollMessages();
  log.scrollTop = log.scrollHeight;
})();
</script>`, strconv.Quote(p), lastID)
}

func normalizePhoneDisplay(s string) string {
	digits := common.DigitsOnly(strings.TrimSpace(s))
	if digits != "" {
		return digits
	}
	return strings.TrimSpace(s)
}

func adminClientInfoBubbleTimestamp(row adminConversationEntry) string {
	ts := html.EscapeString(adminFormatTime(row.Time))
	if strings.EqualFold(strings.TrimSpace(row.Nature), common.MessageNatureVoiceMessage) {
		ts += ` <span style="font-weight:600;">voice</span>`
	}
	return ts
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
