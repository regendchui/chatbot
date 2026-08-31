package common

import (
	"encoding/json"
	"testing"
)

func validRoutingRAGGraph() IntentionRoutingRAGGraph {
	graph := DefaultIntentionRoutingRAGGraph("google/gemini-2.5-flash")
	graph.Nodes = append(graph.Nodes,
		IntentionRoutingRAGNode{
			ID: "router", Type: "routing", Name: "Router",
			Routing: &IntentionRoutingRAGRoute{Mode: "single", Model: "test/model", Threshold: 0.7, Options: []IntentionRoutingRAGOption{{ID: "location", Name: "Location", Description: "User asks for a clinic location."}}},
		},
		IntentionRoutingRAGNode{
			ID: "rag", Type: "rag", Name: "Clinic documents",
			RAG: &IntentionRoutingRAGRetrieval{Documents: []IntentionRoutingRAGDocument{{DocumentName: "location.pdf", TopK: 3, MinSimilarity: 0.2}}},
		},
	)
	graph.Edges = []IntentionRoutingRAGEdge{
		{ID: "input-router", SourceNodeID: "input", TargetNodeID: "router"},
		{ID: "router-rag", SourceNodeID: "router", SourceOptionID: "location", TargetNodeID: "rag"},
	}
	return graph
}

func TestValidateIntentionRoutingRAGGraphAcceptsValidGraph(t *testing.T) {
	docs := map[string]struct{}{"location.pdf": {}}
	if issues := ValidateIntentionRoutingRAGGraph(validRoutingRAGGraph(), docs); len(issues) != 0 {
		t.Fatalf("expected valid graph, got %#v", issues)
	}
}

func TestValidateIntentionRoutingRAGGraphRejectsCycle(t *testing.T) {
	graph := validRoutingRAGGraph()
	graph.Nodes[2].Type = "routing"
	graph.Nodes[2].RAG = nil
	graph.Nodes[2].Routing = &IntentionRoutingRAGRoute{Mode: "single", Model: "test/model", Threshold: 0.5, Options: []IntentionRoutingRAGOption{{ID: "again", Name: "Again", Description: "Route again."}}}
	graph.Edges = append(graph.Edges, IntentionRoutingRAGEdge{ID: "cycle", SourceNodeID: "rag", SourceOptionID: "again", TargetNodeID: "router"})
	issues := ValidateIntentionRoutingRAGGraph(graph, map[string]struct{}{"location.pdf": {}})
	if !hasValidationMessage(issues, "cycle") {
		t.Fatalf("expected cycle issue, got %#v", issues)
	}
}

func TestValidateIntentionRoutingRAGGraphRejectsMissingDocument(t *testing.T) {
	issues := ValidateIntentionRoutingRAGGraph(validRoutingRAGGraph(), map[string]struct{}{})
	if !hasValidationMessage(issues, "no longer exists") {
		t.Fatalf("expected missing-document issue, got %#v", issues)
	}
}

func TestIntentionRoutingDefaultsRemainBackwardCompatible(t *testing.T) {
	if got := EffectiveIntentionRoutingRAGInboundMessageCount(0); got != 1 {
		t.Fatalf("legacy missing count should default to 1, got %d", got)
	}
	if got := EffectiveIntentionRoutingPrompt(""); got != DefaultIntentionRoutingPrompt {
		t.Fatalf("legacy missing prompt got %q", got)
	}
	graph := validRoutingRAGGraph()
	if issues := ValidateIntentionRoutingRAGGraph(graph, map[string]struct{}{"location.pdf": {}}); len(issues) != 0 {
		t.Fatalf("legacy zero-value settings should remain valid: %#v", issues)
	}
}

func TestValidateIntentionRoutingRAGGraphRejectsInvalidMessageCounts(t *testing.T) {
	graph := validRoutingRAGGraph()
	graph.Nodes[1].Routing.InboundMessageCount = IntentionRoutingRAGMaxInboundMessages + 1
	graph.Nodes[2].RAG.InboundMessageCount = -1
	issues := ValidateIntentionRoutingRAGGraph(graph, map[string]struct{}{"location.pdf": {}})
	if len(issues) < 2 {
		t.Fatalf("expected inbound message count issues, got %#v", issues)
	}
}

func TestNormalizeIntentionRoutingRAGGraphUpgradesVersionOneWithoutEnablingGraphRAG(t *testing.T) {
	graph := validRoutingRAGGraph()
	graph.SchemaVersion = 1

	normalized, err := NormalizeIntentionRoutingRAGGraph(graph)
	if err != nil {
		t.Fatalf("normalize legacy workflow: %v", err)
	}
	if normalized.SchemaVersion != IntentionRoutingRAGSchemaVersion {
		t.Fatalf("schema version=%d want %d", normalized.SchemaVersion, IntentionRoutingRAGSchemaVersion)
	}
	if normalized.Nodes[2].RAG.GraphRAG.Mode != GraphRAGModeDisabled {
		t.Fatalf("legacy workflow unexpectedly enabled graph RAG: %#v", normalized.Nodes[2].RAG.GraphRAG)
	}
}

func TestIntentionRoutingRAGVersionTwoJSONKeepsGraphOverride(t *testing.T) {
	graph := validRoutingRAGGraph()
	graph.Nodes[2].RAG.GraphRAG = IntentionRoutingGraphRAG{
		Mode:      GraphRAGModeOverride,
		Documents: []string{"location.pdf"},
		Settings:  DefaultGraphRAGRetrievalSettings(),
	}
	raw, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	var decoded IntentionRoutingRAGGraph
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Nodes[2].RAG.GraphRAG.Mode != GraphRAGModeOverride {
		t.Fatalf("graph override lost after JSON round trip: %s", raw)
	}
}

func TestValidateIntentionRoutingRAGGraphAcceptsGraphOnlyRAGBlock(t *testing.T) {
	graph := validRoutingRAGGraph()
	graph.Nodes[2].RAG.Documents = nil
	graph.Nodes[2].RAG.GraphRAG = IntentionRoutingGraphRAG{
		Mode:      GraphRAGModeOverride,
		Documents: []string{"location.pdf"},
		Settings:  DefaultGraphRAGRetrievalSettings(),
	}
	if issues := ValidateIntentionRoutingRAGGraph(graph, map[string]struct{}{"location.pdf": {}}); len(issues) != 0 {
		t.Fatalf("expected graph-only RAG block to be valid, got %#v", issues)
	}
}

func TestValidateIntentionRoutingRAGGraphRejectsUnsafeGraphLimits(t *testing.T) {
	graph := validRoutingRAGGraph()
	graph.Nodes[2].RAG.GraphRAG = IntentionRoutingGraphRAG{
		Mode:      GraphRAGModeOverride,
		Documents: []string{"location.pdf"},
		Settings: GraphRAGRetrievalSettings{
			InboundMessageCount: 2,
			MaxTraversalDepth:   99,
			MaxSeedEntities:     5,
			MaxEntities:         30,
			MaxRelationships:    50,
			MaxContextChars:     12000,
			MinMatchConfidence:  0.5,
			TimeoutMS:           3000,
		},
	}
	issues := ValidateIntentionRoutingRAGGraph(graph, map[string]struct{}{"location.pdf": {}})
	if !hasValidationMessage(issues, "traversal depth") {
		t.Fatalf("expected traversal depth validation issue, got %#v", issues)
	}
}

func TestValidateIntentionRoutingRAGGraphRequiresActiveGraphDocumentForOverride(t *testing.T) {
	graph := validRoutingRAGGraph()
	graph.Nodes[2].RAG.GraphRAG = IntentionRoutingGraphRAG{
		Mode:      GraphRAGModeOverride,
		Documents: []string{"location.pdf"},
		Settings:  DefaultGraphRAGRetrievalSettings(),
	}
	issues := ValidateIntentionRoutingRAGGraph(graph, map[string]struct{}{"location.pdf": {}}, map[string]struct{}{})
	if !hasValidationMessage(issues, "Graph RAG page") {
		t.Fatalf("expected inactive graph-document issue, got %#v", issues)
	}
}

func hasValidationMessage(issues []IntentionRoutingRAGValidationIssue, needle string) bool {
	for _, issue := range issues {
		if contains(issue.Message, needle) {
			return true
		}
	}
	return false
}

func contains(value, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
