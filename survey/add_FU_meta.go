package survey

import ( // SQL execution.
	"context" // DB context.
	"fmt"     // Errors.
	"strings" // String normalization.

	"whatsapp-bot/common"
	"whatsapp-bot/db"
) // End import.

// SanitizeSurveyIDForMetaColumn turns survey_id into a safe SQL identifier fragment.
func SanitizeSurveyIDForMetaColumn(surveyID string) string {
	return common.SanitizeSurveyIDForMetaColumn(surveyID)
} // End SanitizeSurveyIDForMetaColumn.

// FollowupMetaTimestampColumn returns meta column name for follow-up timestamp (e.g. completion).
func FollowupMetaTimestampColumn(surveyID string) string { // fu_<san>_timestamp pattern.
	return common.FollowupMetaTimestampColumn(surveyID)
} // End FollowupMetaTimestampColumn.

// FollowupMetaCompletedColumn returns meta boolean column for follow-up completion.
func FollowupMetaCompletedColumn(surveyID string) string { // fu_<san>_completed pattern.
	return common.FollowupMetaCompletedColumn(surveyID)
} // End FollowupMetaCompletedColumn.

// EnsureFollowupMetaColumns adds two columns for one follow-up if missing.
func EnsureFollowupMetaColumns(surveyID string) error { // ALTER TABLE meta ADD COLUMN IF NOT EXISTS.
	tsCol := FollowupMetaTimestampColumn(surveyID)                                         // Timestamp column name.
	doneCol := FollowupMetaCompletedColumn(surveyID)                                       // Boolean column name.
	if err := common.ValidateSQLIdentifier(tsCol, "followup meta ts column"); err != nil { // Validate composed name.
		return err // Propagate.
	}
	if err := common.ValidateSQLIdentifier(doneCol, "followup meta completed column"); err != nil { // Validate composed name.
		return err // Propagate.
	}
	q1 := fmt.Sprintf( // Add timestamp column.
		"ALTER TABLE meta ADD COLUMN IF NOT EXISTS %s TIMESTAMPTZ NULL",
		tsCol,
	)
	q2 := fmt.Sprintf( // Add completed flag.
		"ALTER TABLE meta ADD COLUMN IF NOT EXISTS %s BOOLEAN NOT NULL DEFAULT FALSE",
		doneCol,
	)
	if _, err := db.DB.Exec(context.Background(), q1); err != nil { // Execute first ALTER.
		return fmt.Errorf("alter meta add %s: %w", tsCol, err) // Wrap error.
	}
	if _, err := db.DB.Exec(context.Background(), q2); err != nil { // Execute second ALTER.
		return fmt.Errorf("alter meta add %s: %w", doneCol, err) // Wrap error.
	}
	return nil // Success.
} // End EnsureFollowupMetaColumns.

// EnsureAllFollowupMetaColumns ensures columns for every follow-up in config.
func EnsureAllFollowupMetaColumns(cfg *SurveyConfig) error { // Loop all followups.
	if cfg == nil { // Guard nil.
		return fmt.Errorf("survey config is nil") // Error.
	}
	for i := range cfg.Followups { // Each follow-up survey.
		sid := strings.TrimSpace(cfg.Followups[i].SurveyID) // Survey id from JSON.
		if sid == "" {                                      // Skip empty ids.
			return fmt.Errorf("followup at index %d has empty survey_id", i) // Data error.
		}
		if err := EnsureFollowupMetaColumns(sid); err != nil { // Ensure columns for this id.
			return err // Stop on first error.
		}
	}
	return nil // Done.
} // End EnsureAllFollowupMetaColumns.

// MarkFollowupCompleteByMetaID sets completion flag and timestamp for one follow-up.
func MarkFollowupCompleteByMetaID(metaID int64, surveyID string) error { // Called after successful FU form submit.
	tsCol := FollowupMetaTimestampColumn(surveyID)   // Timestamp column.
	doneCol := FollowupMetaCompletedColumn(surveyID) // Completed column.
	q := fmt.Sprintf(                                // Dynamic UPDATE (validated identifiers only).
		"UPDATE meta SET %s = TRUE, %s = NOW() WHERE id = $1 AND %s = FALSE",
		doneCol,
		tsCol,
		doneCol,
	)
	_, err := db.DB.Exec(context.Background(), q, metaID) // Execute update.
	if err != nil {                                       // Check error.
		return fmt.Errorf("mark followup complete: %w", err) // Wrap.
	}
	return nil // OK.
} // End MarkFollowupCompleteByMetaID.
