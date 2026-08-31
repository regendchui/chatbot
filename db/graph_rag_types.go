package db

import "time"

type GraphRAGDocument struct {
	ID                string
	DocumentName      string
	Selected          bool
	ContentHash       string
	ActiveSnapshotID  string
	Status            string
	Stale             bool
	LastError         string
	EntityCount       int
	RelationshipCount int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type GraphRAGJob struct {
	ID                int64
	DocumentID        string
	DocumentName      string
	Kind              string
	Status            string
	SettingsHash      string
	ProcessedChunks   int
	TotalChunks       int
	EntityCount       int
	RelationshipCount int
	PromptTokens      int64
	CompletionTokens  int64
	LastError         string
	CreatedAt         time.Time
	StartedAt         *time.Time
	FinishedAt        *time.Time
	UpdatedAt         time.Time
}

type GraphRAGExtractionAudit struct {
	ID               int64
	JobID            int64
	ChunkIndex       int
	RawResponse      string
	ValidationError  string
	PromptTokens     int64
	CompletionTokens int64
	CreatedAt        time.Time
}

type GraphRAGExtractedEntity struct {
	CanonicalName string
	EntityType    string
	Aliases       []string
	Confidence    float64
}

type GraphRAGExtractedRelationship struct {
	From         string
	To           string
	RelationType string
	Description  string
	Confidence   float64
}

type GraphRAGChunkExtraction struct {
	ChunkIndex    int
	ChunkText     string
	Entities      []GraphRAGExtractedEntity
	Relationships []GraphRAGExtractedRelationship
}

type GraphRAGRelationshipEvidence struct {
	From         string  `json:"from"`
	FromType     string  `json:"from_type"`
	To           string  `json:"to"`
	ToType       string  `json:"to_type"`
	RelationType string  `json:"relation_type"`
	Description  string  `json:"description"`
	Confidence   float64 `json:"confidence"`
	DocumentName string  `json:"document_name"`
	ChunkIndex   int     `json:"chunk_index"`
	Depth        int     `json:"depth"`
}

type GraphRAGQueryResult struct {
	SeedEntities         []string                       `json:"seed_entities"`
	ResolvedSeedEntities []string                       `json:"resolved_seed_entities"`
	Relationships        []GraphRAGRelationshipEvidence `json:"relationships"`
	GraphRevision        string                         `json:"graph_revision"`
}
