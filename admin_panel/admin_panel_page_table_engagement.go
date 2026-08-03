package admin_panel

import (
	"context"
	"fmt"
	"html"
	"math"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/survey"
)

type adminEngagementRow struct {
	Name        string
	Phone       string
	BaselineAt  time.Time
	WeekReached []bool
	WeekCounts  []int
	PeriodDays  int
	WeekCount   int
}

func adminEngagementWeekCount(periodDays int) int {
	if periodDays <= 0 {
		return 0
	}
	return (periodDays + 6) / 7
}

func adminEngagementWeekStart(baselineUTC time.Time, weekIndex0 int) time.Time {
	return baselineUTC.AddDate(0, 0, weekIndex0*7)
}

func adminLoadEngagementRows(phoneFilter string) (rows []adminEngagementRow, weekCount int, periodDays int, err error) {
	cfg := survey.GlobalSurveyConfig()
	if cfg == nil {
		return nil, 0, 0, fmt.Errorf("survey config not loaded")
	}
	periodDays = cfg.Project.InterventionPeriod
	weekCount = adminEngagementWeekCount(periodDays)
	if weekCount == 0 {
		return []adminEngagementRow{}, 0, periodDays, nil
	}

	metaRows, err := db.DB.Query(context.Background(), `
SELECT participant_phone, participant_name, baseline_completed_ts
FROM meta
WHERE baseline_completed_ts IS NOT NULL
ORDER BY baseline_completed_ts ASC, id ASC`)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("query baseline-complete participants: %w", err)
	}
	defer metaRows.Close()

	now := time.Now().UTC()
	out := make([]adminEngagementRow, 0, 64)
	for metaRows.Next() {
		var encPhone, name string
		var baselineAt time.Time
		if err := metaRows.Scan(&encPhone, &name, &baselineAt); err != nil {
			return nil, 0, 0, fmt.Errorf("scan baseline-complete participant: %w", err)
		}
		plainPhone, decErr := common.DecryptPhone(encPhone)
		if decErr != nil {
			continue
		}
		digits := common.DigitsOnly(strings.TrimSpace(plainPhone))
		if digits == "" {
			continue
		}
		if phoneFilter != "" && digits != phoneFilter {
			continue
		}
		if baselineAt.IsZero() {
			continue
		}
		baselineUTC := baselineAt.UTC()
		interventionEnd := baselineUTC.AddDate(0, 0, periodDays)
		reached := make([]bool, weekCount)
		for i := 0; i < weekCount; i++ {
			weekStart := adminEngagementWeekStart(baselineUTC, i)
			reached[i] = !now.Before(weekStart)
		}
		counts, countErr := adminCountInboundMessagesByWeek(encPhone, baselineUTC, interventionEnd, weekCount)
		if countErr != nil {
			return nil, 0, 0, countErr
		}
		out = append(out, adminEngagementRow{
			Name:        strings.TrimSpace(name),
			Phone:       digits,
			BaselineAt:  baselineUTC,
			WeekReached: reached,
			WeekCounts:  counts,
			PeriodDays:  periodDays,
			WeekCount:   weekCount,
		})
	}
	if err := metaRows.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("iterate baseline-complete participants: %w", err)
	}
	return out, weekCount, periodDays, nil
}

// adminCountInboundMessagesByWeek counts inbound messages strictly after baseline_completed_ts
// (created_at > baseline) and before intervention end, bucketed into week columns.
func adminCountInboundMessagesByWeek(encPhone string, baselineUTC, interventionEnd time.Time, weekCount int) ([]int, error) {
	counts := make([]int, weekCount)
	if weekCount <= 0 || encPhone == "" {
		return counts, nil
	}
	// Use created_at > baseline so messages at/before baseline completion are excluded from week 1.
	rows, err := db.DB.Query(context.Background(), `
SELECT created_at
FROM conversation
WHERE participant_phone = $1
  AND direction = 'inbound'
  AND created_at > $2
  AND created_at < $3`, encPhone, baselineUTC, interventionEnd)
	if err != nil {
		return nil, fmt.Errorf("query inbound messages for engagement: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var createdAt time.Time
		if err := rows.Scan(&createdAt); err != nil {
			return nil, fmt.Errorf("scan inbound message for engagement: %w", err)
		}
		msgUTC := createdAt.UTC()
		if !msgUTC.After(baselineUTC) {
			continue
		}
		elapsed := msgUTC.Sub(baselineUTC)
		weekIdx := int(elapsed / (7 * 24 * time.Hour))
		if weekIdx < 0 {
			continue
		}
		if weekIdx >= weekCount {
			weekIdx = weekCount - 1
		}
		counts[weekIdx]++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inbound messages for engagement: %w", err)
	}
	return counts, nil
}

func adminEngagementRates(rows []adminEngagementRow, weekCount int) []float64 {
	rates := make([]float64, weekCount)
	if weekCount <= 0 {
		return rates
	}
	reached := make([]int, weekCount)
	texted := make([]int, weekCount)
	for _, row := range rows {
		for i := 0; i < weekCount && i < len(row.WeekReached); i++ {
			if !row.WeekReached[i] {
				continue
			}
			reached[i]++
			if i < len(row.WeekCounts) && row.WeekCounts[i] > 0 {
				texted[i]++
			}
		}
	}
	for i := 0; i < weekCount; i++ {
		if reached[i] == 0 {
			rates[i] = 0
			continue
		}
		rates[i] = (float64(texted[i]) / float64(reached[i])) * 100
	}
	return rates
}

// adminGenerateEngagementRateGraphHTML builds an SVG bar chart of weekly engagement rate.
// engagement rate = (participants who texted that week) / (participants who reached that week) * 100.
func adminGenerateEngagementRateGraphHTML(rows []adminEngagementRow, weekCount int) string {
	if weekCount <= 0 {
		return `<p>No week columns available for the engagement rate graph.</p>`
	}
	rates := adminEngagementRates(rows, weekCount)

	const (
		width   = 720
		height  = 320
		padL    = 56
		padR    = 24
		padT    = 28
		padB    = 48
	)
	plotW := float64(width - padL - padR)
	plotH := float64(height - padT - padB)

	var b strings.Builder
	b.WriteString(`<h3 style="margin-top:28px;">Engagement Rate by Week</h3>`)
	b.WriteString(`<p style="color:#475569;">Engagement rate = (participants who sent ≥1 inbound message that week) ÷ (participants who have reached that week).</p>`)
	b.WriteString(fmt.Sprintf(
		`<svg viewBox="0 0 %d %d" width="100%%" style="max-width:%dpx;height:auto;background:#fff;border:1px solid #e5e7eb;border-radius:8px;display:block;margin:12px 0;">`,
		width, height, width,
	))

	// Axes
	b.WriteString(fmt.Sprintf(
		`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#94a3b8" stroke-width="1.5"/>`,
		padL, padT, padL, height-padB,
	))
	b.WriteString(fmt.Sprintf(
		`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#94a3b8" stroke-width="1.5"/>`,
		padL, height-padB, width-padR, height-padB,
	))

	// Y-axis grid + labels (0, 25, 50, 75, 100)
	for _, pct := range []int{0, 25, 50, 75, 100} {
		y := float64(padT) + plotH*(1-float64(pct)/100)
		b.WriteString(fmt.Sprintf(
			`<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#e2e8f0" stroke-width="1"/>`,
			padL, y, width-padR, y,
		))
		b.WriteString(fmt.Sprintf(
			`<text x="%d" y="%.1f" text-anchor="end" dominant-baseline="middle" font-size="12" fill="#64748b">%d%%</text>`,
			padL-8, y, pct,
		))
	}

	b.WriteString(fmt.Sprintf(
		`<text x="%d" y="%d" text-anchor="middle" font-size="12" fill="#334155" transform="rotate(-90 %d %d)">Engagement rate</text>`,
		16, height/2, 16, height/2,
	))
	b.WriteString(fmt.Sprintf(
		`<text x="%.1f" y="%d" text-anchor="middle" font-size="12" fill="#334155">Week</text>`,
		float64(padL)+plotW/2, height-10,
	))

	slotW := plotW / float64(weekCount)
	barW := math.Min(slotW*0.62, 56)
	for i := 0; i < weekCount; i++ {
		rate := rates[i]
		if rate < 0 {
			rate = 0
		}
		if rate > 100 {
			rate = 100
		}
		barH := plotH * (rate / 100)
		x := float64(padL) + slotW*float64(i) + (slotW-barW)/2
		y := float64(padT) + plotH - barH
		b.WriteString(fmt.Sprintf(
			`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#2563eb" rx="3"/>`,
			x, y, barW, math.Max(barH, 0),
		))
		labelX := float64(padL) + slotW*float64(i) + slotW/2
		b.WriteString(fmt.Sprintf(
			`<text x="%.1f" y="%d" text-anchor="middle" font-size="12" fill="#334155">W%d</text>`,
			labelX, height-padB+18, i+1,
		))
		if barH > 14 {
			b.WriteString(fmt.Sprintf(
				`<text x="%.1f" y="%.1f" text-anchor="middle" font-size="11" fill="#fff">%.0f%%</text>`,
				labelX, y+14, rate,
			))
		} else {
			b.WriteString(fmt.Sprintf(
				`<text x="%.1f" y="%.1f" text-anchor="middle" font-size="11" fill="#1e3a8a">%.0f%%</text>`,
				labelX, y-6, rate,
			))
		}
	}

	b.WriteString(`</svg>`)
	return b.String()
}

func adminRenderEngagementTableHTML(phoneFilter string) (string, error) {
	rows, weekCount, periodDays, err := adminLoadEngagementRows(phoneFilter)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(`<h3 style="margin-top:28px;">Engagement Table</h3>`)
	if periodDays <= 0 || weekCount <= 0 {
		b.WriteString(`<p>Intervention period is not configured (set <code>intervention_period</code> in survey config).</p>`)
		return b.String(), nil
	}
	b.WriteString(fmt.Sprintf(
		`<p style="color:#475569;">Per week: <strong>Reach</strong> (whether the participant has entered that intervention week) and <strong>Message count</strong> (inbound only, after baseline). Period: %d day(s) → %d week(s).</p>`,
		periodDays, weekCount,
	))
	b.WriteString(`<p><a href="/admin/table/conversation/engagement/export?phone=` + html.EscapeString(phoneFilter) + `">Export engagement table as CSV</a></p>`)
	b.WriteString(adminTableOuterWrapOpen(len(rows)))
	b.WriteString(`<table><tr><th>Participant Name</th><th>Phone Number</th>`)
	for i := 1; i <= weekCount; i++ {
		b.WriteString(fmt.Sprintf(`<th>Reach Week %d</th><th>Message Count Week %d</th>`, i, i))
	}
	b.WriteString(`</tr>`)
	colspan := 2 + weekCount*2
	if len(rows) == 0 {
		b.WriteString(`<tr><td colspan="` + fmt.Sprintf("%d", colspan) + `">No baseline-complete participants found.</td></tr>`)
	} else {
		for _, row := range rows {
			name := row.Name
			if name == "" {
				name = "—"
			}
			b.WriteString("<tr>")
			b.WriteString("<td>" + html.EscapeString(name) + "</td>")
			b.WriteString("<td>" + html.EscapeString(row.Phone) + "</td>")
			for i := 0; i < weekCount; i++ {
				reached := false
				count := 0
				if i < len(row.WeekReached) {
					reached = row.WeekReached[i]
				}
				if i < len(row.WeekCounts) {
					count = row.WeekCounts[i]
				}
				b.WriteString("<td>" + boolLabel(reached, "true", "false") + "</td>")
				b.WriteString("<td>" + fmt.Sprintf("%d", count) + "</td>")
			}
			b.WriteString("</tr>")
		}
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())
	b.WriteString(adminGenerateEngagementRateGraphHTML(rows, weekCount))
	return b.String(), nil
}
