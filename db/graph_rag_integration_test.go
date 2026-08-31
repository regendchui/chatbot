package db

import (
	"context"
	"os"
	"testing"
	"time"

	"whatsapp-bot/common"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGraphRAGAGEIntegration(t *testing.T) {
	dsn := os.Getenv("GRAPH_RAG_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set GRAPH_RAG_INTEGRATION_DSN to run the Apache AGE integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	previous := DB
	DB = pool
	defer func() { DB = previous }()

	if err := EnsureRAGTableExists(); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGraphRAGInfrastructure(); err != nil {
		t.Fatal(err)
	}
	documentName := "graph-integration-location.pdf"
	_, _ = DB.Exec(ctx, `DELETE FROM "RAG" WHERE document_name=$1`, documentName)
	_ = RemoveGraphRAGDocument(ctx, documentName)
	defer func() {
		_ = RemoveGraphRAGDocument(context.Background(), documentName)
		_, _ = DB.Exec(context.Background(), `DELETE FROM "RAG" WHERE document_name=$1`, documentName)
	}()
	if err := InsertRAGEmbedding(documentName, 0, "Wong Tai Sin Clinic is located in Wong Tai Sin.", []float64{1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := SelectGraphRAGDocument(ctx, documentName, "settings-v1"); err != nil {
		t.Fatal(err)
	}
	job, err := ClaimNextGraphRAGJob(ctx)
	if err != nil || job == nil {
		t.Fatalf("claim job: %#v %v", job, err)
	}
	extractions := []GraphRAGChunkExtraction{{
		ChunkIndex: 0,
		ChunkText:  "Wong Tai Sin Clinic is located in Wong Tai Sin.",
		Entities: []GraphRAGExtractedEntity{
			{CanonicalName: "Wong Tai Sin Clinic", EntityType: "Clinic", Aliases: []string{"黃大仙診所"}, Confidence: 0.98},
			{CanonicalName: "Wong Tai Sin", EntityType: "District", Aliases: []string{"黃大仙"}, Confidence: 0.99},
		},
		Relationships: []GraphRAGExtractedRelationship{{From: "Wong Tai Sin Clinic", To: "Wong Tai Sin", RelationType: "LOCATED_IN", Description: "The clinic is in the district.", Confidence: 0.97}},
	}}
	if err := PersistAndActivateGraphRAGSnapshot(ctx, *job, "content-v1", extractions); err != nil {
		t.Fatal(err)
	}
	settings := common.DefaultGraphRAGRetrievalSettings()
	result, err := QueryGraphRAG(ctx, []string{"Wong Tai Sin Clinic"}, []string{documentName}, settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Relationships) != 1 {
		t.Fatalf("relationships=%#v", result.Relationships)
	}
	if result.Relationships[0].DocumentName != documentName || result.Relationships[0].ChunkIndex != 0 {
		t.Fatalf("provenance=%#v", result.Relationships[0])
	}
	if err := RemoveGraphRAGDocument(ctx, documentName); err != nil {
		t.Fatal(err)
	}
	result, err = QueryGraphRAG(ctx, []string{"Wong Tai Sin Clinic"}, []string{documentName}, settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Relationships) != 0 {
		t.Fatalf("removed document still returned evidence: %#v", result.Relationships)
	}
}
