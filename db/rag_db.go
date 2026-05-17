package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RAGEmbeddingRow is one embedding row stored in the RAG table.
type RAGEmbeddingRow struct {
	ID           int64
	DocumentName string
	ChunkIndex   int
	ChunkText    string
	EmbeddingRaw []byte
	CreatedAt    time.Time
}

// EnsureRAGTableExists creates the RAG table for document embeddings.
func EnsureRAGTableExists() error {
	query := `
CREATE TABLE IF NOT EXISTS "RAG" (
    id BIGSERIAL PRIMARY KEY,
    document_name TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,
    chunk_text TEXT NOT NULL,
    embedding JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
	if _, err := DB.Exec(context.Background(), query); err != nil {
		return fmt.Errorf("create RAG table: %w", err)
	}
	indexQuery := `
CREATE INDEX IF NOT EXISTS rag_document_name_idx ON "RAG" (document_name);
`
	if _, err := DB.Exec(context.Background(), indexQuery); err != nil {
		return fmt.Errorf("create RAG index: %w", err)
	}
	return nil
}

// InsertRAGEmbedding stores one chunk embedding row.
func InsertRAGEmbedding(documentName string, chunkIndex int, chunkText string, embedding []float64) error {
	doc := strings.TrimSpace(documentName)
	text := strings.TrimSpace(chunkText)
	if doc == "" {
		return fmt.Errorf("document name is empty")
	}
	if text == "" {
		return fmt.Errorf("chunk text is empty")
	}
	raw, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}
	_, err = DB.Exec(
		context.Background(),
		`INSERT INTO "RAG" (document_name, chunk_index, chunk_text, embedding) VALUES ($1, $2, $3, $4::jsonb)`,
		doc,
		chunkIndex,
		text,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("insert RAG embedding: %w", err)
	}
	return nil
}

// DeleteRAGByDocument removes all embedding rows for a document.
func DeleteRAGByDocument(documentName string) (int64, error) {
	doc := strings.TrimSpace(documentName)
	if doc == "" {
		return 0, fmt.Errorf("document name is empty")
	}
	tag, err := DB.Exec(context.Background(), `DELETE FROM "RAG" WHERE document_name = $1`, doc)
	if err != nil {
		return 0, fmt.Errorf("delete RAG document rows: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ListRAGDocuments returns distinct document names and row counts.
func ListRAGDocuments() (map[string]int64, error) {
	rows, err := DB.Query(context.Background(), `SELECT document_name, COUNT(1) FROM "RAG" GROUP BY document_name ORDER BY document_name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list RAG documents: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var doc string
		var count int64
		if err := rows.Scan(&doc, &count); err != nil {
			return nil, fmt.Errorf("scan RAG documents: %w", err)
		}
		out[strings.TrimSpace(doc)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate RAG documents: %w", err)
	}
	return out, nil
}

// LoadAllRAGEmbeddings returns all rows in the RAG table.
func LoadAllRAGEmbeddings() ([]RAGEmbeddingRow, error) {
	rows, err := DB.Query(context.Background(), `SELECT id, document_name, chunk_index, chunk_text, embedding::text, created_at FROM "RAG" ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query RAG rows: %w", err)
	}
	defer rows.Close()
	out := make([]RAGEmbeddingRow, 0, 64)
	for rows.Next() {
		var row RAGEmbeddingRow
		var embText string
		if err := rows.Scan(&row.ID, &row.DocumentName, &row.ChunkIndex, &row.ChunkText, &embText, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan RAG row: %w", err)
		}
		row.DocumentName = strings.TrimSpace(row.DocumentName)
		row.ChunkText = strings.TrimSpace(row.ChunkText)
		row.EmbeddingRaw = []byte(strings.TrimSpace(embText))
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate RAG rows: %w", err)
	}
	return out, nil
}
