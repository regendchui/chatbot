package admin_panel

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	ai "whatsapp-bot/AI"
	"whatsapp-bot/db"
)

func adminRAGHandler(w http.ResponseWriter, r *http.Request) {
	statusMsg := strings.TrimSpace(r.URL.Query().Get("msg"))
	docsMap, err := db.ListRAGDocuments()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	docNames := make([]string, 0, len(docsMap))
	for name := range docsMap {
		docNames = append(docNames, name)
	}
	sort.Strings(docNames)

	var b strings.Builder
	b.WriteString(adminPageHeader("RAG Documents"))
	b.WriteString(`<h2>RAG Documents</h2>`)
	b.WriteString(adminNav(r))
	if statusMsg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(statusMsg) + `</p>`)
	}
	b.WriteString(`<p>Add a document, chunk + embed it, and store chunks in the <code>RAG</code> table.</p>`)

	b.WriteString(`<h3>Add / Reindex document</h3>`)
	b.WriteString(`<form method="post" action="/admin/rag/add" enctype="multipart/form-data">`)
	b.WriteString(`<p><label>Document name (optional when uploading file)<br><input name="document_name" style="width:100%;max-width:480px;"></label></p>`)
	b.WriteString(`<p><label>Upload file (.pdf, .docx, .csv)<br><input type="file" name="document_file" accept=".pdf,.docx,.csv"></label></p>`)
	b.WriteString(`<p><label>Document content (used when no file is uploaded)<br><textarea name="document_text" rows="14" cols="120"></textarea></label></p>`)
	b.WriteString(`<p><button type="submit">Embed and save document</button></p>`)
	b.WriteString(`</form>`)

	b.WriteString(`<h3>Existing documents</h3>`)
	if len(docNames) == 0 {
		b.WriteString(`<p><em>No documents embedded yet.</em></p>`)
	} else {
		b.WriteString(adminTableOuterWrapOpen(len(docNames)))
		b.WriteString(`<table border="1" cellpadding="6" cellspacing="0"><tr><th>Document</th><th>Chunk rows</th><th>Action</th></tr>`)
		for _, name := range docNames {
			count := docsMap[name]
			b.WriteString(`<tr>`)
			b.WriteString(`<td>` + html.EscapeString(name) + `</td>`)
			b.WriteString(`<td>` + fmt.Sprintf("%d", count) + `</td>`)
			b.WriteString(`<td>`)
			b.WriteString(`<form method="post" action="/admin/rag/delete-document" onsubmit="return confirm('Delete all embeddings for this document?');">`)
			b.WriteString(`<input type="hidden" name="document_name" value="` + html.EscapeString(name) + `">`)
			b.WriteString(`<button type="submit" style="background:#b91c1c;border-color:#991b1b;">Delete document embeddings</button>`)
			b.WriteString(`</form>`)
			b.WriteString(`</td>`)
			b.WriteString(`</tr>`)
		}
		b.WriteString(`</table>`)
		b.WriteString(adminTableOuterWrapClose())
	}
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminRAGAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape("Invalid form data."), http.StatusSeeOther)
		return
	}
	documentName := strings.TrimSpace(r.FormValue("document_name"))
	documentText := strings.TrimSpace(r.FormValue("document_text"))
	file, header, err := r.FormFile("document_file")
	if err == nil && file != nil {
		defer file.Close()
		raw, readErr := io.ReadAll(file)
		if readErr != nil {
			http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape("Failed to read uploaded file."), http.StatusSeeOther)
			return
		}
		extracted, extractErr := ai.ExtractTextFromRAGFile(header.Filename, raw)
		if extractErr != nil {
			http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape("Failed to parse uploaded file: "+extractErr.Error()), http.StatusSeeOther)
			return
		}
		documentText = strings.TrimSpace(extracted)
		if documentName == "" {
			base := strings.TrimSpace(filepath.Base(header.Filename))
			documentName = strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
		}
	}
	if documentName == "" || documentText == "" {
		http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape("Provide document name and either document text or a supported file (.pdf, .docx, .csv)."), http.StatusSeeOther)
		return
	}
	inserted, err := ai.IndexDocumentForRAG(documentName, documentText)
	if err != nil {
		http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape("Failed to embed document: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape(fmt.Sprintf("Document embedded successfully (%d chunk row(s)).", inserted)), http.StatusSeeOther)
}

func adminRAGDeleteDocumentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape("Invalid form data."), http.StatusSeeOther)
		return
	}
	documentName := strings.TrimSpace(r.FormValue("document_name"))
	if documentName == "" {
		http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape("Document name is required."), http.StatusSeeOther)
		return
	}
	deleted, err := ai.DeleteDocumentFromRAG(documentName)
	if err != nil {
		http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape("Delete failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/rag?msg="+url.QueryEscape(fmt.Sprintf("Deleted %d embedding row(s) for document %q.", deleted, documentName)), http.StatusSeeOther)
}
