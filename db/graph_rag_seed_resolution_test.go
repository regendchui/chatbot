package db

import (
	"slices"
	"testing"
)

func TestGraphRAGSeedSearchTermsIncludesBoundedChineseBigrams(t *testing.T) {
	got := graphRAGSeedSearchTerms([]string{"戒煙服務中心", "黃大仙"}, 32)
	for _, want := range []string{"戒煙服務中心", "戒煙", "服務", "中心", "黃大仙", "黃大", "大仙"} {
		if !slices.Contains(got, want) {
			t.Errorf("search terms %q do not contain %q", got, want)
		}
	}
	if len(got) > 32 {
		t.Fatalf("search terms are not bounded: %d", len(got))
	}
}

func TestRankGraphRAGSeedCandidatesResolvesGenericChineseSeeds(t *testing.T) {
	candidates := []graphRAGSeedCandidate{
		{CanonicalKey: "黃大仙新蒲崗七寶街2號東華三院東蒲一樓the grand oasis共享工作空間5號室"},
		{CanonicalKey: "東華三院戒煙綜合服務中心黃大仙辦事處", AliasKeys: []string{"黃大仙辦事處"}},
		{CanonicalKey: "戒煙輔導服務中心"},
	}

	got := rankGraphRAGSeedCandidates([]string{"黃大仙", "戒煙服務中心"}, candidates, 2)
	want := []string{"東華三院戒煙綜合服務中心黃大仙辦事處", "戒煙輔導服務中心"}
	for _, key := range want {
		if !slices.Contains(got, key) {
			t.Errorf("resolved keys %q do not contain %q", got, key)
		}
	}
	if slices.Contains(got, candidates[0].CanonicalKey) {
		t.Errorf("address candidate outranked the matching clinic alias: %q", got)
	}
}

func TestRankGraphRAGSeedCandidatesRejectsUnrelatedEntities(t *testing.T) {
	got := rankGraphRAGSeedCandidates([]string{"黃大仙"}, []graphRAGSeedCandidate{{CanonicalKey: "灣仔總服務處"}}, 5)
	if len(got) != 0 {
		t.Fatalf("unrelated candidate resolved: %q", got)
	}
}
