package main

import (
	"time"

	"whatsapp-bot/db"
	"whatsapp-bot/survey"
)

func isParticipantInterventionEnded(phone string, now time.Time) (bool, error) {
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil {
		return false, nil
	}
	periodDays := cfg.Project.InterventionPeriod
	if periodDays <= 0 {
		return false, nil
	}
	baselineCompletedAt, err := db.ParticipantBaselineCompletedAt(phone)
	if err != nil {
		return false, err
	}
	if baselineCompletedAt == nil || baselineCompletedAt.IsZero() {
		return false, nil
	}
	current := now.UTC()
	if current.IsZero() {
		current = time.Now().UTC()
	}
	interventionEndAt := baselineCompletedAt.UTC().AddDate(0, 0, periodDays)
	return !current.Before(interventionEndAt), nil
}
