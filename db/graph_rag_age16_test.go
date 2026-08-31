package db

import (
	"strings"
	"testing"
)

func TestGraphRAGSnapshotCypherAvoidsUnsupportedAGE16Clauses(t *testing.T) {
	queries := []string{graphRAGEntityUpsertCypher, graphRAGRelationshipCreateCypher, graphRAGDeleteOrphanEntitiesCypher, graphRAGResolveEntityCypher, graphRAGResolveSeedCypher}
	for _, query := range queries {
		if strings.Contains(strings.ToUpper(query), "ON CREATE SET") || strings.Contains(strings.ToUpper(query), "ON MATCH SET") {
			t.Fatalf("AGE 1.6 does not support MERGE ON CREATE/ON MATCH SET: %s", query)
		}
		if strings.Contains(strings.ToUpper(query), "WHERE NOT (") {
			t.Fatalf("AGE 1.6 does not support negated pattern predicates: %s", query)
		}
		upper := strings.ToUpper(query)
		if strings.Contains(upper, "ANY(") && strings.Contains(upper, " WHERE ") {
			t.Fatalf("AGE 1.6 does not support WHERE inside an any() list predicate: %s", query)
		}
	}
}

func TestGraphRAGEntityResolutionKeepsAliasMatchingWithoutAnyPredicate(t *testing.T) {
	for _, want := range []string{"UNWIND $keys AS key", "e.canonical_key = key", "key IN e.alias_keys"} {
		if !strings.Contains(graphRAGResolveEntityCypher, want) {
			t.Errorf("ingestion identity query must contain %q: %s", want, graphRAGResolveEntityCypher)
		}
	}
	for _, want := range []string{"UNWIND $terms AS term", "e.canonical_key = term", "e.canonical_key CONTAINS term", "term IN e.alias_keys"} {
		if !strings.Contains(graphRAGResolveSeedCypher, want) {
			t.Errorf("retrieval seed query must contain %q: %s", want, graphRAGResolveSeedCypher)
		}
	}
}
