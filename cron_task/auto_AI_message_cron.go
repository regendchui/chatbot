package cron_task

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"whatsapp-bot/AI"
	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/messaging"
	"whatsapp-bot/survey"

	"github.com/jackc/pgx/v5"
	"go.mau.fi/whatsmeow"
)

const cronTimeZoneName = "UTC"

func ScheduleAutoAIMessagesForParticipant(phoneDigits string) error {
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
		return fmt.Errorf("check blacklist before AI schedule: %w", err)
	}
	if blacklisted {
		return nil
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return fmt.Errorf("encrypt participant phone: %w", err)
	}

	var baselineCompletedTS time.Time
	var intervalRaw string
	query := `
SELECT baseline_completed_ts, message_interval
FROM meta
WHERE participant_phone = $1
  AND has_baseline_questionnaire = TRUE
  AND baseline_completed_ts IS NOT NULL
LIMIT 1`
	if err := db.DB.QueryRow(context.Background(), query, encryptedPhone).Scan(&baselineCompletedTS, &intervalRaw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // Missing meta row means no cron schedule for this phone by design.
		}
		return fmt.Errorf("load participant meta for AI schedule: %w", err)
	}

	weekdays, everyNWeeks, err := weekdaysForInterval(cfg.Project.ChatbotScheduleQuestions, intervalRaw)
	if err != nil {
		return nil
	}
	hour, minute, err := parseCronTaskTime(cfg.Project.CronTaskTime)
	if err != nil {
		return err
	}
	periodDays := cfg.Project.InterventionPeriod
	if periodDays <= 0 {
		return nil
	}

	utcLoc := loadCronLocation()
	scheduledTimes := buildParticipantSchedule(baselineCompletedTS.In(utcLoc), periodDays, weekdays, everyNWeeks, hour, minute, utcLoc)
	for _, scheduledAt := range scheduledTimes {
		// Do not insert slots at or before baseline completion. Otherwise the first weekday
		// + cron_task_time (UTC) can fall earlier the same wall-clock day in +offset zones,
		// producing a "missed" row immediately after submit.
		if !scheduledAt.After(baselineCompletedTS) {
			continue
		}
		if err := db.InsertAutoMessageSchedule(phone, scheduledAt); err != nil {
			return err
		}
	}
	return nil
}

func StartAutoAIMessageCron(client *whatsmeow.Client) {
	go func() {
		runAutoAIMessageCronOnce(client) // Immediate run at startup.

		for {
			cfg := survey.GlobalSurveyConfig()
			if cfg == nil {
				time.Sleep(1 * time.Minute)
				continue
			}
			hour, minute, err := parseCronTaskTime(cfg.Project.CronTaskTime)
			if err != nil {
				log.Printf("auto AI cron: invalid cron_task_time: %v", err)
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
			runAutoAIMessageCronOnce(client)
		}
	}()
}

func runAutoAIMessageCronOnce(client *whatsmeow.Client) {
	if client == nil {
		return
	}
	now := time.Now()
	tasks, err := db.DueAutoMessageTasks(now)
	if err != nil {
		log.Printf("auto AI cron: load due tasks error: %v", err)
		return
	}
	for _, task := range tasks {
		phone, err := common.DecryptPhone(task.EncryptedPhone)
		if err != nil {
			log.Printf("auto AI cron: decrypt participant phone error (task=%d): %v", task.ID, err)
			continue
		}
		plainPhone := common.DigitsOnly(strings.TrimSpace(phone))
		if plainPhone == "" {
			continue
		}
		blacklisted, err := db.IsPhoneBlacklisted(plainPhone)
		if err != nil {
			log.Printf("auto AI cron: blacklist lookup error (task=%d): %v", task.ID, err)
			continue
		}
		if blacklisted {
			log.Printf("auto AI cron: suspended for blacklisted participant (task=%d phone=%s)", task.ID, plainPhone)
			continue
		}
		content, err := composeAutoAIMessageWithContext(plainPhone)
		if err != nil {
			log.Printf("auto AI cron: generate contextual message error (task=%d): %v", task.ID, err)
			content = composeAutoMessageText() // Fallback to static text when AI generation fails.
		}
		if err := messaging.SendAutoCronMessage(client, plainPhone, content, common.MessageNatureCronAIMessage); err != nil {
			log.Printf("auto AI cron: send task error (task=%d): %v", task.ID, err)
			continue
		}
		if err := db.MarkAutoMessageAsSent(task.ID, content, time.Now()); err != nil {
			log.Printf("auto AI cron: mark sent error (task=%d): %v", task.ID, err)
		}
	}
}

func composeAutoMessageText() string {
	msg := strings.TrimSpace(os.Getenv("AUTO_MESSAGE_TEXT"))
	if msg == "" {
		return "Hello, this is your scheduled check-in message from the chatbot."
	}
	return msg
}

func composeAutoAIMessageWithContext(participantPhone string) (string, error) {
	phone := common.DigitsOnly(strings.TrimSpace(participantPhone))
	if phone == "" {
		return "", fmt.Errorf("participant phone is empty")
	}

	memoryMessages, err := ai.GetLastMessagesForParticipant(phone, ai.GetAIMemoryMessageLimitFromEnv())
	if err != nil {
		return "", fmt.Errorf("load chat memory: %w", err)
	}
	surveyContext, err := ai.BuildParticipantSurveyContextForAI(phone)
	if err != nil {
		return "", fmt.Errorf("load survey context: %w", err)
	}
	phaseContext, err := ai.BuildParticipantPhaseContextForAI(phone, time.Now())
	if err != nil {
		return "", fmt.Errorf("load phase context: %w", err)
	}

	instruction := strings.TrimSpace(os.Getenv("AUTO_MESSAGE_PROMPT"))
	if instruction == "" {
		instruction = "This is a scheduled proactive check-in. Write one short, supportive WhatsApp message."
	}
	msg, err := ai.GenerateAIResponse(instruction, memoryMessages, surveyContext, phaseContext, ai.LatestInboundMediumNone)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(msg), nil
}

func parseCronTaskTime(raw int) (int, int, error) {
	if raw < 0 || raw > 2359 {
		return 0, 0, fmt.Errorf("cron_task_time must be between 0000 and 2359")
	}
	hour := raw / 100
	minute := raw % 100
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("cron_task_time has invalid hour/minute")
	}
	return hour, minute, nil
}

func buildParticipantSchedule(baselineHK time.Time, periodDays int, weekdays []time.Weekday, everyNWeeks int, hour int, minute int, loc *time.Location) []time.Time {
	startDate := time.Date(baselineHK.Year(), baselineHK.Month(), baselineHK.Day(), 0, 0, 0, 0, loc)
	endDate := startDate.AddDate(0, 0, periodDays)
	weekdaySet := map[time.Weekday]struct{}{}
	for _, wd := range weekdays {
		weekdaySet[wd] = struct{}{}
	}
	baseWeekStart := startOfWeekMonday(startDate)
	out := []time.Time{}
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		if _, ok := weekdaySet[d.Weekday()]; !ok {
			continue
		}
		if everyNWeeks > 1 {
			weeksSinceStart := int(startOfWeekMonday(d).Sub(baseWeekStart).Hours() / (24 * 7))
			if weeksSinceStart%everyNWeeks != 0 {
				continue
			}
		}
		out = append(out, time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, loc))
	}
	return out
}

func weekdaysForInterval(scheduleCfg survey.ChatbotScheduleDays, intervalRaw string) ([]time.Weekday, int, error) {
	key := normalizeMessageInterval(intervalRaw)
	var out []time.Weekday
	var everyNWeeks int
	switch key {
	case "once_per_one_week":
		out = parseWeekdays(scheduleCfg.OncePerOneWeek)
		everyNWeeks = 1
	case "twice_per_one_week":
		out = parseWeekdays(scheduleCfg.TwicePerOneWeek)
		everyNWeeks = 1
	case "once_per_two_weeks":
		out = parseWeekdays(scheduleCfg.OncePerTwoWeeks)
		everyNWeeks = 2
	default:
		return nil, 0, fmt.Errorf("unsupported message interval: %s", intervalRaw)
	}
	if len(out) == 0 {
		return nil, 0, fmt.Errorf("no valid weekday configured for interval: %s", intervalRaw)
	}
	return out, everyNWeeks, nil
}

func normalizeMessageInterval(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Join(strings.Fields(s), " ")
	switch s {
	case "once per one week", "once per week":
		return "once_per_one_week"
	case "twice per one week", "twice per week":
		return "twice_per_one_week"
	case "once per two weeks", "once every two weeks":
		return "once_per_two_weeks"
	default:
		return s
	}
}

func parseWeekdays(days []string) []time.Weekday {
	out := []time.Weekday{}
	seen := map[time.Weekday]struct{}{}
	for _, d := range days {
		wd, ok := parseWeekdayName(d)
		if !ok {
			continue
		}
		if _, exists := seen[wd]; exists {
			continue
		}
		seen[wd] = struct{}{}
		out = append(out, wd)
	}
	return out
}

func parseWeekdayName(name string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sunday":
		return time.Sunday, true
	case "monday":
		return time.Monday, true
	case "tuesday":
		return time.Tuesday, true
	case "wednesday":
		return time.Wednesday, true
	case "thursday":
		return time.Thursday, true
	case "friday":
		return time.Friday, true
	case "saturday":
		return time.Saturday, true
	default:
		return time.Sunday, false
	}
}

func loadCronLocation() *time.Location {
	loc, err := time.LoadLocation(cronTimeZoneName)
	if err != nil {
		return time.UTC
	}
	return loc
}

func durationUntilNextRun(hour int, minute int, loc *time.Location) time.Duration {
	now := time.Now().In(loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(now)
}

func startOfWeekMonday(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7 // Monday=>0 ... Sunday=>6.
	day := t.AddDate(0, 0, -offset)
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
}
