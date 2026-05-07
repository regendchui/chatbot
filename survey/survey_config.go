package survey

import ( // Standard library for config I/O.
	"encoding/json" // Parse survey-config.json.
	"fmt"           // Format errors.
	"os"            // Read config file from disk.
	"strings"       // Trim paths and strings.

	"whatsapp-bot/db"
) // End import.

// SurveyConfig is the root structure of survey-config.json.
type SurveyConfig struct { // Top-level config from JSON file.
	Project   SurveyProject  `json:"project"`   // Project metadata.
	Baseline  SurveyBaseline `json:"baseline"`  // Exactly one baseline survey.
	Followups []SurveyFollow `json:"followups"` // Zero or more follow-up surveys.
} // End SurveyConfig.

// SurveyProject holds project-level metadata from JSON.
type SurveyProject struct { // Project block.
	Name                     string              `json:"name"`                       // Human-readable project name.
	Description              string              `json:"description"`                // Project description.
	ConsentFormText          string              `json:"consent_form_text"`          // Baseline consent text shown before questions.
	ConsentFormLabel         string              `json:"consent_form_label"`         // Baseline consent section label.
	Translations             map[string]string   `json:"translations"`               // Optional UI translation dictionary for survey static texts.
	Version                  int                 `json:"version"`                    // Config version.
	DefaultLanguage          string              `json:"default_language"`           // e.g. en.
	InterventionPeriod       int                 `json:"intervention_period"`        // Number of days from baseline completion to keep auto messaging active.
	InterventionEndMessage   string              `json:"intervention_end_message"`   // One-time message sent when intervention window is over.
	VerificationMessage      string              `json:"verification_message"`       // Message sent while waiting for admin verification when verification gate is enabled.
	CronTaskTime             int                 `json:"cron_task_time"`             // Daily trigger time in HHMM format (e.g. 1500 for 15:00).
	ChatbotScheduleQuestions ChatbotScheduleDays `json:"chatbot_schedule_questions"` // Weekday mapping for each interval type.
	Phases                   SurveyProjectPhases `json:"phases"`                     // Optional phase prompts used by AI based on baseline-relative date windows.
} // End SurveyProject.

// SurveyProjectPhases controls optional intervention phases.
type SurveyProjectPhases struct {
	Enabled bool                 `json:"enabled"` // When true, evaluate phase windows and inject matching prompts to AI context.
	Items   []SurveyProjectPhase `json:"items"`   // Phase definitions.
}

// SurveyProjectPhase describes one baseline-relative prompt phase.
type SurveyProjectPhase struct {
	PhaseID   int    `json:"phase_id"`   // Phase identifier.
	StartDate int    `json:"start_date"` // Start offset in days after baseline completion.
	Length    int    `json:"length"`     // Active duration in days.
	Prompt    string `json:"prompt"`     // Prompt text to inject while phase is active.
}

// ChatbotScheduleDays maps interval labels to weekday names.
type ChatbotScheduleDays struct {
	OncePerOneWeek  []string `json:"once_per_one_week"`  // Days used for weekly schedule.
	TwicePerOneWeek []string `json:"twice_per_one_week"` // Days used for twice-weekly schedule.
	OncePerTwoWeeks []string `json:"once_per_two_weeks"` // Days used for once-every-two-weeks schedule.
}

// SurveyBaseline describes the baseline questionnaire.
type SurveyBaseline struct { // Baseline survey definition.
	SurveyID       string           `json:"survey_id"`       // Stable survey id.
	Version        int              `json:"version"`         // Survey version.
	Title          string           `json:"title"`           // Display title.
	InvitationText string           `json:"invitation_text"` // WhatsApp invitation body text.
	LinkSlug       string           `json:"link_slug"`       // URL slug for /survey/{slug}.
	TableName      string           `json:"table_name"`      // PostgreSQL table for responses.
	Trigger        SurveyTrigger    `json:"trigger"`         // send_once / expiry (ignored for now).
	Questions      []SurveyQuestion `json:"questions"`       // Question definitions.
} // End SurveyBaseline.

// SurveyFollow describes one follow-up questionnaire.
type SurveyFollow struct { // Follow-up survey definition.
	SurveyID       string           `json:"survey_id"`       // Stable survey id.
	Version        int              `json:"version"`         // Survey version.
	Title          string           `json:"title"`           // Display title.
	InvitationText string           `json:"invitation_text"` // WhatsApp invitation text.
	LinkSlug       string           `json:"link_slug"`       // URL slug.
	TableName      string           `json:"table_name"`      // PostgreSQL table name.
	Trigger        SurveyTrigger    `json:"trigger"`         // Follow-up reminder scheduling settings.
	Questions      []SurveyQuestion `json:"questions"`       // Questions.
} // End SurveyFollow.

// SurveyTrigger stores scheduling metadata for follow-up prompting.
type SurveyTrigger struct { // Trigger metadata from JSON.
	SendFrequency    int `json:"send_frequency"`    // Maximum number of follow-up prompt sends.
	ReminderInterval int `json:"reminder_interval"` // Days between follow-up reminders when incomplete.
	Time             int `json:"time"`              // Day offset from baseline completion for first prompt.
} // End SurveyTrigger.

// SurveyQuestion is one form field in the JSON.
type SurveyQuestion struct { // Single question.
	ID              string               `json:"id"`               // Question id in JSON.
	ColumnName      string               `json:"column_name"`      // DB column name.
	Type            string               `json:"type"`             // text, multiple_choice, multiple_select, etc.
	Label           string               `json:"label"`            // Label shown on form.
	Required        bool                 `json:"required"`         // Whether required.
	Placeholder     string               `json:"placeholder"`      // Optional placeholder.
	SliderStart     float64              `json:"slider_start"`     // Slider minimum value for type=slider.
	SliderEnd       float64              `json:"slider_end"`       // Slider maximum value for type=slider.
	SliderStep      float64              `json:"slider_step"`      // Slider step value for type=slider.
	Choices         []SurveyChoice       `json:"choices"`          // For choice types.
	VisibilityLogic SurveyVisibilityRule `json:"visibility_logic"` // Optional conditional display logic.
} // End SurveyQuestion.

// SurveyChoice is one option for choice questions.
type SurveyChoice struct { // One choice row.
	Value string `json:"value"` // Stored value (string in DB).
	Label string `json:"label"` // Display label.
} // End SurveyChoice.

// SurveyVisibilityRule controls whether a question is shown based on prior answers.
type SurveyVisibilityRule struct {
	Enabled        bool               `json:"enabled"`         // True if conditional display is enabled.
	GroupConnector string             `json:"group_connector"` // and/or between groups.
	Groups         []SurveyLogicGroup `json:"groups"`          // One or more condition groups.
} // End SurveyVisibilityRule.

// SurveyLogicGroup contains multiple conditions connected by one connector.
type SurveyLogicGroup struct {
	RowConnector string                 `json:"row_connector"` // and/or within this group.
	Conditions   []SurveyLogicCondition `json:"conditions"`    // Individual conditions.
} // End SurveyLogicGroup.

// SurveyLogicCondition is one comparator expression.
type SurveyLogicCondition struct {
	Field      string `json:"field"`      // Referenced question id (e.g. q1).
	Comparator string `json:"comparator"` // equals/not_equals/greater_than/etc.
	Value      string `json:"value"`      // Right-hand value (may be blank for is_answered/is_not_answered).
} // End SurveyLogicCondition.

// globalSurveyConfig holds loaded config after InitSurveyInfrastructure.
var globalSurveyConfig *SurveyConfig // Pointer to loaded config (nil if load failed).

// RespondentPhoneColumn is the forced respondent phone column (digits only, include country code, e.g. 85254036581).
const RespondentPhoneColumn = "respondent_phone"

// Built-in baseline-only question identifiers (injected by backend, not required in JSON).
const (
	MessageIntervalQuestionID = "message_interval_system"
	MessageIntervalColumn     = "message_interval"
	ParticipantNameQuestionID = "participant_name_system"
	ParticipantNameColumn     = "participant_name"
	ConsentColumn             = "consent"
)

// BaselineQuestionsWithSystemFields appends built-in baseline questions when missing from JSON.
func BaselineQuestionsWithSystemFields(questions []SurveyQuestion) []SurveyQuestion {
	defaultName := SurveyQuestion{
		ID:          ParticipantNameQuestionID,
		ColumnName:  ParticipantNameColumn,
		Type:        "text",
		Label:       SurveyTranslate(SurveyTranslationParticipantNameLabel, "Participant name"),
		Required:    true,
		Placeholder: SurveyTranslate(SurveyTranslationParticipantNamePlaceholder, "Enter participant name"),
	}
	defaultInterval := SurveyQuestion{
		ID:         MessageIntervalQuestionID,
		ColumnName: MessageIntervalColumn,
		Type:       "multiple_choice",
		Label:      SurveyTranslate(SurveyTranslationMessageIntervalLabel, "Message interval"),
		Required:   true,
		Choices: []SurveyChoice{
			{Value: "once per one week", Label: SurveyTranslate(SurveyTranslationIntervalOncePerOneWeek, "once per one week")},
			{Value: "twice per one week", Label: SurveyTranslate(SurveyTranslationIntervalTwicePerOneWeek, "twice per one week")},
			{Value: "once per two weeks", Label: SurveyTranslate(SurveyTranslationIntervalOncePerTwoWeeks, "once per two weeks")},
		},
	}

	nameQuestion := defaultName
	intervalQuestion := defaultInterval
	remaining := make([]SurveyQuestion, 0, len(questions))
	for _, q := range questions {
		if strings.EqualFold(strings.TrimSpace(q.ColumnName), ParticipantNameColumn) || strings.EqualFold(strings.TrimSpace(q.ID), ParticipantNameQuestionID) {
			nameQuestion = q
			if strings.TrimSpace(nameQuestion.ColumnName) == "" {
				nameQuestion.ColumnName = ParticipantNameColumn
			}
			if strings.TrimSpace(nameQuestion.ID) == "" {
				nameQuestion.ID = ParticipantNameQuestionID
			}
			nameQuestion.Required = true // Always require participant name in baseline.
			continue
		}
		if strings.EqualFold(strings.TrimSpace(q.ColumnName), MessageIntervalColumn) || strings.EqualFold(strings.TrimSpace(q.ID), MessageIntervalQuestionID) {
			intervalQuestion = q
			if strings.TrimSpace(intervalQuestion.ColumnName) == "" {
				intervalQuestion.ColumnName = MessageIntervalColumn
			}
			if strings.TrimSpace(intervalQuestion.ID) == "" {
				intervalQuestion.ID = MessageIntervalQuestionID
			}
			continue
		}
		remaining = append(remaining, q)
	}

	out := make([]SurveyQuestion, 0, len(remaining)+2)
	out = append(out, nameQuestion)
	out = append(out, intervalQuestion)
	out = append(out, remaining...)
	return out
}

// LoadSurveyConfig reads and parses survey-config.json from path.
func LoadSurveyConfig(path string) (*SurveyConfig, error) { // Load JSON from disk.
	trimmed := strings.TrimSpace(path) // Normalize path.
	if trimmed == "" {                 // Reject empty path.
		return nil, fmt.Errorf("survey config path is empty") // Return validation error.
	}
	raw, err := os.ReadFile(trimmed) // Read entire file.
	if err != nil {                  // Handle read errors.
		return nil, fmt.Errorf("read survey config: %w", err) // Wrap error.
	}
	var cfg SurveyConfig                              // Allocate target struct.
	if err := json.Unmarshal(raw, &cfg); err != nil { // Parse JSON.
		return nil, fmt.Errorf("parse survey config: %w", err) // Wrap parse error.
	}
	return &cfg, nil // Return loaded config.
} // End LoadSurveyConfig.

// ParseSurveyConfigBytes parses a JSON payload into SurveyConfig.
func ParseSurveyConfigBytes(raw []byte) (*SurveyConfig, error) {
	var cfg SurveyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse survey config bytes: %w", err)
	}
	return &cfg, nil
}

// GlobalSurveyConfig returns the in-memory config after init (may be nil).
func GlobalSurveyConfig() *SurveyConfig { // Accessor for handlers and HTTP.
	return globalSurveyConfig // Return pointer.
} // End GlobalSurveyConfig.

// InitSurveyInfrastructure loads config, creates survey tables, and follow-up meta columns.
func InitSurveyInfrastructure() error { // Called from InitDB after core tables exist.
	cfg, err := LoadSurveyConfigFromProjectSetting() // Load and parse config JSON stored in project_setting table.
	if err != nil {                                  // Propagate load errors.
		return err // Return to caller.
	}
	globalSurveyConfig = cfg                           // Store globally for HTTP and invitations.
	if err := CreateAllSurveyTables(cfg); err != nil { // Create response tables from JSON.
		return fmt.Errorf("survey tables: %w", err) // Wrap DDL error.
	}
	if err := EnsureAllFollowupMetaColumns(cfg); err != nil { // ALTER meta for each FU.
		return fmt.Errorf("followup meta columns: %w", err) // Wrap ALTER error.
	}
	return nil // Success.
} // End InitSurveyInfrastructure.

// LoadSurveyConfigFromProjectSetting parses survey config object stored in project_setting.json_variables.
func LoadSurveyConfigFromProjectSetting() (*SurveyConfig, error) {
	raw, err := db.GetProjectJSONVariablesRaw()
	if err != nil {
		return nil, fmt.Errorf("load survey config from project_setting: %w", err)
	}
	cfg, err := ParseSurveyConfigBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("parse survey config from project_setting: %w", err)
	}
	return cfg, nil
}

// ReloadSurveyInfrastructureFromProjectSetting reloads in-memory survey config and reapplies table/column ensures.
func ReloadSurveyInfrastructureFromProjectSetting() error {
	cfg, err := LoadSurveyConfigFromProjectSetting()
	if err != nil {
		return err
	}
	globalSurveyConfig = cfg
	if err := CreateAllSurveyTables(cfg); err != nil {
		return fmt.Errorf("survey tables: %w", err)
	}
	if err := EnsureAllFollowupMetaColumns(cfg); err != nil {
		return fmt.Errorf("followup meta columns: %w", err)
	}
	return nil
}

// SurveyBySlug finds baseline or follow-up by link_slug.
func SurveyBySlug(slug string) (isBaseline bool, baseline *SurveyBaseline, follow *SurveyFollow, err error) { // Lookup helper.
	if globalSurveyConfig == nil { // Guard missing config.
		return false, nil, nil, fmt.Errorf("survey config not loaded") // Config error.
	}
	s := strings.TrimSpace(slug) // Normalize slug.
	if s == "" {                 // Reject empty slug.
		return false, nil, nil, fmt.Errorf("empty slug") // Validation error.
	}
	if strings.TrimSpace(globalSurveyConfig.Baseline.LinkSlug) == s { // Match baseline slug.
		return true, &globalSurveyConfig.Baseline, nil, nil // Baseline hit.
	}
	for i := range globalSurveyConfig.Followups { // Scan follow-ups.
		if strings.TrimSpace(globalSurveyConfig.Followups[i].LinkSlug) == s { // Match follow-up slug.
			return false, nil, &globalSurveyConfig.Followups[i], nil // Follow-up hit.
		}
	}
	return false, nil, nil, fmt.Errorf("unknown survey slug: %s", s) // Not found.
} // End SurveyBySlug.

// BaselineSurveyURL returns full HTTPS/HTTP URL for baseline invitation link.
func BaselineSurveyURL() (string, error) { // Build URL from env base + baseline slug.
	if globalSurveyConfig == nil { // Require loaded config.
		return "", fmt.Errorf("survey config not loaded") // Error if missing.
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("SURVEY_PUBLIC_BASE_URL")), "/") // Base URL without trailing slash.
	if base == "" {                                                                        // Require public base for links in WhatsApp.
		return "", fmt.Errorf("SURVEY_PUBLIC_BASE_URL is required for survey links") // Config error.
	}
	slug := strings.TrimSpace(globalSurveyConfig.Baseline.LinkSlug) // Baseline slug from JSON.
	if slug == "" {                                                 // Reject empty slug.
		return "", fmt.Errorf("baseline link_slug is empty in config") // Data error.
	}
	return fmt.Sprintf("%s/survey/%s", base, slug), nil // Full survey URL.
} // End BaselineSurveyURL.

// FollowupSurveyURL builds public URL for a follow-up slug.
func FollowupSurveyURL(linkSlug string) (string, error) { // Build URL for one follow-up.
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("SURVEY_PUBLIC_BASE_URL")), "/") // Public base.
	if base == "" {                                                                        // Require base URL.
		return "", fmt.Errorf("SURVEY_PUBLIC_BASE_URL is required") // Config error.
	}
	s := strings.TrimSpace(linkSlug) // Normalize slug.
	if s == "" {                     // Reject empty.
		return "", fmt.Errorf("follow-up link_slug is empty") // Validation error.
	}
	return fmt.Sprintf("%s/survey/%s", base, s), nil // Full URL.
} // End FollowupSurveyURL.
