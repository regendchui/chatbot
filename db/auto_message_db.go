package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"whatsapp-bot/common"

	"github.com/jackc/pgx/v5"
)

// AutoMessageTask stores one scheduled outbound self-initiated message.
type AutoMessageTask struct {
	ID             int64
	EncryptedPhone string
	ScheduledAt    time.Time
	IsSent         bool
	SentAt         *time.Time
	Nature         string
	FollowupSurvey string
	MessageContent string
}

func EnsureAutoMessageInfrastructure() {
	createAutoMessageTable()
	if err := CreateConversationTable(); err != nil {
		panic(fmt.Errorf("ensure conversation table: %w", err))
	}
}

func createAutoMessageTable() {
	tableDDL := `
CREATE TABLE IF NOT EXISTS auto_message_db (
    id BIGSERIAL PRIMARY KEY,
    participant_phone TEXT NOT NULL,
    scheduled_time TIMESTAMPTZ NOT NULL,
    is_sent BOOLEAN NOT NULL DEFAULT FALSE,
    sent_timestamp TIMESTAMPTZ NULL,
    nature TEXT NOT NULL DEFAULT 'AI message',
    followup_survey_id TEXT NOT NULL DEFAULT '',
    message_content TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	if _, err := DB.Exec(context.Background(), tableDDL); err != nil {
		panic(fmt.Errorf("create auto_message_db table: %w", err))
	}
	indexDDL := `
CREATE UNIQUE INDEX IF NOT EXISTS auto_message_db_unique_schedule
ON auto_message_db (participant_phone, scheduled_time, nature, followup_survey_id);`
	if _, err := DB.Exec(context.Background(), indexDDL); err != nil {
		panic(fmt.Errorf("create auto_message_db index: %w", err))
	}
}

func InsertAutoMessageSchedule(phoneDigits string, scheduledAt time.Time) error {
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if phone == "" {
		return fmt.Errorf("phone is empty")
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return fmt.Errorf("encrypt participant phone: %w", err)
	}
	query := `
INSERT INTO auto_message_db (participant_phone, scheduled_time, nature)
VALUES ($1, $2, 'AI message')
ON CONFLICT (participant_phone, scheduled_time, nature, followup_survey_id) DO NOTHING`
	if _, err := DB.Exec(context.Background(), query, encryptedPhone, scheduledAt.UTC()); err != nil {
		return fmt.Errorf("insert auto message schedule: %w", err)
	}
	return nil
}

func InsertAutoFollowupMessageSchedule(phoneDigits string, scheduledAt time.Time, surveyID string) error {
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if phone == "" {
		return fmt.Errorf("phone is empty")
	}
	normalizedSurveyID := strings.TrimSpace(surveyID)
	if normalizedSurveyID == "" {
		return fmt.Errorf("follow-up survey_id is empty")
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return fmt.Errorf("encrypt participant phone: %w", err)
	}
	query := `
INSERT INTO auto_message_db (participant_phone, scheduled_time, nature, followup_survey_id)
VALUES ($1, $2, 'follow-up prompt', $3)
ON CONFLICT (participant_phone, scheduled_time, nature, followup_survey_id) DO NOTHING`
	if _, err := DB.Exec(context.Background(), query, encryptedPhone, scheduledAt.UTC(), normalizedSurveyID); err != nil {
		return fmt.Errorf("insert auto follow-up message schedule: %w", err)
	}
	return nil
}

// DueAutoMessageTasks returns unsent AI tasks whose scheduled_time falls on the civil calendar day
// of now in the admin panel offset (ADMIN_PANEL_UTC_OFFSET_HOURS). The daily cron at cron_task_time
// is the send moment; same-day rows are included even if their clock time is later in the day.
func DueAutoMessageTasks(now time.Time) ([]AutoMessageTask, error) {
	startUTC, endUTC := AdminLocalDayRangeUTC(now)
	query := `
SELECT id, participant_phone, scheduled_time, is_sent, sent_timestamp, nature, followup_survey_id, message_content
FROM auto_message_db
WHERE is_sent = FALSE
  AND nature = 'AI message'
  AND scheduled_time >= $1 AND scheduled_time < $2
ORDER BY scheduled_time ASC, id ASC`
	rows, err := DB.Query(context.Background(), query, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("query due auto messages: %w", err)
	}
	defer rows.Close()

	out := []AutoMessageTask{}
	for rows.Next() {
		var row AutoMessageTask
		if err := rows.Scan(&row.ID, &row.EncryptedPhone, &row.ScheduledAt, &row.IsSent, &row.SentAt, &row.Nature, &row.FollowupSurvey, &row.MessageContent); err != nil {
			return nil, fmt.Errorf("scan due auto message: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due auto messages: %w", err)
	}
	return out, nil
}

// DueAutoFollowupMessageTasks is the follow-up counterpart to DueAutoMessageTasks (same day/window rules).
func DueAutoFollowupMessageTasks(now time.Time) ([]AutoMessageTask, error) {
	startUTC, endUTC := AdminLocalDayRangeUTC(now)
	query := `
SELECT id, participant_phone, scheduled_time, is_sent, sent_timestamp, nature, followup_survey_id, message_content
FROM auto_message_db
WHERE is_sent = FALSE
  AND nature = 'follow-up prompt'
  AND scheduled_time >= $1 AND scheduled_time < $2
ORDER BY scheduled_time ASC, id ASC`
	rows, err := DB.Query(context.Background(), query, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("query due auto follow-up messages: %w", err)
	}
	defer rows.Close()

	out := []AutoMessageTask{}
	for rows.Next() {
		var row AutoMessageTask
		if err := rows.Scan(&row.ID, &row.EncryptedPhone, &row.ScheduledAt, &row.IsSent, &row.SentAt, &row.Nature, &row.FollowupSurvey, &row.MessageContent); err != nil {
			return nil, fmt.Errorf("scan due auto follow-up message: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due auto follow-up messages: %w", err)
	}
	return out, nil
}

// DueAutoManualMessageTasks returns unsent manual-message tasks for today's admin-local day window.
func DueAutoManualMessageTasks(now time.Time) ([]AutoMessageTask, error) {
	startUTC, endUTC := AdminLocalDayRangeUTC(now)
	query := `
SELECT id, participant_phone, scheduled_time, is_sent, sent_timestamp, nature, followup_survey_id, message_content
FROM auto_message_db
WHERE is_sent = FALSE
  AND nature = 'manual message'
  AND scheduled_time >= $1 AND scheduled_time < $2
ORDER BY scheduled_time ASC, id ASC`
	rows, err := DB.Query(context.Background(), query, startUTC, endUTC)
	if err != nil {
		return nil, fmt.Errorf("query due manual messages: %w", err)
	}
	defer rows.Close()

	out := []AutoMessageTask{}
	for rows.Next() {
		var row AutoMessageTask
		if err := rows.Scan(&row.ID, &row.EncryptedPhone, &row.ScheduledAt, &row.IsSent, &row.SentAt, &row.Nature, &row.FollowupSurvey, &row.MessageContent); err != nil {
			return nil, fmt.Errorf("scan due manual message: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due manual messages: %w", err)
	}
	return out, nil
}

// GetAutoMessageTaskByID loads one auto_message_db row by id, or nil if not found.
func GetAutoMessageTaskByID(id int64) (*AutoMessageTask, error) {
	if id <= 0 {
		return nil, nil
	}
	row := DB.QueryRow(context.Background(), `
SELECT id, participant_phone, scheduled_time, is_sent, sent_timestamp, nature, followup_survey_id, message_content
FROM auto_message_db
WHERE id = $1`, id)
	var t AutoMessageTask
	var sentAt *time.Time
	if err := row.Scan(&t.ID, &t.EncryptedPhone, &t.ScheduledAt, &t.IsSent, &sentAt, &t.Nature, &t.FollowupSurvey, &t.MessageContent); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load auto_message_db row: %w", err)
	}
	if sentAt != nil {
		t.SentAt = sentAt
	}
	return &t, nil
}

func MarkAutoMessageAsSent(taskID int64, messageContent string, sentAt time.Time) error {
	query := `
UPDATE auto_message_db
SET is_sent = TRUE,
    sent_timestamp = $2,
    message_content = $3
WHERE id = $1 AND is_sent = FALSE`
	if _, err := DB.Exec(context.Background(), query, taskID, sentAt.UTC(), strings.TrimSpace(messageContent)); err != nil {
		return fmt.Errorf("mark auto message sent: %w", err)
	}
	return nil
}

// DeleteAutoMessageTaskByID deletes one auto_message_db row by id. Returns whether a row was removed.
func DeleteAutoMessageTaskByID(id int64) (bool, error) {
	if id <= 0 {
		return false, nil
	}
	cmd, err := DB.Exec(context.Background(), `DELETE FROM auto_message_db WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("delete auto_message_db row: %w", err)
	}
	return cmd.RowsAffected() > 0, nil
}

// InsertAutoMessageManualRow inserts one auto_message_db row (admin manual entry). scheduledUTC and optional sentAtUTC must already be UTC.
func InsertAutoMessageManualRow(phoneDigits string, scheduledUTC time.Time, isSent bool, sentAtUTC *time.Time, nature string, followupSurveyID string, messageContent string) error {
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if phone == "" {
		return fmt.Errorf("participant phone is empty")
	}
	nat := strings.TrimSpace(nature)
	switch nat {
	case "AI message", "follow-up prompt", "manual message":
	default:
		return fmt.Errorf("nature must be %q, %q, or %q", "AI message", "follow-up prompt", "manual message")
	}
	fu := strings.TrimSpace(followupSurveyID)
	if nat == "follow-up prompt" && fu == "" {
		return fmt.Errorf("follow-up survey id is required for follow-up prompt rows")
	}
	if nat == "AI message" || nat == "manual message" {
		fu = ""
	}
	if nat == "manual message" && strings.TrimSpace(messageContent) == "" {
		return fmt.Errorf("message content is required for manual message rows")
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return fmt.Errorf("encrypt participant phone: %w", err)
	}
	var sentPtr interface{}
	if isSent {
		if sentAtUTC != nil && !sentAtUTC.IsZero() {
			t := sentAtUTC.UTC()
			sentPtr = t
		} else {
			sentPtr = time.Now().UTC()
		}
	} else {
		sentPtr = nil
	}
	query := `
INSERT INTO auto_message_db (participant_phone, scheduled_time, is_sent, sent_timestamp, nature, followup_survey_id, message_content)
VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err = DB.Exec(context.Background(), query,
		encryptedPhone,
		scheduledUTC.UTC(),
		isSent,
		sentPtr,
		nat,
		fu,
		strings.TrimSpace(messageContent),
	)
	if err != nil {
		return fmt.Errorf("insert auto_message_db row: %w", err)
	}
	return nil
}

func DeletePendingAutoFollowupSchedules(phoneDigits string, surveyID string) error {
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if phone == "" {
		return fmt.Errorf("phone is empty")
	}
	normalizedSurveyID := strings.TrimSpace(surveyID)
	if normalizedSurveyID == "" {
		return fmt.Errorf("follow-up survey_id is empty")
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return fmt.Errorf("encrypt participant phone: %w", err)
	}
	query := `
DELETE FROM auto_message_db
WHERE participant_phone = $1
  AND nature = 'follow-up prompt'
  AND followup_survey_id = $2
  AND is_sent = FALSE`
	if _, err := DB.Exec(context.Background(), query, encryptedPhone, normalizedSurveyID); err != nil {
		return fmt.Errorf("delete pending auto follow-up schedules: %w", err)
	}
	return nil
}

// DeleteAutoMessagesByParticipantPhoneDigits removes all scheduled/sent auto_message_db rows for one participant phone.
func DeleteAutoMessagesByParticipantPhoneDigits(phoneDigits string) (int64, error) {
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if phone == "" {
		return 0, fmt.Errorf("phone is empty")
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return 0, fmt.Errorf("encrypt participant phone: %w", err)
	}
	tag, err := DB.Exec(context.Background(), `DELETE FROM auto_message_db WHERE participant_phone = $1`, encryptedPhone)
	if err != nil {
		return 0, fmt.Errorf("delete participant auto_message rows: %w", err)
	}
	return tag.RowsAffected(), nil
}
