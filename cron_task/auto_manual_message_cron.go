package cron_task

import (
	"fmt"
	"log"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/messaging"
	"whatsapp-bot/survey"

	"go.mau.fi/whatsmeow"
)

// StartAutoManualMessageCron sends due manual-message rows from auto_message_db.
// It follows the same cron_task_time cadence (UTC) and reacts to config changes within ~1 minute.
func StartAutoManualMessageCron(client *whatsmeow.Client) {
	go func() {
		runAutoManualMessageCronOnce(client) // Immediate run at startup.

		for {
			cfg := survey.GlobalSurveyConfig()
			if cfg == nil {
				time.Sleep(1 * time.Minute)
				continue
			}
			hour, minute, err := parseCronTaskTime(cfg.Project.CronTaskTime)
			if err != nil {
				log.Printf("auto manual cron: invalid cron_task_time: %v", err)
				time.Sleep(5 * time.Minute)
				continue
			}
			wait := durationUntilNextRun(hour, minute, loadCronLocation())
			if wait > time.Minute {
				// Re-check config at least every minute so cron_task_time changes take effect quickly.
				time.Sleep(1 * time.Minute)
				continue
			}
			time.Sleep(wait)
			runAutoManualMessageCronOnce(client)
		}
	}()
}

func runAutoManualMessageCronOnce(client *whatsmeow.Client) {
	if client == nil {
		return
	}
	now := time.Now()
	tasks, err := db.DueAutoManualMessageTasks(now)
	if err != nil {
		log.Printf("auto manual cron: load due tasks error: %v", err)
		return
	}
	for _, task := range tasks {
		if err := sendManualAutoMessageTask(client, task); err != nil {
			log.Printf("auto manual cron: send task error (task=%d): %v", task.ID, err)
		}
	}
}

func sendManualAutoMessageTask(client *whatsmeow.Client, task db.AutoMessageTask) error {
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
	content := strings.TrimSpace(task.MessageContent)
	if content == "" {
		return fmt.Errorf("message content is empty")
	}
	if err := messaging.SendAutoCronMessage(client, plainPhone, content, common.MessageNatureManualMessage); err != nil {
		return fmt.Errorf("send manual message: %w", err)
	}
	if err := db.MarkAutoMessageAsSent(task.ID, content, time.Now()); err != nil {
		return fmt.Errorf("mark sent: %w", err)
	}
	return nil
}
