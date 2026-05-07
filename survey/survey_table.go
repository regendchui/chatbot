package survey

import ( // DB and strings for DDL.
	"context" // SQL context.
	"fmt"     // Errors and SQL formatting.
	"strings" // Building column lists.

	"whatsapp-bot/db"
) // End import.

// surveyColumnDef describes one validated survey column and its source type.
type surveyColumnDef struct {
	Name         string // SQL column name.
	QuestionType string // Normalized question type from config.
}

// CreateAllSurveyTables creates baseline and follow-up response tables if not exist.
func CreateAllSurveyTables(cfg *SurveyConfig) error { // Entry from InitSurveyInfrastructure.
	if cfg == nil { // Guard nil config.
		return fmt.Errorf("survey config is nil") // Validation error.
	}
	tbl := strings.TrimSpace(cfg.Baseline.TableName)                          // Baseline table name from JSON.
	if err := validateSQLIdentifier(tbl, "baseline.table_name"); err != nil { // Validate identifier safety.
		return err // Propagate validation error.
	}
	cols, err := surveyColumnDefs(BaselineQuestionsWithSystemFields(cfg.Baseline.Questions)) // Build column list from baseline + built-in questions.
	if err != nil {                                                                          // Propagate column validation errors.
		return fmt.Errorf("baseline columns: %w", err) // Wrap error.
	}
	if err := createSurveyTableIfNotExists(tbl, cols); err != nil { // Execute CREATE for baseline.
		return fmt.Errorf("create baseline table %s: %w", tbl, err) // Wrap DDL error.
	}
	if err := MigrateSurveyTableColumns(tbl, cols); err != nil { // Add newly configured columns without dropping old columns.
		return fmt.Errorf("migrate baseline table %s: %w", tbl, err)
	}
	if err := ensureBaselineSystemColumns(tbl); err != nil { // Ensure built-in baseline columns exist on old deployments.
		return fmt.Errorf("baseline system columns: %w", err) // Wrap ALTER error.
	}
	if err := ensureQuestionColumnsAsText(tbl, cols); err != nil { // Keep configured question columns as TEXT storage.
		return fmt.Errorf("baseline text columns: %w", err) // Wrap ALTER error.
	}
	for i := range cfg.Followups { // Each follow-up survey.
		ft := strings.TrimSpace(cfg.Followups[i].TableName)                         // Follow-up table name.
		if err := validateSQLIdentifier(ft, "followups[].table_name"); err != nil { // Validate name.
			return err // Stop on bad name.
		}
		fcols, err := surveyColumnDefs(cfg.Followups[i].Questions) // Columns for this FU.
		if err != nil {                                            // Validate FU columns.
			return fmt.Errorf("followup %s columns: %w", cfg.Followups[i].SurveyID, err) // Wrap.
		}
		if err := createSurveyTableIfNotExists(ft, fcols); err != nil { // CREATE TABLE IF NOT EXISTS.
			return fmt.Errorf("create followup table %s: %w", ft, err) // Wrap DDL error.
		}
		if err := MigrateSurveyTableColumns(ft, fcols); err != nil { // Add newly configured columns without dropping old columns.
			return fmt.Errorf("migrate followup table %s: %w", ft, err)
		}
		if err := ensureQuestionColumnsAsText(ft, fcols); err != nil { // Keep configured question columns as TEXT storage.
			return fmt.Errorf("followup %s text columns: %w", cfg.Followups[i].SurveyID, err) // Wrap.
		}
	}
	return nil // All tables ensured.
} // End CreateAllSurveyTables.

// surveyColumnDefs builds validated column names from JSON questions plus forced phone column.
func surveyColumnDefs(questions []SurveyQuestion) ([]surveyColumnDef, error) { // Returns list of validated columns.
	seen := map[string]struct{}{}                             // Detect duplicate column names.
	out := []surveyColumnDef{{Name: RespondentPhoneColumn}}   // Always include forced respondent phone column first.
	seen[strings.ToLower(RespondentPhoneColumn)] = struct{}{} // Reserve phone column name.
	for _, q := range questions {                             // Each JSON question.
		cn := strings.TrimSpace(q.ColumnName) // Column name from JSON.
		if cn == "" {                         // Reject empty column_name.
			return nil, fmt.Errorf("question %s has empty column_name", q.ID) // Validation error.
		}
		if strings.EqualFold(cn, RespondentPhoneColumn) { // Disallow shadowing phone column.
			return nil, fmt.Errorf("column_name %q conflicts with reserved %s", cn, RespondentPhoneColumn) // Error.
		}
		if err := validateSQLIdentifier(cn, "question.column_name"); err != nil { // SQL-safe name.
			return nil, err // Propagate.
		}
		key := strings.ToLower(cn)  // Normalize for duplicate check.
		if _, ok := seen[key]; ok { // Duplicate column.
			return nil, fmt.Errorf("duplicate column_name: %s", cn) // Error.
		}
		seen[key] = struct{}{}                                                                                 // Mark seen.
		out = append(out, surveyColumnDef{Name: cn, QuestionType: strings.ToLower(strings.TrimSpace(q.Type))}) // Append validated column with type.
	}
	return out, nil // Return column list.
} // End surveyColumnDefs.

// validateSQLIdentifier ensures name is safe unquoted PostgreSQL identifier.
func validateSQLIdentifier(name string, ctx string) error { // Allow only [a-z][a-z0-9_]*.
	if name == "" { // Reject empty.
		return fmt.Errorf("%s: empty identifier", ctx) // Error.
	}
	for i, r := range name { // Run validation per rune.
		if i == 0 { // First character rules.
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '_' { // Letter or underscore only.
				return fmt.Errorf("%s: invalid identifier start: %q", ctx, name) // Error.
			}
			continue // Continue to next rune.
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' { // Rest allowed.
			continue // OK.
		}
		return fmt.Errorf("%s: invalid identifier %q", ctx, name) // Bad character.
	}
	lower := strings.ToLower(name)                               // Lowercase for reserved check.
	if lower == "user" || lower == "order" || lower == "group" { // Skip common reserved words.
		return fmt.Errorf("%s: reserved-like identifier %q not allowed", ctx, name) // Error.
	}
	return nil // Valid.
} // End validateSQLIdentifier.

// createSurveyTableIfNotExists runs CREATE TABLE with id, submitted_at, and TEXT columns.
func createSurveyTableIfNotExists(table string, columns []surveyColumnDef) error { // DDL executor.
	var parts []string                                                                   // SQL column fragments.
	parts = append(parts, "id SERIAL PRIMARY KEY")                                       // Surrogate key.
	parts = append(parts, "submitted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP") // Submission time.
	for _, c := range columns {                                                          // Each answer column stored as TEXT.
		if c.Name == RespondentPhoneColumn { // Forced phone must be provided on submit.
			parts = append(parts, fmt.Sprintf("%s TEXT NOT NULL", c.Name)) // No default; required at insert.
			continue                                                       // Next column.
		}
		parts = append(parts, fmt.Sprintf("%s TEXT NOT NULL DEFAULT ''", c.Name)) // All survey answers are persisted as text.
	}
	ddl := fmt.Sprintf( // Full CREATE IF NOT EXISTS.
		"CREATE TABLE IF NOT EXISTS %s (\n    %s\n);",
		table,
		strings.Join(parts, ",\n    "),
	)
	_, err := db.DB.Exec(context.Background(), ddl) // Execute DDL.
	if err != nil {                                 // Check failure.
		return err // Return raw error.
	}
	return nil // Success.
} // End createSurveyTableIfNotExists.

// ensureQuestionColumnsAsText keeps all configured question columns as TEXT for flexible answer storage.
func ensureQuestionColumnsAsText(table string, columns []surveyColumnDef) error {
	for _, c := range columns {
		if c.Name == RespondentPhoneColumn {
			continue
		}
		alterType := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE TEXT USING COALESCE(TRIM(%s::text), '')", table, c.Name, c.Name)
		if _, err := db.DB.Exec(context.Background(), alterType); err != nil {
			return fmt.Errorf("alter %s.%s to TEXT: %w", table, c.Name, err)
		}
		setDefault := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT ''", table, c.Name)
		if _, err := db.DB.Exec(context.Background(), setDefault); err != nil {
			return fmt.Errorf("set default for %s.%s: %w", table, c.Name, err)
		}
		setNotNull := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL", table, c.Name)
		if _, err := db.DB.Exec(context.Background(), setNotNull); err != nil {
			return fmt.Errorf("set not null for %s.%s: %w", table, c.Name, err)
		}
	}
	return nil
}

// ensureBaselineSystemColumns keeps non-JSON baseline helper columns available on existing tables.
func ensureBaselineSystemColumns(table string) error {
	if err := validateSQLIdentifier(MessageIntervalColumn, "baseline system column"); err != nil {
		return err
	}
	if err := validateSQLIdentifier(ConsentColumn, "baseline system column"); err != nil {
		return err
	}
	alter := fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s TEXT NOT NULL DEFAULT ''",
		table, MessageIntervalColumn,
	)
	if _, err := db.DB.Exec(context.Background(), alter); err != nil {
		return fmt.Errorf("ensure %s.%s: %w", table, MessageIntervalColumn, err)
	}
	alterConsent := fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s TEXT NOT NULL DEFAULT ''",
		table, ConsentColumn,
	)
	if _, err := db.DB.Exec(context.Background(), alterConsent); err != nil {
		return fmt.Errorf("ensure %s.%s: %w", table, ConsentColumn, err)
	}
	return nil
}
