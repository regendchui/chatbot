package cron_task

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/messaging"
	"whatsapp-bot/survey"

	"github.com/jackc/pgx/v5"
	"go.mau.fi/whatsmeow"
)

func ScheduleAutoFollowupMessagesForParticipant(phoneDigits string) error {
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil {
		return fmt.Errorf("survey config not loaded")
	}
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if phone == "" {
		return fmt.Errorf("participant phone is empty")
	}
	blacklisted, err := db.IsPhoneBlacklisted(phone)
	if err != nil {
		return fmt.Errorf("check blacklist before FU schedule: %w", err)
	}
	if blacklisted {
		return nil
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return fmt.Errorf("encrypt participant phone: %w", err)
	}

	var baselineCompletedTS time.Time
	query := `
SELECT baseline_completed_ts
FROM meta
WHERE participant_phone = $1
  AND has_baseline_questionnaire = TRUE
  AND baseline_completed_ts IS NOT NULL
LIMIT 1`
	if err := db.DB.QueryRow(context.Background(), query, encryptedPhone).Scan(&baselineCompletedTS); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // No matching participant in meta; skip scheduling by design.
		}
		return fmt.Errorf("load participant baseline completion for FU schedule: %w", err)
	}

	hour, minute, err := parseCronTaskTime(cfg.Project.CronTaskTime)
	if err != nil {
		return err
	}
	utcLoc := loadCronLocation()
	baselineDateUTC := time.Date(
		baselineCompletedTS.In(utcLoc).Year(),
		baselineCompletedTS.In(utcLoc).Month(),
		baselineCompletedTS.In(utcLoc).Day(),
		0, 0, 0, 0,
		utcLoc,
	)

	for _, fu := range cfg.Followups {
		surveyID := strings.TrimSpace(fu.SurveyID)
		if surveyID == "" {
			continue
		}
		sendFrequency := fu.Trigger.SendFrequency
		reminderInterval := fu.Trigger.ReminderInterval
		startOffsetDays := fu.Trigger.Time
		if sendFrequency <= 0 {
			continue
		}
		if reminderInterval <= 0 {
			reminderInterval = 1
		}
		if startOffsetDays < 0 {
			startOffsetDays = 0
		}

		for i := 0; i < sendFrequency; i++ {
			dayOffset := startOffsetDays + (i * reminderInterval)
			d := baselineDateUTC.AddDate(0, 0, dayOffset)
			scheduledAt := time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, utcLoc)
			if !scheduledAt.After(baselineCompletedTS) {
				continue
			}
			if err := db.InsertAutoFollowupMessageSchedule(phone, scheduledAt, surveyID); err != nil {
				return err
			}
		}
	}
	return nil
}

func StartAutoFollowupMessageCron(client *whatsmeow.Client) {
	go func() {
		runAutoFollowupMessageCronOnce(client) // Immediate run at startup.

		for {
			cfg := survey.GlobalSurveyConfig()
			if cfg == nil {
				time.Sleep(1 * time.Minute)
				continue
			}
			hour, minute, err := parseCronTaskTime(cfg.Project.CronTaskTime)
			if err != nil {
				log.Printf("auto follow-up cron: invalid cron_task_time: %v", err)
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
			runAutoFollowupMessageCronOnce(client)
		}
	}()
}

func runAutoFollowupMessageCronOnce(client *whatsmeow.Client) {
	if client == nil {
		return
	}
	now := time.Now()
	tasks, err := db.DueAutoFollowupMessageTasks(now)
	if err != nil {
		log.Printf("auto follow-up cron: load due tasks error: %v", err)
		return
	}
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil {
		return
	}
	for _, task := range tasks {
		phone, err := common.DecryptPhone(task.EncryptedPhone)
		if err != nil {
			log.Printf("auto follow-up cron: decrypt participant phone error (task=%d): %v", task.ID, err)
			continue
		}
		plainPhone := common.DigitsOnly(strings.TrimSpace(phone))
		if plainPhone == "" {
			continue
		}
		blacklisted, err := db.IsPhoneBlacklisted(plainPhone)
		if err != nil {
			log.Printf("auto follow-up cron: blacklist lookup error (task=%d): %v", task.ID, err)
			continue
		}
		if blacklisted {
			log.Printf("auto follow-up cron: suspended for blacklisted participant (task=%d phone=%s)", task.ID, plainPhone)
			continue
		}
		fu, ok := findFollowupBySurveyID(cfg, task.FollowupSurvey)
		if !ok {
			log.Printf("auto follow-up cron: survey not found for task=%d survey_id=%s", task.ID, task.FollowupSurvey)
			continue
		}
		content, err := composeFollowupPromptMessage(fu)
		if err != nil {
			log.Printf("auto follow-up cron: compose prompt failed for task=%d survey_id=%s: %v", task.ID, task.FollowupSurvey, err)
			continue
		}
		if err := messaging.SendAutoCronMessage(client, plainPhone, content, common.MessageNatureCronFollowupInvitation); err != nil {
			log.Printf("auto follow-up cron: send task error (task=%d): %v", task.ID, err)
			continue
		}
		if err := db.MarkAutoMessageAsSent(task.ID, content, time.Now()); err != nil {
			log.Printf("auto follow-up cron: mark sent error (task=%d): %v", task.ID, err)
		}
	}
}

func composeFollowupPromptMessage(fu survey.SurveyFollow) (string, error) {
	url, err := survey.FollowupSurveyURL(fu.LinkSlug)
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(fu.InvitationText)
	if body == "" {
		body = "Please complete your follow-up questionnaire."
	}
	return body + "\n" + url, nil
}

func findFollowupBySurveyID(cfg *survey.SurveyConfig, surveyID string) (survey.SurveyFollow, bool) {
	if cfg == nil {
		return survey.SurveyFollow{}, false
	}
	target := strings.TrimSpace(surveyID)
	if target == "" {
		return survey.SurveyFollow{}, false
	}
	for _, fu := range cfg.Followups {
		if strings.TrimSpace(fu.SurveyID) == target {
			return fu, true
		}
	}
	return survey.SurveyFollow{}, false
}
