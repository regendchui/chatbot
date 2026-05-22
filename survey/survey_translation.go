package survey

import "strings"

// Constants below are JSON object keys for project.translations (survey-config), not the displayed text.
const (
	SurveyTranslationProjectLabel               = "project_label"
	SurveyTranslationDescriptionLabel           = "description_label"
	SurveyTranslationRespondentPhoneLabel       = "respondent_phone_label"
	SurveyTranslationBaselinePhoneLabel         = "baseline_phone_label"
	SurveyTranslationFollowupPhoneLabel         = "followup_phone_label"
	SurveyTranslationParticipantNameLabel       = "participant_name_label"
	SurveyTranslationParticipantNamePlaceholder = "participant_name_placeholder"
	SurveyTranslationMessageIntervalLabel       = "message_interval_label"
	SurveyTranslationConsentFormLabel           = "consent_form_label"
	SurveyTranslationConsentAgreeLabel          = "consent_agree_label"
	SurveyTranslationIntervalOncePerOneWeek     = "interval_once_per_one_week"
	SurveyTranslationIntervalTwicePerOneWeek    = "interval_twice_per_one_week"
	SurveyTranslationIntervalOncePerTwoWeeks    = "interval_once_per_two_weeks"
	SurveyTranslationResponseSummary            = "response_summary"
	SurveyTranslationReturnToWhatsApp           = "return_to_whatsapp"
	SurveyTranslationConsentRecorded            = "consent_recorded"
)

// SurveyTranslate returns a translated UI string from project.translations.
// When no translation exists, it falls back to the provided default text.
func SurveyTranslate(key string, fallback string) string {
	trimmedFallback := strings.TrimSpace(fallback)
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return trimmedFallback
	}
	cfg := GlobalSurveyConfig()
	if cfg == nil || cfg.Project.Translations == nil {
		return trimmedFallback
	}
	translated := strings.TrimSpace(cfg.Project.Translations[trimmedKey])
	if translated == "" {
		return trimmedFallback
	}
	return translated
}
