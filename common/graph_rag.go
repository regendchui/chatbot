package common

import (
	"fmt"
	"math"
)

type GraphRAGMode string

const (
	GraphRAGModeInherit  GraphRAGMode = "inherit"
	GraphRAGModeOverride GraphRAGMode = "override"
	GraphRAGModeDisabled GraphRAGMode = "disabled"
)

const (
	DefaultGraphRAGInboundMessageCount = 2
	DefaultGraphRAGTraversalDepth      = 2
	DefaultGraphRAGSeedEntities        = 5
	DefaultGraphRAGMaxEntities         = 30
	DefaultGraphRAGMaxRelationships    = 50
	DefaultGraphRAGMaxContextChars     = 12000
	DefaultGraphRAGMinMatchConfidence  = 0.5
	DefaultGraphRAGTimeoutMS           = 3000
	DefaultHybridRAGMaxContextChars    = 20000
)

type GraphRAGRetrievalSettings struct {
	InboundMessageCount int     `json:"inbound_message_count"`
	MaxTraversalDepth   int     `json:"max_traversal_depth"`
	MaxSeedEntities     int     `json:"max_seed_entities"`
	MaxEntities         int     `json:"max_entities"`
	MaxRelationships    int     `json:"max_relationships"`
	MaxContextChars     int     `json:"max_context_chars"`
	MinMatchConfidence  float64 `json:"min_match_confidence"`
	TimeoutMS           int     `json:"timeout_ms"`
}

type GraphRAGSettingIssue struct {
	Field   string
	Message string
}

func DefaultGraphRAGRetrievalSettings() GraphRAGRetrievalSettings {
	return GraphRAGRetrievalSettings{
		InboundMessageCount: DefaultGraphRAGInboundMessageCount,
		MaxTraversalDepth:   DefaultGraphRAGTraversalDepth,
		MaxSeedEntities:     DefaultGraphRAGSeedEntities,
		MaxEntities:         DefaultGraphRAGMaxEntities,
		MaxRelationships:    DefaultGraphRAGMaxRelationships,
		MaxContextChars:     DefaultGraphRAGMaxContextChars,
		MinMatchConfidence:  DefaultGraphRAGMinMatchConfidence,
		TimeoutMS:           DefaultGraphRAGTimeoutMS,
	}
}

func ValidateGraphRAGRetrievalSettings(settings GraphRAGRetrievalSettings) []GraphRAGSettingIssue {
	issues := make([]GraphRAGSettingIssue, 0)
	add := func(field, message string) {
		issues = append(issues, GraphRAGSettingIssue{Field: field, Message: message})
	}
	if settings.InboundMessageCount < 1 || settings.InboundMessageCount > IntentionRoutingRAGMaxInboundMessages {
		add("inbound_message_count", fmt.Sprintf("must be between 1 and %d", IntentionRoutingRAGMaxInboundMessages))
	}
	if settings.MaxTraversalDepth < 1 || settings.MaxTraversalDepth > 5 {
		add("max_traversal_depth", "traversal depth must be between 1 and 5")
	}
	if settings.MaxSeedEntities < 1 || settings.MaxSeedEntities > 50 {
		add("max_seed_entities", "must be between 1 and 50")
	}
	if settings.MaxEntities < 1 || settings.MaxEntities > 200 {
		add("max_entities", "must be between 1 and 200")
	}
	if settings.MaxRelationships < 1 || settings.MaxRelationships > 500 {
		add("max_relationships", "must be between 1 and 500")
	}
	if settings.MaxContextChars < 1000 || settings.MaxContextChars > 100000 {
		add("max_context_chars", "must be between 1000 and 100000")
	}
	if math.IsNaN(settings.MinMatchConfidence) || math.IsInf(settings.MinMatchConfidence, 0) || settings.MinMatchConfidence < 0 || settings.MinMatchConfidence > 1 {
		add("min_match_confidence", "must be between 0 and 1")
	}
	if settings.TimeoutMS < 100 || settings.TimeoutMS > 30000 {
		add("timeout_ms", "must be between 100 and 30000")
	}
	return issues
}
