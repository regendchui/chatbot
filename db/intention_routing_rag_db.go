package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrIntentionRoutingRAGConflict = errors.New("intention routing RAG workflow was changed by another administrator")

type IntentionRoutingRAGWorkflowRecord struct {
	ID          int64
	Revision    int
	LockVersion int
	Status      string
	Graph       json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt *time.Time
	CreatedBy   string
	PublishedBy string
}

func EnsureIntentionRoutingRAGWorkflowTableExists() error {
	query := `
CREATE TABLE IF NOT EXISTS intention_routing_rag_workflow (
    id BIGSERIAL PRIMARY KEY,
    revision INTEGER NOT NULL UNIQUE,
    lock_version INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL CHECK (status IN ('draft', 'published', 'archived')),
    graph JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMPTZ,
    created_by TEXT NOT NULL DEFAULT '',
    published_by TEXT NOT NULL DEFAULT ''
);`
	if _, err := DB.Exec(context.Background(), query); err != nil {
		return fmt.Errorf("create intention routing RAG workflow table: %w", err)
	}
	indexes := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS intention_routing_rag_one_published_idx ON intention_routing_rag_workflow (status) WHERE status = 'published'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS intention_routing_rag_one_draft_idx ON intention_routing_rag_workflow (status) WHERE status = 'draft'`,
	}
	for _, index := range indexes {
		if _, err := DB.Exec(context.Background(), index); err != nil {
			return fmt.Errorf("create intention routing RAG workflow index: %w", err)
		}
	}
	return nil
}

func LoadIntentionRoutingRAGDraft(ctx context.Context) (*IntentionRoutingRAGWorkflowRecord, error) {
	return loadIntentionRoutingRAGWorkflow(ctx, "draft")
}

func LoadPublishedIntentionRoutingRAGWorkflow(ctx context.Context) (*IntentionRoutingRAGWorkflowRecord, error) {
	return loadIntentionRoutingRAGWorkflow(ctx, "published")
}

func loadIntentionRoutingRAGWorkflow(ctx context.Context, status string) (*IntentionRoutingRAGWorkflowRecord, error) {
	row := DB.QueryRow(ctx, `
SELECT id, revision, lock_version, status, graph::text, created_at, updated_at,
       published_at, created_by, published_by
FROM intention_routing_rag_workflow
WHERE status = $1
LIMIT 1`, strings.TrimSpace(status))
	return scanIntentionRoutingRAGWorkflow(row)
}

type workflowRowScanner interface {
	Scan(dest ...any) error
}

func scanIntentionRoutingRAGWorkflow(row workflowRowScanner) (*IntentionRoutingRAGWorkflowRecord, error) {
	var record IntentionRoutingRAGWorkflowRecord
	var graphText string
	err := row.Scan(
		&record.ID,
		&record.Revision,
		&record.LockVersion,
		&record.Status,
		&graphText,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.PublishedAt,
		&record.CreatedBy,
		&record.PublishedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan intention routing RAG workflow: %w", err)
	}
	record.Graph = json.RawMessage(strings.TrimSpace(graphText))
	return &record, nil
}

func SaveIntentionRoutingRAGDraft(ctx context.Context, graph json.RawMessage, actor string, expectedLockVersion int) (*IntentionRoutingRAGWorkflowRecord, error) {
	normalized, err := normalizeWorkflowGraphJSON(graph)
	if err != nil {
		return nil, err
	}
	tx, err := DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin save intention routing RAG draft: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE intention_routing_rag_workflow IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return nil, fmt.Errorf("lock intention routing RAG workflow table: %w", err)
	}

	existing, err := scanIntentionRoutingRAGWorkflow(tx.QueryRow(ctx, `
SELECT id, revision, lock_version, status, graph::text, created_at, updated_at,
       published_at, created_by, published_by
FROM intention_routing_rag_workflow WHERE status = 'draft' FOR UPDATE`))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if existing != nil {
		if expectedLockVersion != existing.LockVersion {
			return nil, ErrIntentionRoutingRAGConflict
		}
		if _, err := tx.Exec(ctx, `
UPDATE intention_routing_rag_workflow
SET graph = $2::jsonb, lock_version = lock_version + 1, updated_at = $3, created_by = $4
WHERE id = $1`, existing.ID, string(normalized), now, strings.TrimSpace(actor)); err != nil {
			return nil, fmt.Errorf("update intention routing RAG draft: %w", err)
		}
	} else {
		if expectedLockVersion != 0 {
			return nil, ErrIntentionRoutingRAGConflict
		}
		var revision int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM intention_routing_rag_workflow`).Scan(&revision); err != nil {
			return nil, fmt.Errorf("allocate intention routing RAG revision: %w", err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO intention_routing_rag_workflow
    (revision, lock_version, status, graph, created_at, updated_at, created_by)
VALUES ($1, 1, 'draft', $2::jsonb, $3, $3, $4)`, revision, string(normalized), now, strings.TrimSpace(actor)); err != nil {
			return nil, fmt.Errorf("insert intention routing RAG draft: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit intention routing RAG draft: %w", err)
	}
	return LoadIntentionRoutingRAGDraft(ctx)
}

func PublishIntentionRoutingRAGDraft(ctx context.Context, actor string, expectedLockVersion int) (*IntentionRoutingRAGWorkflowRecord, error) {
	tx, err := DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin publish intention routing RAG workflow: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE intention_routing_rag_workflow IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return nil, fmt.Errorf("lock intention routing RAG workflow table: %w", err)
	}

	draft, err := scanIntentionRoutingRAGWorkflow(tx.QueryRow(ctx, `
SELECT id, revision, lock_version, status, graph::text, created_at, updated_at,
       published_at, created_by, published_by
FROM intention_routing_rag_workflow WHERE status = 'draft' FOR UPDATE`))
	if err != nil {
		return nil, err
	}
	if draft == nil {
		return nil, fmt.Errorf("no draft workflow exists")
	}
	if draft.LockVersion != expectedLockVersion {
		return nil, ErrIntentionRoutingRAGConflict
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
UPDATE intention_routing_rag_workflow
SET status = 'archived', updated_at = $1
WHERE status = 'published'`, now); err != nil {
		return nil, fmt.Errorf("archive published intention routing RAG workflow: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE intention_routing_rag_workflow
SET status = 'published', lock_version = lock_version + 1, updated_at = $2,
    published_at = $2, published_by = $3
WHERE id = $1`, draft.ID, now, strings.TrimSpace(actor)); err != nil {
		return nil, fmt.Errorf("publish intention routing RAG workflow: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit published intention routing RAG workflow: %w", err)
	}
	return LoadPublishedIntentionRoutingRAGWorkflow(ctx)
}

func DiscardIntentionRoutingRAGDraft(ctx context.Context, actor string, expectedLockVersion int) (*IntentionRoutingRAGWorkflowRecord, error) {
	tx, err := DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin discard intention routing RAG draft: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE intention_routing_rag_workflow IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return nil, fmt.Errorf("lock intention routing RAG workflow table: %w", err)
	}
	draft, err := scanIntentionRoutingRAGWorkflow(tx.QueryRow(ctx, `
SELECT id, revision, lock_version, status, graph::text, created_at, updated_at,
       published_at, created_by, published_by
FROM intention_routing_rag_workflow WHERE status = 'draft' FOR UPDATE`))
	if err != nil {
		return nil, err
	}
	if draft != nil && draft.LockVersion != expectedLockVersion {
		return nil, ErrIntentionRoutingRAGConflict
	}
	published, err := scanIntentionRoutingRAGWorkflow(tx.QueryRow(ctx, `
SELECT id, revision, lock_version, status, graph::text, created_at, updated_at,
       published_at, created_by, published_by
FROM intention_routing_rag_workflow WHERE status = 'published' FOR SHARE`))
	if err != nil {
		return nil, err
	}
	if draft == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty intention routing RAG discard: %w", err)
		}
		return published, nil
	}
	if published == nil {
		if _, err := tx.Exec(ctx, `DELETE FROM intention_routing_rag_workflow WHERE id = $1`, draft.ID); err != nil {
			return nil, fmt.Errorf("delete intention routing RAG draft: %w", err)
		}
	} else {
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `
UPDATE intention_routing_rag_workflow
SET graph = $2::jsonb, lock_version = lock_version + 1, updated_at = $3, created_by = $4
WHERE id = $1`, draft.ID, string(published.Graph), now, strings.TrimSpace(actor)); err != nil {
			return nil, fmt.Errorf("reset intention routing RAG draft: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit discarded intention routing RAG draft: %w", err)
	}
	return LoadIntentionRoutingRAGDraft(ctx)
}

func normalizeWorkflowGraphJSON(raw json.RawMessage) (json.RawMessage, error) {
	var value any
	if len(raw) == 0 {
		return nil, fmt.Errorf("workflow graph is empty")
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("parse workflow graph: %w", err)
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow graph: %w", err)
	}
	return normalized, nil
}
