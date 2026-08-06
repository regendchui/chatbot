package db

import ( // Import packages needed for meta-table operations.
	"context" // Provide context for DB operations.
	"fmt"     // Return and print wrapped errors.
	"strings" // Normalize participant phone input.
	"time"    // Scan nullable timestamptz into pointer.

	"whatsapp-bot/common"
) // End import block.

const MetaMessageIntervalColumn = "message_interval" // Participant-level preferred message interval captured at baseline submit.
const MetaParticipantNameColumn = "participant_name" // Participant name captured at baseline submit.

func EnsureMetaTableExists() error { // Ensure participant metadata table exists before handling messages.
	// Define table creation SQL for participant metadata.
	query := `
CREATE TABLE IF NOT EXISTS meta (
    id SERIAL PRIMARY KEY,
    participant_phone TEXT NOT NULL UNIQUE,
    first_contact_ts TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    has_baseline_questionnaire BOOLEAN NOT NULL DEFAULT FALSE,
    baseline_completed_ts TIMESTAMPTZ NULL,
    participant_name TEXT NOT NULL DEFAULT '',
    message_interval TEXT NOT NULL DEFAULT '',
    end_message BOOLEAN NOT NULL DEFAULT FALSE,
    verification BOOLEAN NOT NULL DEFAULT FALSE,
    exclude_from_engagement BOOLEAN NOT NULL DEFAULT FALSE
);`

	_, err := DB.Exec(context.Background(), query) // Execute meta table DDL.
	if err != nil {                                // Check DDL execution error.
		return fmt.Errorf("create meta table: %w", err) // Stop app if meta schema cannot be created.
	} // End meta DDL error branch.

	// Keep local schema in sync when table already exists from older runs.
	if _, err := DB.Exec(context.Background(), `ALTER TABLE meta ADD COLUMN IF NOT EXISTS end_message BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return fmt.Errorf("alter meta add end_message: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE meta ADD COLUMN IF NOT EXISTS verification BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return fmt.Errorf("alter meta add verification: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE meta ADD COLUMN IF NOT EXISTS exclude_from_engagement BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return fmt.Errorf("alter meta add exclude_from_engagement: %w", err)
	}
	return nil
} // End EnsureMetaTableExists function.

func EnsureParticipantMeta(phone string) (bool, error) { // Insert participant metadata row on first contact only.
	normalizedPhone := strings.TrimSpace(phone) // Normalize phone input.
	if normalizedPhone == "" {                  // Validate phone input.
		return false, fmt.Errorf("participant phone is empty") // Return explicit validation error.
	} // End phone validation branch.

	encryptedPhone, err := common.EncryptPhone(normalizedPhone) // Encrypt participant phone before writing to meta table.
	if err != nil {                                             // Check encryption failure.
		return false, fmt.Errorf("encrypt participant phone: %w", err) // Return wrapped encryption error.
	} // End encryption error branch.

	// Insert-once: avoid duplicate rows for existing participants.
	query := `
INSERT INTO meta (participant_phone)
VALUES ($1)
ON CONFLICT (participant_phone) DO NOTHING`

	tag, err := DB.Exec(context.Background(), query, encryptedPhone) // Execute insert statement.
	if err != nil {                                                  // Check insert execution failure.
		return false, fmt.Errorf("insert participant meta: %w", err) // Return wrapped insert error.
	} // End insert error branch.

	isNewParticipant := tag.RowsAffected() > 0 // Detect whether this call created a new row.
	return isNewParticipant, nil               // Return whether participant was newly inserted.
} // End EnsureParticipantMeta function.

// IsParticipantBaselineComplete returns true if any meta row for this phone (decrypted match) finished baseline.
func IsParticipantBaselineComplete(phone string) (bool, error) {
	normalizedPhone := strings.TrimSpace(phone)
	if normalizedPhone == "" {
		return false, fmt.Errorf("participant phone is empty")
	}
	want := common.DigitsOnly(normalizedPhone)
	if want == "" {
		return false, nil
	}
	rows, err := DB.Query(context.Background(),
		`SELECT has_baseline_questionnaire, baseline_completed_ts, participant_phone FROM meta`)
	if err != nil {
		return false, fmt.Errorf("meta lookup baseline: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var hasBL bool
		var completedAt *time.Time
		var encPhone string
		if err := rows.Scan(&hasBL, &completedAt, &encPhone); err != nil {
			return false, fmt.Errorf("meta scan baseline: %w", err)
		}
		plain, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		if common.DigitsOnly(plain) != want {
			continue
		}
		if hasBL || completedAt != nil {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("meta iterate baseline: %w", err)
	}
	return false, nil
} // End IsParticipantBaselineComplete.

// MarkParticipantBaselineComplete sets baseline completion fields on meta row by primary key.
func MarkParticipantBaselineCompleteByMetaID(metaID int64) error { // After baseline survey INSERT succeeds.
	q := `UPDATE meta SET has_baseline_questionnaire = TRUE, baseline_completed_ts = NOW() WHERE id = $1 AND has_baseline_questionnaire = FALSE AND baseline_completed_ts IS NULL` // Update only first completion.
	_, err := DB.Exec(context.Background(), q, metaID)                                                                                                                             // Execute update.
	if err != nil {                                                                                                                                                                // Check DB error.
		return fmt.Errorf("update meta baseline complete: %w", err) // Wrap.
	}
	return nil // OK.
} // End MarkParticipantBaselineCompleteByMetaID.

// metaIDsForRespondentDigits lists meta.id rows whose decrypted phone matches full digit string.
func metaIDsForRespondentDigits(respondentDigits string) ([]int64, error) {
	want := common.DigitsOnly(strings.TrimSpace(respondentDigits))
	if len(want) < 8 || len(want) > 15 {
		return nil, fmt.Errorf("respondent phone must be 8-15 digits (include country code)")
	}
	rows, err := DB.Query(context.Background(), `SELECT id, participant_phone FROM meta`)
	if err != nil {
		return nil, fmt.Errorf("list meta: %w", err)
	}
	defer rows.Close()
	var matches []int64
	for rows.Next() {
		var id int64
		var encPhone string
		if err := rows.Scan(&id, &encPhone); err != nil {
			return nil, fmt.Errorf("scan meta: %w", err)
		}
		plain, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		if common.DigitsOnly(plain) == want {
			matches = append(matches, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate meta: %w", err)
	}
	return matches, nil
}

// MarkParticipantBaselineCompleteForPhoneDigits sets baseline completion on every meta row for this phone (handles duplicate rows).
func MarkParticipantBaselineCompleteForPhoneDigits(respondentDigits string) (int64, error) {
	return MarkParticipantBaselineCompleteWithIntervalForPhoneDigits(respondentDigits, "")
}

// MarkParticipantBaselineCompleteWithIntervalForPhoneDigits sets baseline completion + message interval on all matching meta rows.
func MarkParticipantBaselineCompleteWithIntervalForPhoneDigits(respondentDigits string, messageInterval string) (int64, error) {
	return MarkParticipantBaselineCompleteWithProfileForPhoneDigits(respondentDigits, messageInterval, "")
}

// MarkParticipantBaselineCompleteWithProfileForPhoneDigits sets baseline completion + profile fields on all matching meta rows.
func MarkParticipantBaselineCompleteWithProfileForPhoneDigits(respondentDigits string, messageInterval string, participantName string) (int64, error) {
	ids, err := metaIDsForRespondentDigits(respondentDigits)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("no meta row matches phone %s", common.DigitsOnly(strings.TrimSpace(respondentDigits)))
	}
	ph := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	interval := strings.TrimSpace(messageInterval)
	name := strings.TrimSpace(participantName)
	args = append(args, interval)
	intervalArg := fmt.Sprintf("$%d", len(args))
	args = append(args, name)
	nameArg := fmt.Sprintf("$%d", len(args))
	q := fmt.Sprintf(
		`UPDATE meta SET has_baseline_questionnaire = TRUE, baseline_completed_ts = NOW(), %s = %s, %s = %s WHERE id IN (%s) AND has_baseline_questionnaire = FALSE AND baseline_completed_ts IS NULL`,
		MetaMessageIntervalColumn, intervalArg, MetaParticipantNameColumn, nameArg, strings.Join(ph, ","),
	)
	tag, err := DB.Exec(context.Background(), q, args...)
	if err != nil {
		return 0, fmt.Errorf("update meta baseline complete: %w", err)
	}
	return tag.RowsAffected(), nil
}

// MarkFollowupCompleteForPhoneDigits sets follow-up completion on every meta row for this phone.
func MarkFollowupCompleteForPhoneDigits(respondentDigits string, surveyID string) (int64, error) {
	ids, err := metaIDsForRespondentDigits(respondentDigits)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("no meta row matches phone %s", common.DigitsOnly(strings.TrimSpace(respondentDigits)))
	}
	tsCol := common.FollowupMetaTimestampColumn(surveyID)
	doneCol := common.FollowupMetaCompletedColumn(surveyID)
	if err := common.ValidateSQLIdentifier(tsCol, "followup meta ts column"); err != nil {
		return 0, err
	}
	if err := common.ValidateSQLIdentifier(doneCol, "followup meta completed column"); err != nil {
		return 0, err
	}
	ph := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := fmt.Sprintf(
		`UPDATE meta SET %s = TRUE, %s = NOW() WHERE id IN (%s) AND %s = FALSE`,
		doneCol, tsCol, strings.Join(ph, ","), doneCol,
	)
	tag, err := DB.Exec(context.Background(), q, args...)
	if err != nil {
		return 0, fmt.Errorf("mark followup complete: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ParticipantBaselineCompletedAt returns the most recent baseline completion timestamp for this participant.
func ParticipantBaselineCompletedAt(phone string) (*time.Time, error) {
	normalizedPhone := strings.TrimSpace(phone)
	if normalizedPhone == "" {
		return nil, fmt.Errorf("participant phone is empty")
	}
	want := common.DigitsOnly(normalizedPhone)
	if want == "" {
		return nil, nil
	}
	rows, err := DB.Query(context.Background(), `SELECT baseline_completed_ts, participant_phone FROM meta`)
	if err != nil {
		return nil, fmt.Errorf("meta lookup baseline timestamp: %w", err)
	}
	defer rows.Close()

	var latest *time.Time
	for rows.Next() {
		var completedAt *time.Time
		var encPhone string
		if err := rows.Scan(&completedAt, &encPhone); err != nil {
			return nil, fmt.Errorf("meta scan baseline timestamp: %w", err)
		}
		plain, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		if common.DigitsOnly(plain) != want || completedAt == nil {
			continue
		}
		if latest == nil || completedAt.After(*latest) {
			t := completedAt.UTC()
			latest = &t
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("meta iterate baseline timestamp: %w", err)
	}
	return latest, nil
}

// IsParticipantEndMessageSent returns true when at least one matching meta row has end_message=true.
func IsParticipantEndMessageSent(phone string) (bool, error) {
	normalizedPhone := strings.TrimSpace(phone)
	if normalizedPhone == "" {
		return false, fmt.Errorf("participant phone is empty")
	}
	want := common.DigitsOnly(normalizedPhone)
	if want == "" {
		return false, nil
	}
	rows, err := DB.Query(context.Background(), `SELECT end_message, participant_phone FROM meta`)
	if err != nil {
		return false, fmt.Errorf("meta lookup end_message: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var endMessageSent bool
		var encPhone string
		if err := rows.Scan(&endMessageSent, &encPhone); err != nil {
			return false, fmt.Errorf("meta scan end_message: %w", err)
		}
		plain, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		if common.DigitsOnly(plain) != want {
			continue
		}
		if endMessageSent {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("meta iterate end_message: %w", err)
	}
	return false, nil
}

// MarkParticipantEndMessageSentForPhoneDigits marks end_message=true on all matching meta rows.
func MarkParticipantEndMessageSentForPhoneDigits(respondentDigits string) (int64, error) {
	ids, err := metaIDsForRespondentDigits(respondentDigits)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("no meta row matches phone %s", common.DigitsOnly(strings.TrimSpace(respondentDigits)))
	}
	ph := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := fmt.Sprintf(`UPDATE meta SET end_message = TRUE WHERE id IN (%s) AND end_message = FALSE`, strings.Join(ph, ","))
	tag, err := DB.Exec(context.Background(), q, args...)
	if err != nil {
		return 0, fmt.Errorf("mark end_message sent: %w", err)
	}
	return tag.RowsAffected(), nil
}

// IsParticipantVerified returns true when at least one matching meta row has verification=true.
func IsParticipantVerified(phone string) (bool, error) {
	normalizedPhone := strings.TrimSpace(phone)
	if normalizedPhone == "" {
		return false, fmt.Errorf("participant phone is empty")
	}
	want := common.DigitsOnly(normalizedPhone)
	if want == "" {
		return false, nil
	}
	rows, err := DB.Query(context.Background(), `SELECT verification, participant_phone FROM meta`)
	if err != nil {
		return false, fmt.Errorf("meta lookup verification: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var verified bool
		var encPhone string
		if err := rows.Scan(&verified, &encPhone); err != nil {
			return false, fmt.Errorf("meta scan verification: %w", err)
		}
		plain, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		if common.DigitsOnly(plain) != want {
			continue
		}
		if verified {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("meta iterate verification: %w", err)
	}
	return false, nil
}

// MarkParticipantVerifiedForPhoneDigits marks verification=true on all matching meta rows.
func MarkParticipantVerifiedForPhoneDigits(respondentDigits string) (int64, error) {
	ids, err := metaIDsForRespondentDigits(respondentDigits)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("no meta row matches phone %s", common.DigitsOnly(strings.TrimSpace(respondentDigits)))
	}
	ph := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := fmt.Sprintf(`UPDATE meta SET verification = TRUE WHERE id IN (%s) AND verification = FALSE`, strings.Join(ph, ","))
	tag, err := DB.Exec(context.Background(), q, args...)
	if err != nil {
		return 0, fmt.Errorf("mark participant verified: %w", err)
	}
	return tag.RowsAffected(), nil
}

// MarkParticipantUnverifiedForPhoneDigits sets verification=false on all matching meta rows.
func MarkParticipantUnverifiedForPhoneDigits(respondentDigits string) (int64, error) {
	ids, err := metaIDsForRespondentDigits(respondentDigits)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("no meta row matches phone %s", common.DigitsOnly(strings.TrimSpace(respondentDigits)))
	}
	ph := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := fmt.Sprintf(`UPDATE meta SET verification = FALSE WHERE id IN (%s) AND verification = TRUE`, strings.Join(ph, ","))
	tag, err := DB.Exec(context.Background(), q, args...)
	if err != nil {
		return 0, fmt.Errorf("mark participant unverified: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SetExcludeFromEngagementForPhoneDigits marks whether a participant is excluded from engagement-rate stats (e.g. test accounts).
func SetExcludeFromEngagementForPhoneDigits(respondentDigits string, exclude bool) (int64, error) {
	ids, err := metaIDsForRespondentDigits(respondentDigits)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("no meta row matches phone %s", common.DigitsOnly(strings.TrimSpace(respondentDigits)))
	}
	ph := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, exclude)
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, id)
	}
	q := fmt.Sprintf(`UPDATE meta SET exclude_from_engagement = $1 WHERE id IN (%s)`, strings.Join(ph, ","))
	tag, err := DB.Exec(context.Background(), q, args...)
	if err != nil {
		return 0, fmt.Errorf("set exclude_from_engagement: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ParticipantExistsForPhoneDigits returns true when at least one meta row matches this phone.
func ParticipantExistsForPhoneDigits(respondentDigits string) (bool, error) {
	ids, err := metaIDsForRespondentDigits(respondentDigits)
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// DeleteParticipantMetaForPhoneDigits deletes all meta rows for one participant phone.
func DeleteParticipantMetaForPhoneDigits(respondentDigits string) (int64, error) {
	ids, err := metaIDsForRespondentDigits(respondentDigits)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	ph := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := fmt.Sprintf(`DELETE FROM meta WHERE id IN (%s)`, strings.Join(ph, ","))
	tag, err := DB.Exec(context.Background(), q, args...)
	if err != nil {
		return 0, fmt.Errorf("delete meta rows: %w", err)
	}
	return tag.RowsAffected(), nil
}
