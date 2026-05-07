package survey

import (
	"context"
	"fmt"
	"strings"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
)

func MigrateSurveyTableColumns(table string, columns []surveyColumnDef) error {
	tbl := strings.TrimSpace(table)
	if err := common.ValidateSQLIdentifier(tbl, "migration table"); err != nil {
		return err
	}
	for _, c := range columns {
		col := strings.TrimSpace(c.Name)
		if col == "" {
			continue
		}
		if err := common.ValidateSQLIdentifier(col, "migration column"); err != nil {
			return err
		}

		stmt := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s TEXT NOT NULL DEFAULT ''",
			tbl, col,
		)
		if _, err := db.DB.Exec(context.Background(), stmt); err != nil {
			return fmt.Errorf("add missing column %s.%s: %w", tbl, col, err)
		}
	}
	return nil
}
