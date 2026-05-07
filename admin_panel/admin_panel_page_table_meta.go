package admin_panel

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
)

func adminMetaHandler(w http.ResponseWriter, r *http.Request) {
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	rows, headers, err := adminLoadMetaRows(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var b strings.Builder
	b.WriteString(adminPageHeader("Meta"))
	b.WriteString(`<h2>Meta</h2>`)
	b.WriteString(adminNav(r))
	b.WriteString(adminPhoneFilterForm("/admin/table/meta", phoneFilter))
	b.WriteString(`<p><a href="/admin/table/meta/export?phone=` + html.EscapeString(phoneFilter) + `">Export current table as CSV</a></p>`)
	b.WriteString(adminTableOuterWrapOpen(len(rows)))
	b.WriteString(`<table border="1" cellpadding="6" cellspacing="0"><tr>`)
	for _, h := range headers {
		b.WriteString("<th>" + html.EscapeString(h) + "</th>")
	}
	b.WriteString("</tr>")
	for _, row := range rows {
		b.WriteString("<tr>")
		for _, h := range headers {
			b.WriteString("<td>" + html.EscapeString(adminFormatValueByColumn(h, row.Values[h])) + "</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminLoadMetaRows(phoneFilter string) ([]adminMetaRow, []string, error) {
	query := `SELECT * FROM meta ORDER BY id DESC LIMIT 500`
	rows, err := db.DB.Query(context.Background(), query)
	if err != nil {
		return nil, nil, fmt.Errorf("query meta: %w", err)
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
			return nil, nil, fmt.Errorf("meta values: %w", err)
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
		if enc, ok := rowMap["participant_phone"]; ok {
			plainPhone, err := common.DecryptPhone(enc)
			if err != nil {
				rowMap["participant_phone"] = "[decrypt-error]"
			} else {
				rowMap["participant_phone"] = common.DigitsOnly(strings.TrimSpace(plainPhone))
			}
		}
		if phoneFilter != "" && rowMap["participant_phone"] != phoneFilter {
			continue
		}
		out = append(out, adminMetaRow{Values: rowMap})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate meta: %w", err)
	}
	return out, headers, nil
}
