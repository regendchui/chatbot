package admin_panel

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"whatsapp-bot/db"
)

func adminEmbeddingTableHandler(w http.ResponseWriter, r *http.Request) {
	docFilter := strings.TrimSpace(r.URL.Query().Get("document"))
	rows, err := db.LoadAllRAGEmbeddings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filtered := make([]db.RAGEmbeddingRow, 0, len(rows))
	for _, row := range rows {
		if docFilter != "" && !strings.EqualFold(strings.TrimSpace(row.DocumentName), docFilter) {
			continue
		}
		filtered = append(filtered, row)
	}

	var b strings.Builder
	b.WriteString(adminPageHeader("Embedding Table (RAG)"))
	b.WriteString(`<h2>Embedding Table (RAG)</h2>`)
	b.WriteString(adminNav(r))
	b.WriteString(`<form method="get" action="/admin/table/embedding"><label>Filter by document name: <input name="document" value="` + html.EscapeString(docFilter) + `"></label> <button type="submit">Search</button></form>`)
	b.WriteString(`<p>Debug view of <code>RAG</code> table rows. Embedding vectors are intentionally not fully displayed.</p>`)
	b.WriteString(adminTableOuterWrapOpen(len(filtered)))
	b.WriteString(`<table border="1" cellpadding="6" cellspacing="0"><tr><th>ID</th><th>Document</th><th>Chunk Index</th><th>Chunk Text</th><th>Embedding Size</th><th>Created At</th></tr>`)
	for _, row := range filtered {
		chunkPreview := row.ChunkText
		if len([]rune(chunkPreview)) > 220 {
			chunkPreview = string([]rune(chunkPreview)[:220]) + "..."
		}
		b.WriteString(`<tr>`)
		b.WriteString(`<td>` + html.EscapeString(fmt.Sprintf("%d", row.ID)) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(row.DocumentName) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(fmt.Sprintf("%d", row.ChunkIndex)) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(chunkPreview) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(fmt.Sprintf("%d bytes", len(row.EmbeddingRaw))) + `</td>`)
		b.WriteString(`<td>` + html.EscapeString(adminFormatTime(row.CreatedAt)) + `</td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
	b.WriteString(adminTableOuterWrapClose())
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}
