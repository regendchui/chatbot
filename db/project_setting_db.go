package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	projectSettingSingletonID         = 1
	defaultAIMemoryMessageLimit       = 20
	defaultOpenRouterModel            = "google/gemini-2.5-flash"
	defaultAdminPanelUsername         = "admin"
	defaultAdminPanelPassword         = "admin123"
	defaultSendAIErrorFallback        = false
	defaultInboundReplayWindowSecs    = 10
	defaultCronSendMinDelaySecs       = 30
	defaultCronSendMaxDelaySecs       = 45
	defaultInterventionEndMessageVal  = "As the service period is over, it is time to say goodbye. Thank you for using our service."
	defaultRequireVerification        = false
	defaultAdminPanelUTCOffsetHours   = 0
	defaultSurveyPhoneDigits          = 0
	defaultRAGEnabled                 = false
	defaultRAGChunkSize               = 800
	defaultRAGChunkOverlap            = 100
	defaultRAGTopK                    = 3
	defaultRAGMinSimilarity           = "0.2"
	defaultRAGEmbeddingModel          = "openai/text-embedding-3-small"
	defaultRAGEmbeddingURL            = "https://openrouter.ai/api/v1/embeddings"
	defaultRAGMaxContextChars         = 2500
	defaultRAGSliceProtectOpenSignal  = ""
	defaultRAGSliceProtectCloseSignal = ""
	defaultCollectiveResponse         = false
	defaultCollectiveResponseDelaySec = 3
	defaultMessageSliceEnabled        = false
	defaultMessageSliceDelaySeconds   = 1
	defaultIntervalOncePerOneWeek     = true
	defaultIntervalTwicePerOneWeek    = true
	defaultIntervalOncePerTwoWeek     = true
	defaultVoiceMessageEnabled        = false
	defaultVoiceMessageModel          = "openai/whisper-1"
	defaultVoiceMessageTranscriptionURL   = "https://openrouter.ai/api/v1/audio/transcriptions"
	defaultVoiceMessageUnintelligibleReply = "I couldn't hear anything in that voice note."
	defaultRiskyPhrases                   = ""
	defaultThankYouMessage                = "Thank you for your response"
)

type projectSettingRow struct {
	EnvVariables  map[string]string
	JSONVariables json.RawMessage
}

// EnsureProjectSettingTableExists creates project_setting table if needed.
func EnsureProjectSettingTableExists() error {
	query := `
CREATE TABLE IF NOT EXISTS project_setting (
    id SMALLINT PRIMARY KEY,
    env_variables JSONB NOT NULL DEFAULT '{}'::jsonb,
    json_variables JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	if _, err := DB.Exec(context.Background(), query); err != nil {
		return fmt.Errorf("create project_setting table: %w", err)
	}
	return nil
}

// EnsureProjectSettingsInitialized bootstraps singleton row from env + survey-config.json once.
func EnsureProjectSettingsInitialized() error {
	if err := EnsureProjectSettingTableExists(); err != nil {
		return err
	}
	exists, err := projectSettingExists()
	if err != nil {
		return err
	}
	if exists {
		return ensureProjectSettingDefaultsForExistingRow()
	}
	defaultEnv, err := defaultProjectEnvVariables()
	if err != nil {
		return err
	}
	defaultJSON, err := loadDefaultSurveyConfigJSON()
	if err != nil {
		return err
	}
	query := `
INSERT INTO project_setting (id, env_variables, json_variables)
VALUES ($1, $2, $3)`
	if _, err := DB.Exec(context.Background(), query, projectSettingSingletonID, defaultEnv, defaultJSON); err != nil {
		return fmt.Errorf("insert default project_setting row: %w", err)
	}
	return nil
}

func ensureProjectSettingDefaultsForExistingRow() error {
	row, err := loadProjectSettingRow()
	if err != nil {
		return err
	}
	defaultEnv, err := defaultProjectEnvVariables()
	if err != nil {
		return err
	}
	if row.EnvVariables == nil {
		row.EnvVariables = map[string]string{}
	}
	changed := false
	for k, v := range defaultEnv {
		if strings.TrimSpace(row.EnvVariables[k]) == "" {
			row.EnvVariables[k] = strings.TrimSpace(v)
			changed = true
		}
	}
	defaultJSON, err := loadDefaultSurveyConfigJSON()
	if err != nil {
		return err
	}
	mergedJSON, jsonChanged, err := mergeMissingProjectJSONDefaults(row.JSONVariables, defaultJSON)
	if err != nil {
		return err
	}
	if jsonChanged {
		row.JSONVariables = mergedJSON
		changed = true
	}
	if changed {
		return saveProjectSettingRow(row)
	}
	return nil
}

func mergeMissingProjectJSONDefaults(currentRaw json.RawMessage, defaultRaw json.RawMessage) (json.RawMessage, bool, error) {
	current := map[string]interface{}{}
	if len(currentRaw) > 0 {
		if err := json.Unmarshal(currentRaw, &current); err != nil {
			return nil, false, fmt.Errorf("parse current json_variables: %w", err)
		}
	}
	defaultObj := map[string]interface{}{}
	if len(defaultRaw) > 0 {
		if err := json.Unmarshal(defaultRaw, &defaultObj); err != nil {
			return nil, false, fmt.Errorf("parse default json_variables: %w", err)
		}
	}
	changed := false

	// Ensure top-level survey keys always exist in stored JSON.
	for _, key := range []string{"project", "baseline", "followups"} {
		currentVal, exists := current[key]
		if !exists || isNilLikeJSONValue(currentVal) {
			if defaultVal, ok := defaultObj[key]; ok && !isNilLikeJSONValue(defaultVal) {
				current[key] = defaultVal
				changed = true
			}
		}
	}

	// Ensure project.verification_message exists when default JSON defines it.
	currentProject, _ := current["project"].(map[string]interface{})
	if currentProject == nil {
		currentProject = map[string]interface{}{}
	}
	defaultProject, _ := defaultObj["project"].(map[string]interface{})
	if defaultProject == nil {
		defaultProject = map[string]interface{}{}
	}
	currentVerification := jsonStringValue(currentProject["verification_message"])
	if currentVerification == "" {
		defaultVerification := jsonStringValue(defaultProject["verification_message"])
		if defaultVerification != "" {
			currentProject["verification_message"] = defaultVerification
			current["project"] = currentProject
			changed = true
		}
	}

	if !changed {
		if len(currentRaw) == 0 {
			return defaultRaw, true, nil
		}
		return currentRaw, false, nil
	}
	updated, err := json.Marshal(current)
	if err != nil {
		return nil, false, fmt.Errorf("marshal merged json_variables: %w", err)
	}
	normalized, err := normalizeJSONObject(updated)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

func jsonStringValue(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		trimmed := strings.TrimSpace(x)
		if strings.EqualFold(trimmed, "<nil>") || strings.EqualFold(trimmed, "null") {
			return ""
		}
		return trimmed
	default:
		trimmed := strings.TrimSpace(fmt.Sprintf("%v", x))
		if strings.EqualFold(trimmed, "<nil>") || strings.EqualFold(trimmed, "null") {
			return ""
		}
		return trimmed
	}
}

func isNilLikeJSONValue(v interface{}) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		t := strings.TrimSpace(x)
		return t == "" || strings.EqualFold(t, "<nil>") || strings.EqualFold(t, "null")
	default:
		return false
	}
}

func projectSettingExists() (bool, error) {
	var count int
	if err := DB.QueryRow(context.Background(), `SELECT COUNT(1) FROM project_setting WHERE id = $1`, projectSettingSingletonID).Scan(&count); err != nil {
		return false, fmt.Errorf("check project_setting row exists: %w", err)
	}
	return count > 0, nil
}

func loadDefaultSurveyConfigJSON() (json.RawMessage, error) {
	path := strings.TrimSpace(os.Getenv("SURVEY_CONFIG_PATH"))
	if path == "" {
		path = "survey-config.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read default survey config file %s: %w", path, err)
	}
	return normalizeJSONObject(raw)
}

func defaultProjectEnvVariables() (map[string]string, error) {
	requireVerification := strings.TrimSpace(os.Getenv("REQUIRE_VERIFICATION"))
	if requireVerification == "" {
		requireVerification = strings.TrimSpace(os.Getenv("REQUIRE_Verification"))
	}
	envVars := map[string]string{
		"AI_SYSTEM_PROMPT":                    strings.TrimSpace(os.Getenv("AI_SYSTEM_PROMPT")),
		"AI_MEMORY_MESSAGE_LIMIT":             nonEmptyOrDefault(strings.TrimSpace(os.Getenv("AI_MEMORY_MESSAGE_LIMIT")), strconv.Itoa(defaultAIMemoryMessageLimit)),
		"OPENROUTER_MODEL":                    nonEmptyOrDefault(strings.TrimSpace(os.Getenv("OPENROUTER_MODEL")), defaultOpenRouterModel),
		"ADMIN_PANEL_USERNAME":                nonEmptyOrDefault(strings.TrimSpace(os.Getenv("ADMIN_PANEL_USERNAME")), defaultAdminPanelUsername),
		"SEND_AI_ERROR_FALLBACK":              nonEmptyOrDefault(strings.TrimSpace(os.Getenv("SEND_AI_ERROR_FALLBACK")), boolString(defaultSendAIErrorFallback)),
		"INBOUND_REPLAY_GRACE_WINDOW_SECONDS": nonEmptyOrDefault(strings.TrimSpace(os.Getenv("INBOUND_REPLAY_GRACE_WINDOW_SECONDS")), strconv.Itoa(defaultInboundReplayWindowSecs)),
		"CRON_SEND_MIN_DELAY_SECONDS":         nonEmptyOrDefault(strings.TrimSpace(os.Getenv("CRON_SEND_MIN_DELAY_SECONDS")), strconv.Itoa(defaultCronSendMinDelaySecs)),
		"CRON_SEND_MAX_DELAY_SECONDS":         nonEmptyOrDefault(strings.TrimSpace(os.Getenv("CRON_SEND_MAX_DELAY_SECONDS")), strconv.Itoa(defaultCronSendMaxDelaySecs)),
		"INTERVENTION_END_MESSAGE":            nonEmptyOrDefault(strings.TrimSpace(os.Getenv("INTERVENTION_END_MESSAGE")), defaultInterventionEndMessageVal),
		"REQUIRE_VERIFICATION":                nonEmptyOrDefault(requireVerification, boolString(defaultRequireVerification)),
		"ADMIN_PANEL_UTC_OFFSET_HOURS":        nonEmptyOrDefault(strings.TrimSpace(os.Getenv("ADMIN_PANEL_UTC_OFFSET_HOURS")), strconv.Itoa(defaultAdminPanelUTCOffsetHours)),
		"SURVEY_PHONE_DIGITS":                 nonEmptyOrDefault(strings.TrimSpace(os.Getenv("SURVEY_PHONE_DIGITS")), strconv.Itoa(defaultSurveyPhoneDigits)),
		"RAG_ENABLED":                         nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_ENABLED")), boolString(defaultRAGEnabled)),
		"RAG_CHUNK_SIZE":                      nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_CHUNK_SIZE")), strconv.Itoa(defaultRAGChunkSize)),
		"RAG_CHUNK_OVERLAP":                   nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_CHUNK_OVERLAP")), strconv.Itoa(defaultRAGChunkOverlap)),
		"RAG_TOP_K":                           nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_TOP_K")), strconv.Itoa(defaultRAGTopK)),
		"RAG_MIN_SIMILARITY":                  nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_MIN_SIMILARITY")), defaultRAGMinSimilarity),
		"RAG_EMBEDDING_MODEL":                 nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_EMBEDDING_MODEL")), defaultRAGEmbeddingModel),
		"RAG_EMBEDDING_URL":                   nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_EMBEDDING_URL")), defaultRAGEmbeddingURL),
		"RAG_MAX_CONTEXT_CHARS":               nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_MAX_CONTEXT_CHARS")), strconv.Itoa(defaultRAGMaxContextChars)),
		"RAG_SLICE_PROTECT_OPEN_SIGNAL":       nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_SLICE_PROTECT_OPEN_SIGNAL")), defaultRAGSliceProtectOpenSignal),
		"RAG_SLICE_PROTECT_CLOSE_SIGNAL":      nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RAG_SLICE_PROTECT_CLOSE_SIGNAL")), defaultRAGSliceProtectCloseSignal),
		"COLLECTIVE_RESPONSE":                 nonEmptyOrDefault(strings.TrimSpace(os.Getenv("COLLECTIVE_RESPONSE")), boolString(defaultCollectiveResponse)),
		"DELAY_COLLECTIVE_RESPONSE_SECONDS":   nonEmptyOrDefault(strings.TrimSpace(os.Getenv("DELAY_COLLECTIVE_RESPONSE_SECONDS")), strconv.Itoa(defaultCollectiveResponseDelaySec)),
		"MESSAGE_SLICE_ENABLED":               nonEmptyOrDefault(strings.TrimSpace(os.Getenv("MESSAGE_SLICE_ENABLED")), boolString(defaultMessageSliceEnabled)),
		"MESSAGE_SLICE_DELAY_SECONDS":         nonEmptyOrDefault(strings.TrimSpace(os.Getenv("MESSAGE_SLICE_DELAY_SECONDS")), strconv.Itoa(defaultMessageSliceDelaySeconds)),
		"MESSAGE_INTERVAL_ONCE_PER_ONE_WEEK":  nonEmptyOrDefault(strings.TrimSpace(os.Getenv("MESSAGE_INTERVAL_ONCE_PER_ONE_WEEK")), boolString(defaultIntervalOncePerOneWeek)),
		"MESSAGE_INTERVAL_TWICE_PER_ONE_WEEK": nonEmptyOrDefault(strings.TrimSpace(os.Getenv("MESSAGE_INTERVAL_TWICE_PER_ONE_WEEK")), boolString(defaultIntervalTwicePerOneWeek)),
		"MESSAGE_INTERVAL_ONCE_PER_TWO_WEEK":  nonEmptyOrDefault(strings.TrimSpace(os.Getenv("MESSAGE_INTERVAL_ONCE_PER_TWO_WEEK")), boolString(defaultIntervalOncePerTwoWeek)),
		"VOICE_MESSAGE_ENABLED":               nonEmptyOrDefault(strings.TrimSpace(os.Getenv("VOICE_MESSAGE_ENABLED")), boolString(defaultVoiceMessageEnabled)),
		"VOICE_MESSAGE_MODEL":                 nonEmptyOrDefault(strings.TrimSpace(os.Getenv("VOICE_MESSAGE_MODEL")), defaultVoiceMessageModel),
		"VOICE_MESSAGE_TRANSCRIPTION_URL":      nonEmptyOrDefault(strings.TrimSpace(os.Getenv("VOICE_MESSAGE_TRANSCRIPTION_URL")), defaultVoiceMessageTranscriptionURL),
		"VOICE_MESSAGE_UNINTELLIGIBLE_REPLY": nonEmptyOrDefault(strings.TrimSpace(os.Getenv("VOICE_MESSAGE_UNINTELLIGIBLE_REPLY")), defaultVoiceMessageUnintelligibleReply),
		"RISKY_PHRASES":                      nonEmptyOrDefault(strings.TrimSpace(os.Getenv("RISKY_PHRASES")), defaultRiskyPhrases),
		"THANKYOU_MESSAGE":                   nonEmptyOrDefault(strings.TrimSpace(os.Getenv("THANKYOU_MESSAGE")), defaultThankYouMessage),
	}
	defaultPassword := nonEmptyOrDefault(strings.TrimSpace(os.Getenv("ADMIN_PANEL_PASSWORD")), defaultAdminPanelPassword)
	encryptedPassword, err := encryptAdminPanelPassword(defaultPassword)
	if err != nil {
		return nil, err
	}
	envVars["ADMIN_PANEL_PASSWORD"] = encryptedPassword
	return envVars, nil
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func nonEmptyOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func normalizeJSONObject(raw []byte) (json.RawMessage, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parse json object: %w", err)
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized json object: %w", err)
	}
	return normalized, nil
}

func loadProjectSettingRow() (*projectSettingRow, error) {
	if err := EnsureProjectSettingTableExists(); err != nil {
		return nil, err
	}
	var envRaw []byte
	var jsonRaw []byte
	err := DB.QueryRow(
		context.Background(),
		`SELECT env_variables::text, json_variables::text FROM project_setting WHERE id = $1`,
		projectSettingSingletonID,
	).Scan(&envRaw, &jsonRaw)
	if err != nil {
		return nil, fmt.Errorf("load project_setting row: %w", err)
	}
	envVars := map[string]string{}
	if len(envRaw) > 0 {
		if err := json.Unmarshal(envRaw, &envVars); err != nil {
			return nil, fmt.Errorf("parse project_setting env_variables: %w", err)
		}
	}
	if len(jsonRaw) == 0 {
		jsonRaw = []byte("{}")
	}
	return &projectSettingRow{
		EnvVariables:  envVars,
		JSONVariables: json.RawMessage(jsonRaw),
	}, nil
}

func saveProjectSettingRow(row *projectSettingRow) error {
	if row == nil {
		return fmt.Errorf("project_setting row is nil")
	}
	envVars := row.EnvVariables
	if envVars == nil {
		envVars = map[string]string{}
	}
	jsonVars := row.JSONVariables
	if len(jsonVars) == 0 {
		jsonVars = json.RawMessage("{}")
	}
	query := `
INSERT INTO project_setting (id, env_variables, json_variables, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE SET
    env_variables = EXCLUDED.env_variables,
    json_variables = EXCLUDED.json_variables,
    updated_at = EXCLUDED.updated_at`
	if _, err := DB.Exec(context.Background(), query, projectSettingSingletonID, envVars, jsonVars, time.Now().UTC()); err != nil {
		return fmt.Errorf("save project_setting row: %w", err)
	}
	return nil
}

// LoadProjectEnvVariables returns managed env settings from db.
func LoadProjectEnvVariables() (map[string]string, error) {
	row, err := loadProjectSettingRow()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range row.EnvVariables {
		out[k] = strings.TrimSpace(v)
	}
	return out, nil
}

// GetProjectSettingString returns one managed setting value with fallback.
func GetProjectSettingString(key string, fallback string) string {
	vars, err := LoadProjectEnvVariables()
	if err != nil {
		return fallback
	}
	v := strings.TrimSpace(vars[strings.TrimSpace(key)])
	if v == "" {
		return fallback
	}
	return v
}

// GetProjectSettingInt returns one managed setting parsed as int.
func GetProjectSettingInt(key string, fallback int) int {
	raw := strings.TrimSpace(GetProjectSettingString(key, ""))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

// GetProjectSettingBool returns one managed setting parsed as bool-like text.
func GetProjectSettingBool(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(GetProjectSettingString(key, "")))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

// GetAdminPanelOffsetHoursForDisplay parses ADMIN_PANEL_UTC_OFFSET_HOURS (-12..+14, default 0 on invalid).
// Used for admin UI timestamps and for “today” boundaries that match the admin panel offset.
func GetAdminPanelOffsetHoursForDisplay() int {
	raw := strings.TrimSpace(GetProjectSettingString("ADMIN_PANEL_UTC_OFFSET_HOURS", "0"))
	if raw == "" {
		raw = "0"
	}
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	normalized = strings.TrimPrefix(normalized, "UTC")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		normalized = "0"
	}
	offsetHours, err := strconv.Atoi(normalized)
	if err != nil || offsetHours < -12 || offsetHours > 14 {
		return 0
	}
	return offsetHours
}

// AdminPanelDisplayLocation returns a fixed-offset zone derived from ADMIN_PANEL_UTC_OFFSET_HOURS.
func AdminPanelDisplayLocation() *time.Location {
	h := GetAdminPanelOffsetHoursForDisplay()
	return time.FixedZone(fmt.Sprintf("UTC%+d", h), h*3600)
}

// AdminLocalDayRangeUTC returns [startUTC, endUTC) for the civil calendar day of t in the admin panel offset zone.
func AdminLocalDayRangeUTC(t time.Time) (time.Time, time.Time) {
	loc := AdminPanelDisplayLocation()
	n := t.In(loc)
	startLocal := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	endLocal := startLocal.AddDate(0, 0, 1)
	return startLocal.UTC(), endLocal.UTC()
}

// GetProjectJSONVariablesRaw returns stored JSON object (survey-config mirror).
func GetProjectJSONVariablesRaw() ([]byte, error) {
	row, err := loadProjectSettingRow()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), row.JSONVariables...), nil
}

// GetProjectJSONVariablesPretty returns indented JSON for admin rendering.
func GetProjectJSONVariablesPretty() (string, error) {
	raw, err := GetProjectJSONVariablesRaw()
	if err != nil {
		return "", err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("parse stored json_variables: %w", err)
	}
	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal pretty json_variables: %w", err)
	}
	return string(pretty), nil
}

// UpdateProjectEnvVariables merges provided keys into env_variables.
func UpdateProjectEnvVariables(updates map[string]string) error {
	row, err := loadProjectSettingRow()
	if err != nil {
		return err
	}
	if row.EnvVariables == nil {
		row.EnvVariables = map[string]string{}
	}
	for k, v := range updates {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		row.EnvVariables[key] = strings.TrimSpace(v)
	}
	return saveProjectSettingRow(row)
}

// UpdateProjectJSONVariables replaces json_variables content.
func UpdateProjectJSONVariables(raw []byte) error {
	normalized, err := normalizeJSONObject(raw)
	if err != nil {
		return err
	}
	row, err := loadProjectSettingRow()
	if err != nil {
		return err
	}
	row.JSONVariables = normalized
	return saveProjectSettingRow(row)
}

// UpdateProjectJSONVariablesFromURL loads JSON content from a URL and replaces json_variables.
func UpdateProjectJSONVariablesFromURL(url string) error {
	raw, err := FetchJSONFromURL(url)
	if err != nil {
		return err
	}
	return UpdateProjectJSONVariables(raw)
}

// FetchJSONFromURL downloads JSON content from a URL.
func FetchJSONFromURL(url string) ([]byte, error) {
	target := strings.TrimSpace(url)
	if target == "" {
		return nil, fmt.Errorf("json url is empty")
	}
	resp, err := http.Get(target)
	if err != nil {
		return nil, fmt.Errorf("fetch json url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch json url status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read json url body: %w", err)
	}
	return raw, nil
}

// VerifyAdminCredentials checks username and password against project_setting.
func VerifyAdminCredentials(username string, password string) (bool, error) {
	row, err := loadProjectSettingRow()
	if err != nil {
		return false, err
	}
	expectUser := strings.TrimSpace(row.EnvVariables["ADMIN_PANEL_USERNAME"])
	if expectUser == "" {
		return false, nil
	}
	if strings.TrimSpace(username) != expectUser {
		return false, nil
	}
	encryptedPassword := strings.TrimSpace(row.EnvVariables["ADMIN_PANEL_PASSWORD"])
	if encryptedPassword == "" {
		return false, nil
	}
	plain, err := decryptAdminPanelPassword(encryptedPassword)
	if err != nil {
		return false, err
	}
	if subtle.ConstantTimeCompare([]byte(plain), []byte(strings.TrimSpace(password))) == 1 {
		return true, nil
	}
	return false, nil
}

// UpdateAdminUsername updates admin username in project_setting.
func UpdateAdminUsername(username string) error {
	value := strings.TrimSpace(username)
	if value == "" {
		return fmt.Errorf("admin username is empty")
	}
	return UpdateProjectEnvVariables(map[string]string{"ADMIN_PANEL_USERNAME": value})
}

// UpdateAdminPassword updates admin password in project_setting using encryption-at-rest.
func UpdateAdminPassword(newPassword string) error {
	plain := strings.TrimSpace(newPassword)
	if plain == "" {
		return fmt.Errorf("new admin password is empty")
	}
	encrypted, err := encryptAdminPanelPassword(plain)
	if err != nil {
		return err
	}
	return UpdateProjectEnvVariables(map[string]string{"ADMIN_PANEL_PASSWORD": encrypted})
}

func adminPasswordCipherKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("ADMIN_PW_ENCRYPTION_KEY"))
	if raw == "" {
		return nil, fmt.Errorf("ADMIN_PW_ENCRYPTION_KEY is required")
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

func encryptAdminPanelPassword(plain string) (string, error) {
	key, err := adminPasswordCipherKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("admin password cipher init: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("admin password gcm init: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("admin password nonce: %w", err)
	}
	cipherText := gcm.Seal(nil, nonce, []byte(strings.TrimSpace(plain)), nil)
	payload := append(nonce, cipherText...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func decryptAdminPanelPassword(encoded string) (string, error) {
	key, err := adminPasswordCipherKey()
	if err != nil {
		return "", err
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("decode admin password payload: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("admin password cipher init: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("admin password gcm init: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(payload) < nonceSize {
		return "", fmt.Errorf("admin password payload too short")
	}
	nonce := payload[:nonceSize]
	cipherText := payload[nonceSize:]
	plain, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt admin password: %w", err)
	}
	return string(plain), nil
}
