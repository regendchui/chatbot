package common

import "testing"

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
