package ai

import ( // Import packages required to query/decrypt memory records.
	"context" // Provide context for PostgreSQL query execution.
	"errors"
	"fmt"  // Return wrapped errors with readable context.
	"sort" // Reorder newest-first query result into oldest-first memory sequence.
	"strconv"
	"strings" // Normalize decrypted text values before prompt construction.
	"time"    // Parse and compare timestamps safely for chronological ordering.

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/survey"

	"github.com/jackc/pgx/v5"
) // End import block.

const defaultAIMemoryMessageLimit = 20

// GetAIMemoryMessageLimitFromEnv reads AI memory size from project_setting env_variables with safe fallback.
func GetAIMemoryMessageLimitFromEnv() int {
	raw := strings.TrimSpace(db.GetProjectSettingString("AI_MEMORY_MESSAGE_LIMIT", strconv.Itoa(defaultAIMemoryMessageLimit)))
	if raw == "" {
		return defaultAIMemoryMessageLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultAIMemoryMessageLimit
	}
	return n
}

func GetLastMessages(limit int) ([]common.Message, error) { // Load last N messages and return them as AI memory context.
	if limit <= 0 { // Validate caller-provided memory size.
		limit = defaultAIMemoryMessageLimit // Default message window when caller passes invalid value.
	}

	// Newest rows first; LIMIT applied in SQL.
	query := `
SELECT id, sender, receiver, content, direction, nature, to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
FROM conversation
ORDER BY created_at DESC
LIMIT $1`

	rows, err := db.DB.Query(context.Background(), query, limit) // Execute memory query against PostgreSQL.
	if err != nil {                                              // Check query execution errors.
		return nil, fmt.Errorf("query last messages: %w", err) // Return wrapped query error.
	}
	defer rows.Close() // Ensure cursor resources are released.

	messages := make([]common.Message, 0, limit) // Prepare output slice with expected capacity.
	for rows.Next() {                            // Iterate over each returned row.
		var msg common.Message       // Allocate message struct for scanned row data.
		var encryptedSender string   // Hold encrypted sender value from db.DB.
		var encryptedReceiver string // Hold encrypted receiver value from db.DB.

		if err := rows.Scan( // Scan selected columns into variables.
			&msg.ID,            // Scan message ID column.
			&encryptedSender,   // Scan encrypted sender column.
			&encryptedReceiver, // Scan encrypted receiver column.
			&msg.Content,       // Scan message content column.
			&msg.Direction,     // Scan message direction column.
			&msg.Nature,        // Scan message nature.
			&msg.Timestamp,     // Scan timestamp as text for prompt readability.
		); err != nil { // Check scan errors.
			return nil, fmt.Errorf("scan memory row: %w", err) // Return wrapped scan error.
		}

		decryptedSender, err := common.DecryptPhone(encryptedSender) // Decrypt sender phone value.
		if err != nil {                                              // Handle decryption failures gracefully.
			decryptedSender = "[decrypt-error]" // Use fallback marker when sender decryption fails.
		}

		decryptedReceiver, err := common.DecryptPhone(encryptedReceiver) // Decrypt receiver phone value.
		if err != nil {                                                  // Handle decryption failures gracefully.
			decryptedReceiver = "[decrypt-error]" // Use fallback marker when receiver decryption fails.
		}

		msg.Sender = strings.TrimSpace(decryptedSender)     // Assign normalized decrypted sender.
		msg.Receiver = strings.TrimSpace(decryptedReceiver) // Assign normalized decrypted receiver.
		msg.Content = strings.TrimSpace(msg.Content)        // Normalize content spacing.
		msg.Direction = strings.TrimSpace(msg.Direction)    // Normalize direction spacing.
		// Do not send phone numbers to Gemini. Keep role-style participants only.
		if strings.EqualFold(msg.Direction, "outbound") { // Detect messages sent by bot/assistant side.
			msg.Sender = "AI"
			msg.Receiver = "USER"
		} else {
			msg.Sender = "USER"
			msg.Receiver = "AI"
		}

		messages = append(messages, msg) // Append parsed message to output memory list.
	}

	if err := rows.Err(); err != nil { // Check row iteration errors.
		return nil, fmt.Errorf("iterate memory rows: %w", err) // Return wrapped iteration error.
	}

	sort.Slice(messages, func(i int, j int) bool { // Reorder list into chronological oldest->newest order.
		ti, errI := time.Parse(time.RFC3339, messages[i].Timestamp) // Parse left timestamp for accurate comparison.
		tj, errJ := time.Parse(time.RFC3339, messages[j].Timestamp) // Parse right timestamp for accurate comparison.
		if errI == nil && errJ == nil {                             // Use timestamp comparison when both parses succeed.
			return ti.Before(tj) // Keep older timestamp before newer timestamp.
		}
		return messages[i].ID < messages[j].ID // Fallback to ID order if timestamp parsing fails.
	})

	return messages, nil // Return final decrypted and ordered memory list.
} // End GetLastMessages function.

type participantSurveyCompletion struct {
	BaselineCompletedAt time.Time
	FollowupCompletedAt map[string]time.Time
	ParticipantName     string
}

// BuildParticipantSurveyContextForAI returns completed baseline/follow-up answers for prompt context.
func BuildParticipantSurveyContextForAI(participantPhone string) (string, error) {
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil {
		return "", nil
	}
	phoneDigits := common.DigitsOnly(strings.TrimSpace(participantPhone))
	if phoneDigits == "" {
		return "", nil
	}
	completion, err := loadParticipantSurveyCompletion(phoneDigits, cfg)
	if err != nil {
		return "", err
	}

	sections := []string{}
	if strings.TrimSpace(completion.ParticipantName) != "" {
		sections = append(sections, fmt.Sprintf(`Participant Profile
name: "%s"`, strings.ReplaceAll(strings.TrimSpace(completion.ParticipantName), `"`, `'`)))
	}
	if !completion.BaselineCompletedAt.IsZero() {
		lines, err := loadLatestSurveyAnswerLines(cfg.Baseline.TableName, survey.BaselineQuestionsWithSystemFields(cfg.Baseline.Questions), phoneDigits)
		if err != nil {
			return "", err
		}
		if len(lines) > 0 {
			sections = append(sections, formatSurveyContextSection("Baseline Survey", completion.BaselineCompletedAt, lines))
		}
	}

	for _, fu := range cfg.Followups {
		completedAt, ok := completion.FollowupCompletedAt[strings.TrimSpace(fu.SurveyID)]
		if !ok || completedAt.IsZero() {
			continue
		}
		lines, err := loadLatestSurveyAnswerLines(fu.TableName, fu.Questions, phoneDigits)
		if err != nil {
			return "", err
		}
		if len(lines) == 0 {
			continue
		}
		heading := strings.TrimSpace(fu.Title)
		if heading == "" {
			heading = strings.TrimSpace(fu.SurveyID)
		}
		sections = append(sections, formatSurveyContextSection("Follow-up Survey: "+heading, completedAt, lines))
	}

	return strings.Join(sections, "\n\n"), nil
}

// BuildParticipantPhaseContextForAI returns currently active phase prompts for this participant.
// This is intentionally separate from survey context builder.
func BuildParticipantPhaseContextForAI(participantPhone string, now time.Time) (string, error) {
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil || !cfg.Project.Phases.Enabled {
		return "", nil
	}
	phoneDigits := common.DigitsOnly(strings.TrimSpace(participantPhone))
	if phoneDigits == "" {
		return "", nil
	}
	completion, err := loadParticipantSurveyCompletion(phoneDigits, cfg)
	if err != nil {
		return "", err
	}
	if completion.BaselineCompletedAt.IsZero() {
		return "", nil
	}

	current := now.UTC()
	if current.IsZero() {
		current = time.Now().UTC()
	}

	activeLines := []string{}
	baselineAt := completion.BaselineCompletedAt.UTC()
	for _, phase := range cfg.Project.Phases.Items {
		if phase.Length <= 0 {
			continue
		}
		prompt := strings.TrimSpace(phase.Prompt)
		if prompt == "" {
			continue
		}
		startDays := phase.StartDate
		if startDays < 0 {
			startDays = 0
		}
		phaseStart := baselineAt.AddDate(0, 0, startDays)
		phaseEnd := phaseStart.AddDate(0, 0, phase.Length)
		if current.Before(phaseStart) || !current.Before(phaseEnd) {
			continue
		}
		activeLines = append(activeLines, fmt.Sprintf("- [phase_id=%d] %s", phase.PhaseID, prompt))
	}
	if len(activeLines) == 0 {
		return "", nil
	}
	return "ACTIVE PHASE PROMPTS:\n" + strings.Join(activeLines, "\n"), nil
}

func loadParticipantSurveyCompletion(phoneDigits string, cfg *survey.SurveyConfig) (participantSurveyCompletion, error) {
	out := participantSurveyCompletion{
		FollowupCompletedAt: map[string]time.Time{},
	}
	if cfg == nil {
		return out, nil
	}
	columns := []string{"participant_phone", "baseline_completed_ts", "participant_name"}
	followupIDs := make([]string, 0, len(cfg.Followups))
	for _, fu := range cfg.Followups {
		surveyID := strings.TrimSpace(fu.SurveyID)
		if surveyID == "" {
			continue
		}
		tsCol := common.FollowupMetaTimestampColumn(surveyID)
		if err := common.ValidateSQLIdentifier(tsCol, "followup completion timestamp column"); err != nil {
			return out, err
		}
		columns = append(columns, tsCol)
		followupIDs = append(followupIDs, surveyID)
	}
	query := fmt.Sprintf("SELECT %s FROM meta", strings.Join(columns, ", "))
	rows, err := db.DB.Query(context.Background(), query)
	if err != nil {
		return out, fmt.Errorf("query meta survey completion: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return out, fmt.Errorf("meta survey completion values: %w", err)
		}
		if len(values) < 3 {
			continue
		}
		encPhone, ok := values[0].(string)
		if !ok {
			continue
		}
		plainPhone, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		if common.DigitsOnly(strings.TrimSpace(plainPhone)) != phoneDigits {
			continue
		}

		if t, ok := asTime(values[1]); ok {
			if out.BaselineCompletedAt.IsZero() || t.After(out.BaselineCompletedAt) {
				out.BaselineCompletedAt = t
			}
		}
		name := ""
		if values[2] != nil {
			name = strings.TrimSpace(fmt.Sprintf("%v", values[2]))
		}
		if name != "" {
			out.ParticipantName = name
		}
		for i, surveyID := range followupIDs {
			idx := i + 3
			if idx >= len(values) {
				break
			}
			if t, ok := asTime(values[idx]); ok {
				prev, exists := out.FollowupCompletedAt[surveyID]
				if !exists || t.After(prev) {
					out.FollowupCompletedAt[surveyID] = t
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate meta survey completion: %w", err)
	}
	return out, nil
}

func loadLatestSurveyAnswerLines(tableName string, questions []survey.SurveyQuestion, phoneDigits string) ([]string, error) {
	tbl := strings.TrimSpace(tableName)
	if tbl == "" {
		return nil, nil
	}
	if err := common.ValidateSQLIdentifier(tbl, "survey response table name"); err != nil {
		return nil, err
	}
	selectCols := []string{}
	columnOrder := []string{}
	for _, q := range questions {
		col := strings.TrimSpace(q.ColumnName)
		if col == "" {
			continue
		}
		if err := common.ValidateSQLIdentifier(col, "survey response column"); err != nil {
			return nil, err
		}
		selectCols = append(selectCols, fmt.Sprintf("COALESCE(TRIM(%s::text), '') AS %s", col, col))
		columnOrder = append(columnOrder, col)
	}
	if len(selectCols) == 0 {
		return nil, nil
	}
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = $1 ORDER BY submitted_at DESC, id DESC LIMIT 1",
		strings.Join(selectCols, ", "),
		tbl,
		survey.RespondentPhoneColumn,
	)
	row := db.DB.QueryRow(context.Background(), query, phoneDigits)
	rawValues := make([]string, len(columnOrder))
	scanArgs := make([]interface{}, len(columnOrder))
	for i := range rawValues {
		scanArgs[i] = &rawValues[i]
	}
	if err := row.Scan(scanArgs...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan latest survey response from %s: %w", tbl, err)
	}
	labelByColumn := map[string]string{}
	for _, q := range questions {
		col := strings.TrimSpace(q.ColumnName)
		if col == "" {
			continue
		}
		label := strings.TrimSpace(q.Label)
		if label == "" {
			label = strings.TrimSpace(q.ID)
		}
		labelByColumn[col] = label
	}
	lines := []string{}
	for i, col := range columnOrder {
		answer := strings.TrimSpace(rawValues[i])
		if answer == "" {
			continue // Skip unanswered questions.
		}
		label := strings.TrimSpace(labelByColumn[col])
		if label == "" {
			label = col
		}
		escapedLabel := strings.ReplaceAll(label, `"`, `'`)
		escapedAnswer := strings.ReplaceAll(answer, `"`, `'`)
		lines = append(lines, fmt.Sprintf(`question: "%s" response: "%s"`, escapedLabel, escapedAnswer))
	}
	return lines, nil
}

func formatSurveyContextSection(title string, completedAt time.Time, lines []string) string {
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString("timestamp of that survey completion: ")
	b.WriteString(completedAt.UTC().Format(time.RFC3339))
	b.WriteString("\n")
	for i, line := range lines {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(line)
	}
	return b.String()
}

func asTime(v interface{}) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case *time.Time:
		if t == nil {
			return time.Time{}, false
		}
		return *t, true
	default:
		return time.Time{}, false
	}
}
