package common

import "testing"

func TestDefaultGraphRAGRetrievalSettingsMatchProductContract(t *testing.T) {
	got := DefaultGraphRAGRetrievalSettings()
	if got.InboundMessageCount != 2 || got.MaxTraversalDepth != 2 || got.MaxSeedEntities != 5 {
		t.Fatalf("unexpected query defaults: %#v", got)
	}
	if got.MaxEntities != 30 || got.MaxRelationships != 50 || got.MaxContextChars != 12000 {
		t.Fatalf("unexpected result defaults: %#v", got)
	}
	if got.MinMatchConfidence != 0.5 || got.TimeoutMS != 3000 {
		t.Fatalf("unexpected confidence/timeout defaults: %#v", got)
	}
}

func TestValidateGraphRAGRetrievalSettingsAcceptsDefaults(t *testing.T) {
	if issues := ValidateGraphRAGRetrievalSettings(DefaultGraphRAGRetrievalSettings()); len(issues) != 0 {
		t.Fatalf("default settings should be valid: %#v", issues)
	}
}
