package admin_panel

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	ai "whatsapp-bot/AI"
	"whatsapp-bot/common"
	"whatsapp-bot/db"
)

//go:embed intention_routing_rag_editor.html
var intentionRoutingRAGEditorHTML string

type intentionRoutingRAGEditorState struct {
	Graph             common.IntentionRoutingRAGGraph `json:"graph"`
	Status            string                          `json:"status"`
	Revision          int                             `json:"revision"`
	LockVersion       int                             `json:"lock_version"`
	PublishedRevision int                             `json:"published_revision"`
	FeatureEnabled    bool                            `json:"feature_enabled"`
	DefaultModel      string                          `json:"default_model"`
	Documents         []string                        `json:"documents"`
	UpdatedAt         string                          `json:"updated_at,omitempty"`
}

type intentionRoutingRAGGraphRequest struct {
	Graph       common.IntentionRoutingRAGGraph `json:"graph"`
	LockVersion int                             `json:"lock_version"`
	Enquiry     string                          `json:"enquiry,omitempty"`
}

type intentionRoutingRAGPublishRequest struct {
	LockVersion int `json:"lock_version"`
}

var intentionRoutingRAGTestGuards = struct {
	sync.Mutex
	LastByActor map[string]time.Time
}{LastByActor: map[string]time.Time{}}

func adminIntentionRoutingRAGHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin/intention-routing-rag" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state, err := loadIntentionRoutingRAGEditorState(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bootstrap, err := json.Marshal(state)
	if err != nil {
		http.Error(w, "failed to prepare editor", http.StatusInternalServerError)
		return
	}
	session, _ := adminSessionFromRequest(r)
	body := strings.ReplaceAll(intentionRoutingRAGEditorHTML, "__CSRF_TOKEN__", html.EscapeString(session.CSRFToken))
	body = strings.ReplaceAll(body, "__BOOTSTRAP_JSON__", string(bootstrap))
	var page strings.Builder
	page.WriteString(adminPageHeader("Intention Routing RAG"))
	page.WriteString(`<h2>Intention Routing RAG</h2>`)
	page.WriteString(adminNav(r))
	page.WriteString(body)
	page.WriteString(adminPageFooter())
	adminWriteHTML(w, page.String())
}

func adminIntentionRoutingRAGWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state, err := loadIntentionRoutingRAGEditorState(r.Context())
	if err != nil {
		adminWriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	adminWriteJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
}

func adminIntentionRoutingRAGDraftHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	var request intentionRoutingRAGGraphRequest
	if !adminDecodeJSON(w, r, &request) {
		return
	}
	if issue := validateIntentionRoutingRAGDraftShape(request.Graph); issue != "" {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": issue})
		return
	}
	raw, err := json.Marshal(request.Graph)
	if err != nil {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Invalid workflow graph."})
		return
	}
	record, err := db.SaveIntentionRoutingRAGDraft(r.Context(), raw, adminActor(r), request.LockVersion)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrIntentionRoutingRAGConflict) {
			status = http.StatusConflict
		}
		adminWriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	adminRecordConfigUpdateHistory(r, "save_intention_routing_rag_draft", fmt.Sprintf("Saved Intention Routing RAG draft revision %d", record.Revision))
	adminWriteJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Draft saved.", "revision": record.Revision, "lock_version": record.LockVersion, "updated_at": record.UpdatedAt})
}

func adminIntentionRoutingRAGValidateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	var request intentionRoutingRAGGraphRequest
	if !adminDecodeJSON(w, r, &request) {
		return
	}
	issues, err := validateIntentionRoutingRAGGraphAgainstDocuments(request.Graph)
	if err != nil {
		adminWriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	adminWriteJSON(w, http.StatusOK, map[string]any{"ok": len(issues) == 0, "valid": len(issues) == 0, "issues": issues})
}

func adminIntentionRoutingRAGPublishHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	var request intentionRoutingRAGPublishRequest
	if !adminDecodeJSON(w, r, &request) {
		return
	}
	draft, err := db.LoadIntentionRoutingRAGDraft(r.Context())
	if err != nil {
		adminWriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if draft == nil {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Save a draft before publishing."})
		return
	}
	if draft.LockVersion != request.LockVersion {
		adminWriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": db.ErrIntentionRoutingRAGConflict.Error()})
		return
	}
	var graph common.IntentionRoutingRAGGraph
	if err := json.Unmarshal(draft.Graph, &graph); err != nil {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Draft graph JSON is invalid."})
		return
	}
	issues, err := validateIntentionRoutingRAGGraphAgainstDocuments(graph)
	if err != nil {
		adminWriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(issues) > 0 {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Workflow validation failed.", "issues": issues})
		return
	}
	record, err := db.PublishIntentionRoutingRAGDraft(r.Context(), adminActor(r), request.LockVersion)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrIntentionRoutingRAGConflict) {
			status = http.StatusConflict
		}
		adminWriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	adminRecordConfigUpdateHistory(r, "publish_intention_routing_rag", fmt.Sprintf("Published Intention Routing RAG revision %d", record.Revision))
	adminWriteJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Workflow published.", "revision": record.Revision, "lock_version": 0})
}

func adminIntentionRoutingRAGDiscardDraftHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	var request intentionRoutingRAGPublishRequest
	if !adminDecodeJSON(w, r, &request) {
		return
	}
	_, err := db.DiscardIntentionRoutingRAGDraft(r.Context(), adminActor(r), request.LockVersion)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrIntentionRoutingRAGConflict) {
			status = http.StatusConflict
		}
		adminWriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	state, err := loadIntentionRoutingRAGEditorState(r.Context())
	if err != nil {
		adminWriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	adminRecordConfigUpdateHistory(r, "discard_intention_routing_rag_draft", "Discarded Intention Routing RAG draft changes")
	adminWriteJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Draft reset to the published workflow.", "state": state})
}

func adminIntentionRoutingRAGTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	var request intentionRoutingRAGGraphRequest
	if !adminDecodeJSON(w, r, &request) {
		return
	}
	request.Enquiry = strings.TrimSpace(request.Enquiry)
	if request.Enquiry == "" {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "A test enquiry is required."})
		return
	}
	if len([]rune(request.Enquiry)) > 10000 {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "The test enquiry is too long."})
		return
	}
	issues, err := validateIntentionRoutingRAGGraphAgainstDocuments(request.Graph)
	if err != nil {
		adminWriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(issues) > 0 {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Workflow validation failed.", "issues": issues})
		return
	}
	actor := adminActor(r)
	if !allowIntentionRoutingRAGTest(actor) {
		adminWriteJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "Please wait a few seconds before running another test."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := ai.ExecuteIntentionRoutingRAGGraph(ctx, request.Enquiry, 0, request.Graph)
	if err != nil {
		adminWriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	adminRecordConfigUpdateHistory(r, "test_intention_routing_rag", "Ran an Intention Routing RAG draft test")
	adminWriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func loadIntentionRoutingRAGEditorState(ctx context.Context) (intentionRoutingRAGEditorState, error) {
	defaultModel := strings.TrimSpace(db.GetProjectSettingString("OPENROUTER_MODEL", "google/gemini-2.5-flash"))
	state := intentionRoutingRAGEditorState{
		Graph:          common.DefaultIntentionRoutingRAGGraph(defaultModel),
		Status:         "new",
		DefaultModel:   defaultModel,
		FeatureEnabled: db.GetProjectSettingBool("INTENTION_ROUTING_RAG_ENABLED", false),
	}
	docs, err := db.ListRAGDocuments()
	if err != nil {
		return state, err
	}
	for name := range docs {
		state.Documents = append(state.Documents, name)
	}
	sort.Strings(state.Documents)
	published, err := db.LoadPublishedIntentionRoutingRAGWorkflow(ctx)
	if err != nil {
		return state, err
	}
	if published != nil {
		state.PublishedRevision = published.Revision
		state.Status = "published"
		state.Revision = published.Revision
		state.UpdatedAt = published.UpdatedAt.Format(time.RFC3339)
		if err := json.Unmarshal(published.Graph, &state.Graph); err != nil {
			return state, fmt.Errorf("parse published workflow: %w", err)
		}
	}
	draft, err := db.LoadIntentionRoutingRAGDraft(ctx)
	if err != nil {
		return state, err
	}
	if draft != nil {
		state.Status = "draft"
		state.Revision = draft.Revision
		state.LockVersion = draft.LockVersion
		state.UpdatedAt = draft.UpdatedAt.Format(time.RFC3339)
		if err := json.Unmarshal(draft.Graph, &state.Graph); err != nil {
			return state, fmt.Errorf("parse draft workflow: %w", err)
		}
	}
	return state, nil
}

func validateIntentionRoutingRAGGraphAgainstDocuments(graph common.IntentionRoutingRAGGraph) ([]common.IntentionRoutingRAGValidationIssue, error) {
	docs, err := db.ListRAGDocuments()
	if err != nil {
		return nil, err
	}
	docSet := make(map[string]struct{}, len(docs))
	for name := range docs {
		docSet[name] = struct{}{}
	}
	return common.ValidateIntentionRoutingRAGGraph(graph, docSet), nil
}

func validateIntentionRoutingRAGDraftShape(graph common.IntentionRoutingRAGGraph) string {
	if graph.SchemaVersion != common.IntentionRoutingRAGSchemaVersion {
		return fmt.Sprintf("schema_version must be %d", common.IntentionRoutingRAGSchemaVersion)
	}
	if len(graph.Nodes) == 0 || len(graph.Nodes) > common.IntentionRoutingRAGMaxBlocks {
		return fmt.Sprintf("A draft must contain 1 through %d blocks.", common.IntentionRoutingRAGMaxBlocks)
	}
	inputCount := 0
	seenIDs := map[string]struct{}{}
	for _, node := range graph.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return "Every draft block must have an ID."
		}
		if _, exists := seenIDs[node.ID]; exists {
			return "Draft block IDs must be unique."
		}
		seenIDs[node.ID] = struct{}{}
		if node.Type == "input" {
			inputCount++
		}
		if len([]rune(node.Name)) > 120 {
			return "A block name cannot exceed 120 characters."
		}
		if node.Routing != nil && len(node.Routing.Options) > common.IntentionRoutingRAGMaxOptions {
			return fmt.Sprintf("A Routing block cannot contain more than %d options.", common.IntentionRoutingRAGMaxOptions)
		}
		if node.Routing != nil {
			if len([]rune(node.Routing.Model)) > 240 {
				return "A routing model identifier cannot exceed 240 characters."
			}
			for _, option := range node.Routing.Options {
				if len([]rune(option.Name)) > 160 || len([]rune(option.Description)) > 4000 {
					return "An intention option name or description is too long."
				}
			}
		}
	}
	if inputCount != 1 {
		return "A draft must contain exactly one Input block."
	}
	return ""
}

func adminDecodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Invalid JSON request: " + err.Error()})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "JSON request contains trailing data."})
		return false
	}
	return true
}

func adminWriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func adminActor(r *http.Request) string {
	if session, ok := adminSessionFromRequest(r); ok {
		return strings.TrimSpace(session.Username)
	}
	return "unknown"
}

func allowIntentionRoutingRAGTest(actor string) bool {
	key := strings.TrimSpace(actor)
	now := time.Now()
	intentionRoutingRAGTestGuards.Lock()
	defer intentionRoutingRAGTestGuards.Unlock()
	last := intentionRoutingRAGTestGuards.LastByActor[key]
	if !last.IsZero() && now.Sub(last) < 3*time.Second {
		return false
	}
	intentionRoutingRAGTestGuards.LastByActor[key] = now
	return true
}
