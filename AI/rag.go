package ai

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"whatsapp-bot/db"

	pdf "github.com/ledongthuc/pdf"
)

const defaultRAGEmbeddingURL = "https://openrouter.ai/api/v1/embeddings"

type openRouterEmbeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type openRouterEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

type ragScoredChunk struct {
	DocumentName string
	ChunkIndex   int
	ChunkText    string
	Score        float64
	VectorSize   int
}

func ragEnabled() bool {
	return db.GetProjectSettingBool("RAG_ENABLED", false)
}

func ragChunkSize() int {
	n := db.GetProjectSettingInt("RAG_CHUNK_SIZE", 800)
	if n < 100 {
		return 800
	}
	return n
}

func ragChunkOverlap() int {
	n := db.GetProjectSettingInt("RAG_CHUNK_OVERLAP", 100)
	if n < 0 {
		return 0
	}
	if n >= ragChunkSize() {
		return ragChunkSize() / 4
	}
	return n
}

func ragTopK() int {
	n := db.GetProjectSettingInt("RAG_TOP_K", 3)
	if n <= 0 {
		return 3
	}
	return n
}

func ragMaxContextChars() int {
	n := db.GetProjectSettingInt("RAG_MAX_CONTEXT_CHARS", 2500)
	if n < 200 {
		return 2500
	}
	return n
}

func ragMinSimilarity() float64 {
	raw := strings.TrimSpace(db.GetProjectSettingString("RAG_MIN_SIMILARITY", "0.2"))
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0.2
	}
	if v < -1 || v > 1 {
		return 0.2
	}
	return v
}

func ragEmbeddingModel() string {
	model := strings.TrimSpace(db.GetProjectSettingString("RAG_EMBEDDING_MODEL", "openai/text-embedding-3-small"))
	if model == "" {
		model = strings.TrimSpace(os.Getenv("RAG_EMBEDDING_MODEL"))
	}
	if model == "" {
		return "openai/text-embedding-3-small"
	}
	return model
}

func ragEmbeddingURL() string {
	if url := strings.TrimSpace(db.GetProjectSettingString("RAG_EMBEDDING_URL", "")); url != "" {
		return url
	}
	if url := strings.TrimSpace(os.Getenv("RAG_EMBEDDING_URL")); url != "" {
		return url
	}
	openRouterURL := strings.TrimSpace(db.GetProjectSettingString("OPENROUTER_URL", ""))
	if openRouterURL == "" {
		openRouterURL = strings.TrimSpace(os.Getenv("OPENROUTER_URL"))
	}
	if openRouterURL != "" {
		if strings.Contains(openRouterURL, "/chat/completions") {
			return strings.Replace(openRouterURL, "/chat/completions", "/embeddings", 1)
		}
		trimmed := strings.TrimRight(openRouterURL, "/")
		if strings.HasSuffix(trimmed, "/api/v1") || strings.HasSuffix(trimmed, "/v1") {
			return trimmed + "/embeddings"
		}
	}
	return defaultRAGEmbeddingURL
}

func ragSliceProtectSignals() (string, string) {
	openSignal := strings.TrimSpace(db.GetProjectSettingString("RAG_SLICE_PROTECT_OPEN_SIGNAL", ""))
	closeSignal := strings.TrimSpace(db.GetProjectSettingString("RAG_SLICE_PROTECT_CLOSE_SIGNAL", ""))
	if openSignal == "" || closeSignal == "" || openSignal == closeSignal {
		return "", ""
	}
	return openSignal, closeSignal
}

// IndexDocumentForRAG chunks and embeds a document, replacing previous rows for same name.
func IndexDocumentForRAG(documentName string, documentText string) (int, error) {
	doc := strings.TrimSpace(documentName)
	text := normalizeRAGText(documentText)
	if doc == "" {
		return 0, fmt.Errorf("document name is empty")
	}
	if text == "" {
		return 0, fmt.Errorf("document text is empty")
	}
	chunks := chunkText(text, ragChunkSize(), ragChunkOverlap())
	if len(chunks) == 0 {
		return 0, fmt.Errorf("document produced no chunks")
	}
	if _, err := db.DeleteRAGByDocument(doc); err != nil {
		return 0, err
	}
	inserted := 0
	for i, chunk := range chunks {
		embedding, err := embedTextForRAG(chunk)
		if err != nil {
			return inserted, fmt.Errorf("embed chunk %d: %w", i, err)
		}
		if err := db.InsertRAGEmbedding(doc, i, chunk, embedding); err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

// DeleteDocumentFromRAG removes all embeddings for one document name.
func DeleteDocumentFromRAG(documentName string) (int64, error) {
	return db.DeleteRAGByDocument(documentName)
}

// BuildRAGContext retrieves most relevant chunks for query text.
func BuildRAGContext(query string) (string, error) {
	contextText, _, err := buildRAGContextInternal(query)
	return contextText, err
}

// BuildRAGContextWithDebug returns RAG context plus debug diagnostics for logging.
func BuildRAGContextWithDebug(query string) (string, string, error) {
	return buildRAGContextInternal(query)
}

func buildRAGContextInternal(query string) (string, string, error) {
	if !ragEnabled() {
		return "", "rag_enabled=false", nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return "", "rag_enabled=true query_empty=true", nil
	}
	rows, err := db.LoadAllRAGEmbeddings()
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 {
		return "", "rag_enabled=true total_embeddings=0", nil
	}
	queryVec, err := embedTextForRAG(q)
	if err != nil {
		return "", "", err
	}
	scored := make([]ragScoredChunk, 0, len(rows))
	minScore := ragMinSimilarity()
	for _, row := range rows {
		chunkVec, err := parseEmbeddingJSON(row.EmbeddingRaw)
		if err != nil {
			continue
		}
		score := cosineSimilarity(queryVec, chunkVec)
		if score < minScore {
			continue
		}
		scored = append(scored, ragScoredChunk{
			DocumentName: row.DocumentName,
			ChunkIndex:   row.ChunkIndex,
			ChunkText:    row.ChunkText,
			Score:        score,
			VectorSize:   len(chunkVec),
		})
	}
	if len(scored) == 0 {
		debug := fmt.Sprintf("rag_enabled=true total_embeddings=%d query_vector_size=%d min_similarity=%.4f matched_chunks=0", len(rows), len(queryVec), minScore)
		return "", debug, nil
	}
	sort.Slice(scored, func(i int, j int) bool { return scored[i].Score > scored[j].Score })
	topK := ragTopK()
	if topK > len(scored) {
		topK = len(scored)
	}
	var b strings.Builder
	b.WriteString("RAG KNOWLEDGE CONTEXT (most relevant chunks):\n")
	maxChars := ragMaxContextChars()
	for i := 0; i < topK; i++ {
		chunk := scored[i]
		prettyChunk := normalizeRAGText(chunk.ChunkText)
		line := fmt.Sprintf("- [doc=%s chunk=%d score=%.3f] %s\n", chunk.DocumentName, chunk.ChunkIndex, chunk.Score, prettyChunk)
		if b.Len()+len(line) > maxChars {
			break
		}
		b.WriteString(line)
	}
	selected := make([]string, 0, topK)
	for i := 0; i < topK; i++ {
		chunk := scored[i]
		selected = append(selected, fmt.Sprintf("%s#%d(score=%.4f,vector=%d)", chunk.DocumentName, chunk.ChunkIndex, chunk.Score, chunk.VectorSize))
	}
	debug := fmt.Sprintf(
		"rag_enabled=true total_embeddings=%d query_vector_size=%d min_similarity=%.4f matched_chunks=%d top_k=%d selected=%s",
		len(rows),
		len(queryVec),
		minScore,
		len(scored),
		topK,
		strings.Join(selected, "; "),
	)
	return strings.TrimSpace(b.String()), debug, nil
}

func embedTextForRAG(text string) ([]float64, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is required for RAG embeddings")
	}
	reqPayload := openRouterEmbeddingRequest{
		Model: ragEmbeddingModel(),
		Input: strings.TrimSpace(text),
	}
	reqJSON, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal RAG embedding request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, ragEmbeddingURL(), bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("create RAG embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if referer := strings.TrimSpace(os.Getenv("OPENROUTER_SITE_URL")); referer != "" {
		req.Header.Set("HTTP-Referer", referer)
	}
	if title := strings.TrimSpace(os.Getenv("OPENROUTER_APP_NAME")); title != "" {
		req.Header.Set("X-Title", title)
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call RAG embedding API: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read RAG embedding response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("RAG embedding API status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed openRouterEmbeddingResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal RAG embedding response: %w", err)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("RAG embedding API returned empty vector")
	}
	return parsed.Data[0].Embedding, nil
}

func parseEmbeddingJSON(raw []byte) ([]float64, error) {
	out := []float64{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("embedding is empty")
	}
	return out, nil
}

func cosineSimilarity(a []float64, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return -1
	}
	dot := 0.0
	normA := 0.0
	normB := 0.0
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return -1
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func chunkText(text string, chunkSize int, overlap int) []string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return nil
	}
	openSignal, closeSignal := ragSliceProtectSignals()
	units := splitIntoChunkUnits(clean, openSignal, closeSignal)
	if len(units) == 0 {
		return nil
	}
	if len(units) == 1 && utf8.RuneCountInString(units[0]) <= chunkSize {
		return []string{units[0]}
	}
	out := []string{}
	for start := 0; start < len(units); {
		var b strings.Builder
		curLen := 0
		end := start
		for end < len(units) {
			unit := strings.TrimSpace(units[end])
			if unit == "" {
				end++
				continue
			}
			unitLen := utf8.RuneCountInString(unit)
			if end > start && curLen+1+unitLen > chunkSize {
				break
			}
			if end == start && unitLen > chunkSize {
				b.WriteString(unit) // Keep very long protected block whole.
				curLen = unitLen
				end++
				break
			}
			if b.Len() > 0 {
				b.WriteString(" ")
				curLen++
			}
			b.WriteString(unit)
			curLen += unitLen
			end++
		}
		chunk := strings.TrimSpace(b.String())
		if chunk != "" {
			out = append(out, chunk)
		}
		if end >= len(units) {
			break
		}
		if overlap <= 0 {
			start = end
			continue
		}
		carryLen := 0
		nextStart := end
		for i := end - 1; i >= start; i-- {
			carryLen += utf8.RuneCountInString(strings.TrimSpace(units[i]))
			nextStart = i
			if carryLen >= overlap {
				break
			}
		}
		if nextStart >= end {
			start = end
		} else {
			start = nextStart
		}
	}
	return out
}

func splitIntoChunkUnits(text string, openSignal string, closeSignal string) []string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return nil
	}
	if openSignal == "" || closeSignal == "" {
		return []string{clean}
	}
	units := []string{}
	cursor := 0
	for cursor < len(clean) {
		openIdx := strings.Index(clean[cursor:], openSignal)
		if openIdx < 0 {
			rest := strings.TrimSpace(clean[cursor:])
			if rest != "" {
				units = append(units, rest)
			}
			break
		}
		absOpen := cursor + openIdx
		prefix := strings.TrimSpace(clean[cursor:absOpen])
		if prefix != "" {
			units = append(units, prefix)
		}
		afterOpen := absOpen + len(openSignal)
		closeIdx := strings.Index(clean[afterOpen:], closeSignal)
		if closeIdx < 0 {
			rest := strings.TrimSpace(clean[absOpen:])
			if rest != "" {
				units = append(units, rest)
			}
			break
		}
		absCloseEnd := afterOpen + closeIdx + len(closeSignal)
		protected := strings.TrimSpace(clean[absOpen:absCloseEnd])
		if protected != "" {
			units = append(units, protected)
		}
		cursor = absCloseEnd
	}
	if len(units) == 0 {
		return []string{clean}
	}
	return units
}

// ExtractTextFromRAGFile parses supported file types into plain text for chunking/embedding.
func ExtractTextFromRAGFile(fileName string, fileBytes []byte) (string, error) {
	name := strings.TrimSpace(fileName)
	if name == "" {
		return "", fmt.Errorf("file name is empty")
	}
	if len(fileBytes) == 0 {
		return "", fmt.Errorf("file is empty")
	}
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(name)))
	switch ext {
	case ".pdf":
		return extractTextFromPDFBytes(fileBytes)
	case ".docx":
		return extractTextFromDOCXBytes(fileBytes)
	case ".csv":
		return extractTextFromCSVBytes(fileBytes)
	default:
		return "", fmt.Errorf("unsupported file type %q (supported: .pdf, .docx, .csv)", ext)
	}
}

func extractTextFromPDFBytes(raw []byte) (string, error) {
	tmp, err := os.CreateTemp("", "rag-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp PDF: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write temp PDF: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp PDF: %w", err)
	}
	f, reader, err := pdf.Open(tmpPath)
	if err != nil {
		return "", fmt.Errorf("open PDF: %w", err)
	}
	defer f.Close()
	var b strings.Builder
	totalPage := reader.NumPage()
	for i := 1; i <= totalPage; i++ {
		p := reader.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimSpace(text))
	}
	out := normalizeRAGText(b.String())
	if out == "" {
		return "", fmt.Errorf("no extractable text found in PDF")
	}
	return out, nil
}

type docxDocument struct {
	Body docxBody `xml:"body"`
}

type docxBody struct {
	Paragraphs []docxParagraph `xml:"p"`
}

type docxParagraph struct {
	Runs []docxRun `xml:"r"`
}

type docxRun struct {
	Texts []string `xml:"t"`
}

func extractTextFromDOCXBytes(raw []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("open DOCX zip: %w", err)
	}
	var docXML []byte
	for _, f := range zr.File {
		if strings.EqualFold(strings.TrimSpace(f.Name), "word/document.xml") {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("open DOCX document.xml: %w", err)
			}
			docXML, err = io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return "", fmt.Errorf("read DOCX document.xml: %w", err)
			}
			break
		}
	}
	if len(docXML) == 0 {
		return "", fmt.Errorf("DOCX missing word/document.xml")
	}
	var parsed docxDocument
	if err := xml.Unmarshal(docXML, &parsed); err != nil {
		return "", fmt.Errorf("parse DOCX XML: %w", err)
	}
	lines := make([]string, 0, len(parsed.Body.Paragraphs))
	for _, p := range parsed.Body.Paragraphs {
		var line strings.Builder
		for _, run := range p.Runs {
			for _, t := range run.Texts {
				line.WriteString(t)
			}
		}
		txt := strings.TrimSpace(line.String())
		if txt != "" {
			lines = append(lines, txt)
		}
	}
	out := normalizeRAGText(strings.Join(lines, "\n"))
	if out == "" {
		return "", fmt.Errorf("no extractable text found in DOCX")
	}
	return out, nil
}

func extractTextFromCSVBytes(raw []byte) (string, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return "", fmt.Errorf("parse CSV: %w", err)
	}
	if len(records) == 0 {
		return "", fmt.Errorf("CSV is empty")
	}
	lines := make([]string, 0, len(records))
	for _, rec := range records {
		cells := make([]string, 0, len(rec))
		for _, c := range rec {
			clean := strings.TrimSpace(c)
			if clean != "" {
				cells = append(cells, clean)
			}
		}
		if len(cells) == 0 {
			continue
		}
		lines = append(lines, strings.Join(cells, " | "))
	}
	out := normalizeRAGText(strings.Join(lines, "\n"))
	if out == "" {
		return "", fmt.Errorf("no extractable text found in CSV")
	}
	return out, nil
}

// normalizeRAGText cleans extraction artifacts so chunking/embedding stays semantic.
func normalizeRAGText(raw string) string {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	// Remove whitespace between two CJK characters (common PDF artifact: one char per line).
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if unicode.IsSpace(r) {
			prev := rune(0)
			next := rune(0)
			hasPrev := false
			hasNext := false
			for j := i - 1; j >= 0; j-- {
				if unicode.IsSpace(runes[j]) {
					continue
				}
				prev = runes[j]
				hasPrev = true
				break
			}
			for j := i + 1; j < len(runes); j++ {
				if unicode.IsSpace(runes[j]) {
					continue
				}
				next = runes[j]
				hasNext = true
				break
			}
			if hasPrev && hasNext && isCJKRune(prev) && isCJKRune(next) {
				continue
			}
			if b.Len() > 0 {
				bs := b.String()
				if len(bs) > 0 && bs[len(bs)-1] != ' ' {
					b.WriteRune(' ')
				}
			}
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	return out
}

func isCJKRune(r rune) bool {
	return unicode.In(r,
		unicode.Han,
		unicode.Hiragana,
		unicode.Katakana,
		unicode.Hangul,
	)
}
