package admin_panel

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"

	"whatsapp-bot/db"
)

func adminDBTablesHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := adminLoadTableColumns()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var b strings.Builder
	b.WriteString(adminPageHeader("DB Tables"))
	b.WriteString(`<h2>DB Tables and Columns</h2>`)
	b.WriteString(adminNav(r))
	b.WriteString(`<p><a href="/admin/table/db-tables/export">Export current table as CSV</a></p>`)
	b.WriteString(adminTableOuterWrapOpen(len(rows)))
	b.WriteString(`<table border="1" cellpadding="6" cellspacing="0"><tr><th>Table</th><th>Column</th><th>Type</th><th>Nullable</th></tr>`)
	for _, row := range rows {
		b.WriteString("<tr>")
		b.WriteString("<td>" + html.EscapeString(row.TableName) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.ColumnName) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.DataType) + "</td>")
		b.WriteString("<td>" + html.EscapeString(row.IsNullable) + "</td>")
		b.WriteString("</tr>")
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminLoadTableColumns() ([]adminTableColumnRow, error) {
	query := `
SELECT table_name, column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema = 'public'
ORDER BY table_name ASC, ordinal_position ASC`
	rows, err := db.DB.Query(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("query table columns: %w", err)
	}
	defer rows.Close()
	out := []adminTableColumnRow{}
	for rows.Next() {
		var row adminTableColumnRow
		if err := rows.Scan(&row.TableName, &row.ColumnName, &row.DataType, &row.IsNullable); err != nil {
			return nil, fmt.Errorf("scan table columns: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table columns: %w", err)
	}
	return out, nil
}
