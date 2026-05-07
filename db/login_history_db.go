package db

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type LoginHistoryRow struct {
	ID          int64
	Username    string
	UserType    string
	Success     bool
	FailureType string
	ClientIP    string
	CreatedAt   time.Time
}

func EnsureLoginHistoryTableExists() error {
	query := `
CREATE TABLE IF NOT EXISTS login_history (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL DEFAULT '',
    user_type TEXT NOT NULL DEFAULT '',
    success BOOLEAN NOT NULL DEFAULT false,
    failure_type TEXT NOT NULL DEFAULT '',
    client_ip TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	if _, err := DB.Exec(context.Background(), query); err != nil {
		return fmt.Errorf("create login_history table: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `CREATE INDEX IF NOT EXISTS idx_login_history_created_at ON login_history (created_at DESC)`); err != nil {
		return fmt.Errorf("index login_history.created_at: %w", err)
	}
	return nil
}

func InsertLoginHistory(username string, userType string, success bool, failureType string, clientIP string) error {
	_, err := DB.Exec(
		context.Background(),
		`INSERT INTO login_history (username, user_type, success, failure_type, client_ip) VALUES ($1, $2, $3, $4, $5)`,
		strings.TrimSpace(username),
		strings.TrimSpace(userType),
		success,
		strings.TrimSpace(failureType),
		strings.TrimSpace(clientIP),
	)
	if err != nil {
		return fmt.Errorf("insert login history: %w", err)
	}
	return nil
}

func ListRecentLoginHistory(limit int) ([]LoginHistoryRow, error) {
	n := limit
	if n <= 0 {
		n = 100
	}
	if n > 1000 {
		n = 1000
	}
	rows, err := DB.Query(
		context.Background(),
		`SELECT id, username, user_type, success, failure_type, client_ip, created_at
		 FROM login_history
		 ORDER BY id DESC
		 LIMIT $1`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("list login history: %w", err)
	}
	defer rows.Close()

	out := make([]LoginHistoryRow, 0, n)
	for rows.Next() {
		var row LoginHistoryRow
		if err := rows.Scan(&row.ID, &row.Username, &row.UserType, &row.Success, &row.FailureType, &row.ClientIP, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan login history row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate login history rows: %w", err)
	}
	return out, nil
}
