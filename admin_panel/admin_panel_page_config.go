package admin_panel

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"whatsapp-bot/db"
	"whatsapp-bot/survey"
)

func adminConfigurationHandler(w http.ResponseWriter, r *http.Request) {
	envVars, err := db.LoadProjectEnvVariables()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonPretty, err := db.GetProjectJSONVariablesPretty()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	statusMsg := strings.TrimSpace(r.URL.Query().Get("msg"))
	if strings.TrimSpace(envVars["ADMIN_PANEL_UTC_OFFSET_HOURS"]) == "" {
		envVars["ADMIN_PANEL_UTC_OFFSET_HOURS"] = "0"
	}
	verificationMsg := ""
	if cfg := survey.GlobalSurveyConfig(); cfg != nil {
		verificationMsg = strings.TrimSpace(cfg.Project.VerificationMessage)
	}
	var b strings.Builder
	b.WriteString(adminPageHeader("Configuration"))
	b.WriteString(`<h2>Configuration</h2>`)
	b.WriteString(adminNav(r))
	if statusMsg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(statusMsg) + `</p>`)
	}

	b.WriteString(`<h3>AI Settings</h3>`)
	b.WriteString(`<form method="post" action="/admin/configuration/update/ai">`)
	b.WriteString(`<p><label>AI_SYSTEM_PROMPT<br><textarea name="AI_SYSTEM_PROMPT" rows="6" cols="120">` + html.EscapeString(envVars["AI_SYSTEM_PROMPT"]) + `</textarea></label></p>`)
	b.WriteString(`<p><label>AI_MEMORY_MESSAGE_LIMIT<br><input type="number" min="1" step="1" inputmode="numeric" name="AI_MEMORY_MESSAGE_LIMIT" value="` + html.EscapeString(envVars["AI_MEMORY_MESSAGE_LIMIT"]) + `" required></label></p>`)
	b.WriteString(`<p><label>OPENROUTER_MODEL<br><input name="OPENROUTER_MODEL" value="` + html.EscapeString(envVars["OPENROUTER_MODEL"]) + `" style="width:100%;max-width:520px;" required></label></p>`)
	b.WriteString(`<p><button type="submit">Save AI settings</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Voice Message Settings</h3>`)
	b.WriteString(`<form method="post" action="/admin/configuration/update/voice-message">`)
	b.WriteString(`<p>VOICE_MESSAGE_ENABLED<br>` + adminBoolRadioGroup("VOICE_MESSAGE_ENABLED", envVars["VOICE_MESSAGE_ENABLED"]) + `</p>`)
	b.WriteString(`<p><label>VOICE_MESSAGE_MODEL (OpenRouter STT model, e.g. openai/whisper-1)<br><input name="VOICE_MESSAGE_MODEL" value="` + html.EscapeString(envVars["VOICE_MESSAGE_MODEL"]) + `" style="width:100%;max-width:520px;" required></label></p>`)
	b.WriteString(`<p><label>VOICE_MESSAGE_TRANSCRIPTION_URL<br><input name="VOICE_MESSAGE_TRANSCRIPTION_URL" value="` + html.EscapeString(envVars["VOICE_MESSAGE_TRANSCRIPTION_URL"]) + `" style="width:100%;max-width:720px;" required></label></p>`)
	b.WriteString(`<p><button type="submit">Save voice message settings</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Behavior Settings</h3>`)
	b.WriteString(`<form method="post" action="/admin/configuration/update/behavior">`)
	b.WriteString(`<p>SEND_AI_ERROR_FALLBACK<br>` + adminBoolRadioGroup("SEND_AI_ERROR_FALLBACK", envVars["SEND_AI_ERROR_FALLBACK"]) + `</p>`)
	b.WriteString(`<p>REQUIRE_VERIFICATION<br>` + adminBoolRadioGroup("REQUIRE_VERIFICATION", envVars["REQUIRE_VERIFICATION"]) + `</p>`)
	b.WriteString(`<p><label>ADMIN_PANEL_UTC_OFFSET_HOURS (display only, -12 to +14)<br><input type="text" inputmode="numeric" name="ADMIN_PANEL_UTC_OFFSET_HOURS" value="` + html.EscapeString(envVars["ADMIN_PANEL_UTC_OFFSET_HOURS"]) + `" placeholder="+8 or -5" required></label></p>`)
	b.WriteString(`<p><label>INBOUND_REPLAY_GRACE_WINDOW_SECONDS<br><input type="number" min="0" step="1" inputmode="numeric" name="INBOUND_REPLAY_GRACE_WINDOW_SECONDS" value="` + html.EscapeString(envVars["INBOUND_REPLAY_GRACE_WINDOW_SECONDS"]) + `" required></label></p>`)
	b.WriteString(`<p><label>SURVEY_PHONE_DIGITS (0 = allow 8-15 digits, otherwise exact length)<br><input type="number" min="0" step="1" inputmode="numeric" name="SURVEY_PHONE_DIGITS" value="` + html.EscapeString(envVars["SURVEY_PHONE_DIGITS"]) + `" required></label></p>`)
	b.WriteString(`<p>COLLECTIVE_RESPONSE<br>` + adminBoolRadioGroup("COLLECTIVE_RESPONSE", envVars["COLLECTIVE_RESPONSE"]) + `</p>`)
	b.WriteString(`<p><label>DELAY_COLLECTIVE_RESPONSE_SECONDS<br><input type="number" min="0" step="1" inputmode="numeric" name="DELAY_COLLECTIVE_RESPONSE_SECONDS" value="` + html.EscapeString(envVars["DELAY_COLLECTIVE_RESPONSE_SECONDS"]) + `" required></label></p>`)
	b.WriteString(`<p>MESSAGE_SLICE_ENABLED<br>` + adminBoolRadioGroup("MESSAGE_SLICE_ENABLED", envVars["MESSAGE_SLICE_ENABLED"]) + `</p>`)
	b.WriteString(`<p><label>MESSAGE_SLICE_DELAY_SECONDS (>=0)<br><input type="number" min="0" step="1" inputmode="numeric" name="MESSAGE_SLICE_DELAY_SECONDS" value="` + html.EscapeString(envVars["MESSAGE_SLICE_DELAY_SECONDS"]) + `" required></label></p>`)
	b.WriteString(`<p><strong>Baseline message interval options</strong><br>`)
	b.WriteString(adminBoolCheckbox("MESSAGE_INTERVAL_ONCE_PER_ONE_WEEK", db.GetProjectSettingBool("MESSAGE_INTERVAL_ONCE_PER_ONE_WEEK", true), "once per one week"))
	b.WriteString(`<br>`)
	b.WriteString(adminBoolCheckbox("MESSAGE_INTERVAL_TWICE_PER_ONE_WEEK", db.GetProjectSettingBool("MESSAGE_INTERVAL_TWICE_PER_ONE_WEEK", true), "twice per one week"))
	b.WriteString(`<br>`)
	b.WriteString(adminBoolCheckbox("MESSAGE_INTERVAL_ONCE_PER_TWO_WEEK", db.GetProjectSettingBool("MESSAGE_INTERVAL_ONCE_PER_TWO_WEEK", true), "once per two week"))
	b.WriteString(`</p>`)
	b.WriteString(`<p style="font-size:13px;color:#64748b;">At least one interval must be selected before saving.</p>`)
	b.WriteString(`<p><button type="submit">Save behavior settings</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>RAG Settings</h3>`)
	b.WriteString(`<form method="post" action="/admin/configuration/update/rag">`)
	b.WriteString(`<p>RAG_ENABLED<br>` + adminBoolRadioGroup("RAG_ENABLED", envVars["RAG_ENABLED"]) + `</p>`)
	b.WriteString(`<p><label>RAG_CHUNK_SIZE<br><input type="number" min="100" step="1" inputmode="numeric" name="RAG_CHUNK_SIZE" value="` + html.EscapeString(envVars["RAG_CHUNK_SIZE"]) + `" required></label></p>`)
	b.WriteString(`<p><label>RAG_CHUNK_OVERLAP<br><input type="number" min="0" step="1" inputmode="numeric" name="RAG_CHUNK_OVERLAP" value="` + html.EscapeString(envVars["RAG_CHUNK_OVERLAP"]) + `" required></label></p>`)
	b.WriteString(`<p><label>RAG_TOP_K<br><input type="number" min="1" step="1" inputmode="numeric" name="RAG_TOP_K" value="` + html.EscapeString(envVars["RAG_TOP_K"]) + `" required></label></p>`)
	b.WriteString(`<p><label>RAG_MIN_SIMILARITY (-1 to 1)<br><input type="text" name="RAG_MIN_SIMILARITY" value="` + html.EscapeString(envVars["RAG_MIN_SIMILARITY"]) + `" required></label></p>`)
	b.WriteString(`<p><label>RAG_EMBEDDING_MODEL<br><input name="RAG_EMBEDDING_MODEL" value="` + html.EscapeString(envVars["RAG_EMBEDDING_MODEL"]) + `" style="width:100%;max-width:520px;" required></label></p>`)
	b.WriteString(`<p><label>RAG_EMBEDDING_URL<br><input name="RAG_EMBEDDING_URL" value="` + html.EscapeString(envVars["RAG_EMBEDDING_URL"]) + `" style="width:100%;max-width:520px;" required></label></p>`)
	b.WriteString(`<p><label>RAG_MAX_CONTEXT_CHARS<br><input type="number" min="200" step="1" inputmode="numeric" name="RAG_MAX_CONTEXT_CHARS" value="` + html.EscapeString(envVars["RAG_MAX_CONTEXT_CHARS"]) + `" required></label></p>`)
	b.WriteString(`<p><label>RAG_SLICE_PROTECT_OPEN_SIGNAL (optional)<br><input name="RAG_SLICE_PROTECT_OPEN_SIGNAL" value="` + html.EscapeString(envVars["RAG_SLICE_PROTECT_OPEN_SIGNAL"]) + `" style="width:100%;max-width:520px;" placeholder="{$open_signal$}"></label></p>`)
	b.WriteString(`<p><label>RAG_SLICE_PROTECT_CLOSE_SIGNAL (optional)<br><input name="RAG_SLICE_PROTECT_CLOSE_SIGNAL" value="` + html.EscapeString(envVars["RAG_SLICE_PROTECT_CLOSE_SIGNAL"]) + `" style="width:100%;max-width:520px;" placeholder="{$close_signal$}"></label></p>`)
	b.WriteString(`<p><button type="submit">Save RAG settings</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Verification Message</h3>`)
	b.WriteString(`<form method="post" action="/admin/configuration/update/verification-message">`)
	b.WriteString(`<p><label>project.verification_message<br><textarea name="verification_message" rows="4" cols="120">` + html.EscapeString(verificationMsg) + `</textarea></label></p>`)
	b.WriteString(`<p><button type="submit">Save verification message</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Cron Throttle Window</h3>`)
	b.WriteString(`<form method="post" action="/admin/configuration/update/cron-delay">`)
	b.WriteString(`<p><label>CRON_SEND_MIN_DELAY_SECONDS<br><input type="number" min="0" step="1" inputmode="numeric" name="CRON_SEND_MIN_DELAY_SECONDS" value="` + html.EscapeString(envVars["CRON_SEND_MIN_DELAY_SECONDS"]) + `" required></label></p>`)
	b.WriteString(`<p><label>CRON_SEND_MAX_DELAY_SECONDS<br><input type="number" min="0" step="1" inputmode="numeric" name="CRON_SEND_MAX_DELAY_SECONDS" value="` + html.EscapeString(envVars["CRON_SEND_MAX_DELAY_SECONDS"]) + `" required></label></p>`)
	b.WriteString(`<p><button type="submit">Save cron delay settings</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Intervention End Message</h3>`)
	b.WriteString(`<form method="post" action="/admin/configuration/update/intervention-message">`)
	b.WriteString(`<p><label>INTERVENTION_END_MESSAGE<br><textarea name="INTERVENTION_END_MESSAGE" rows="4" cols="120">` + html.EscapeString(envVars["INTERVENTION_END_MESSAGE"]) + `</textarea></label></p>`)
	b.WriteString(`<p><button type="submit">Save intervention end message</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Admin Credentials</h3>`)
	b.WriteString(`<form method="post" action="/admin/configuration/update/admin-credentials">`)
	b.WriteString(`<p><label>ADMIN_PANEL_USERNAME<br><input name="ADMIN_PANEL_USERNAME" value="` + html.EscapeString(envVars["ADMIN_PANEL_USERNAME"]) + `"></label></p>`)
	b.WriteString(`<p><label>Old password (required to change password)<br><input type="password" name="old_password"></label></p>`)
	b.WriteString(`<p><label>New password<br><input type="password" name="new_password"></label></p>`)
	b.WriteString(`<p><label>Confirm new password<br><input type="password" name="confirm_new_password"></label></p>`)
	b.WriteString(`<p><button type="submit">Save admin credentials</button></p>`)
	b.WriteString(`</form>`)
	b.WriteString(`<form method="post" action="/admin/configuration/logout-all-admin-sessions" onsubmit="return confirm('Logout all admin/role users from admin panel now? They must login again.');" style="margin-top:10px;">`)
	b.WriteString(`<p><button type="submit" style="background:#b91c1c;border-color:#991b1b;">Logout All Admin Sessions</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>JSON Variables (survey-config object)</h3>`)
	b.WriteString(`<form method="post" action="/admin/configuration/update/json/text">`)
	b.WriteString(`<p><label>Replace JSON by text<br><textarea name="json_payload" rows="22" cols="120">` + html.EscapeString(jsonPretty) + `</textarea></label></p>`)
	b.WriteString(`<p><button type="submit">Replace JSON from text</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<form method="post" action="/admin/configuration/update/json/url">`)
	b.WriteString(`<p><label>Replace JSON by URL (raw GitHub URL supported)<br><input name="json_url" size="120" placeholder="https://raw.githubusercontent.com/..."></label></p>`)
	b.WriteString(`<p><button type="submit">Replace JSON from URL</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<form method="post" action="/admin/configuration/update/json/file" enctype="multipart/form-data">`)
	b.WriteString(`<p><label>Replace JSON by file upload<br><input type="file" name="json_file" accept=".json,application/json"></label></p>`)
	b.WriteString(`<p><button type="submit">Replace JSON from file</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminBoolRadioGroup(name string, currentValue string) string {
	normalized := strings.ToLower(strings.TrimSpace(currentValue))
	trueChecked := ""
	falseChecked := ""
	if normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "on" {
		trueChecked = ` checked`
	} else {
		falseChecked = ` checked`
	}
	return `<label><input type="radio" name="` + html.EscapeString(name) + `" value="true"` + trueChecked + `> true</label> ` +
		`<label><input type="radio" name="` + html.EscapeString(name) + `" value="false"` + falseChecked + `> false</label>`
}

func adminBoolCheckbox(name string, checked bool, label string) string {
	checkedAttr := ""
	if checked {
		checkedAttr = ` checked`
	}
	return `<label><input type="checkbox" name="` + html.EscapeString(name) + `" value="true"` + checkedAttr + `> ` + html.EscapeString(label) + `</label>`
}

func adminConfigurationUpdateAIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	memoryLimit := strings.TrimSpace(r.FormValue("AI_MEMORY_MESSAGE_LIMIT"))
	if _, err := strconv.Atoi(memoryLimit); err != nil {
		adminConfigRedirect(w, r, "AI_MEMORY_MESSAGE_LIMIT must be an integer.")
		return
	}
	openRouterModel := strings.TrimSpace(r.FormValue("OPENROUTER_MODEL"))
	if openRouterModel == "" {
		adminConfigRedirect(w, r, "OPENROUTER_MODEL is required.")
		return
	}
	if err := db.UpdateProjectEnvVariables(map[string]string{
		"AI_SYSTEM_PROMPT":        strings.TrimSpace(r.FormValue("AI_SYSTEM_PROMPT")),
		"AI_MEMORY_MESSAGE_LIMIT": memoryLimit,
		"OPENROUTER_MODEL":        openRouterModel,
	}); err != nil {
		adminConfigRedirect(w, r, "Failed to save AI settings.")
		return
	}
	adminRecordConfigUpdateHistory(r, "update_ai_settings", "Updated AI_SYSTEM_PROMPT, AI_MEMORY_MESSAGE_LIMIT, and OPENROUTER_MODEL")
	adminConfigRedirect(w, r, "AI settings updated.")
}

func adminConfigurationUpdateVoiceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	voiceModel := strings.TrimSpace(r.FormValue("VOICE_MESSAGE_MODEL"))
	if voiceModel == "" {
		adminConfigRedirect(w, r, "VOICE_MESSAGE_MODEL is required.")
		return
	}
	transcriptionURL := strings.TrimSpace(r.FormValue("VOICE_MESSAGE_TRANSCRIPTION_URL"))
	if transcriptionURL == "" {
		adminConfigRedirect(w, r, "VOICE_MESSAGE_TRANSCRIPTION_URL is required.")
		return
	}
	if err := db.UpdateProjectEnvVariables(map[string]string{
		"VOICE_MESSAGE_ENABLED":           strings.TrimSpace(r.FormValue("VOICE_MESSAGE_ENABLED")),
		"VOICE_MESSAGE_MODEL":             voiceModel,
		"VOICE_MESSAGE_TRANSCRIPTION_URL": transcriptionURL,
	}); err != nil {
		adminConfigRedirect(w, r, "Failed to save voice message settings.")
		return
	}
	adminRecordConfigUpdateHistory(r, "update_voice_message_settings", "Updated VOICE_MESSAGE_ENABLED, VOICE_MESSAGE_MODEL, and VOICE_MESSAGE_TRANSCRIPTION_URL")
	adminConfigRedirect(w, r, "Voice message settings updated.")
}

func adminConfigurationUpdateBehaviorHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	replay := strings.TrimSpace(r.FormValue("INBOUND_REPLAY_GRACE_WINDOW_SECONDS"))
	if _, err := strconv.Atoi(replay); err != nil {
		adminConfigRedirect(w, r, "INBOUND_REPLAY_GRACE_WINDOW_SECONDS must be an integer.")
		return
	}
	surveyPhoneDigits := strings.TrimSpace(r.FormValue("SURVEY_PHONE_DIGITS"))
	surveyPhoneDigitsVal, err := strconv.Atoi(surveyPhoneDigits)
	if err != nil || surveyPhoneDigitsVal < 0 {
		adminConfigRedirect(w, r, "SURVEY_PHONE_DIGITS must be a non-negative integer.")
		return
	}
	collectiveDelay := strings.TrimSpace(r.FormValue("DELAY_COLLECTIVE_RESPONSE_SECONDS"))
	collectiveDelayVal, err := strconv.Atoi(collectiveDelay)
	if err != nil || collectiveDelayVal < 0 {
		adminConfigRedirect(w, r, "DELAY_COLLECTIVE_RESPONSE_SECONDS must be a non-negative integer.")
		return
	}
	messageSliceDelaySeconds := strings.TrimSpace(r.FormValue("MESSAGE_SLICE_DELAY_SECONDS"))
	messageSliceDelaySecondsVal, err := strconv.Atoi(messageSliceDelaySeconds)
	if err != nil || messageSliceDelaySecondsVal < 0 {
		adminConfigRedirect(w, r, "MESSAGE_SLICE_DELAY_SECONDS must be a non-negative integer.")
		return
	}
	offsetHours := strings.TrimSpace(r.FormValue("ADMIN_PANEL_UTC_OFFSET_HOURS"))
	offsetVal, err := strconv.Atoi(offsetHours)
	if err != nil || offsetVal < -12 || offsetVal > 14 {
		adminConfigRedirect(w, r, "ADMIN_PANEL_UTC_OFFSET_HOURS must be an integer between -12 and +14.")
		return
	}
	intervalOncePerOneWeek := strings.TrimSpace(r.FormValue("MESSAGE_INTERVAL_ONCE_PER_ONE_WEEK")) == "true"
	intervalTwicePerOneWeek := strings.TrimSpace(r.FormValue("MESSAGE_INTERVAL_TWICE_PER_ONE_WEEK")) == "true"
	intervalOncePerTwoWeek := strings.TrimSpace(r.FormValue("MESSAGE_INTERVAL_ONCE_PER_TWO_WEEK")) == "true"
	if !intervalOncePerOneWeek && !intervalTwicePerOneWeek && !intervalOncePerTwoWeek {
		adminConfigRedirect(w, r, "At least one baseline message interval option must be selected.")
		return
	}
	if err := db.UpdateProjectEnvVariables(map[string]string{
		"SEND_AI_ERROR_FALLBACK":              strings.TrimSpace(r.FormValue("SEND_AI_ERROR_FALLBACK")),
		"REQUIRE_VERIFICATION":                strings.TrimSpace(r.FormValue("REQUIRE_VERIFICATION")),
		"ADMIN_PANEL_UTC_OFFSET_HOURS":        offsetHours,
		"INBOUND_REPLAY_GRACE_WINDOW_SECONDS": replay,
		"SURVEY_PHONE_DIGITS":                 surveyPhoneDigits,
		"COLLECTIVE_RESPONSE":                 strings.TrimSpace(r.FormValue("COLLECTIVE_RESPONSE")),
		"DELAY_COLLECTIVE_RESPONSE_SECONDS":   collectiveDelay,
		"MESSAGE_SLICE_ENABLED":               strings.TrimSpace(r.FormValue("MESSAGE_SLICE_ENABLED")),
		"MESSAGE_SLICE_DELAY_SECONDS":         messageSliceDelaySeconds,
		"MESSAGE_INTERVAL_ONCE_PER_ONE_WEEK":  strconv.FormatBool(intervalOncePerOneWeek),
		"MESSAGE_INTERVAL_TWICE_PER_ONE_WEEK": strconv.FormatBool(intervalTwicePerOneWeek),
		"MESSAGE_INTERVAL_ONCE_PER_TWO_WEEK":  strconv.FormatBool(intervalOncePerTwoWeek),
	}); err != nil {
		adminConfigRedirect(w, r, "Failed to save behavior settings.")
		return
	}
	adminRecordConfigUpdateHistory(r, "update_behavior_settings", "Updated behavior settings in configuration page")
	adminConfigRedirect(w, r, "Behavior settings updated.")
}

func adminConfigurationUpdateRAGHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	chunkSize := strings.TrimSpace(r.FormValue("RAG_CHUNK_SIZE"))
	chunkSizeVal, err := strconv.Atoi(chunkSize)
	if err != nil || chunkSizeVal < 100 {
		adminConfigRedirect(w, r, "RAG_CHUNK_SIZE must be an integer >= 100.")
		return
	}
	chunkOverlap := strings.TrimSpace(r.FormValue("RAG_CHUNK_OVERLAP"))
	chunkOverlapVal, err := strconv.Atoi(chunkOverlap)
	if err != nil || chunkOverlapVal < 0 || chunkOverlapVal >= chunkSizeVal {
		adminConfigRedirect(w, r, "RAG_CHUNK_OVERLAP must be >= 0 and less than RAG_CHUNK_SIZE.")
		return
	}
	topK := strings.TrimSpace(r.FormValue("RAG_TOP_K"))
	topKVal, err := strconv.Atoi(topK)
	if err != nil || topKVal <= 0 {
		adminConfigRedirect(w, r, "RAG_TOP_K must be a positive integer.")
		return
	}
	maxContextChars := strings.TrimSpace(r.FormValue("RAG_MAX_CONTEXT_CHARS"))
	maxContextCharsVal, err := strconv.Atoi(maxContextChars)
	if err != nil || maxContextCharsVal < 200 {
		adminConfigRedirect(w, r, "RAG_MAX_CONTEXT_CHARS must be an integer >= 200.")
		return
	}
	minSimilarity := strings.TrimSpace(r.FormValue("RAG_MIN_SIMILARITY"))
	minSimVal, err := strconv.ParseFloat(minSimilarity, 64)
	if err != nil || minSimVal < -1 || minSimVal > 1 {
		adminConfigRedirect(w, r, "RAG_MIN_SIMILARITY must be a number between -1 and 1.")
		return
	}
	embeddingModel := strings.TrimSpace(r.FormValue("RAG_EMBEDDING_MODEL"))
	embeddingURL := strings.TrimSpace(r.FormValue("RAG_EMBEDDING_URL"))
	openSignal := strings.TrimSpace(r.FormValue("RAG_SLICE_PROTECT_OPEN_SIGNAL"))
	closeSignal := strings.TrimSpace(r.FormValue("RAG_SLICE_PROTECT_CLOSE_SIGNAL"))
	if embeddingModel == "" || embeddingURL == "" {
		adminConfigRedirect(w, r, "RAG_EMBEDDING_MODEL and RAG_EMBEDDING_URL are required.")
		return
	}
	if (openSignal == "") != (closeSignal == "") {
		adminConfigRedirect(w, r, "RAG slice protect signals must be both empty or both set.")
		return
	}
	if openSignal != "" && openSignal == closeSignal {
		adminConfigRedirect(w, r, "RAG open and close signals must be different.")
		return
	}
	if err := db.UpdateProjectEnvVariables(map[string]string{
		"RAG_ENABLED":                    strings.TrimSpace(r.FormValue("RAG_ENABLED")),
		"RAG_CHUNK_SIZE":                 chunkSize,
		"RAG_CHUNK_OVERLAP":              chunkOverlap,
		"RAG_TOP_K":                      topK,
		"RAG_MIN_SIMILARITY":             minSimilarity,
		"RAG_EMBEDDING_MODEL":            embeddingModel,
		"RAG_EMBEDDING_URL":              embeddingURL,
		"RAG_MAX_CONTEXT_CHARS":          maxContextChars,
		"RAG_SLICE_PROTECT_OPEN_SIGNAL":  openSignal,
		"RAG_SLICE_PROTECT_CLOSE_SIGNAL": closeSignal,
	}); err != nil {
		adminConfigRedirect(w, r, "Failed to save RAG settings.")
		return
	}
	adminRecordConfigUpdateHistory(r, "update_rag_settings", "Updated RAG settings in configuration page")
	adminConfigRedirect(w, r, "RAG settings updated.")
}

func adminConfigurationUpdateVerificationMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	value := strings.TrimSpace(r.FormValue("verification_message"))
	raw, err := db.GetProjectJSONVariablesRaw()
	if err != nil {
		adminConfigRedirect(w, r, "Failed to load existing JSON variables.")
		return
	}
	root := map[string]interface{}{}
	if err := json.Unmarshal(raw, &root); err != nil {
		adminConfigRedirect(w, r, "Stored JSON variables are invalid.")
		return
	}
	projectObj, _ := root["project"].(map[string]interface{})
	if projectObj == nil {
		projectObj = map[string]interface{}{}
	}
	projectObj["verification_message"] = value
	root["project"] = projectObj
	updated, err := json.Marshal(root)
	if err != nil {
		adminConfigRedirect(w, r, "Failed to serialize JSON variables.")
		return
	}
	if err := db.UpdateProjectJSONVariables(updated); err != nil {
		adminConfigRedirect(w, r, "Failed to save verification message.")
		return
	}
	if err := survey.ReloadSurveyInfrastructureFromProjectSetting(); err != nil {
		adminConfigRedirect(w, r, "Verification message saved, but survey reload failed: "+err.Error())
		return
	}
	adminRecordConfigUpdateHistory(r, "update_verification_message", "Updated project.verification_message")
	adminConfigRedirect(w, r, "Verification message updated.")
}

func adminConfigurationUpdateCronDelayHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	minDelay := strings.TrimSpace(r.FormValue("CRON_SEND_MIN_DELAY_SECONDS"))
	maxDelay := strings.TrimSpace(r.FormValue("CRON_SEND_MAX_DELAY_SECONDS"))
	minV, err := strconv.Atoi(minDelay)
	if err != nil || minV < 0 {
		adminConfigRedirect(w, r, "CRON_SEND_MIN_DELAY_SECONDS must be a non-negative integer.")
		return
	}
	maxV, err := strconv.Atoi(maxDelay)
	if err != nil || maxV < 0 {
		adminConfigRedirect(w, r, "CRON_SEND_MAX_DELAY_SECONDS must be a non-negative integer.")
		return
	}
	if maxV < minV {
		adminConfigRedirect(w, r, "CRON_SEND_MAX_DELAY_SECONDS must be >= CRON_SEND_MIN_DELAY_SECONDS.")
		return
	}
	if err := db.UpdateProjectEnvVariables(map[string]string{
		"CRON_SEND_MIN_DELAY_SECONDS": minDelay,
		"CRON_SEND_MAX_DELAY_SECONDS": maxDelay,
	}); err != nil {
		adminConfigRedirect(w, r, "Failed to save cron delay settings.")
		return
	}
	adminRecordConfigUpdateHistory(r, "update_cron_delay", "Updated CRON_SEND_MIN_DELAY_SECONDS and CRON_SEND_MAX_DELAY_SECONDS")
	adminConfigRedirect(w, r, "Cron delay settings updated.")
}

func adminConfigurationUpdateInterventionMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	if err := db.UpdateProjectEnvVariables(map[string]string{
		"INTERVENTION_END_MESSAGE": strings.TrimSpace(r.FormValue("INTERVENTION_END_MESSAGE")),
	}); err != nil {
		adminConfigRedirect(w, r, "Failed to save intervention end message.")
		return
	}
	adminRecordConfigUpdateHistory(r, "update_intervention_end_message", "Updated INTERVENTION_END_MESSAGE")
	adminConfigRedirect(w, r, "Intervention end message updated.")
}

func adminConfigurationUpdateAdminCredentialsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	newUsername := strings.TrimSpace(r.FormValue("ADMIN_PANEL_USERNAME"))
	oldPassword := strings.TrimSpace(r.FormValue("old_password"))
	newPassword := strings.TrimSpace(r.FormValue("new_password"))
	confirm := strings.TrimSpace(r.FormValue("confirm_new_password"))

	currentUsername := db.GetProjectSettingString("ADMIN_PANEL_USERNAME", "")
	if newPassword != "" || confirm != "" || oldPassword != "" {
		if newPassword == "" || confirm == "" {
			adminConfigRedirect(w, r, "Both new password fields are required.")
			return
		}
		if newPassword != confirm {
			adminConfigRedirect(w, r, "New password and confirmation do not match.")
			return
		}
		if currentUsername == "" {
			currentUsername = strings.TrimSpace(newUsername)
		}
		ok, err := db.VerifyAdminCredentials(currentUsername, oldPassword)
		if err != nil || !ok {
			adminConfigRedirect(w, r, "Old password is incorrect.")
			return
		}
		if err := db.UpdateAdminPassword(newPassword); err != nil {
			adminConfigRedirect(w, r, "Failed to update admin password.")
			return
		}
		if newUsername != "" && newUsername != currentUsername {
			if err := db.UpdateAdminUsername(newUsername); err != nil {
				adminConfigRedirect(w, r, "Password updated, but failed to update username.")
				return
			}
		}
		adminRecordConfigUpdateHistory(r, "update_admin_credentials", "Updated admin password and/or username")
		adminDestroySession(r)
		adminExpireSessionCookie(w, r)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	if newUsername != "" && newUsername != currentUsername {
		if err := db.UpdateAdminUsername(newUsername); err != nil {
			adminConfigRedirect(w, r, "Failed to update admin username.")
			return
		}
		adminRecordConfigUpdateHistory(r, "update_admin_credentials", "Updated admin username")
	} else {
		adminRecordConfigUpdateHistory(r, "update_admin_credentials", "Submitted admin credentials update")
	}
	adminConfigRedirect(w, r, "Admin credentials updated.")
}

func adminConfigurationLogoutAllAdminSessionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	adminDestroyAllSessions()
	adminExpireSessionCookie(w, r)
	adminRecordConfigUpdateHistory(r, "logout_all_admin_sessions", "Logged out all admin and role user sessions")
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func adminConfigurationUpdateJSONTextHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	jsonPayload := strings.TrimSpace(r.FormValue("json_payload"))
	if jsonPayload == "" {
		adminConfigRedirect(w, r, "JSON payload is empty.")
		return
	}
	if _, err := survey.ParseSurveyConfigBytes([]byte(jsonPayload)); err != nil {
		adminConfigRedirect(w, r, "Invalid JSON payload: "+err.Error())
		return
	}
	if err := db.UpdateProjectJSONVariables([]byte(jsonPayload)); err != nil {
		adminConfigRedirect(w, r, "Failed to update JSON variables.")
		return
	}
	if err := survey.ReloadSurveyInfrastructureFromProjectSetting(); err != nil {
		adminConfigRedirect(w, r, "JSON saved, but survey reload failed: "+err.Error())
		return
	}
	adminRecordConfigUpdateHistory(r, "update_json_text", "Replaced JSON variables from text")
	adminConfigRedirect(w, r, "JSON variables updated from text.")
}

func adminConfigurationUpdateJSONURLHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminConfigRedirect(w, r, "Invalid form data.")
		return
	}
	url := strings.TrimSpace(r.FormValue("json_url"))
	if url == "" {
		adminConfigRedirect(w, r, "JSON URL is empty.")
		return
	}
	raw, err := db.FetchJSONFromURL(url)
	if err != nil {
		adminConfigRedirect(w, r, "Failed to update JSON from URL.")
		return
	}
	if _, err := survey.ParseSurveyConfigBytes(raw); err != nil {
		adminConfigRedirect(w, r, "Invalid JSON from URL: "+err.Error())
		return
	}
	if err := db.UpdateProjectJSONVariables(raw); err != nil {
		adminConfigRedirect(w, r, "Failed to persist JSON from URL.")
		return
	}
	if err := survey.ReloadSurveyInfrastructureFromProjectSetting(); err != nil {
		adminConfigRedirect(w, r, "JSON saved, but survey reload failed: "+err.Error())
		return
	}
	adminRecordConfigUpdateHistory(r, "update_json_url", "Replaced JSON variables from URL")
	adminConfigRedirect(w, r, "JSON variables updated from URL.")
}

func adminConfigurationUpdateJSONFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		adminConfigRedirect(w, r, "Invalid multipart form.")
		return
	}
	file, _, err := r.FormFile("json_file")
	if err != nil {
		adminConfigRedirect(w, r, "JSON file is required.")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil {
		adminConfigRedirect(w, r, "Failed to read uploaded JSON file.")
		return
	}
	if _, err := survey.ParseSurveyConfigBytes(raw); err != nil {
		adminConfigRedirect(w, r, "Invalid uploaded JSON: "+err.Error())
		return
	}
	if err := db.UpdateProjectJSONVariables(raw); err != nil {
		adminConfigRedirect(w, r, "Failed to update JSON from uploaded file.")
		return
	}
	if err := survey.ReloadSurveyInfrastructureFromProjectSetting(); err != nil {
		adminConfigRedirect(w, r, "JSON saved, but survey reload failed: "+err.Error())
		return
	}
	adminRecordConfigUpdateHistory(r, "update_json_file", "Replaced JSON variables from uploaded file")
	adminConfigRedirect(w, r, "JSON variables updated from file upload.")
}

func adminConfigRedirect(w http.ResponseWriter, r *http.Request, msg string) {
	target := "/admin/configuration"
	if strings.TrimSpace(msg) != "" {
		target = fmt.Sprintf("/admin/configuration?msg=%s", urlQueryEscape(msg))
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func urlQueryEscape(v string) string {
	return url.QueryEscape(v)
}

func adminRecordConfigUpdateHistory(r *http.Request, action string, description string) {
	actor := "unknown"
	if session, ok := adminSessionFromRequest(r); ok {
		if username := strings.TrimSpace(session.Username); username != "" {
			actor = username
		}
	}
	if err := db.InsertConfigUpdateHistory(actor, action, description); err != nil {
		log.Printf("config update history insert error: %v", err)
	}
}
