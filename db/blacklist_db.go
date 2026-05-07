package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"whatsapp-bot/common"
)

type BlacklistEntry struct {
	Phone       string
	Blacklisted time.Time
}

func EnsureBlacklistTableExists() error {
	query := `
CREATE TABLE IF NOT EXISTS blacklist (
    blacklisted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    participant_phone TEXT PRIMARY KEY
);`
	if _, err := DB.Exec(context.Background(), query); err != nil {
		return fmt.Errorf("create blacklist table: %w", err)
	}
	return nil
}

func AddBlacklistedPhone(phoneDigits string) error {
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if len(phone) < 8 || len(phone) > 15 {
		return fmt.Errorf("phone must be 8-15 digits")
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return fmt.Errorf("encrypt blacklist phone: %w", err)
	}
	query := `
INSERT INTO blacklist (participant_phone, blacklisted_at)
VALUES ($1, $2)
ON CONFLICT (participant_phone) DO UPDATE SET blacklisted_at = EXCLUDED.blacklisted_at`
	if _, err := DB.Exec(context.Background(), query, encryptedPhone, time.Now().UTC()); err != nil {
		return fmt.Errorf("upsert blacklist phone: %w", err)
	}
	return nil
}

func RemoveBlacklistedPhone(phoneDigits string) (bool, error) {
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if len(phone) < 8 || len(phone) > 15 {
		return false, fmt.Errorf("phone must be 8-15 digits")
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return false, fmt.Errorf("encrypt blacklist phone: %w", err)
	}
	tag, err := DB.Exec(context.Background(), `DELETE FROM blacklist WHERE participant_phone = $1`, encryptedPhone)
	if err != nil {
		return false, fmt.Errorf("delete blacklist phone: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func IsPhoneBlacklisted(phoneDigits string) (bool, error) {
	phone := common.DigitsOnly(strings.TrimSpace(phoneDigits))
	if len(phone) < 8 || len(phone) > 15 {
		return false, nil
	}
	encryptedPhone, err := common.EncryptPhone(phone)
	if err != nil {
		return false, fmt.Errorf("encrypt blacklist phone: %w", err)
	}
	var count int
	if err := DB.QueryRow(context.Background(), `SELECT COUNT(1) FROM blacklist WHERE participant_phone = $1`, encryptedPhone).Scan(&count); err != nil {
		return false, fmt.Errorf("check blacklist phone: %w", err)
	}
	return count > 0, nil
}

func ListBlacklistedPhones() ([]BlacklistEntry, error) {
	rows, err := DB.Query(context.Background(), `SELECT participant_phone, blacklisted_at FROM blacklist ORDER BY blacklisted_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query blacklist: %w", err)
	}
	defer rows.Close()

	out := make([]BlacklistEntry, 0, 64)
	for rows.Next() {
		var encPhone string
		var ts time.Time
		if err := rows.Scan(&encPhone, &ts); err != nil {
			return nil, fmt.Errorf("scan blacklist row: %w", err)
		}
		plain, err := common.DecryptPhone(encPhone)
		if err != nil {
			plain = "[decrypt-error]"
		}
		out = append(out, BlacklistEntry{
			Phone:       common.DigitsOnly(strings.TrimSpace(plain)),
			Blacklisted: ts,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate blacklist rows: %w", err)
	}
	return out, nil
}
