package db

import (
	"context"
	"fmt"
	"strings"

	"whatsapp-bot/common"
)

func CreateConversationTable() error {
	renameIfNeeded := `
DO $$
BEGIN
    IF to_regclass('public.conversation') IS NULL AND to_regclass('public.ai_memory') IS NOT NULL THEN
        ALTER TABLE ai_memory RENAME TO conversation;
    END IF;
END $$;`
	if _, err := DB.Exec(context.Background(), renameIfNeeded); err != nil {
		return fmt.Errorf("rename ai_memory to conversation: %w", err)
	}

	query := `
CREATE TABLE IF NOT EXISTS conversation (
    id BIGSERIAL PRIMARY KEY,
    participant_phone TEXT NOT NULL DEFAULT '',
    sender TEXT NOT NULL DEFAULT '',
    receiver TEXT NOT NULL DEFAULT '',
    direction TEXT NOT NULL DEFAULT 'outbound',
    nature TEXT NOT NULL DEFAULT 'manual_message',
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	if _, err := DB.Exec(context.Background(), query); err != nil {
		return fmt.Errorf("create conversation table: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE conversation ADD COLUMN IF NOT EXISTS participant_phone TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("alter conversation add participant_phone: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE conversation ADD COLUMN IF NOT EXISTS sender TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("alter conversation add sender: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE conversation ADD COLUMN IF NOT EXISTS receiver TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("alter conversation add receiver: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE conversation ADD COLUMN IF NOT EXISTS direction TEXT NOT NULL DEFAULT 'outbound'`); err != nil {
		return fmt.Errorf("alter conversation add direction: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE conversation ADD COLUMN IF NOT EXISTS nature TEXT NOT NULL DEFAULT 'manual_message'`); err != nil {
		return fmt.Errorf("alter conversation add nature: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE conversation ADD COLUMN IF NOT EXISTS content TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("alter conversation add content: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `ALTER TABLE conversation ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP`); err != nil {
		return fmt.Errorf("alter conversation add created_at: %w", err)
	}
	return nil
}

func SaveMessage(msg common.Message) {
	normalizedDirection := strings.TrimSpace(msg.Direction)
	if normalizedDirection == "" {
		normalizedDirection = "outbound"
	}
	normalizedNature := strings.TrimSpace(msg.Nature)
	if normalizedNature == "" {
		if normalizedDirection == "inbound" {
			normalizedNature = common.MessageNatureClientMessage
		} else {
			normalizedNature = common.MessageNatureManualMessage
		}
	}
	normalizedSender := strings.TrimSpace(msg.Sender)
	normalizedReceiver := strings.TrimSpace(msg.Receiver)
	normalizedContent := strings.TrimSpace(msg.Content)
	if normalizedSender == "" || normalizedReceiver == "" || normalizedContent == "" {
		fmt.Println("conversation insert skipped: sender/receiver/content is empty")
		return
	}
	participantPhone := normalizedReceiver
	if normalizedDirection == "inbound" {
		participantPhone = normalizedSender
	}

	encryptedParticipantPhone, err := common.EncryptPhone(participantPhone)
	if err != nil {
		fmt.Println("Phone encryption error (participant_phone):", err)
		return
	}
	encryptedSender, err := common.EncryptPhone(normalizedSender)
	if err != nil {
		fmt.Println("Phone encryption error (sender):", err)
		return
	}

	encryptedReceiver, err := common.EncryptPhone(normalizedReceiver)
	if err != nil {
		fmt.Println("Phone encryption error (receiver):", err)
		return
	}

	query := `
INSERT INTO conversation (participant_phone, sender, receiver, direction, nature, content)
VALUES ($1, $2, $3, $4, $5, $6)`

	_, err = DB.Exec(
		context.Background(),
		query,
		encryptedParticipantPhone,
		encryptedSender,
		encryptedReceiver,
		normalizedDirection,
		normalizedNature,
		normalizedContent,
	)
	if err != nil {
		fmt.Println("DB insert error:", err)
		return
	}
}

// DeleteConversationByParticipantPhoneDigits removes all conversation rows for one participant phone.
func DeleteConversationByParticipantPhoneDigits(phoneDigits string) (int64, error) {
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if phone == "" {
		return 0, fmt.Errorf("participant phone is empty")
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return 0, fmt.Errorf("encrypt participant phone: %w", err)
	}
	tag, err := DB.Exec(context.Background(), `DELETE FROM conversation WHERE participant_phone = $1`, encryptedPhone)
	if err != nil {
		return 0, fmt.Errorf("delete participant conversation rows: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteConversationByID removes one conversation row by primary key.
func DeleteConversationByID(id int64) (bool, error) {
	if id <= 0 {
		return false, fmt.Errorf("conversation id must be positive")
	}
	tag, err := DB.Exec(context.Background(), `DELETE FROM conversation WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("delete conversation row by id: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
