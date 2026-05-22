package cron_task

import (
	"fmt"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/messaging"
	"whatsapp-bot/survey"

	"go.mau.fi/whatsmeow"
)

const (
	autoMessageNatureAI       = "AI message"
	autoMessageNatureFollowup = "follow-up prompt"
	autoMessageNatureManual   = "manual message"
)

// RetryPastDueAutoMessageTask sends one unsent auto_message_db row whose scheduled instant is already
// past (server clock), then marks it sent. Used for admin recovery of missed cron sends (including same calendar day).
func RetryPastDueAutoMessageTask(client *whatsmeow.Client, taskID int64) error {
	if client == nil {
		return fmt.Errorf("WhatsApp client is not ready")
	}
	if taskID <= 0 {
		return fmt.Errorf("invalid task id")
	}
	task, err := db.GetAutoMessageTaskByID(taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return fmt.Errorf("task not found")
	}
	if task.IsSent {
		return fmt.Errorf("task already marked as sent")
	}
	if !task.ScheduledAt.Before(time.Now()) {
		return fmt.Errorf("retry is only allowed after the scheduled time has passed")
	}
	phone, err := common.DecryptPhone(task.EncryptedPhone)
	if err != nil {
		return fmt.Errorf("decrypt participant phone: %w", err)
	}
	plainPhone := common.DigitsOnly(strings.TrimSpace(phone))
	if plainPhone == "" {
		return fmt.Errorf("participant phone is empty")
	}
	blacklisted, err := db.IsPhoneBlacklisted(plainPhone)
	if err != nil {
		return fmt.Errorf("blacklist lookup: %w", err)
	}
	if blacklisted {
		return fmt.Errorf("participant is blacklisted")
	}

	nature := strings.TrimSpace(task.Nature)
	var content string
	switch nature {
	case autoMessageNatureAI:
		var genErr error
		content, genErr = composeAutoAIMessageWithContext(plainPhone)
		if genErr != nil {
			content = composeAutoMessageText()
		}
		if err := messaging.SendAutoCronMessage(client, plainPhone, content, common.MessageNatureCronAIMessage); err != nil {
			return fmt.Errorf("send auto AI message: %w", err)
		}
	case autoMessageNatureFollowup:
		cfg := survey.GlobalSurveyConfig()
		if cfg == nil {
			return fmt.Errorf("survey config not loaded")
		}
		fu, ok := findFollowupBySurveyID(cfg, task.FollowupSurvey)
		if !ok {
			return fmt.Errorf("follow-up survey not found for id %q", task.FollowupSurvey)
		}
		var compErr error
		content, compErr = composeFollowupPromptMessage(fu, plainPhone)
		if compErr != nil {
			return fmt.Errorf("compose follow-up prompt: %w", compErr)
		}
		if err := messaging.SendAutoCronMessage(client, plainPhone, content, common.MessageNatureCronFollowupInvitation); err != nil {
			return fmt.Errorf("send follow-up prompt: %w", err)
		}
	case autoMessageNatureManual:
		content = strings.TrimSpace(task.MessageContent)
		if content == "" {
			return fmt.Errorf("manual message content is empty")
		}
		if err := messaging.SendAutoCronMessage(client, plainPhone, content, common.MessageNatureManualMessage); err != nil {
			return fmt.Errorf("send manual message: %w", err)
		}
	default:
		return fmt.Errorf("unsupported nature %q (only %q, %q, and %q can be retried)", nature, autoMessageNatureAI, autoMessageNatureFollowup, autoMessageNatureManual)
	}

	if err := db.MarkAutoMessageAsSent(taskID, content, time.Now()); err != nil {
		return fmt.Errorf("mark task sent: %w", err)
	}
	return nil
}
