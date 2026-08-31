package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"whatsapp-bot/common"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const graphRAGGraphName = "knowledge_graph"

const graphRAGEntityUpsertCypher = `MATCH (c:Chunk {snapshot_id: $snapshot_id, chunk_index: $chunk_index}) MERGE (e:Entity {canonical_key: $canonical_key}) SET e.canonical_name=$canonical_name, e.entity_type=$entity_type, e.aliases=$aliases, e.alias_keys=$alias_keys, e.confidence=CASE WHEN coalesce(e.confidence,0) < $confidence THEN $confidence ELSE e.confidence END MERGE (c)-[:MENTIONS {snapshot_id: $snapshot_id, document_name: $document_name, chunk_index: $chunk_index}]->(e) RETURN e.canonical_name`

const graphRAGRelationshipCreateCypher = `MERGE (a:Entity {canonical_key: $from_key}) SET a.canonical_name=coalesce(a.canonical_name,$from_name), a.entity_type=coalesce(a.entity_type,'Unknown'), a.aliases=coalesce(a.aliases,[]), a.alias_keys=coalesce(a.alias_keys,[]) MERGE (b:Entity {canonical_key: $to_key}) SET b.canonical_name=coalesce(b.canonical_name,$to_name), b.entity_type=coalesce(b.entity_type,'Unknown'), b.aliases=coalesce(b.aliases,[]), b.alias_keys=coalesce(b.alias_keys,[]) CREATE (a)-[:RELATED_TO {relation_key:$relation_key, from_key:$from_key, to_key:$to_key, relation_type:$relation_type, description:$description, confidence:$confidence, snapshot_id:$snapshot_id, document_name:$document_name, chunk_index:$chunk_index}]->(b) RETURN a.canonical_name`

const graphRAGDeleteOrphanEntitiesCypher = `MATCH (e:Entity) OPTIONAL MATCH (e)<-[m:MENTIONS]-() OPTIONAL MATCH (e)-[r:RELATED_TO]-() WITH e,m,r WHERE m IS NULL AND r IS NULL DELETE e RETURN count(e)`

const graphRAGResolveEntityCypher = `UNWIND $keys AS key MATCH (e:Entity) WHERE e.canonical_key = key OR key IN e.alias_keys RETURN e.canonical_key,e.aliases,e.alias_keys ORDER BY e.canonical_key LIMIT 1`

const graphRAGResolveSeedCypher = `UNWIND $terms AS term MATCH (e:Entity) WHERE e.canonical_key = term OR e.canonical_key CONTAINS term OR term CONTAINS e.canonical_key OR term IN e.alias_keys RETURN DISTINCT e.canonical_key,e.alias_keys ORDER BY e.canonical_key LIMIT $candidate_limit`

const graphRAGMinimumSeedMatchScore = 0.35

type graphRAGSeedCandidate struct {
	CanonicalKey string
	AliasKeys    []string
}

func EnsureGraphRAGInfrastructure() error {
	metadata := `
CREATE TABLE IF NOT EXISTS graph_rag_documents (
    id TEXT PRIMARY KEY,
    document_name TEXT NOT NULL UNIQUE,
    selected BOOLEAN NOT NULL DEFAULT FALSE,
    content_hash TEXT NOT NULL DEFAULT '',
    active_snapshot_id TEXT NOT NULL DEFAULT '',
    required_settings_hash TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'not_built',
    stale BOOLEAN NOT NULL DEFAULT TRUE,
    last_error TEXT NOT NULL DEFAULT '',
    entity_count INTEGER NOT NULL DEFAULT 0,
    relationship_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS graph_rag_jobs (
    id BIGSERIAL PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES graph_rag_documents(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'rebuild',
    status TEXT NOT NULL DEFAULT 'queued',
    settings_hash TEXT NOT NULL DEFAULT '',
    processed_chunks INTEGER NOT NULL DEFAULT 0,
    total_chunks INTEGER NOT NULL DEFAULT 0,
    entity_count INTEGER NOT NULL DEFAULT 0,
    relationship_count INTEGER NOT NULL DEFAULT 0,
    prompt_tokens BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS graph_rag_jobs_status_created_idx ON graph_rag_jobs(status, created_at);
CREATE TABLE IF NOT EXISTS graph_rag_snapshots (
    id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES graph_rag_documents(id) ON DELETE CASCADE,
    content_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    entity_count INTEGER NOT NULL DEFAULT 0,
    relationship_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    activated_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS graph_rag_audit (
    id BIGSERIAL PRIMARY KEY,
    action TEXT NOT NULL,
    document_name TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS graph_rag_extraction_audit (
    id BIGSERIAL PRIMARY KEY,
    job_id BIGINT NOT NULL REFERENCES graph_rag_jobs(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    raw_response TEXT NOT NULL DEFAULT '',
    validation_error TEXT NOT NULL DEFAULT '',
    prompt_tokens BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE graph_rag_documents ADD COLUMN IF NOT EXISTS required_settings_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE graph_rag_jobs ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;`
	if _, err := DB.Exec(context.Background(), metadata); err != nil {
		return fmt.Errorf("create Graph RAG metadata tables: %w", err)
	}
	if _, err := DB.Exec(context.Background(), `CREATE EXTENSION IF NOT EXISTS age`); err != nil {
		return fmt.Errorf("enable Apache AGE extension: %w", err)
	}
	conn, err := DB.Acquire(context.Background())
	if err != nil {
		return fmt.Errorf("acquire AGE setup connection: %w", err)
	}
	defer conn.Release()
	if err := configureAGEConnection(context.Background(), conn); err != nil {
		return err
	}
	var exists bool
	if err := conn.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1)`, graphRAGGraphName).Scan(&exists); err != nil {
		return fmt.Errorf("check AGE graph: %w", err)
	}
	if !exists {
		if _, err := conn.Exec(context.Background(), `SELECT ag_catalog.create_graph($1)`, graphRAGGraphName); err != nil {
			return fmt.Errorf("create AGE graph: %w", err)
		}
	}
	return nil
}

func GraphRAGAvailable(ctx context.Context) error {
	var available bool
	err := DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'age') AND EXISTS (SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1)`, graphRAGGraphName).Scan(&available)
	if err != nil {
		return fmt.Errorf("check Graph RAG availability: %w", err)
	}
	if !available {
		return fmt.Errorf("Apache AGE graph is not initialized")
	}
	return nil
}

func ListGraphRAGDocuments(ctx context.Context) ([]GraphRAGDocument, error) {
	rows, err := DB.Query(ctx, `SELECT id, document_name, selected, content_hash, active_snapshot_id, status, stale, last_error, entity_count, relationship_count, created_at, updated_at FROM graph_rag_documents ORDER BY document_name`)
	if err != nil {
		return nil, fmt.Errorf("list Graph RAG documents: %w", err)
	}
	defer rows.Close()
	result := make([]GraphRAGDocument, 0)
	for rows.Next() {
		var document GraphRAGDocument
		if err := rows.Scan(&document.ID, &document.DocumentName, &document.Selected, &document.ContentHash, &document.ActiveSnapshotID, &document.Status, &document.Stale, &document.LastError, &document.EntityCount, &document.RelationshipCount, &document.CreatedAt, &document.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan Graph RAG document: %w", err)
		}
		result = append(result, document)
	}
	return result, rows.Err()
}

func SelectGraphRAGDocument(ctx context.Context, documentName, settingsHash string) error {
	name := strings.TrimSpace(documentName)
	if name == "" {
		return fmt.Errorf("document name is required")
	}
	var exists bool
	if err := DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM "RAG" WHERE document_name = $1)`, name).Scan(&exists); err != nil {
		return fmt.Errorf("check RAG document: %w", err)
	}
	if !exists {
		return fmt.Errorf("RAG document %q does not exist", name)
	}
	documentID, err := ensureGraphRAGDocument(ctx, name)
	if err != nil {
		return err
	}
	if _, err := DB.Exec(ctx, `UPDATE graph_rag_documents SET selected=TRUE,stale=TRUE,required_settings_hash=$2,status=CASE WHEN active_snapshot_id='' THEN 'queued' ELSE status END,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, documentID, settingsHash); err != nil {
		return fmt.Errorf("select Graph RAG document: %w", err)
	}
	return queueGraphRAGJob(ctx, documentID, "rebuild", settingsHash)
}

func QueueGraphRAGRebuild(ctx context.Context, documentName, settingsHash string) error {
	documentID, err := ensureGraphRAGDocument(ctx, documentName)
	if err != nil {
		return err
	}
	if _, err := DB.Exec(ctx, `UPDATE graph_rag_documents SET selected=TRUE,stale=TRUE,required_settings_hash=$2,status='queued',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, documentID, settingsHash); err != nil {
		return fmt.Errorf("mark Graph RAG document queued: %w", err)
	}
	return queueGraphRAGJob(ctx, documentID, "rebuild", settingsHash)
}

func MarkGraphRAGDocumentStaleAndQueueIfSelected(ctx context.Context, documentName, settingsHash string) error {
	var documentID string
	var selected bool
	err := DB.QueryRow(ctx, `SELECT id, selected FROM graph_rag_documents WHERE document_name=$1`, strings.TrimSpace(documentName)).Scan(&documentID, &selected)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load Graph RAG document selection: %w", err)
	}
	if _, err := DB.Exec(ctx, `UPDATE graph_rag_documents SET stale=TRUE,required_settings_hash=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, documentID, settingsHash); err != nil {
		return fmt.Errorf("mark Graph RAG document stale: %w", err)
	}
	if selected {
		return queueGraphRAGJob(ctx, documentID, "rebuild", settingsHash)
	}
	return nil
}

func MarkAllGraphRAGDocumentsStale(ctx context.Context, settingsHash string) error {
	_, err := DB.Exec(ctx, `UPDATE graph_rag_documents SET stale=TRUE,required_settings_hash=$1,updated_at=CURRENT_TIMESTAMP WHERE selected=TRUE`, settingsHash)
	if err != nil {
		return fmt.Errorf("mark Graph RAG documents stale: %w", err)
	}
	return nil
}

func QueueAllStaleGraphRAGDocuments(ctx context.Context, settingsHash string) (int64, error) {
	if _, err := DB.Exec(ctx, `UPDATE graph_rag_documents SET required_settings_hash=$1,updated_at=CURRENT_TIMESTAMP WHERE selected=TRUE AND stale=TRUE`, settingsHash); err != nil {
		return 0, fmt.Errorf("snapshot Graph RAG settings for stale documents: %w", err)
	}
	rows, err := DB.Query(ctx, `SELECT id FROM graph_rag_documents WHERE selected=TRUE AND stale=TRUE ORDER BY document_name`)
	if err != nil {
		return 0, fmt.Errorf("list stale Graph RAG documents: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	var queued int64
	for _, id := range ids {
		if err := queueGraphRAGJob(ctx, id, "rebuild", settingsHash); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

func ensureGraphRAGDocument(ctx context.Context, documentName string) (string, error) {
	name := strings.TrimSpace(documentName)
	if name == "" {
		return "", fmt.Errorf("document name is required")
	}
	id, err := randomGraphID("doc")
	if err != nil {
		return "", err
	}
	var storedID string
	err = DB.QueryRow(ctx, `INSERT INTO graph_rag_documents(id, document_name) VALUES($1,$2) ON CONFLICT(document_name) DO UPDATE SET updated_at=CURRENT_TIMESTAMP RETURNING id`, id, name).Scan(&storedID)
	if err != nil {
		return "", fmt.Errorf("ensure Graph RAG document: %w", err)
	}
	return storedID, nil
}

func queueGraphRAGJob(ctx context.Context, documentID, kind, settingsHash string) error {
	tx, err := DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, documentID); err != nil {
		return fmt.Errorf("lock Graph RAG job queue: %w", err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM graph_rag_jobs WHERE document_id=$1 AND status='queued')`, documentID).Scan(&exists); err != nil {
		return fmt.Errorf("check active Graph RAG job: %w", err)
	}
	if exists {
		if _, err := tx.Exec(ctx, `UPDATE graph_rag_jobs SET settings_hash=$2,updated_at=CURRENT_TIMESTAMP WHERE document_id=$1 AND status='queued'`, documentID, settingsHash); err != nil {
			return fmt.Errorf("refresh queued Graph RAG job: %w", err)
		}
		return tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `INSERT INTO graph_rag_jobs(document_id,kind,status,settings_hash,updated_at) VALUES($1,$2,'queued',$3,CURRENT_TIMESTAMP)`, documentID, kind, settingsHash)
	if err != nil {
		return fmt.Errorf("queue Graph RAG job: %w", err)
	}
	return tx.Commit(ctx)
}

func ClaimNextGraphRAGJob(ctx context.Context) (*GraphRAGJob, error) {
	if _, err := DB.Exec(ctx, `WITH recovered AS (
UPDATE graph_rag_jobs SET status='queued',started_at=NULL,last_error='Recovered after interrupted worker',updated_at=CURRENT_TIMESTAMP
WHERE status='running' AND updated_at < CURRENT_TIMESTAMP-INTERVAL '5 minutes' RETURNING document_id
) UPDATE graph_rag_documents d SET status='queued',stale=TRUE,updated_at=CURRENT_TIMESTAMP FROM recovered r WHERE d.id=r.document_id`); err != nil {
		return nil, fmt.Errorf("recover abandoned Graph RAG jobs: %w", err)
	}
	tx, err := DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var job GraphRAGJob
	err = tx.QueryRow(ctx, `SELECT j.id,j.document_id,d.document_name,j.kind,j.status,j.settings_hash,j.processed_chunks,j.total_chunks,j.entity_count,j.relationship_count,j.prompt_tokens,j.completion_tokens,j.last_error,j.created_at,j.started_at,j.finished_at,j.updated_at FROM graph_rag_jobs j JOIN graph_rag_documents d ON d.id=j.document_id WHERE j.status='queued' AND d.selected=TRUE ORDER BY j.created_at FOR UPDATE OF j SKIP LOCKED LIMIT 1`).Scan(
		&job.ID, &job.DocumentID, &job.DocumentName, &job.Kind, &job.Status, &job.SettingsHash, &job.ProcessedChunks, &job.TotalChunks, &job.EntityCount, &job.RelationshipCount, &job.PromptTokens, &job.CompletionTokens, &job.LastError, &job.CreatedAt, &job.StartedAt, &job.FinishedAt, &job.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim Graph RAG job: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE graph_rag_jobs SET status='running',started_at=CURRENT_TIMESTAMP,last_error='',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, job.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE graph_rag_documents SET status='building',last_error='',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, job.DocumentID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	job.Status = "running"
	return &job, nil
}

func UpdateGraphRAGJobProgress(ctx context.Context, jobID int64, processed, total, entities, relationships int, promptTokens, completionTokens int64) error {
	_, err := DB.Exec(ctx, `UPDATE graph_rag_jobs SET processed_chunks=$2,total_chunks=$3,entity_count=$4,relationship_count=$5,prompt_tokens=$6,completion_tokens=$7,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, jobID, processed, total, entities, relationships, promptTokens, completionTokens)
	return err
}

func HeartbeatGraphRAGJob(ctx context.Context, jobID int64) error {
	command, err := DB.Exec(ctx, `UPDATE graph_rag_jobs SET updated_at=CURRENT_TIMESTAMP WHERE id=$1 AND status='running'`, jobID)
	if err != nil {
		return fmt.Errorf("heartbeat Graph RAG job: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("Graph RAG job lease is no longer active")
	}
	return nil
}

func RecordGraphRAGExtractionAudit(ctx context.Context, jobID int64, chunkIndex int, rawResponse, validationError string, promptTokens, completionTokens int64) error {
	_, err := DB.Exec(ctx, `INSERT INTO graph_rag_extraction_audit(job_id,chunk_index,raw_response,validation_error,prompt_tokens,completion_tokens) VALUES($1,$2,$3,$4,$5,$6)`, jobID, chunkIndex, cleanGraphDBText(rawResponse, 50000), cleanGraphDBText(validationError, 4000), promptTokens, completionTokens)
	if err != nil {
		return fmt.Errorf("record Graph RAG extraction audit: %w", err)
	}
	return nil
}

func ListGraphRAGExtractionAudits(ctx context.Context, limit int) ([]GraphRAGExtractionAudit, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := DB.Query(ctx, `SELECT id,job_id,chunk_index,raw_response,validation_error,prompt_tokens,completion_tokens,created_at FROM graph_rag_extraction_audit ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	audits := make([]GraphRAGExtractionAudit, 0)
	for rows.Next() {
		var audit GraphRAGExtractionAudit
		if err := rows.Scan(&audit.ID, &audit.JobID, &audit.ChunkIndex, &audit.RawResponse, &audit.ValidationError, &audit.PromptTokens, &audit.CompletionTokens, &audit.CreatedAt); err != nil {
			return nil, err
		}
		audits = append(audits, audit)
	}
	return audits, rows.Err()
}

func FailGraphRAGJob(ctx context.Context, job GraphRAGJob, cause error) error {
	message := cleanGraphDBText(cause.Error(), 4000)
	tx, err := DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE graph_rag_jobs SET status='failed',last_error=$2,finished_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, job.ID, message); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE graph_rag_documents SET status=CASE WHEN active_snapshot_id='' THEN 'failed' ELSE 'ready' END,stale=TRUE,last_error=$2,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, job.DocumentID, message); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ListGraphRAGJobs(ctx context.Context, limit int) ([]GraphRAGJob, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := DB.Query(ctx, `SELECT j.id,j.document_id,d.document_name,j.kind,j.status,j.settings_hash,j.processed_chunks,j.total_chunks,j.entity_count,j.relationship_count,j.prompt_tokens,j.completion_tokens,j.last_error,j.created_at,j.started_at,j.finished_at,j.updated_at FROM graph_rag_jobs j JOIN graph_rag_documents d ON d.id=j.document_id ORDER BY j.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]GraphRAGJob, 0)
	for rows.Next() {
		var job GraphRAGJob
		if err := rows.Scan(&job.ID, &job.DocumentID, &job.DocumentName, &job.Kind, &job.Status, &job.SettingsHash, &job.ProcessedChunks, &job.TotalChunks, &job.EntityCount, &job.RelationshipCount, &job.PromptTokens, &job.CompletionTokens, &job.LastError, &job.CreatedAt, &job.StartedAt, &job.FinishedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func PersistAndActivateGraphRAGSnapshot(ctx context.Context, job GraphRAGJob, contentHash string, chunks []GraphRAGChunkExtraction) error {
	if err := GraphRAGAvailable(ctx); err != nil {
		return err
	}
	snapshotID, err := randomGraphID("snapshot")
	if err != nil {
		return err
	}
	tx, err := DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := configureAGEConnection(ctx, tx); err != nil {
		return fmt.Errorf("configure AGE for snapshot: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO graph_rag_snapshots(id,document_id,content_hash,status) VALUES($1,$2,$3,'building')`, snapshotID, job.DocumentID, contentHash); err != nil {
		return err
	}
	entityKeys := map[string]struct{}{}
	relationshipCount := 0
	for _, chunk := range chunks {
		base := map[string]any{"document_id": job.DocumentID, "document_name": job.DocumentName, "snapshot_id": snapshotID, "chunk_index": chunk.ChunkIndex, "chunk_text": cleanGraphDBText(chunk.ChunkText, 12000)}
		if err := execAGECypher(ctx, tx, `MERGE (d:Document {snapshot_id: $snapshot_id}) SET d.document_id=$document_id, d.document_name=$document_name MERGE (c:Chunk {snapshot_id: $snapshot_id, chunk_index: $chunk_index}) SET c.text=$chunk_text MERGE (d)-[:HAS_CHUNK {snapshot_id: $snapshot_id}]->(c) RETURN c.chunk_index`, base); err != nil {
			return err
		}
		entityKeyByName := map[string]string{}
		for _, entity := range chunk.Entities {
			key, aliases, aliasKeys, resolveErr := resolveGraphEntityIdentity(ctx, tx, entity)
			if resolveErr != nil {
				return resolveErr
			}
			if key == "" {
				continue
			}
			entityKeyByName[normalizeGraphKey(entity.CanonicalName)] = key
			for _, alias := range entity.Aliases {
				entityKeyByName[normalizeGraphKey(alias)] = key
			}
			entityKeys[key] = struct{}{}
			params := cloneGraphParams(base)
			params["canonical_key"] = key
			params["canonical_name"] = cleanGraphDBText(entity.CanonicalName, 500)
			params["entity_type"] = cleanGraphDBText(entity.EntityType, 120)
			params["aliases"] = aliases
			params["alias_keys"] = aliasKeys
			params["confidence"] = entity.Confidence
			if err := execAGECypher(ctx, tx, graphRAGEntityUpsertCypher, params); err != nil {
				return err
			}
		}
		for index, relationship := range chunk.Relationships {
			fromKey := entityKeyByName[normalizeGraphKey(relationship.From)]
			if fromKey == "" {
				fromKey = normalizeGraphKey(relationship.From)
			}
			toKey := entityKeyByName[normalizeGraphKey(relationship.To)]
			if toKey == "" {
				toKey = normalizeGraphKey(relationship.To)
			}
			if fromKey == "" || toKey == "" {
				continue
			}
			entityKeys[fromKey] = struct{}{}
			entityKeys[toKey] = struct{}{}
			params := cloneGraphParams(base)
			params["from_key"] = fromKey
			params["from_name"] = cleanGraphDBText(relationship.From, 500)
			params["to_key"] = toKey
			params["to_name"] = cleanGraphDBText(relationship.To, 500)
			params["relation_key"] = fmt.Sprintf("%s:%d:%d", snapshotID, chunk.ChunkIndex, index)
			params["relation_type"] = cleanGraphDBText(relationship.RelationType, 120)
			params["description"] = cleanGraphDBText(relationship.Description, 2000)
			params["confidence"] = relationship.Confidence
			if err := execAGECypher(ctx, tx, graphRAGRelationshipCreateCypher, params); err != nil {
				return err
			}
			relationshipCount++
		}
	}
	oldSnapshot := ""
	requiredSettingsHash := ""
	if err := tx.QueryRow(ctx, `SELECT active_snapshot_id,required_settings_hash FROM graph_rag_documents WHERE id=$1 FOR UPDATE`, job.DocumentID).Scan(&oldSnapshot, &requiredSettingsHash); err != nil {
		return err
	}
	if requiredSettingsHash != "" && requiredSettingsHash != job.SettingsHash {
		return fmt.Errorf("Graph RAG extraction settings changed during build; rebuild required")
	}
	if _, err := tx.Exec(ctx, `UPDATE graph_rag_snapshots SET status='active',entity_count=$2,relationship_count=$3,activated_at=CURRENT_TIMESTAMP WHERE id=$1`, snapshotID, len(entityKeys), relationshipCount); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE graph_rag_documents SET content_hash=$2,active_snapshot_id=$3,status='ready',stale=FALSE,last_error='',entity_count=$4,relationship_count=$5,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, job.DocumentID, contentHash, snapshotID, len(entityKeys), relationshipCount); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE graph_rag_jobs SET status='completed',entity_count=$2,relationship_count=$3,finished_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, job.ID, len(entityKeys), relationshipCount); err != nil {
		return err
	}
	if oldSnapshot != "" && oldSnapshot != snapshotID {
		if _, err := tx.Exec(ctx, `UPDATE graph_rag_snapshots SET status='superseded' WHERE id=$1`, oldSnapshot); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if oldSnapshot != "" && oldSnapshot != snapshotID {
		_ = DeleteGraphRAGSnapshot(context.Background(), oldSnapshot)
	}
	return nil
}

func resolveGraphEntityIdentity(ctx context.Context, runner graphRAGQuerier, entity GraphRAGExtractedEntity) (string, []string, []string, error) {
	aliases := cleanGraphStringSlice(entity.Aliases, 500)
	allNames := append([]string{cleanGraphDBText(entity.CanonicalName, 500)}, aliases...)
	aliasKeys := normalizeGraphKeys(allNames)
	if len(aliasKeys) == 0 {
		return "", nil, nil, nil
	}
	rows, err := queryAGECypher(ctx, runner, graphRAGResolveEntityCypher, map[string]any{"keys": aliasKeys}, 3)
	if err != nil {
		return "", nil, nil, err
	}
	key := canonicalGraphKey(entity.CanonicalName, entity.Aliases)
	if len(rows) > 0 {
		key = rows[0][0]
		aliases = mergeGraphStringJSON(aliases, rows[0][1])
		aliasKeys = mergeGraphStringJSON(aliasKeys, rows[0][2])
	}
	return key, aliases, aliasKeys, nil
}

func mergeGraphStringJSON(values []string, raw string) []string {
	var existing []string
	_ = json.Unmarshal([]byte(raw), &existing)
	return uniqueGraphStrings(append(values, existing...), len(values)+len(existing))
}

func DeleteGraphRAGSnapshot(ctx context.Context, snapshotID string) error {
	if strings.TrimSpace(snapshotID) == "" {
		return nil
	}
	conn, err := DB.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if err := configureAGEConnection(ctx, conn); err != nil {
		return err
	}
	params := map[string]any{"snapshot_id": snapshotID}
	if err := execAGECypher(ctx, conn, `MATCH ()-[r:RELATED_TO {snapshot_id:$snapshot_id}]-() DELETE r RETURN count(r)`, params); err != nil {
		return err
	}
	if err := execAGECypher(ctx, conn, `MATCH (n) WHERE n.snapshot_id=$snapshot_id DETACH DELETE n RETURN count(n)`, params); err != nil {
		return err
	}
	if err := execAGECypher(ctx, conn, graphRAGDeleteOrphanEntitiesCypher, map[string]any{}); err != nil {
		return err
	}
	_, _ = DB.Exec(ctx, `DELETE FROM graph_rag_snapshots WHERE id=$1 AND status<>'active'`, snapshotID)
	return nil
}

func RemoveGraphRAGDocument(ctx context.Context, documentName string) error {
	name := strings.TrimSpace(documentName)
	var documentID, snapshotID string
	err := DB.QueryRow(ctx, `SELECT id,active_snapshot_id FROM graph_rag_documents WHERE document_name=$1`, name).Scan(&documentID, &snapshotID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if snapshotID != "" {
		if err := DeleteGraphRAGSnapshot(ctx, snapshotID); err != nil {
			return err
		}
	}
	details, _ := json.Marshal(map[string]any{"document_id": documentID, "snapshot_id": snapshotID})
	tx, err := DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO graph_rag_audit(action,document_name,details) VALUES('remove_document',$1,$2::jsonb)`, name, string(details)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM graph_rag_documents WHERE id=$1`, documentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func DeleteEntireGraphRAG(ctx context.Context) error {
	documents, err := ListGraphRAGDocuments(ctx)
	if err != nil {
		return err
	}
	for _, document := range documents {
		if err := RemoveGraphRAGDocument(ctx, document.DocumentName); err != nil {
			return err
		}
	}
	_, err = DB.Exec(ctx, `INSERT INTO graph_rag_audit(action,details) VALUES('delete_entire_graph','{}'::jsonb)`)
	return err
}

func QueryGraphRAG(ctx context.Context, seeds []string, documentNames []string, settings common.GraphRAGRetrievalSettings) (GraphRAGQueryResult, error) {
	result := GraphRAGQueryResult{}
	if issues := common.ValidateGraphRAGRetrievalSettings(settings); len(issues) > 0 {
		return result, fmt.Errorf("invalid Graph RAG setting %s: %s", issues[0].Field, issues[0].Message)
	}
	seedKeys := normalizeGraphKeys(seeds)
	if len(seedKeys) == 0 {
		return result, nil
	}
	query := `SELECT document_name,active_snapshot_id FROM graph_rag_documents WHERE selected=TRUE AND active_snapshot_id<>''`
	args := []any{}
	if len(documentNames) > 0 {
		query += ` AND document_name=ANY($1)`
		args = append(args, documentNames)
	}
	rows, err := DB.Query(ctx, query, args...)
	if err != nil {
		return result, err
	}
	snapshots := make([]string, 0)
	for rows.Next() {
		var documentName, snapshot string
		if err := rows.Scan(&documentName, &snapshot); err != nil {
			rows.Close()
			return result, err
		}
		snapshots = append(snapshots, snapshot)
	}
	rows.Close()
	if len(snapshots) == 0 {
		return result, nil
	}
	sort.Strings(snapshots)
	result.GraphRevision = strings.Join(snapshots, ",")
	result.SeedEntities = append([]string(nil), seeds...)
	conn, err := DB.Acquire(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Release()
	if err := configureAGEConnection(ctx, conn); err != nil {
		return result, err
	}
	searchTerms := graphRAGSeedSearchTerms(seedKeys, graphRAGSeedSearchTermLimit(settings.MaxSeedEntities))
	resolvedRows, err := queryAGECypher(ctx, conn, graphRAGResolveSeedCypher, map[string]any{"terms": searchTerms, "candidate_limit": graphRAGSeedCandidateLimit(settings.MaxSeedEntities)}, 2)
	if err != nil {
		return result, err
	}
	candidates := make([]graphRAGSeedCandidate, 0, len(resolvedRows))
	for _, values := range resolvedRows {
		if len(values) == 2 {
			candidates = append(candidates, graphRAGSeedCandidate{CanonicalKey: values[0], AliasKeys: mergeGraphStringJSON(nil, values[1])})
		}
	}
	frontier := rankGraphRAGSeedCandidates(seedKeys, candidates, settings.MaxSeedEntities)
	result.ResolvedSeedEntities = append([]string(nil), frontier...)
	discovered := map[string]struct{}{}
	for _, key := range frontier {
		discovered[key] = struct{}{}
	}
	dedup := map[string]struct{}{}
	for depth := 1; depth <= settings.MaxTraversalDepth && len(frontier) > 0 && len(result.Relationships) < settings.MaxRelationships; depth++ {
		params := map[string]any{"keys": frontier, "snapshots": snapshots, "min_confidence": settings.MinMatchConfidence, "limit": settings.MaxRelationships - len(result.Relationships)}
		queryRows, err := queryAGECypher(ctx, conn, `MATCH (a:Entity)-[r:RELATED_TO]-(b:Entity) WHERE a.canonical_key IN $keys AND r.snapshot_id IN $snapshots AND r.confidence >= $min_confidence RETURN a.canonical_key,a.canonical_name,a.entity_type,b.canonical_key,b.canonical_name,b.entity_type,r.relation_type,r.description,r.confidence,r.document_name,r.chunk_index,r.relation_key,r.from_key,r.to_key ORDER BY r.confidence DESC,r.document_name,r.chunk_index LIMIT $limit`, params, 14)
		if err != nil {
			return result, err
		}
		next := make([]string, 0)
		for _, values := range queryRows {
			if len(values) != 14 {
				continue
			}
			confidence, _ := strconv.ParseFloat(values[8], 64)
			chunkIndex, _ := strconv.Atoi(values[10])
			key := values[11]
			if key == "" {
				key = strings.Join([]string{values[12], values[13], values[6], values[9], values[10]}, "|")
			}
			if _, exists := dedup[key]; exists {
				continue
			}
			dedup[key] = struct{}{}
			from, fromType, to, toType := values[1], values[2], values[4], values[5]
			if values[12] == values[3] {
				from, fromType, to, toType = values[4], values[5], values[1], values[2]
			}
			result.Relationships = append(result.Relationships, GraphRAGRelationshipEvidence{From: from, FromType: fromType, To: to, ToType: toType, RelationType: values[6], Description: values[7], Confidence: confidence, DocumentName: values[9], ChunkIndex: chunkIndex, Depth: depth})
			if _, seen := discovered[values[3]]; !seen && len(discovered) < settings.MaxEntities {
				next = append(next, values[3])
				discovered[values[3]] = struct{}{}
			}
		}
		frontier = uniqueGraphStrings(next, len(next))
	}
	return result, nil
}

// PreviewGraphRAGDocument returns a bounded, read-only view of one document's
// active graph snapshot. It intentionally bypasses query seed resolution so an
// administrator can inspect what ingestion produced, including isolated
// entities that are not part of an extracted relationship.
func PreviewGraphRAGDocument(ctx context.Context, documentName string, maxEntities, maxRelationships int) (GraphRAGQueryResult, error) {
	result := GraphRAGQueryResult{}
	name := strings.TrimSpace(documentName)
	if name == "" {
		return result, fmt.Errorf("document name is required")
	}
	if maxEntities < 1 {
		maxEntities = 200
	}
	if maxEntities > 500 {
		maxEntities = 500
	}
	if maxRelationships < 1 {
		maxRelationships = 400
	}
	if maxRelationships > 1000 {
		maxRelationships = 1000
	}
	var snapshotID string
	if err := DB.QueryRow(ctx, `SELECT active_snapshot_id FROM graph_rag_documents WHERE document_name=$1 AND selected=TRUE AND active_snapshot_id<>''`, name).Scan(&snapshotID); err != nil {
		if err == pgx.ErrNoRows {
			return result, fmt.Errorf("document does not have a ready graph snapshot")
		}
		return result, err
	}
	result.GraphRevision = snapshotID
	conn, err := DB.Acquire(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Release()
	if err := configureAGEConnection(ctx, conn); err != nil {
		return result, err
	}
	entityRows, err := queryAGECypher(ctx, conn, `MATCH (:Chunk {snapshot_id:$snapshot_id})-[:MENTIONS]->(e:Entity) RETURN DISTINCT e.canonical_key,e.canonical_name,e.entity_type ORDER BY e.canonical_name LIMIT $limit`, map[string]any{"snapshot_id": snapshotID, "limit": maxEntities}, 3)
	if err != nil {
		return result, err
	}
	seenEntities := make(map[string]struct{}, len(entityRows))
	entityKeys := make([]string, 0, len(entityRows))
	appendEntity := func(key, entityName, entityType string) {
		identity := key
		if identity == "" {
			identity = entityName + "\x00" + entityType
		}
		if _, exists := seenEntities[identity]; exists {
			return
		}
		seenEntities[identity] = struct{}{}
		result.Entities = append(result.Entities, GraphRAGEntityEvidence{Key: key, Name: entityName, EntityType: entityType})
	}
	for _, values := range entityRows {
		if len(values) == 3 {
			appendEntity(values[0], values[1], values[2])
			if values[0] != "" {
				entityKeys = append(entityKeys, values[0])
			}
		}
	}
	if len(entityKeys) == 0 {
		return result, nil
	}
	relationshipRows, err := queryAGECypher(ctx, conn, `MATCH (a:Entity)-[r:RELATED_TO]->(b:Entity) WHERE r.snapshot_id=$snapshot_id AND a.canonical_key IN $entity_keys AND b.canonical_key IN $entity_keys RETURN a.canonical_key,a.canonical_name,a.entity_type,b.canonical_key,b.canonical_name,b.entity_type,r.relation_type,r.description,r.confidence,r.document_name,r.chunk_index ORDER BY r.confidence DESC,r.chunk_index LIMIT $limit`, map[string]any{"snapshot_id": snapshotID, "entity_keys": entityKeys, "limit": maxRelationships}, 11)
	if err != nil {
		return result, err
	}
	for _, values := range relationshipRows {
		if len(values) != 11 {
			continue
		}
		confidence, _ := strconv.ParseFloat(values[8], 64)
		chunkIndex, _ := strconv.Atoi(values[10])
		result.Relationships = append(result.Relationships, GraphRAGRelationshipEvidence{From: values[1], FromType: values[2], To: values[4], ToType: values[5], RelationType: values[6], Description: values[7], Confidence: confidence, DocumentName: values[9], ChunkIndex: chunkIndex, Depth: 1})
		appendEntity(values[0], values[1], values[2])
		appendEntity(values[3], values[4], values[5])
	}
	return result, nil
}

type graphRAGQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type graphRAGExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func configureAGEConnection(ctx context.Context, runner graphRAGExecutor) error {
	if _, err := runner.Exec(ctx, `LOAD 'age'`); err != nil {
		return fmt.Errorf("load Apache AGE: %w", err)
	}
	if _, err := runner.Exec(ctx, `SET search_path = ag_catalog, "$user", public`); err != nil {
		return fmt.Errorf("set Apache AGE search path: %w", err)
	}
	return nil
}

type execAGEQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func execAGECypher(ctx context.Context, runner execAGEQuerier, cypher string, params map[string]any) error {
	rows, err := queryAGECypher(ctx, runner, cypher, params, 1)
	if err != nil {
		return err
	}
	_ = rows
	return nil
}

func queryAGECypher(ctx context.Context, runner graphRAGQuerier, cypher string, params map[string]any, columns int) ([][]string, error) {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	columnDefs := make([]string, columns)
	selectColumns := make([]string, columns)
	for i := 0; i < columns; i++ {
		name := fmt.Sprintf("c%d", i+1)
		columnDefs[i] = name + " ag_catalog.agtype"
		selectColumns[i] = name + "::text"
	}
	sql := `SELECT ` + strings.Join(selectColumns, ",") + ` FROM ag_catalog.cypher('` + graphRAGGraphName + `', $$` + cypher + `$$, $1::ag_catalog.agtype) AS (` + strings.Join(columnDefs, ",") + `)`
	rows, err := runner.Query(ctx, sql, string(rawParams))
	if err != nil {
		return nil, fmt.Errorf("execute AGE cypher: %w", err)
	}
	defer rows.Close()
	result := make([][]string, 0)
	for rows.Next() {
		values := make([]string, columns)
		dest := make([]any, columns)
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		for i := range values {
			values[i] = decodeAGScalar(values[i])
		}
		result = append(result, values)
	}
	return result, rows.Err()
}

func decodeAGScalar(value string) string {
	trimmed := strings.TrimSpace(value)
	var decoded any
	if json.Unmarshal([]byte(trimmed), &decoded) == nil {
		if _, ok := decoded.([]any); ok {
			encoded, _ := json.Marshal(decoded)
			return string(encoded)
		}
		return fmt.Sprint(decoded)
	}
	return strings.Trim(trimmed, `"`)
}

func randomGraphID(prefix string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate Graph RAG ID: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(raw), nil
}

func normalizeGraphKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func normalizeGraphKeys(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		key := normalizeGraphKey(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func graphRAGSeedSearchTermLimit(maxSeedEntities int) int {
	limit := maxSeedEntities * 16
	if limit < 32 {
		return 32
	}
	if limit > 128 {
		return 128
	}
	return limit
}

func graphRAGSeedCandidateLimit(maxSeedEntities int) int {
	limit := maxSeedEntities * 100
	if limit < 200 {
		return 200
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func graphRAGSeedSearchTerms(seeds []string, limit int) []string {
	terms := make([]string, 0, len(seeds)*8)
	for _, seed := range normalizeGraphKeys(seeds) {
		terms = append(terms, seed)
		runes := []rune(seed)
		for index := 0; index+1 < len(runes); index++ {
			pair := strings.TrimSpace(string(runes[index : index+2]))
			if len([]rune(pair)) == 2 {
				terms = append(terms, pair)
			}
		}
		for _, word := range strings.Fields(seed) {
			if len([]rune(word)) >= 2 {
				terms = append(terms, word)
			}
		}
	}
	return uniqueGraphStrings(terms, limit)
}

func rankGraphRAGSeedCandidates(seeds []string, candidates []graphRAGSeedCandidate, limit int) []string {
	if limit <= 0 {
		return nil
	}
	normalizedSeeds := normalizeGraphKeys(seeds)
	uniqueCandidates := make([]graphRAGSeedCandidate, 0, len(candidates))
	seenCandidates := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate.CanonicalKey = normalizeGraphKey(candidate.CanonicalKey)
		candidate.AliasKeys = normalizeGraphKeys(candidate.AliasKeys)
		if candidate.CanonicalKey == "" {
			continue
		}
		if _, exists := seenCandidates[candidate.CanonicalKey]; exists {
			continue
		}
		seenCandidates[candidate.CanonicalKey] = struct{}{}
		uniqueCandidates = append(uniqueCandidates, candidate)
	}

	selected := map[string]struct{}{}
	result := make([]string, 0, limit)
	for _, seed := range normalizedSeeds {
		bestKey := ""
		bestScore := 0.0
		for _, candidate := range uniqueCandidates {
			if _, exists := selected[candidate.CanonicalKey]; exists {
				continue
			}
			score := graphRAGSeedCandidateScore(seed, candidate)
			if score > bestScore || (score == bestScore && score > 0 && candidate.CanonicalKey < bestKey) {
				bestKey = candidate.CanonicalKey
				bestScore = score
			}
		}
		if bestScore >= graphRAGMinimumSeedMatchScore {
			selected[bestKey] = struct{}{}
			result = append(result, bestKey)
			if len(result) == limit {
				return result
			}
		}
	}

	type scoredCandidate struct {
		key   string
		score float64
	}
	remaining := make([]scoredCandidate, 0, len(uniqueCandidates))
	for _, candidate := range uniqueCandidates {
		if _, exists := selected[candidate.CanonicalKey]; exists {
			continue
		}
		bestScore := 0.0
		for _, seed := range normalizedSeeds {
			if score := graphRAGSeedCandidateScore(seed, candidate); score > bestScore {
				bestScore = score
			}
		}
		if bestScore >= graphRAGMinimumSeedMatchScore {
			remaining = append(remaining, scoredCandidate{key: candidate.CanonicalKey, score: bestScore})
		}
	}
	sort.Slice(remaining, func(i, j int) bool {
		if remaining[i].score == remaining[j].score {
			return remaining[i].key < remaining[j].key
		}
		return remaining[i].score > remaining[j].score
	})
	for _, candidate := range remaining {
		result = append(result, candidate.key)
		if len(result) == limit {
			break
		}
	}
	return result
}

func graphRAGSeedCandidateScore(seed string, candidate graphRAGSeedCandidate) float64 {
	best := graphRAGStringSimilarity(seed, candidate.CanonicalKey)
	for _, alias := range candidate.AliasKeys {
		if score := graphRAGStringSimilarity(seed, alias); score > best {
			best = score
		}
	}
	return best
}

func graphRAGStringSimilarity(left, right string) float64 {
	left = normalizeGraphKey(left)
	right = normalizeGraphKey(right)
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 1
	}
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if strings.Contains(left, right) || strings.Contains(right, left) {
		shorter, longer := len(leftRunes), len(rightRunes)
		if shorter > longer {
			shorter, longer = longer, shorter
		}
		return 0.7 + 0.3*(float64(shorter)/float64(longer))
	}
	leftPairs := graphRAGRunePairs(leftRunes)
	rightPairs := graphRAGRunePairs(rightRunes)
	if len(leftPairs) == 0 || len(rightPairs) == 0 {
		return 0
	}
	overlap := 0
	for pair := range leftPairs {
		if _, exists := rightPairs[pair]; exists {
			overlap++
		}
	}
	return float64(2*overlap) / float64(len(leftPairs)+len(rightPairs))
}

func graphRAGRunePairs(runes []rune) map[string]struct{} {
	result := map[string]struct{}{}
	for index := 0; index+1 < len(runes); index++ {
		result[string(runes[index:index+2])] = struct{}{}
	}
	return result
}

func canonicalGraphKey(name string, aliases []string) string {
	keys := normalizeGraphKeys(append([]string{name}, aliases...))
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return keys[0]
}

func uniqueGraphStrings(values []string, limit int) []string {
	result := normalizeGraphKeys(values)
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

func cleanGraphDBText(value string, maxRunes int) string {
	cleaned := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(cleaned)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return cleaned
}

func cleanGraphStringSlice(values []string, maxRunes int) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if cleaned := cleanGraphDBText(value, maxRunes); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

func cloneGraphParams(values map[string]any) map[string]any {
	clone := make(map[string]any, len(values)+8)
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
