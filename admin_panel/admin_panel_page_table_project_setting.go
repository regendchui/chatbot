package admin_panel

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"whatsapp-bot/db"
)

func adminProjectSettingTableHandler(w http.ResponseWriter, r *http.Request) {
	rows, headers, err := adminLoadProjectSettingRows()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var b strings.Builder
	b.WriteString(adminPageHeader("Project Setting (Raw)"))
	b.WriteString(`<h2>Project Setting (Raw Debug)</h2>`)
	b.WriteString(adminNav(r))
	b.WriteString(`<p style="color:#b91c1c;"><strong>Warning:</strong> This page exposes raw configuration and secrets-like values. Do not use this page in production.</p>`)
	b.WriteString(adminTableOuterWrapOpen(len(rows)))
	b.WriteString(`<table border="1" cellpadding="6" cellspacing="0"><tr>`)
	for _, h := range headers {
		b.WriteString("<th>" + html.EscapeString(h) + "</th>")
	}
	b.WriteString("</tr>")
	for _, row := range rows {
		b.WriteString("<tr>")
		for _, h := range headers {
			b.WriteString("<td><pre style=\"white-space:pre-wrap;word-break:break-word;margin:0;\">" + html.EscapeString(adminFormatValueByColumn(h, row.Values[h])) + "</pre></td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())
	if len(rows) == 0 {
		b.WriteString(`<p>No row found in project_setting.</p>`)
	}
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminLoadProjectSettingRows() ([]adminMetaRow, []string, error) {
	query := `
SELECT id, env_variables::text AS env_variables, json_variables::text AS json_variables, created_at, updated_at
FROM project_setting
ORDER BY id ASC
LIMIT 20`
	rows, err := db.DB.Query(context.Background(), query)
	if err != nil {
		return nil, nil, fmt.Errorf("query project_setting raw table: %w", err)
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
			return nil, nil, fmt.Errorf("project_setting values: %w", err)
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
		return nil, nil, fmt.Errorf("iterate project_setting rows: %w", err)
	}
	return out, headers, nil
}
