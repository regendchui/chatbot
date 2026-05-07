package db

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ConfigUpdateHistoryRow struct {
	ID          int64
	Actor       string
	Action      string
	Description string
	CreatedAt   time.Time
}

func EnsureConfigUpdateHistoryTableExists() error {
	query := `
CREATE TABLE IF NOT EXISTS config_update_history (
    id BIGSERIAL PRIMARY KEY,
    actor TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	if _, err := DB.Exec(context.Background(), query); err != nil {
		return fmt.Errorf("create config_update_history table: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `CREATE INDEX IF NOT EXISTS idx_config_update_history_created_at ON config_update_history (created_at DESC)`); err != nil {
		return fmt.Errorf("index config_update_history.created_at: %w", err)
	}
	return nil
}

func InsertConfigUpdateHistory(actor string, action string, description string) error {
	_, err := DB.Exec(
		context.Background(),
		`INSERT INTO config_update_history (actor, action, description) VALUES ($1, $2, $3)`,
		strings.TrimSpace(actor),
		strings.TrimSpace(action),
		strings.TrimSpace(description),
	)
	if err != nil {
		return fmt.Errorf("insert config update history: %w", err)
	}
	return nil
}

func ListRecentConfigUpdateHistory(limit int) ([]ConfigUpdateHistoryRow, error) {
	n := limit
	if n <= 0 {
		n = 100
	}
	if n > 1000 {
		n = 1000
	}
	rows, err := DB.Query(
		context.Background(),
		`SELECT id, actor, action, description, created_at
		 FROM config_update_history
		 ORDER BY id DESC
		 LIMIT $1`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("list config update history: %w", err)
	}
	defer rows.Close()

	out := make([]ConfigUpdateHistoryRow, 0, n)
	for rows.Next() {
		var row ConfigUpdateHistoryRow
		if err := rows.Scan(&row.ID, &row.Actor, &row.Action, &row.Description, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan config update history row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config update history rows: %w", err)
	}
	return out, nil
}
