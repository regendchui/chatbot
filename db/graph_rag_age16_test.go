package db

import (
	"strings"
	"testing"
)

func TestGraphRAGSnapshotCypherAvoidsUnsupportedAGE16MergeClauses(t *testing.T) {
	queries := []string{graphRAGEntityUpsertCypher, graphRAGRelationshipCreateCypher, graphRAGDeleteOrphanEntitiesCypher}
	for _, query := range queries {
		if strings.Contains(strings.ToUpper(query), "ON CREATE SET") || strings.Contains(strings.ToUpper(query), "ON MATCH SET") {
			t.Fatalf("AGE 1.6 does not support MERGE ON CREATE/ON MATCH SET: %s", query)
		}
		if strings.Contains(strings.ToUpper(query), "WHERE NOT (") {
			t.Fatalf("AGE 1.6 does not support negated pattern predicates: %s", query)
		}
	}
}
