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

type adminRiskConversationRow struct {
	adminConversationRow
	MatchedPhrase string
}

func adminRiskMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	phoneFilter := common.DigitsOnly(strings.TrimSpace(r.URL.Query().Get("phone")))
	statusMsg := strings.TrimSpace(r.URL.Query().Get("msg"))
	phrases := adminRiskyPhrasesFromConfig()

	var rows []adminRiskConversationRow
	var err error
	if len(phrases) > 0 {
		rows, err = adminLoadRiskConversationRows(phoneFilter, phrases)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	var b strings.Builder
	b.WriteString(adminPageHeader("Risk Message"))
	b.WriteString(`<h2>Risk Message</h2>`)
	b.WriteString(adminNav(r))
	if statusMsg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(statusMsg) + `</p>`)
	}
	b.WriteString(`<p>Conversation rows whose content matches a phrase in <code>RISKY_PHRASES</code> (newest first, up to 500 rows). Edit phrases under <a href="/admin/configuration">Configuration</a>.</p>`)
	b.WriteString(`<p><strong>Active phrases:</strong> ` + html.EscapeString(fmt.Sprintf("%d", len(phrases))) + `</p>`)
	b.WriteString(adminConversationPhoneFilterForm("/admin/risk-message", phoneFilter))

	if len(phrases) == 0 {
		b.WriteString(`<p>No risky phrases configured. Set <code>RISKY_PHRASES</code> in Configuration.</p>`)
	} else if len(rows) == 0 {
		b.WriteString(`<p>No matching conversation rows found.</p>`)
	} else {
		b.WriteString(adminTableOuterWrapOpen(len(rows)))
		b.WriteString(`<table><tr><th>ID</th><th>Phone</th><th>Sender</th><th>Receiver</th><th>Direction</th><th>Nature</th><th>Matched Phrase</th><th>Content</th><th>Created At</th></tr>`)
		for _, row := range rows {
			b.WriteString("<tr>")
			b.WriteString("<td>" + fmt.Sprintf("%d", row.ID) + "</td>")
			b.WriteString("<td>" + html.EscapeString(row.Phone) + "</td>")
			b.WriteString("<td>" + html.EscapeString(row.Sender) + "</td>")
			b.WriteString("<td>" + html.EscapeString(row.Receiver) + "</td>")
			b.WriteString("<td>" + html.EscapeString(row.Direction) + "</td>")
			b.WriteString("<td>" + html.EscapeString(row.Nature) + "</td>")
			b.WriteString("<td>" + html.EscapeString(row.MatchedPhrase) + "</td>")
			b.WriteString("<td>" + html.EscapeString(row.Content) + "</td>")
			b.WriteString("<td>" + html.EscapeString(adminFormatTime(row.CreatedAt)) + "</td>")
			b.WriteString("</tr>")
		}
		b.WriteString(`</table>`)
		b.WriteString(adminTableOuterWrapClose())
	}
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminRiskyPhrasesFromConfig() []string {
	return parseRiskyPhrases(db.GetProjectSettingString("RISKY_PHRASES", ""))
}

func parseRiskyPhrases(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		phrase := strings.TrimSpace(part)
		if phrase == "" {
			continue
		}
		key := strings.ToLower(phrase)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, phrase)
	}
	return out
}

func matchRiskyPhrase(content string, phrases []string) (string, bool) {
	lowerContent := strings.ToLower(content)
	for _, phrase := range phrases {
		if phrase == "" {
			continue
		}
		if strings.Contains(lowerContent, strings.ToLower(phrase)) {
			return phrase, true
		}
	}
	return "", false
}

func escapeILIKELiteral(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

func adminLoadRiskConversationRows(phoneFilter string, phrases []string) ([]adminRiskConversationRow, error) {
	if len(phrases) == 0 {
		return []adminRiskConversationRow{}, nil
	}

	conditions := make([]string, 0, len(phrases))
	args := make([]interface{}, 0, len(phrases))
	for i, phrase := range phrases {
		conditions = append(conditions, fmt.Sprintf("content ILIKE $%d ESCAPE E'\\'", i+1))
		args = append(args, "%"+escapeILIKELiteral(phrase)+"%")
	}
	query := `
SELECT id, participant_phone, sender, receiver, direction, nature, content, created_at
FROM conversation
WHERE (` + strings.Join(conditions, " OR ") + `)
ORDER BY created_at DESC
LIMIT 500`

	rows, err := db.DB.Query(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("query risky conversation rows: %w", err)
	}
	defer rows.Close()

	out := make([]adminRiskConversationRow, 0, 64)
	for rows.Next() {
		var id int64
		var encParticipantPhone, encSender, encReceiver, direction, nature, content string
		var createdAt time.Time
		if err := rows.Scan(&id, &encParticipantPhone, &encSender, &encReceiver, &direction, &nature, &content, &createdAt); err != nil {
			return nil, fmt.Errorf("scan risky conversation row: %w", err)
		}
		content = strings.TrimSpace(content)
		matchedPhrase, ok := matchRiskyPhrase(content, phrases)
		if !ok {
			continue
		}

		plainParticipantPhone, err := common.DecryptPhone(encParticipantPhone)
		if err != nil {
			plainParticipantPhone = "[decrypt-error]"
		}
		plainSender, err := common.DecryptPhone(encSender)
		if err != nil {
			plainSender = "[decrypt-error]"
		}
		plainReceiver, err := common.DecryptPhone(encReceiver)
		if err != nil {
			plainReceiver = "[decrypt-error]"
		}

		normalizedPhone := common.DigitsOnly(strings.TrimSpace(plainParticipantPhone))
		if normalizedPhone == "" {
			normalizedPhone = strings.TrimSpace(plainParticipantPhone)
		}
		normalizedSender := common.DigitsOnly(strings.TrimSpace(plainSender))
		if normalizedSender == "" {
			normalizedSender = strings.TrimSpace(plainSender)
		}
		normalizedReceiver := common.DigitsOnly(strings.TrimSpace(plainReceiver))
		if normalizedReceiver == "" {
			normalizedReceiver = strings.TrimSpace(plainReceiver)
		}
		if phoneFilter != "" && normalizedPhone != phoneFilter && normalizedSender != phoneFilter && normalizedReceiver != phoneFilter {
			continue
		}

		out = append(out, adminRiskConversationRow{
			adminConversationRow: adminConversationRow{
				ID:        id,
				Phone:     normalizedPhone,
				Sender:    normalizedSender,
				Receiver:  normalizedReceiver,
				Direction: strings.TrimSpace(direction),
				Nature:    strings.TrimSpace(nature),
				Content:   content,
				CreatedAt: createdAt,
			},
			MatchedPhrase: matchedPhrase,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate risky conversation rows: %w", err)
	}
	return out, nil
}
