package admin_panel

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"

	"whatsapp-bot/common"
	"whatsapp-bot/survey"
)

func adminConversationExportCSVHandler(w http.ResponseWriter, r *http.Request) {
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	rows, err := adminLoadConversationRows(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	headers := []string{"id", "phone", "sender", "receiver", "direction", "nature", "content", "created_at"}
	data := make([][]string, 0, len(rows))
	for _, row := range rows {
		data = append(data, []string{
			fmt.Sprintf("%d", row.ID),
			row.Phone,
			row.Sender,
			row.Receiver,
			row.Direction,
			row.Nature,
			row.Content,
			adminFormatTime(row.CreatedAt),
		})
	}
	adminWriteCSV(w, "conversation.csv", headers, data)
}

func adminMetaExportCSVHandler(w http.ResponseWriter, r *http.Request) {
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	rows, headers, err := adminLoadMetaRows(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := make([][]string, 0, len(rows))
	for _, row := range rows {
		record := make([]string, 0, len(headers))
		for _, h := range headers {
			record = append(record, adminFormatValueByColumn(h, row.Values[h]))
		}
		data = append(data, record)
	}
	adminWriteCSV(w, "meta.csv", headers, data)
}

func adminAutoMessagesExportCSVHandler(w http.ResponseWriter, r *http.Request) {
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	rows, err := adminLoadAutoMessageRows(phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	headers := []string{"id", "phone", "scheduled_at", "is_sent", "sent_at", "nature", "followup_survey", "content"}
	data := make([][]string, 0, len(rows))
	for _, row := range rows {
		data = append(data, []string{
			fmt.Sprintf("%d", row.ID),
			row.Phone,
			adminFormatTime(row.ScheduledAt),
			fmt.Sprintf("%t", row.IsSent),
			adminFormatTimestampString(row.SentAt),
			row.Nature,
			row.FollowupID,
			row.Content,
		})
	}
	adminWriteCSV(w, "auto-messages.csv", headers, data)
}

func adminDBTablesExportCSVHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := adminLoadTableColumns()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	headers := []string{"table_name", "column_name", "data_type", "is_nullable"}
	data := make([][]string, 0, len(rows))
	for _, row := range rows {
		data = append(data, []string{
			row.TableName,
			row.ColumnName,
			row.DataType,
			row.IsNullable,
		})
	}
	adminWriteCSV(w, "db-tables.csv", headers, data)
}

func adminSurveyResponsesExportCSVHandler(w http.ResponseWriter, r *http.Request) {
	tableName := strings.TrimSpace(r.URL.Query().Get("table"))
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	if tableName == "" {
		http.Error(w, "missing table query parameter", http.StatusBadRequest)
		return
	}
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil {
		http.Error(w, "survey config not loaded", http.StatusInternalServerError)
		return
	}
	allowed := map[string]struct{}{}
	if strings.TrimSpace(cfg.Baseline.TableName) != "" {
		allowed[strings.TrimSpace(cfg.Baseline.TableName)] = struct{}{}
	}
	for _, fu := range cfg.Followups {
		if strings.TrimSpace(fu.TableName) != "" {
			allowed[strings.TrimSpace(fu.TableName)] = struct{}{}
		}
	}
	if _, ok := allowed[tableName]; !ok {
		http.Error(w, "table is not an allowed survey table", http.StatusBadRequest)
		return
	}
	rows, headers, err := adminLoadGenericTableRows(tableName, phoneFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := make([][]string, 0, len(rows))
	for _, row := range rows {
		record := make([]string, 0, len(headers))
		for _, h := range headers {
			record = append(record, adminFormatValueByColumn(h, row.Values[h]))
		}
		data = append(data, record)
	}
	filename := "survey-" + strings.ReplaceAll(tableName, " ", "_") + ".csv"
	adminWriteCSV(w, filename, headers, data)
}

func adminWriteCSV(w http.ResponseWriter, filename string, headers []string, rows [][]string) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if len(headers) > 0 {
		_ = writer.Write(headers)
	}
	for _, row := range rows {
		_ = writer.Write(row)
	}
	writer.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = w.Write(buf.Bytes())
}
