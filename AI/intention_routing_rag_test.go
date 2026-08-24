package ai

import (
	"reflect"
	"testing"

	"whatsapp-bot/common"
)

func routingTestOptions() []common.IntentionRoutingRAGOption {
	return []common.IntentionRoutingRAGOption{
		{ID: "first", Name: "First", Description: "First option"},
		{ID: "second", Name: "Second", Description: "Second option"},
		{ID: "third", Name: "Third", Description: "Third option"},
	}
}

func TestSelectRoutingOptionsSingleUsesStrictThresholdAndStableTie(t *testing.T) {
	probabilities := []RoutingProbability{
		{OptionID: "first", Probability: 0.8},
		{OptionID: "second", Probability: 0.8},
		{OptionID: "third", Probability: 0.2},
	}
	if got := selectRoutingOptions("single", 0.8, routingTestOptions(), probabilities); got != nil {
		t.Fatalf("equal threshold must not proceed, got %#v", got)
	}
	want := []string{"first"}
	if got := selectRoutingOptions("single", 0.79, routingTestOptions(), probabilities); !reflect.DeepEqual(got, want) {
		t.Fatalf("stable tie should choose first option: got %#v want %#v", got, want)
	}
}

func TestSelectRoutingOptionsMultipleUsesStrictThreshold(t *testing.T) {
	probabilities := []RoutingProbability{
		{OptionID: "first", Probability: 0.81},
		{OptionID: "second", Probability: 0.8},
		{OptionID: "third", Probability: 0.95},
	}
	want := []string{"first", "third"}
	if got := selectRoutingOptions("multiple", 0.8, routingTestOptions(), probabilities); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestValidateRoutingAPIResponseRejectsMissingAndUnknownOptions(t *testing.T) {
	if _, err := validateRoutingAPIResponse(`{"options":[{"option_id":"first","probability":0.5}]}`, routingTestOptions()); err == nil {
		t.Fatal("expected missing option error")
	}
	if _, err := validateRoutingAPIResponse(`{"options":[{"option_id":"unknown","probability":0.5}]}`, routingTestOptions()); err == nil {
		t.Fatal("expected unknown option error")
	}
}

func TestValidateRoutingAPIResponseAcceptsCompleteJSONCodeFence(t *testing.T) {
	raw := "```json\n" + `{"options":[{"option_id":"first","probability":0.7},{"option_id":"second","probability":0.2},{"option_id":"third","probability":0.1}]}` + "\n```"
	got, err := validateRoutingAPIResponse(raw, routingTestOptions())
	if err != nil {
		t.Fatalf("expected fenced JSON to be accepted: %v", err)
	}
	want := []RoutingProbability{
		{OptionID: "first", Probability: 0.7},
		{OptionID: "second", Probability: 0.2},
		{OptionID: "third", Probability: 0.1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestValidateRoutingAPIResponseRejectsFenceWithCommentary(t *testing.T) {
	raw := "Result:\n```json\n" + `{"options":[{"option_id":"first","probability":0.7},{"option_id":"second","probability":0.2},{"option_id":"third","probability":0.1}]}` + "\n```"
	if _, err := validateRoutingAPIResponse(raw, routingTestOptions()); err == nil {
		t.Fatal("expected commentary outside the JSON fence to be rejected")
	}
}

func TestAssembleIntentionRoutingRAGContextKeepsSeparateDocumentParts(t *testing.T) {
	parts := []RAGPromptPart{
		{BlockName: "Clinic", DocumentName: "location.pdf", TopK: 2, MinSimilarity: 0.2, Chunks: []RoutingRAGChunk{{ChunkIndex: 1, ChunkText: "Location content", Score: 0.9}}},
		{BlockName: "Clinic", DocumentName: "phone.pdf", TopK: 1, MinSimilarity: 0.3, Chunks: []RoutingRAGChunk{{ChunkIndex: 2, ChunkText: "Phone content", Score: 0.8}}},
	}
	contextText := assembleIntentionRoutingRAGContext(parts, 2000)
	for _, expected := range []string{"RAG PART 1", "location.pdf", "RAG PART 2", "phone.pdf"} {
		if !stringsContain(contextText, expected) {
			t.Fatalf("context missing %q: %s", expected, contextText)
		}
	}
}

func stringsContain(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return needle == ""
}
