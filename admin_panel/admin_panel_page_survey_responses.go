package admin_panel

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/survey"
)

func adminSurveyResponsesHandler(w http.ResponseWriter, r *http.Request) {
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	sections, err := adminLoadSurveyResponseSections(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var b strings.Builder
	b.WriteString(adminPageHeader("Survey Responses"))
	b.WriteString(`<h2>Survey Responses</h2>`)
	b.WriteString(adminNav(r))
	b.WriteString(adminPhoneFilterForm("/admin/survey-responses", phoneFilter))
	msg := strings.TrimSpace(r.URL.Query().Get("msg"))
	if msg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(msg) + `</p>`)
	}
	if len(sections) == 0 {
		b.WriteString(`<p>No survey tables found in current survey config.</p>`)
		b.WriteString(adminPageFooter())
		adminWriteHTML(w, b.String())
		return
	}
	for _, section := range sections {
		b.WriteString(`<h3>` + html.EscapeString(section.Heading) + `</h3>`)
		b.WriteString(`<p>Table: <code>` + html.EscapeString(section.TableName) + `</code></p>`)
		if strings.TrimSpace(section.SurveyURL) != "" {
			b.WriteString(`<p><a href="` + html.EscapeString(section.SurveyURL) + `" target="_blank" rel="noopener noreferrer">` + html.EscapeString(section.SurveyURL) + `</a></p>`)
		}
		b.WriteString(`<p><a href="/admin/survey-responses/export?table=` + html.EscapeString(section.TableName) + `&phone=` + html.EscapeString(phoneFilter) + `">Export this table as CSV</a></p>`)
		b.WriteString(`<form method="post" action="/admin/survey-responses/delete-orphans" onsubmit="return confirm('Delete all responses in ` + html.EscapeString(section.TableName) + ` that do not belong to any participant in meta?');">`)
		b.WriteString(`<input type="hidden" name="table" value="` + html.EscapeString(section.TableName) + `">`)
		b.WriteString(`<p><button type="submit" style="background:#b91c1c;color:#fff;">Delete Orphan Responses (Not in Meta)</button></p>`)
		b.WriteString(`</form>`)
		if len(section.Headers) == 0 {
			b.WriteString(`<p>No columns found.</p>`)
			continue
		}
		b.WriteString(adminTableOuterWrapOpen(len(section.Rows)))
		b.WriteString(`<table border="1" cellpadding="6" cellspacing="0"><tr>`)
		for _, h := range section.Headers {
			b.WriteString("<th>" + html.EscapeString(h) + "</th>")
		}
		b.WriteString("<th>Action</th>")
		b.WriteString("</tr>")
		for _, row := range section.Rows {
			b.WriteString("<tr>")
			for _, h := range section.Headers {
				b.WriteString("<td>" + html.EscapeString(adminFormatValueByColumn(h, row.Values[h])) + "</td>")
			}
			b.WriteString("<td>")
			rowID := strings.TrimSpace(row.Values["id"])
			if rowID != "" {
				b.WriteString(`<form method="post" action="/admin/survey-responses/delete-one" onsubmit="return confirm('Delete this response record from ` + html.EscapeString(section.TableName) + `?');">`)
				b.WriteString(`<input type="hidden" name="table" value="` + html.EscapeString(section.TableName) + `">`)
				b.WriteString(`<input type="hidden" name="id" value="` + html.EscapeString(rowID) + `">`)
				b.WriteString(`<button type="submit" style="background:#b91c1c;color:#fff;">Delete Response</button>`)
				b.WriteString(`</form>`)
			}
			b.WriteString("</td>")
			b.WriteString("</tr>")
		}
		b.WriteString(`</table>`)
		b.WriteString(adminTableOuterWrapClose())
		if len(section.Rows) == 0 {
			b.WriteString(`<p>No responses yet.</p>`)
		}
	}
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminLoadSurveyResponseSections(phoneFilter string) ([]adminSurveyResponseSection, error) {
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil {
		return nil, fmt.Errorf("survey config not loaded")
	}
	sections := []adminSurveyResponseSection{}

	baselineTable := strings.TrimSpace(cfg.Baseline.TableName)
	if baselineTable != "" {
		rows, headers, err := adminLoadGenericTableRows(baselineTable, phoneFilter)
		if err != nil {
			return nil, fmt.Errorf("load baseline responses (%s): %w", baselineTable, err)
		}
		baselineURL := ""
		if slug := strings.TrimSpace(cfg.Baseline.LinkSlug); slug != "" {
			baselineURL = adminSurveyURLBySlug(slug)
		}
		sections = append(sections, adminSurveyResponseSection{
			Heading:    "Baseline: " + strings.TrimSpace(cfg.Baseline.Title),
			TableName:  baselineTable,
			SurveyID:   strings.TrimSpace(cfg.Baseline.SurveyID),
			IsBaseline: true,
			SurveyURL:  baselineURL,
			Headers:    headers,
			Rows:       rows,
		})
	}
	for _, fu := range cfg.Followups {
		followupTable := strings.TrimSpace(fu.TableName)
		if followupTable == "" {
			continue
		}
		rows, headers, err := adminLoadGenericTableRows(followupTable, phoneFilter)
		if err != nil {
			return nil, fmt.Errorf("load follow-up responses (%s): %w", followupTable, err)
		}
		followupURL := ""
		if slug := strings.TrimSpace(fu.LinkSlug); slug != "" {
			followupURL = adminSurveyURLBySlug(slug)
		}
		sections = append(sections, adminSurveyResponseSection{
			Heading:    "Follow-up: " + strings.TrimSpace(fu.Title) + " (" + strings.TrimSpace(fu.SurveyID) + ")",
			TableName:  followupTable,
			SurveyID:   strings.TrimSpace(fu.SurveyID),
			IsBaseline: false,
			SurveyURL:  followupURL,
			Headers:    headers,
			Rows:       rows,
		})
	}
	return sections, nil
}

func adminLoadGenericTableRows(tableName string, phoneFilter string) ([]adminMetaRow, []string, error) {
	tbl := strings.TrimSpace(tableName)
	if tbl == "" {
		return nil, nil, fmt.Errorf("table name is empty")
	}
	if err := common.ValidateSQLIdentifier(tbl, "admin generic table name"); err != nil {
		return nil, nil, err
	}
	query := fmt.Sprintf("SELECT * FROM %s ORDER BY id DESC LIMIT 500", tbl)
	rows, err := db.DB.Query(context.Background(), query)
	if err != nil {
		return nil, nil, fmt.Errorf("query %s: %w", tbl, err)
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	headers := make([]string, 0, len(fields))
	for _, fd := range fields {
		headers = append(headers, string(fd.Name))
	}
	out := []adminMetaRow{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, nil, fmt.Errorf("%s values: %w", tbl, err)
		}
		rowMap := map[string]string{}
		for i, v := range values {
			col := headers[i]
			if v == nil {
				rowMap[col] = ""
				continue
			}
			switch x := v.(type) {
			case time.Time:
				rowMap[col] = adminFormatTime(x)
			case *time.Time:
				if x == nil {
					rowMap[col] = ""
				} else {
					rowMap[col] = adminFormatTime(*x)
				}
			default:
				rowMap[col] = fmt.Sprintf("%v", v)
			}
		}
		out = append(out, adminMetaRow{Values: rowMap})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate %s: %w", tbl, err)
	}
	if phoneFilter == "" {
		return out, headers, nil
	}
	filtered := make([]adminMetaRow, 0, len(out))
	for _, row := range out {
		if strings.Contains(common.DigitsOnly(strings.TrimSpace(row.Values[survey.RespondentPhoneColumn])), phoneFilter) {
			filtered = append(filtered, row)
		}
	}
	return filtered, headers, nil
}

func adminSurveyURLBySlug(slug string) string {
	trimmedSlug := strings.TrimSpace(slug)
	if trimmedSlug == "" {
		return ""
	}
	base := strings.TrimSpace(os.Getenv("SURVEY_PUBLIC_BASE_URL"))
	if base == "" {
		base = "http://localhost:8080"
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return strings.TrimRight(base, "/") + "/survey/" + trimmedSlug
}

func adminSurveyResponsesDeleteOneHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/survey-responses?msg=Invalid+form+data.", http.StatusSeeOther)
		return
	}
	table := strings.TrimSpace(r.FormValue("table"))
	rowIDRaw := strings.TrimSpace(r.FormValue("id"))
	if err := common.ValidateSQLIdentifier(table, "survey responses table"); err != nil {
		http.Redirect(w, r, "/admin/survey-responses?msg=Invalid+table+name.", http.StatusSeeOther)
		return
	}
	rowID, err := strconv.ParseInt(rowIDRaw, 10, 64)
	if err != nil || rowID <= 0 {
		http.Redirect(w, r, "/admin/survey-responses?msg=Invalid+response+ID.", http.StatusSeeOther)
		return
	}
	section, ok, err := adminSurveySectionByTable(table)
	if err != nil {
		http.Redirect(w, r, "/admin/survey-responses?msg=Failed+to+load+survey+configuration.", http.StatusSeeOther)
		return
	}
	if !ok {
		http.Redirect(w, r, "/admin/survey-responses?msg=Unknown+survey+table.", http.StatusSeeOther)
		return
	}

	var respondentPhone string
	lookupQuery := fmt.Sprintf("SELECT %s FROM %s WHERE id = $1", survey.RespondentPhoneColumn, table)
	if err := db.DB.QueryRow(context.Background(), lookupQuery, rowID).Scan(&respondentPhone); err != nil {
		http.Redirect(w, r, "/admin/survey-responses?msg=Response+record+not+found.", http.StatusSeeOther)
		return
	}
	respondentPhone = common.DigitsOnly(strings.TrimSpace(respondentPhone))

	if section.IsBaseline {
		existsInMeta, err := db.ParticipantExistsForPhoneDigits(respondentPhone)
		if err != nil {
			http.Redirect(w, r, "/admin/survey-responses?msg=Failed+to+validate+meta+participant.", http.StatusSeeOther)
			return
		}
		if existsInMeta {
			var baselineCount int64
			countQuery := fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE %s = $1", table, survey.RespondentPhoneColumn)
			if err := db.DB.QueryRow(context.Background(), countQuery, respondentPhone).Scan(&baselineCount); err != nil {
				http.Redirect(w, r, "/admin/survey-responses?msg=Failed+to+validate+baseline+record+count.", http.StatusSeeOther)
				return
			}
			if baselineCount <= 1 {
				http.Redirect(w, r, "/admin/survey-responses?msg=Cannot+delete+the+only+baseline+record+for+a+participant+in+meta.", http.StatusSeeOther)
				return
			}
		}
	}

	deleteQuery := fmt.Sprintf("DELETE FROM %s WHERE id = $1", table)
	tag, err := db.DB.Exec(context.Background(), deleteQuery, rowID)
	if err != nil {
		http.Redirect(w, r, "/admin/survey-responses?msg=Failed+to+delete+response+record.", http.StatusSeeOther)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Redirect(w, r, "/admin/survey-responses?msg=Response+record+not+found.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/survey-responses?msg=Response+record+deleted.", http.StatusSeeOther)
}

func adminSurveyResponsesDeleteOrphansHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/survey-responses?msg=Invalid+form+data.", http.StatusSeeOther)
		return
	}
	table := strings.TrimSpace(r.FormValue("table"))
	if err := common.ValidateSQLIdentifier(table, "survey responses table"); err != nil {
		http.Redirect(w, r, "/admin/survey-responses?msg=Invalid+table+name.", http.StatusSeeOther)
		return
	}
	_, ok, err := adminSurveySectionByTable(table)
	if err != nil {
		http.Redirect(w, r, "/admin/survey-responses?msg=Failed+to+load+survey+configuration.", http.StatusSeeOther)
		return
	}
	if !ok {
		http.Redirect(w, r, "/admin/survey-responses?msg=Unknown+survey+table.", http.StatusSeeOther)
		return
	}
	deleted, err := adminDeleteOrphanSurveyRows(table)
	if err != nil {
		http.Redirect(w, r, "/admin/survey-responses?msg=Failed+to+delete+orphan+responses.", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/survey-responses?msg="+urlQueryEscape(fmt.Sprintf("Deleted %d orphan response(s).", deleted)), http.StatusSeeOther)
}

func adminSurveySectionByTable(tableName string) (adminSurveyResponseSection, bool, error) {
	sections, err := adminLoadSurveyResponseSections("")
	if err != nil {
		return adminSurveyResponseSection{}, false, err
	}
	target := strings.TrimSpace(tableName)
	for _, section := range sections {
		if strings.TrimSpace(section.TableName) == target {
			return section, true, nil
		}
	}
	return adminSurveyResponseSection{}, false, nil
}

func adminDeleteOrphanSurveyRows(tableName string) (int64, error) {
	metaPhones, err := adminLoadMetaPhoneSet()
	if err != nil {
		return 0, err
	}
	query := fmt.Sprintf("SELECT id, %s FROM %s", survey.RespondentPhoneColumn, tableName)
	rows, err := db.DB.Query(context.Background(), query)
	if err != nil {
		return 0, fmt.Errorf("query survey rows: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, 64)
	for rows.Next() {
		var id int64
		var respondentPhone string
		if err := rows.Scan(&id, &respondentPhone); err != nil {
			return 0, fmt.Errorf("scan survey row: %w", err)
		}
		phoneDigits := common.DigitsOnly(strings.TrimSpace(respondentPhone))
		if phoneDigits == "" {
			ids = append(ids, id)
			continue
		}
		if _, ok := metaPhones[phoneDigits]; !ok {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate survey rows: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	deleteQuery := fmt.Sprintf("DELETE FROM %s WHERE id IN (%s)", tableName, strings.Join(placeholders, ","))
	tag, err := db.DB.Exec(context.Background(), deleteQuery, args...)
	if err != nil {
		return 0, fmt.Errorf("delete orphan survey rows: %w", err)
	}
	return tag.RowsAffected(), nil
}

func adminLoadMetaPhoneSet() (map[string]struct{}, error) {
	rows, err := db.DB.Query(context.Background(), `SELECT participant_phone FROM meta`)
	if err != nil {
		return nil, fmt.Errorf("query meta participants: %w", err)
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var encPhone string
		if err := rows.Scan(&encPhone); err != nil {
			return nil, fmt.Errorf("scan meta participant: %w", err)
		}
		plain, err := common.DecryptPhone(encPhone)
		if err != nil {
			continue
		}
		digits := common.DigitsOnly(strings.TrimSpace(plain))
		if digits == "" {
			continue
		}
		out[digits] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate meta participants: %w", err)
	}
	return out, nil
}
