package common

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var intentionRoutingRAGIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

const (
	IntentionRoutingRAGSchemaVersion = 1
	IntentionRoutingRAGMaxBlocks     = 100
	IntentionRoutingRAGMaxOptions    = 20
	IntentionRoutingRAGMaxDepth      = 10
)

type IntentionRoutingRAGGraph struct {
	SchemaVersion int                         `json:"schema_version"`
	InputNodeID   string                      `json:"input_node_id"`
	Nodes         []IntentionRoutingRAGNode   `json:"nodes"`
	Edges         []IntentionRoutingRAGEdge   `json:"edges"`
	Viewport      IntentionRoutingRAGViewport `json:"viewport"`
}

type IntentionRoutingRAGViewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

type IntentionRoutingRAGPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type IntentionRoutingRAGNode struct {
	ID       string                        `json:"id"`
	Type     string                        `json:"type"`
	Name     string                        `json:"name"`
	Position IntentionRoutingRAGPosition   `json:"position"`
	Routing  *IntentionRoutingRAGRoute     `json:"routing,omitempty"`
	RAG      *IntentionRoutingRAGRetrieval `json:"rag,omitempty"`
}

type IntentionRoutingRAGRoute struct {
	Mode      string                      `json:"mode"`
	Model     string                      `json:"model"`
	Threshold float64                     `json:"threshold"`
	Options   []IntentionRoutingRAGOption `json:"options"`
	Documents []string                    `json:"documents,omitempty"`
}

type IntentionRoutingRAGOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	TerminalNoRAG bool   `json:"terminal_no_rag,omitempty"`
}

type IntentionRoutingRAGRetrieval struct {
	Documents []IntentionRoutingRAGDocument `json:"documents"`
}

type IntentionRoutingRAGDocument struct {
	DocumentName  string  `json:"document_name"`
	TopK          int     `json:"top_k"`
	MinSimilarity float64 `json:"min_similarity"`
}

type IntentionRoutingRAGEdge struct {
	ID             string `json:"id"`
	SourceNodeID   string `json:"source_node_id"`
	SourceOptionID string `json:"source_option_id,omitempty"`
	TargetNodeID   string `json:"target_node_id"`
}

type IntentionRoutingRAGValidationIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func DefaultIntentionRoutingRAGGraph(model string) IntentionRoutingRAGGraph {
	return IntentionRoutingRAGGraph{
		SchemaVersion: IntentionRoutingRAGSchemaVersion,
		InputNodeID:   "input",
		Nodes: []IntentionRoutingRAGNode{{
			ID:       "input",
			Type:     "input",
			Name:     "Input",
			Position: IntentionRoutingRAGPosition{X: 80, Y: 180},
		}},
		Edges:    []IntentionRoutingRAGEdge{},
		Viewport: IntentionRoutingRAGViewport{Zoom: 1},
	}
}

func ValidateIntentionRoutingRAGGraph(graph IntentionRoutingRAGGraph, documents map[string]struct{}) []IntentionRoutingRAGValidationIssue {
	issues := make([]IntentionRoutingRAGValidationIssue, 0)
	add := func(path, message string) {
		issues = append(issues, IntentionRoutingRAGValidationIssue{Path: path, Message: message})
	}
	if graph.SchemaVersion != IntentionRoutingRAGSchemaVersion {
		add("schema_version", fmt.Sprintf("must be %d", IntentionRoutingRAGSchemaVersion))
	}
	if len(graph.Nodes) > IntentionRoutingRAGMaxBlocks {
		add("nodes", fmt.Sprintf("cannot contain more than %d blocks", IntentionRoutingRAGMaxBlocks))
	}

	nodes := make(map[string]IntentionRoutingRAGNode, len(graph.Nodes))
	nameSet := map[string]string{}
	inputCount := 0
	optionByNode := map[string]map[string]IntentionRoutingRAGOption{}
	for i, node := range graph.Nodes {
		path := fmt.Sprintf("nodes[%d]", i)
		id := strings.TrimSpace(node.ID)
		if id == "" {
			add(path+".id", "is required")
		} else if !intentionRoutingRAGIDPattern.MatchString(id) {
			add(path+".id", "must contain only letters, numbers, underscores, or hyphens and be at most 128 characters")
		} else if _, exists := nodes[id]; exists {
			add(path+".id", "must be unique")
		} else {
			nodes[id] = node
		}
		name := strings.TrimSpace(node.Name)
		if name == "" {
			add(path+".name", "is required")
		} else {
			if utf8.RuneCountInString(name) > 120 {
				add(path+".name", "cannot exceed 120 characters")
			}
			key := strings.ToLower(name)
			if previous, exists := nameSet[key]; exists {
				add(path+".name", "must be unique; already used by "+previous)
			} else {
				nameSet[key] = id
			}
		}
		switch strings.TrimSpace(node.Type) {
		case "input":
			inputCount++
			if node.Routing != nil || node.RAG != nil {
				add(path, "Input block cannot contain Routing or RAG settings")
			}
		case "routing":
			if node.Routing == nil {
				add(path+".routing", "is required for a Routing block")
				continue
			}
			if node.RAG != nil {
				add(path+".rag", "is not allowed for a Routing block")
			}
			validateRoutingNode(path, node, documents, optionByNode, add)
		case "rag":
			if node.RAG == nil {
				add(path+".rag", "is required for a RAG block")
				continue
			}
			if node.Routing != nil {
				add(path+".routing", "is not allowed for a RAG block")
			}
			validateRAGNode(path, node, documents, add)
		default:
			add(path+".type", "must be input, routing, or rag")
		}
	}

	if inputCount != 1 {
		add("nodes", "must contain exactly one Input block")
	}
	inputID := strings.TrimSpace(graph.InputNodeID)
	input, inputExists := nodes[inputID]
	if inputID == "" {
		add("input_node_id", "is required")
	} else if !inputExists || input.Type != "input" {
		add("input_node_id", "must identify the Input block")
	}

	edgeIDs := map[string]struct{}{}
	outgoing := map[string][]IntentionRoutingRAGEdge{}
	incoming := map[string][]IntentionRoutingRAGEdge{}
	for i, edge := range graph.Edges {
		path := fmt.Sprintf("edges[%d]", i)
		id := strings.TrimSpace(edge.ID)
		if id == "" {
			add(path+".id", "is required")
		} else if !intentionRoutingRAGIDPattern.MatchString(id) {
			add(path+".id", "must contain only letters, numbers, underscores, or hyphens and be at most 128 characters")
		} else if _, exists := edgeIDs[id]; exists {
			add(path+".id", "must be unique")
		} else {
			edgeIDs[id] = struct{}{}
		}
		sourceID := strings.TrimSpace(edge.SourceNodeID)
		targetID := strings.TrimSpace(edge.TargetNodeID)
		source, sourceExists := nodes[sourceID]
		_, targetExists := nodes[targetID]
		if !sourceExists {
			add(path+".source_node_id", "references a missing block")
		}
		if !targetExists {
			add(path+".target_node_id", "references a missing block")
		}
		if sourceID != "" && sourceID == targetID {
			add(path, "self-connections are not allowed")
		}
		if sourceExists {
			switch source.Type {
			case "input":
				if strings.TrimSpace(edge.SourceOptionID) != "" {
					add(path+".source_option_id", "must be empty for the Input block")
				}
			case "routing":
				optionID := strings.TrimSpace(edge.SourceOptionID)
				if optionID == "" {
					add(path+".source_option_id", "is required for a Routing block connection")
				} else if _, exists := optionByNode[sourceID][optionID]; !exists {
					add(path+".source_option_id", "references a missing intention option")
				}
			case "rag":
				add(path, "RAG blocks cannot have outgoing connections")
			}
		}
		if sourceExists && targetExists {
			outgoing[sourceID] = append(outgoing[sourceID], edge)
			incoming[targetID] = append(incoming[targetID], edge)
		}
	}

	if inputExists && len(outgoing[inputID]) != 1 {
		add("input_node_id", "Input block must have exactly one outgoing connection")
	}
	if inputExists && len(incoming[inputID]) != 0 {
		add("input_node_id", "Input block cannot have incoming connections")
	}
	for _, node := range graph.Nodes {
		if node.Type != "routing" || node.Routing == nil {
			continue
		}
		counts := map[string]int{}
		for _, edge := range outgoing[node.ID] {
			counts[edge.SourceOptionID]++
		}
		for optionIndex, option := range node.Routing.Options {
			count := counts[option.ID]
			path := fmt.Sprintf("nodes[%s].routing.options[%d]", node.ID, optionIndex)
			if count > 1 {
				add(path, "can have at most one outgoing connection")
			}
			if option.TerminalNoRAG && count > 0 {
				add(path+".terminal_no_rag", "cannot be enabled when the option is connected")
			}
			if !option.TerminalNoRAG && count == 0 {
				add(path, "must be connected or marked terminal no-RAG")
			}
		}
	}

	if inputExists {
		validateReachabilityAndDepth(inputID, nodes, outgoing, add)
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path == issues[j].Path {
			return issues[i].Message < issues[j].Message
		}
		return issues[i].Path < issues[j].Path
	})
	return issues
}

func validateRoutingNode(path string, node IntentionRoutingRAGNode, documents map[string]struct{}, optionByNode map[string]map[string]IntentionRoutingRAGOption, add func(string, string)) {
	route := node.Routing
	if route.Mode != "single" && route.Mode != "multiple" {
		add(path+".routing.mode", "must be single or multiple")
	}
	if strings.TrimSpace(route.Model) == "" {
		add(path+".routing.model", "is required")
	} else if utf8.RuneCountInString(strings.TrimSpace(route.Model)) > 240 {
		add(path+".routing.model", "cannot exceed 240 characters")
	}
	if math.IsNaN(route.Threshold) || math.IsInf(route.Threshold, 0) || route.Threshold < 0 || route.Threshold > 1 {
		add(path+".routing.threshold", "must be between 0 and 1")
	}
	if len(route.Options) == 0 {
		add(path+".routing.options", "must contain at least one intention option")
	}
	if len(route.Options) > IntentionRoutingRAGMaxOptions {
		add(path+".routing.options", fmt.Sprintf("cannot contain more than %d options", IntentionRoutingRAGMaxOptions))
	}
	ids := map[string]IntentionRoutingRAGOption{}
	names := map[string]struct{}{}
	for i, option := range route.Options {
		optionPath := fmt.Sprintf("%s.routing.options[%d]", path, i)
		id := strings.TrimSpace(option.ID)
		if id == "" {
			add(optionPath+".id", "is required")
		} else if !intentionRoutingRAGIDPattern.MatchString(id) {
			add(optionPath+".id", "must contain only letters, numbers, underscores, or hyphens and be at most 128 characters")
		} else if _, exists := ids[id]; exists {
			add(optionPath+".id", "must be unique inside the Routing block")
		} else {
			ids[id] = option
		}
		name := strings.TrimSpace(option.Name)
		if name == "" {
			add(optionPath+".name", "is required")
		} else {
			if utf8.RuneCountInString(name) > 160 {
				add(optionPath+".name", "cannot exceed 160 characters")
			}
			key := strings.ToLower(name)
			if _, exists := names[key]; exists {
				add(optionPath+".name", "must be unique inside the Routing block")
			}
			names[key] = struct{}{}
		}
		if strings.TrimSpace(option.Description) == "" {
			add(optionPath+".description", "is required")
		} else if utf8.RuneCountInString(strings.TrimSpace(option.Description)) > 4000 {
			add(optionPath+".description", "cannot exceed 4000 characters")
		}
	}
	optionByNode[node.ID] = ids
	seenDocs := map[string]struct{}{}
	for i, rawName := range route.Documents {
		name := strings.TrimSpace(rawName)
		docPath := fmt.Sprintf("%s.routing.documents[%d]", path, i)
		if name == "" {
			add(docPath, "document name is required")
			continue
		}
		if utf8.RuneCountInString(name) > 500 {
			add(docPath, "document name cannot exceed 500 characters")
		}
		if _, exists := seenDocs[name]; exists {
			add(docPath, "document cannot be selected twice")
		}
		seenDocs[name] = struct{}{}
		if documents != nil {
			if _, exists := documents[name]; !exists {
				add(docPath, "document is not indexed or no longer exists")
			}
		}
	}
}

func validateRAGNode(path string, node IntentionRoutingRAGNode, documents map[string]struct{}, add func(string, string)) {
	if len(node.RAG.Documents) == 0 {
		add(path+".rag.documents", "must contain at least one document")
	}
	seen := map[string]struct{}{}
	for i, document := range node.RAG.Documents {
		docPath := fmt.Sprintf("%s.rag.documents[%d]", path, i)
		name := strings.TrimSpace(document.DocumentName)
		if name == "" {
			add(docPath+".document_name", "is required")
		} else {
			if utf8.RuneCountInString(name) > 500 {
				add(docPath+".document_name", "cannot exceed 500 characters")
			}
			if _, exists := seen[name]; exists {
				add(docPath+".document_name", "document cannot be selected twice in one RAG block")
			}
			seen[name] = struct{}{}
			if documents != nil {
				if _, exists := documents[name]; !exists {
					add(docPath+".document_name", "document is not indexed or no longer exists")
				}
			}
		}
		if document.TopK <= 0 || document.TopK > 100 {
			add(docPath+".top_k", "must be an integer between 1 and 100")
		}
		if math.IsNaN(document.MinSimilarity) || math.IsInf(document.MinSimilarity, 0) || document.MinSimilarity < -1 || document.MinSimilarity > 1 {
			add(docPath+".min_similarity", "must be between -1 and 1")
		}
	}
}

func validateReachabilityAndDepth(inputID string, nodes map[string]IntentionRoutingRAGNode, outgoing map[string][]IntentionRoutingRAGEdge, add func(string, string)) {
	state := map[string]int{}
	reached := map[string]bool{}
	cycleReported := false
	depthReported := false
	var visit func(string, int)
	visit = func(id string, routingDepth int) {
		if state[id] == 1 {
			if !cycleReported {
				add("edges", "workflow cannot contain a directed cycle")
				cycleReported = true
			}
			return
		}
		if state[id] == 2 {
			return
		}
		state[id] = 1
		reached[id] = true
		if node, ok := nodes[id]; ok && node.Type == "routing" {
			routingDepth++
			if routingDepth > IntentionRoutingRAGMaxDepth && !depthReported {
				add("nodes", fmt.Sprintf("routing depth cannot exceed %d", IntentionRoutingRAGMaxDepth))
				depthReported = true
			}
		}
		for _, edge := range outgoing[id] {
			visit(edge.TargetNodeID, routingDepth)
		}
		state[id] = 2
	}
	visit(inputID, 0)
	for id := range nodes {
		if !reached[id] {
			add("nodes["+id+"]", "block is not reachable from Input")
		}
	}
}
