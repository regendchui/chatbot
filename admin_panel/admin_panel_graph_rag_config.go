package admin_panel

import (
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	"whatsapp-bot/common"
)

func adminGraphRAGSettingsHTML(env map[string]string) string {
	var b strings.Builder
	b.WriteString(`<fieldset style="margin-top:24px;padding:16px;border:1px solid #cbd5e1;border-radius:10px;"><legend><strong>Graph RAG settings</strong></legend>`)
	b.WriteString(`<p style="color:#475569;max-width:900px;">Graph RAG uses Apache AGE to connect entities and relationships extracted from selected RAG documents. It can run independently or together with traditional RAG. Saving extraction settings marks existing graph documents as requiring rebuild but does not automatically spend model tokens.</p>`)
	b.WriteString(`<p>GRAPH_RAG_ENABLED<br>` + adminBoolRadioGroup("GRAPH_RAG_ENABLED", env["GRAPH_RAG_ENABLED"]) + `<br><small>Enables general Graph RAG retrieval. Intention RAG blocks may inherit, override, or disable it.</small></p>`)
	b.WriteString(graphTextInput("GRAPH_RAG_EXTRACTION_MODEL", env, "Model used by background ingestion to extract entities and relationships."))
	b.WriteString(`<p><label>GRAPH_RAG_EXTRACTION_PROMPT<br><textarea name="GRAPH_RAG_EXTRACTION_PROMPT" rows="5" cols="120" required>` + html.EscapeString(env["GRAPH_RAG_EXTRACTION_PROMPT"]) + `</textarea></label><br><small>Instruction used for document extraction. The application adds the strict JSON schema and untrusted-evidence safeguards.</small></p>`)
	b.WriteString(graphNumberInput("GRAPH_RAG_MIN_EXTRACTION_CONFIDENCE", env, "0", "1", "0.01", "Discard extracted entities and relationships below this confidence."))
	b.WriteString(graphNumberInput("GRAPH_RAG_BATCH_SIZE", env, "1", "50", "1", "Maximum chunks scheduled in one processing wave."))
	b.WriteString(graphNumberInput("GRAPH_RAG_CONCURRENCY", env, "1", "8", "1", "Maximum simultaneous extraction calls. Higher values increase API usage."))
	b.WriteString(graphNumberInput("GRAPH_RAG_RETRY_COUNT", env, "0", "5", "1", "Retries for invalid JSON or provider errors."))
	b.WriteString(graphNumberInput("GRAPH_RAG_EXTRACTION_TIMEOUT_MS", env, "1000", "120000", "100", "Timeout for each extraction call, in milliseconds."))
	b.WriteString(graphTextInput("GRAPH_RAG_QUERY_MODEL", env, "Latency-sensitive model used to resolve entities from each enquiry."))
	b.WriteString(`<p><label>GRAPH_RAG_QUERY_PROMPT<br><textarea name="GRAPH_RAG_QUERY_PROMPT" rows="4" cols="120" required>` + html.EscapeString(env["GRAPH_RAG_QUERY_PROMPT"]) + `</textarea></label><br><small>Instruction used to identify seed entities. The application adds strict JSON requirements.</small></p>`)
	b.WriteString(graphNumberInput("GRAPH_RAG_INBOUND_MESSAGE_COUNT", env, "1", strconv.Itoa(common.IntentionRoutingRAGMaxInboundMessages), "1", "Latest inbound messages combined for entity resolution."))
	b.WriteString(graphNumberInput("GRAPH_RAG_MAX_TRAVERSAL_DEPTH", env, "1", "5", "1", "Maximum relationship hops. Larger values can expand rapidly."))
	b.WriteString(graphNumberInput("GRAPH_RAG_MAX_SEED_ENTITIES", env, "1", "50", "1", "Maximum entities resolved directly from the enquiry."))
	b.WriteString(graphNumberInput("GRAPH_RAG_MAX_ENTITIES", env, "1", "200", "1", "Maximum distinct entities explored."))
	b.WriteString(graphNumberInput("GRAPH_RAG_MAX_RELATIONSHIPS", env, "1", "500", "1", "Maximum relationships returned before context assembly."))
	b.WriteString(graphNumberInput("GRAPH_RAG_MAX_CONTEXT_CHARS", env, "1000", "100000", "1", "Maximum Graph RAG context before hybrid budgeting."))
	b.WriteString(graphNumberInput("GRAPH_RAG_MIN_MATCH_CONFIDENCE", env, "0", "1", "0.01", "Minimum query/entity and relationship confidence."))
	b.WriteString(graphNumberInput("GRAPH_RAG_TIMEOUT_MS", env, "100", "30000", "100", "Total query-time entity resolution and traversal timeout."))
	b.WriteString(graphNumberInput("HYBRID_RAG_MAX_CONTEXT_CHARS", env, "1000", "200000", "1", "Shared traditional + graph prompt budget. Initial allocation is 60% / 40% and unused space flows between them."))
	b.WriteString(`<p>GRAPH_RAG_INCLUDE_CITATIONS<br>` + adminBoolRadioGroup("GRAPH_RAG_INCLUDE_CITATIONS", env["GRAPH_RAG_INCLUDE_CITATIONS"]) + `<br><small>When enabled, asks the answer model to cite document names. Internal provenance is always included.</small></p>`)
	b.WriteString(`</fieldset>`)
	return b.String()
}

func graphTextInput(name string, env map[string]string, explanation string) string {
	return `<p><label>` + name + `<br><input name="` + name + `" value="` + html.EscapeString(env[name]) + `" style="width:100%;max-width:720px;" required></label><br><small>` + html.EscapeString(explanation) + `</small></p>`
}

func graphNumberInput(name string, env map[string]string, min, max, step, explanation string) string {
	return `<p><label>` + name + `<br><input type="number" name="` + name + `" min="` + min + `" max="` + max + `" step="` + step + `" value="` + html.EscapeString(env[name]) + `" required></label><br><small>` + html.EscapeString(explanation) + `</small></p>`
}

func adminParseGraphRAGSettings(r *http.Request) (map[string]string, error) {
	values := map[string]string{
		"GRAPH_RAG_ENABLED":                   strings.TrimSpace(r.FormValue("GRAPH_RAG_ENABLED")),
		"GRAPH_RAG_EXTRACTION_MODEL":          strings.TrimSpace(r.FormValue("GRAPH_RAG_EXTRACTION_MODEL")),
		"GRAPH_RAG_EXTRACTION_PROMPT":         strings.TrimSpace(r.FormValue("GRAPH_RAG_EXTRACTION_PROMPT")),
		"GRAPH_RAG_MIN_EXTRACTION_CONFIDENCE": strings.TrimSpace(r.FormValue("GRAPH_RAG_MIN_EXTRACTION_CONFIDENCE")),
		"GRAPH_RAG_BATCH_SIZE":                strings.TrimSpace(r.FormValue("GRAPH_RAG_BATCH_SIZE")),
		"GRAPH_RAG_CONCURRENCY":               strings.TrimSpace(r.FormValue("GRAPH_RAG_CONCURRENCY")),
		"GRAPH_RAG_RETRY_COUNT":               strings.TrimSpace(r.FormValue("GRAPH_RAG_RETRY_COUNT")),
		"GRAPH_RAG_EXTRACTION_TIMEOUT_MS":     strings.TrimSpace(r.FormValue("GRAPH_RAG_EXTRACTION_TIMEOUT_MS")),
		"GRAPH_RAG_QUERY_MODEL":               strings.TrimSpace(r.FormValue("GRAPH_RAG_QUERY_MODEL")),
		"GRAPH_RAG_QUERY_PROMPT":              strings.TrimSpace(r.FormValue("GRAPH_RAG_QUERY_PROMPT")),
		"GRAPH_RAG_INBOUND_MESSAGE_COUNT":     strings.TrimSpace(r.FormValue("GRAPH_RAG_INBOUND_MESSAGE_COUNT")),
		"GRAPH_RAG_MAX_TRAVERSAL_DEPTH":       strings.TrimSpace(r.FormValue("GRAPH_RAG_MAX_TRAVERSAL_DEPTH")),
		"GRAPH_RAG_MAX_SEED_ENTITIES":         strings.TrimSpace(r.FormValue("GRAPH_RAG_MAX_SEED_ENTITIES")),
		"GRAPH_RAG_MAX_ENTITIES":              strings.TrimSpace(r.FormValue("GRAPH_RAG_MAX_ENTITIES")),
		"GRAPH_RAG_MAX_RELATIONSHIPS":         strings.TrimSpace(r.FormValue("GRAPH_RAG_MAX_RELATIONSHIPS")),
		"GRAPH_RAG_MAX_CONTEXT_CHARS":         strings.TrimSpace(r.FormValue("GRAPH_RAG_MAX_CONTEXT_CHARS")),
		"GRAPH_RAG_MIN_MATCH_CONFIDENCE":      strings.TrimSpace(r.FormValue("GRAPH_RAG_MIN_MATCH_CONFIDENCE")),
		"GRAPH_RAG_TIMEOUT_MS":                strings.TrimSpace(r.FormValue("GRAPH_RAG_TIMEOUT_MS")),
		"HYBRID_RAG_MAX_CONTEXT_CHARS":        strings.TrimSpace(r.FormValue("HYBRID_RAG_MAX_CONTEXT_CHARS")),
		"GRAPH_RAG_INCLUDE_CITATIONS":         strings.TrimSpace(r.FormValue("GRAPH_RAG_INCLUDE_CITATIONS")),
	}
	if values["GRAPH_RAG_EXTRACTION_MODEL"] == "" || values["GRAPH_RAG_EXTRACTION_PROMPT"] == "" || values["GRAPH_RAG_QUERY_MODEL"] == "" || values["GRAPH_RAG_QUERY_PROMPT"] == "" {
		return nil, fmt.Errorf("Graph RAG models and prompts are required")
	}
	settings := common.GraphRAGRetrievalSettings{}
	var err error
	if settings.InboundMessageCount, err = parseGraphInt(values, "GRAPH_RAG_INBOUND_MESSAGE_COUNT"); err != nil {
		return nil, err
	}
	if settings.MaxTraversalDepth, err = parseGraphInt(values, "GRAPH_RAG_MAX_TRAVERSAL_DEPTH"); err != nil {
		return nil, err
	}
	if settings.MaxSeedEntities, err = parseGraphInt(values, "GRAPH_RAG_MAX_SEED_ENTITIES"); err != nil {
		return nil, err
	}
	if settings.MaxEntities, err = parseGraphInt(values, "GRAPH_RAG_MAX_ENTITIES"); err != nil {
		return nil, err
	}
	if settings.MaxRelationships, err = parseGraphInt(values, "GRAPH_RAG_MAX_RELATIONSHIPS"); err != nil {
		return nil, err
	}
	if settings.MaxContextChars, err = parseGraphInt(values, "GRAPH_RAG_MAX_CONTEXT_CHARS"); err != nil {
		return nil, err
	}
	if settings.TimeoutMS, err = parseGraphInt(values, "GRAPH_RAG_TIMEOUT_MS"); err != nil {
		return nil, err
	}
	if settings.MinMatchConfidence, err = strconv.ParseFloat(values["GRAPH_RAG_MIN_MATCH_CONFIDENCE"], 64); err != nil {
		return nil, fmt.Errorf("GRAPH_RAG_MIN_MATCH_CONFIDENCE must be a number")
	}
	if issues := common.ValidateGraphRAGRetrievalSettings(settings); len(issues) > 0 {
		return nil, fmt.Errorf("%s %s", issues[0].Field, issues[0].Message)
	}
	for _, key := range []string{"GRAPH_RAG_BATCH_SIZE", "GRAPH_RAG_CONCURRENCY", "GRAPH_RAG_RETRY_COUNT", "GRAPH_RAG_EXTRACTION_TIMEOUT_MS", "HYBRID_RAG_MAX_CONTEXT_CHARS"} {
		if _, err := parseGraphInt(values, key); err != nil {
			return nil, err
		}
	}
	ranges := []struct {
		key      string
		min, max int
	}{
		{"GRAPH_RAG_BATCH_SIZE", 1, 50}, {"GRAPH_RAG_CONCURRENCY", 1, 8}, {"GRAPH_RAG_RETRY_COUNT", 0, 5},
		{"GRAPH_RAG_EXTRACTION_TIMEOUT_MS", 1000, 120000}, {"HYBRID_RAG_MAX_CONTEXT_CHARS", 1000, 200000},
	}
	for _, item := range ranges {
		value, _ := strconv.Atoi(values[item.key])
		if value < item.min || value > item.max {
			return nil, fmt.Errorf("%s must be between %d and %d", item.key, item.min, item.max)
		}
	}
	extractionConfidence, err := strconv.ParseFloat(values["GRAPH_RAG_MIN_EXTRACTION_CONFIDENCE"], 64)
	if err != nil || extractionConfidence < 0 || extractionConfidence > 1 {
		return nil, fmt.Errorf("GRAPH_RAG_MIN_EXTRACTION_CONFIDENCE must be between 0 and 1")
	}
	return values, nil
}

func parseGraphInt(values map[string]string, key string) (int, error) {
	value, err := strconv.Atoi(values[key])
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return value, nil
}
