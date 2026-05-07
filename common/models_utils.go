package common

import (
	"fmt"
	"strings"
	"unicode"
)

type Message struct {
	ID        int
	Sender    string
	Receiver  string
	Content   string
	Timestamp string
	Direction string
	Nature    string
}

const (
	MessageNatureCronAIMessage             = "cron_ai_message"
	MessageNatureCronFollowupInvitation    = "cron_followup_invitation_message"
	MessageNatureRegularAIMessage          = "regular_ai_message"
	MessageNatureClientMessage             = "client_message"
	MessageNatureVerificationMessage       = "verification_message"
	MessageNatureInterventionEndMessage    = "intervention_end_message"
	MessageNatureBaselineInvitationMessage = "baseline_invitation_message"
	MessageNatureManualMessage             = "manual_message"
)

func ExtractPhone(jid string) string {
	trimmed := strings.TrimSpace(jid)
	if trimmed == "" {
		return ""
	}

	parts := strings.Split(trimmed, "@")
	if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
		local := strings.TrimSpace(parts[0])
		if i := strings.Index(local, ":"); i >= 0 {
			local = strings.TrimSpace(local[:i])
		}
		digits := DigitsOnly(local)
		if digits != "" {
			return digits
		}
		return local
	}
	return trimmed
}

func DigitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func ValidateSQLIdentifier(name string, ctx string) error {
	if name == "" {
		return fmt.Errorf("%s: empty identifier", ctx)
	}
	for i, r := range name {
		if i == 0 {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '_' {
				return fmt.Errorf("%s: invalid identifier start: %q", ctx, name)
			}
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return fmt.Errorf("%s: invalid identifier %q", ctx, name)
	}
	lower := strings.ToLower(name)
	if lower == "user" || lower == "order" || lower == "group" {
		return fmt.Errorf("%s: reserved-like identifier %q not allowed", ctx, name)
	}
	return nil
}

func SanitizeSurveyIDForMetaColumn(surveyID string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(surveyID) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if r == '-' || r == '.' || r == ' ' {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "survey"
	}
	return strings.ToLower(out)
}

func FollowupMetaTimestampColumn(surveyID string) string {
	return fmt.Sprintf("fu_%s_timestamp", SanitizeSurveyIDForMetaColumn(surveyID))
}

func FollowupMetaCompletedColumn(surveyID string) string {
	return fmt.Sprintf("fu_%s_completed", SanitizeSurveyIDForMetaColumn(surveyID))
}
