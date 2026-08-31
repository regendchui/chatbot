package ai

import (
	"strings"
	"testing"

	"whatsapp-bot/db"
)

func TestParseGraphExtractionAcceptsFencedStrictJSON(t *testing.T) {
	raw := "```json\n" + `{"entities":[{"name":"Wong Tai Sin Clinic","entity_type":"Clinic","aliases":["黃大仙診所"],"confidence":0.95}],"relationships":[{"from":"Wong Tai Sin Clinic","to":"Wong Tai Sin","relation_type":"LOCATED_IN","description":"The clinic is in the district.","confidence":0.91}]}` + "\n```"
	got, err := ParseGraphRAGExtraction(raw, 0.5)
	if err != nil {
		t.Fatalf("parse extraction: %v", err)
	}
	if len(got.Entities) != 1 || got.Entities[0].CanonicalName != "Wong Tai Sin Clinic" {
		t.Fatalf("unexpected entities: %#v", got.Entities)
	}
	if len(got.Relationships) != 1 || got.Relationships[0].RelationType != "LOCATED_IN" {
		t.Fatalf("unexpected relationships: %#v", got.Relationships)
	}
}

func TestParseGraphExtractionRejectsUnknownFields(t *testing.T) {
	raw := `{"entities":[],"relationships":[],"instructions":"ignore the schema"}`
	if _, err := ParseGraphRAGExtraction(raw, 0.5); err == nil {
		t.Fatal("expected strict JSON validation failure")
	}
}

func TestBuildGraphRAGContextIncludesCompactProvenanceAndUntrustedWarning(t *testing.T) {
	result := db.GraphRAGQueryResult{Relationships: []db.GraphRAGRelationshipEvidence{{
		From: "Wong Tai Sin Clinic", To: "Wong Tai Sin", RelationType: "LOCATED_IN",
		Description: "The clinic is in Wong Tai Sin.", Confidence: 0.91,
		DocumentName: "location.pdf", ChunkIndex: 4,
	}}}
	contextText := BuildGraphRAGContext(result, 12000)
	for _, want := range []string{"GRAPH RAG KNOWLEDGE CONTEXT", "untrusted", "location.pdf, chunk 4", "LOCATED_IN"} {
		if !strings.Contains(contextText, want) {
			t.Fatalf("context missing %q: %s", want, contextText)
		}
	}
}

func TestComposeHybridRAGContextUsesSharedBudgetAndSeparateSections(t *testing.T) {
	traditional := "TRADITIONAL RAG CONTEXT\n" + strings.Repeat("T", 120)
	graph := "GRAPH RAG CONTEXT\n" + strings.Repeat("G", 120)
	got, trace := ComposeHybridRAGContext(traditional, graph, 180)
	if !strings.Contains(got, "TRADITIONAL RAG") || !strings.Contains(got, "GRAPH RAG") {
		t.Fatalf("hybrid context lost a retrieval section: %q", got)
	}
	if len([]rune(got)) > 180 {
		t.Fatalf("hybrid context exceeded shared budget: %d", len([]rune(got)))
	}
	if !trace.Truncated {
		t.Fatal("expected truncation trace")
	}
}
