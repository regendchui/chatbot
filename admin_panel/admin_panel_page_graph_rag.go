package admin_panel

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	ai "whatsapp-bot/AI"
	"whatsapp-bot/db"
)

const graphRAGDeleteConfirmation = "confirm delete"

func adminGraphRAGHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	indexed, err := db.ListRAGDocuments()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	graphDocuments, graphErr := db.ListGraphRAGDocuments(r.Context())
	jobs, jobsErr := db.ListGraphRAGJobs(r.Context(), 100)
	audits, auditsErr := db.ListGraphRAGExtractionAudits(r.Context(), 100)
	selected := map[string]db.GraphRAGDocument{}
	for _, document := range graphDocuments {
		selected[document.DocumentName] = document
	}
	names := make([]string, 0, len(indexed))
	for name := range indexed {
		names = append(names, name)
	}
	sort.Strings(names)
	availability := "Available"
	if err := db.GraphRAGAvailable(r.Context()); err != nil {
		availability = "Unavailable: " + err.Error()
	}
	var b strings.Builder
	b.WriteString(adminPageHeader("Graph RAG"))
	b.WriteString(`<h2>Graph RAG</h2>`)
	b.WriteString(adminNav(r))
	if msg := strings.TrimSpace(r.URL.Query().Get("msg")); msg != "" {
		b.WriteString(`<p style="color:#065f46;">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<p><strong>Apache AGE:</strong> ` + html.EscapeString(availability) + `</p>`)
	b.WriteString(`<p>Choose existing RAG documents for automatic entity and relationship extraction. Builds run in the background and the previous successful snapshot remains live until replacement succeeds.</p>`)
	b.WriteString(`<h3>Document ingestion</h3>`)
	b.WriteString(graphRAGTableStart("Document ingestion table", 1000))
	b.WriteString(`<tr><th>Document</th><th>Chunks</th><th>Selected</th><th>Status</th><th>Entities</th><th>Relationships</th><th>Last error</th><th>Actions</th></tr>`)
	for _, name := range names {
		document, exists := selected[name]
		b.WriteString(`<tr><td>` + html.EscapeString(name) + `</td><td>` + fmt.Sprint(indexed[name]) + `</td><td>` + fmt.Sprint(exists && document.Selected) + `</td><td>` + html.EscapeString(document.Status) + graphStaleBadge(document.Stale) + `</td><td>` + fmt.Sprint(document.EntityCount) + `</td><td>` + fmt.Sprint(document.RelationshipCount) + `</td><td>` + html.EscapeString(document.LastError) + `</td><td>`)
		if !exists || !document.Selected {
			b.WriteString(graphRAGActionForm(r, "/admin/graph-rag/select", name, "Select & build", ""))
		} else {
			if document.ActiveSnapshotID != "" {
				b.WriteString(graphRAGDocumentPreviewButton(name))
			}
			b.WriteString(graphRAGActionForm(r, "/admin/graph-rag/rebuild", name, "Rebuild", ""))
			b.WriteString(graphRAGActionForm(r, "/admin/graph-rag/remove", name, "Remove from graph", "Remove this document's graph provenance? The traditional RAG document will remain."))
		}
		b.WriteString(`</td></tr>`)
	}
	b.WriteString(`</table></div>`)
	b.WriteString(`<div style="display:flex;flex-wrap:wrap;gap:8px;margin-top:12px;">` + graphRAGCompletePreviewButton() + `<form method="post" action="/admin/graph-rag/rebuild-stale">` + adminCSRFInput(r) + `<button type="submit">Rebuild all stale documents</button></form></div>`)

	b.WriteString(`<h3>Background jobs</h3>`)
	if graphErr != nil || jobsErr != nil {
		b.WriteString(`<p style="color:#b91c1c;">` + html.EscapeString(fmt.Sprint(graphErr, " ", jobsErr)) + `</p>`)
	} else {
		b.WriteString(graphRAGTableStart("Background jobs table", 1280))
		b.WriteString(`<tr><th>ID</th><th>Document</th><th>Status</th><th>Progress</th><th>Entities</th><th>Relationships</th><th>Tokens</th><th>Created</th><th>Started</th><th>Finished</th><th>Error</th></tr>`)
		for _, job := range jobs {
			b.WriteString(fmt.Sprintf(`<tr><td>%d</td><td>%s</td><td>%s</td><td>%d/%d</td><td>%d</td><td>%d</td><td>%d + %d</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`, job.ID, html.EscapeString(job.DocumentName), html.EscapeString(job.Status), job.ProcessedChunks, job.TotalChunks, job.EntityCount, job.RelationshipCount, job.PromptTokens, job.CompletionTokens, html.EscapeString(job.CreatedAt.Format(time.RFC3339)), graphOptionalTime(job.StartedAt), graphOptionalTime(job.FinishedAt), html.EscapeString(job.LastError)))
		}
		b.WriteString(`</table></div>`)
	}
	b.WriteString(`<h3>Extraction audit</h3><p>Authenticated operational trace of provider output, validation errors, and token use. Output is truncated for display.</p>`)
	if auditsErr != nil {
		b.WriteString(`<p style="color:#b91c1c;">` + html.EscapeString(auditsErr.Error()) + `</p>`)
	} else {
		b.WriteString(graphRAGTableStart("Extraction audit table", 1000))
		b.WriteString(`<tr><th>ID</th><th>Job</th><th>Chunk</th><th>Tokens</th><th>Created</th><th>Validation error</th><th>Provider output</th></tr>`)
		for _, audit := range audits {
			b.WriteString(fmt.Sprintf(`<tr><td>%d</td><td>%d</td><td>%d</td><td>%d + %d</td><td>%s</td><td><pre>%s</pre></td><td><details><summary>View</summary><pre>%s</pre></details></td></tr>`, audit.ID, audit.JobID, audit.ChunkIndex, audit.PromptTokens, audit.CompletionTokens, html.EscapeString(audit.CreatedAt.Format(time.RFC3339)), html.EscapeString(graphDisplayText(audit.ValidationError, 2000)), html.EscapeString(graphDisplayText(audit.RawResponse, 4000))))
		}
		b.WriteString(`</table></div>`)
	}

	b.WriteString(`<h3>Natural-language search and graph preview</h3><p>The test uses the current general Graph RAG retrieval settings. It shows resolved seeds, traversed evidence, provenance, latency diagnostics, and the exact context supplied to generation.</p>`)
	b.WriteString(`<form id="graph-test-form">` + adminCSRFInput(r) + `<textarea name="enquiry" rows="4" cols="100" maxlength="10000" required placeholder="Ask a question about the selected graph documents"></textarea><div style="display:flex;flex-wrap:wrap;gap:8px;margin:8px 0;"><label>Entity type filter <input id="graph-filter-entity" placeholder="Clinic"></label><label>Relationship filter <input id="graph-filter-relation" placeholder="LOCATED_IN"></label><label>Document filter <input id="graph-filter-document" placeholder="location.pdf"></label><label>Minimum confidence <input id="graph-filter-confidence" type="number" min="0" max="1" step="0.01" value="0"></label></div><button type="submit">Run Graph RAG test</button></form>`)
	b.WriteString(`<pre id="graph-test-debug" style="white-space:pre-wrap;background:#f8fafc;padding:10px;"></pre>`)
	b.WriteString(graphRAGPreviewControls())
	b.WriteString(`<div id="graph-preview" style="overflow:hidden;border:1px solid #cbd5e1;min-height:520px;touch-action:none;background:#fff;"></div><h4>Evidence table</h4><div id="graph-evidence" style="max-height:420px;overflow:auto;"></div><h4>Generated graph context</h4><pre id="graph-context" style="white-space:pre-wrap;background:#f8fafc;padding:10px;"></pre>`)
	b.WriteString(graphRAGPreviewScript())

	b.WriteString(`<h3>Delete entire graph</h3><p>This deletes Graph RAG snapshots and provenance only. Traditional RAG documents remain.</p><form method="post" action="/admin/graph-rag/delete-all" onsubmit="return confirm('Delete the entire Graph RAG dataset?');">` + adminCSRFInput(r) + `<label>Type <strong>` + graphRAGDeleteConfirmation + `</strong><br><input name="delete_confirmation" autocomplete="off" required></label> <button type="submit" style="background:#b91c1c;color:white;">Delete entire graph</button></form>`)
	b.WriteString(adminPageFooter())
	adminWriteHTML(w, b.String())
}

func adminGraphRAGSelectHandler(w http.ResponseWriter, r *http.Request) {
	adminGraphRAGDocumentAction(w, r, func(ctx context.Context, name string) error {
		return db.SelectGraphRAGDocument(ctx, name, ai.GraphRAGExtractionSettingsHash())
	}, "Document selected and build queued.")
}

func adminGraphRAGRebuildHandler(w http.ResponseWriter, r *http.Request) {
	adminGraphRAGDocumentAction(w, r, func(ctx context.Context, name string) error {
		return db.QueueGraphRAGRebuild(ctx, name, ai.GraphRAGExtractionSettingsHash())
	}, "Graph rebuild queued.")
}

func adminGraphRAGRemoveHandler(w http.ResponseWriter, r *http.Request) {
	adminGraphRAGDocumentAction(w, r, db.RemoveGraphRAGDocument, "Document provenance removed from Graph RAG.")
}

func adminGraphRAGDocumentAction(w http.ResponseWriter, r *http.Request, action func(context.Context, string) error, success string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("document_name"))
	if name == "" {
		graphRAGRedirect(w, r, "Document name is required.")
		return
	}
	if err := action(r.Context(), name); err != nil {
		graphRAGRedirect(w, r, "Action failed: "+err.Error())
		return
	}
	adminRecordConfigUpdateHistory(r, "graph_rag_document_action", success+" Document: "+name)
	graphRAGRedirect(w, r, success)
}

func adminGraphRAGRebuildStaleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	count, err := db.QueueAllStaleGraphRAGDocuments(r.Context(), ai.GraphRAGExtractionSettingsHash())
	if err != nil {
		graphRAGRedirect(w, r, "Queue failed: "+err.Error())
		return
	}
	adminRecordConfigUpdateHistory(r, "graph_rag_rebuild_stale", fmt.Sprintf("Queued %d stale Graph RAG documents", count))
	graphRAGRedirect(w, r, fmt.Sprintf("Queued %d stale document(s).", count))
}

func adminGraphRAGDeleteAllHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	if !graphRAGDeleteConfirmationMatches(r.FormValue("delete_confirmation")) {
		graphRAGRedirect(w, r, `Type "confirm delete" to delete the graph.`)
		return
	}
	if err := db.DeleteEntireGraphRAG(r.Context()); err != nil {
		graphRAGRedirect(w, r, "Delete failed: "+err.Error())
		return
	}
	adminRecordConfigUpdateHistory(r, "graph_rag_delete_all", "Deleted the complete Graph RAG dataset")
	graphRAGRedirect(w, r, "Entire Graph RAG dataset deleted.")
}

func adminGraphRAGTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		adminWriteJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	enquiry := strings.TrimSpace(r.FormValue("enquiry"))
	if enquiry == "" || len([]rune(enquiry)) > 10000 {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Enquiry must contain 1 through 10000 characters."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	result, err := ai.RetrieveGraphRAGWithDebug(ctx, enquiry, nil, nil, ai.CurrentGraphRAGRetrievalSettings())
	if err != nil {
		adminWriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "result": result})
		return
	}
	adminRecordConfigUpdateHistory(r, "graph_rag_test", "Ran an authenticated Graph RAG retrieval test")
	adminWriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func adminGraphRAGDocumentPreviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		adminWriteJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	documentName := strings.TrimSpace(r.FormValue("document_name"))
	if documentName == "" || len([]rune(documentName)) > 1000 {
		adminWriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Document name must contain 1 through 1000 characters."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	graph, err := db.PreviewGraphRAGDocument(ctx, documentName, 250, 500)
	if err != nil {
		adminWriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	debug := fmt.Sprintf("document_preview=true document=%s entity_count=%d relationship_count=%d revision=%s", documentName, len(graph.Entities), len(graph.Relationships), graph.GraphRevision)
	adminWriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": map[string]any{"graph": graph, "debug": debug, "context": ""}})
}

func adminGraphRAGCompletePreviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		adminWriteJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	if !adminRequireCSRF(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	graph, err := db.PreviewCompleteGraphRAG(ctx, 250, 500)
	if err != nil {
		adminWriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	debug := fmt.Sprintf("complete_graph_preview=true entity_count=%d relationship_count=%d revision=%s", len(graph.Entities), len(graph.Relationships), graph.GraphRevision)
	adminWriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": map[string]any{"graph": graph, "debug": debug, "context": ""}})
}

func graphRAGTableStart(label string, minWidth int) string {
	return fmt.Sprintf(`<div class="graph-rag-table-scroll" role="region" aria-label="%s" tabindex="0" style="max-height:420px;overflow:auto;-webkit-overflow-scrolling:touch;"><table border="1" cellpadding="6" cellspacing="0" style="min-width:%dpx;width:100%%;border-collapse:collapse;">`, html.EscapeString(label), minWidth)
}

func graphRAGDeleteConfirmationMatches(value string) bool {
	return strings.TrimSpace(value) == graphRAGDeleteConfirmation
}

func graphRAGRedirect(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/admin/graph-rag?msg="+url.QueryEscape(message), http.StatusSeeOther)
}

func graphStaleBadge(stale bool) string {
	if stale {
		return ` <strong style="color:#b45309;">(rebuild required)</strong>`
	}
	return ""
}

func graphOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return html.EscapeString(value.Format(time.RFC3339))
}

func graphDisplayText(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}

func graphRAGActionForm(r *http.Request, action, documentName, label, confirm string) string {
	confirmAttribute := ""
	if confirm != "" {
		encoded, _ := json.Marshal(confirm)
		confirmAttribute = ` onsubmit="return confirm(` + html.EscapeString(string(encoded)) + `);"`
	}
	return `<form method="post" action="` + html.EscapeString(action) + `" style="display:inline;"` + confirmAttribute + `>` + adminCSRFInput(r) + `<input type="hidden" name="document_name" value="` + html.EscapeString(documentName) + `"><button type="submit">` + html.EscapeString(label) + `</button></form> `
}

func graphRAGDocumentPreviewButton(documentName string) string {
	return `<button type="button" class="graph-document-preview" data-document-name="` + html.EscapeString(documentName) + `">Preview graph</button> `
}

func graphRAGCompletePreviewButton() string {
	return `<button type="button" id="graph-complete-preview" title="Summarizes the most important connected concepts across all selected documents.">Preview combined concept graph</button>`
}

func graphRAGPreviewControls() string {
	return `<div style="display:flex;align-items:center;justify-content:space-between;gap:12px;margin:12px 0 6px;"><strong id="graph-preview-title">Graph preview</strong><span style="display:flex;align-items:center;gap:6px;"><button id="graph-zoom-out" type="button" aria-label="Zoom out">−</button><span id="graph-zoom-level" style="min-width:52px;text-align:center;">100%</span><button id="graph-zoom-in" type="button" aria-label="Zoom in">+</button><button id="graph-fit" type="button">Fit</button></span></div>`
}

func graphRAGPreviewScript() string {
	return `<script>
const graphViewState={svg:null,base:null,current:null};
const graphNS='http://www.w3.org/2000/svg';
function graphSVGElement(name,attributes){const element=document.createElementNS(graphNS,name);Object.entries(attributes||{}).forEach(entry=>element.setAttribute(entry[0],String(entry[1])));return element}
function graphSetViewBox(box){if(!graphViewState.svg||!box)return;graphViewState.current={x:box.x,y:box.y,width:box.width,height:box.height};graphViewState.svg.setAttribute('viewBox',[box.x,box.y,box.width,box.height].join(' '));const percent=Math.round(100*graphViewState.base.width/box.width);document.getElementById('graph-zoom-level').textContent=percent+'%'}
function graphFit(){if(graphViewState.base)graphSetViewBox(graphViewState.base)}
function graphZoom(factor,clientX,clientY){if(!graphViewState.current||!graphViewState.svg)return;const current=graphViewState.current,base=graphViewState.base;let width=current.width*factor,height=current.height*factor;const minimum=base.width/4,maximum=base.width*4;if(width<minimum||width>maximum)return;const rect=graphViewState.svg.getBoundingClientRect();const ratioX=clientX===undefined?0.5:Math.max(0,Math.min(1,(clientX-rect.left)/rect.width));const ratioY=clientY===undefined?0.5:Math.max(0,Math.min(1,(clientY-rect.top)/rect.height));graphSetViewBox({x:current.x+(current.width-width)*ratioX,y:current.y+(current.height-height)*ratioY,width:width,height:height})}
function graphLabelLines(value,maxUnits,maxLines){const characters=Array.from(String(value||'')),lines=[];let current='',units=0;characters.forEach(function(character){const characterUnits=/\s/.test(character)?0.5:(character.codePointAt(0)>255?2:1);if(current&&units+characterUnits>maxUnits){lines.push(current.trim());current='';units=0}current+=character;units+=characterUnits});if(current.trim()||!lines.length)lines.push(current.trim());if(lines.length<=maxLines)return lines;const visible=lines.slice(0,maxLines);let last=Array.from(visible[maxLines-1]);if(last.length)last=last.slice(0,-1);visible[maxLines-1]=last.join('')+'…';return visible}
function graphRenderEvidence(relationships){const evidence=document.getElementById('graph-evidence');evidence.replaceChildren();const table=document.createElement('table');table.border='1';table.cellPadding='6';table.style.minWidth='1000px';table.style.width='100%';table.style.borderCollapse='collapse';const head=document.createElement('tr');['From','From type','Relationship','To','To type','Confidence','Source','Depth'].forEach(function(value){const th=document.createElement('th');th.textContent=value;head.appendChild(th)});table.appendChild(head);relationships.forEach(function(rel){const tr=document.createElement('tr');[rel.from,rel.from_type,rel.relation_type,rel.to,rel.to_type,Number(rel.confidence||0).toFixed(3),(rel.document_name||'')+', chunk '+rel.chunk_index,rel.depth].forEach(function(value){const td=document.createElement('td');td.textContent=value;tr.appendChild(td)});table.appendChild(tr)});evidence.appendChild(table)}
function graphActivateNavigation(svg,width,height){graphViewState.svg=svg;graphViewState.base={x:0,y:0,width:width,height:height};graphSetViewBox(graphViewState.base);let dragging=false,lastX=0,lastY=0;svg.addEventListener('pointerdown',function(event){dragging=true;lastX=event.clientX;lastY=event.clientY;svg.setPointerCapture(event.pointerId);svg.style.cursor='grabbing'});svg.addEventListener('pointermove',function(event){if(!dragging)return;const rect=svg.getBoundingClientRect(),current=graphViewState.current;graphSetViewBox({x:current.x-(event.clientX-lastX)*current.width/rect.width,y:current.y-(event.clientY-lastY)*current.height/rect.height,width:current.width,height:current.height});lastX=event.clientX;lastY=event.clientY});function stopDragging(event){dragging=false;svg.style.cursor='grab';if(event.pointerId!==undefined&&svg.hasPointerCapture(event.pointerId))svg.releasePointerCapture(event.pointerId)}svg.addEventListener('pointerup',stopDragging);svg.addEventListener('pointercancel',stopDragging);svg.addEventListener('wheel',function(event){event.preventDefault();graphZoom(event.deltaY<0?0.85:1.18,event.clientX,event.clientY)},{passive:false})}
function graphSelectImportantSubgraph(nodes,relationships,maxNodes,maxEdges){const documents=new Map();nodes.forEach(function(node){node.score=0;documents.set(node.name,new Set())});relationships.forEach(function(rel){const confidence=Number(rel.confidence||0),from=nodes.get(String(rel.from)),to=nodes.get(String(rel.to));if(from){from.score+=2+confidence;documents.get(from.name).add(String(rel.document_name||''))}if(to){to.score+=2+confidence;documents.get(to.name).add(String(rel.document_name||''))}});nodes.forEach(function(node){node.score+=(documents.get(node.name)||new Set()).size*1.5});const ranked=Array.from(nodes.values()).sort(function(a,b){return b.score-a.score||b.degree-a.degree||a.name.localeCompare(b.name)}),selected=[],selectedNames=new Set(),representedTypes=new Set();ranked.forEach(function(node){if(selected.length>=maxNodes)return;if(!representedTypes.has(node.type)){selected.push(node);selectedNames.add(node.name);representedTypes.add(node.type)}});ranked.forEach(function(node){if(selected.length<maxNodes&&!selectedNames.has(node.name)){selected.push(node);selectedNames.add(node.name)}});const selectedRelationships=relationships.filter(function(rel){return selectedNames.has(String(rel.from))&&selectedNames.has(String(rel.to))}).sort(function(a,b){const aScore=Number(a.confidence||0)+(nodes.get(String(a.from)).score+nodes.get(String(a.to)).score)/100,bScore=Number(b.confidence||0)+(nodes.get(String(b.from)).score+nodes.get(String(b.to)).score)/100;return bScore-aScore}).slice(0,maxEdges);return{nodes:selected,relationships:selectedRelationships,totalNodes:nodes.size,totalRelationships:relationships.length}}
function graphTypeColor(type){const palette=[['#fed7aa','#ea580c'],['#bae6fd','#0284c7'],['#bbf7d0','#16a34a'],['#fde68a','#ca8a04'],['#ddd6fe','#7c3aed'],['#fecdd3','#e11d48'],['#bfdbfe','#2563eb'],['#ccfbf1','#0f766e']];let hash=0;Array.from(String(type||'Unknown')).forEach(function(character){hash=(hash*31+character.codePointAt(0))>>>0});return palette[hash%palette.length]}
function graphRenderConceptMap(preview,nodes,relationships,title,totalNodes,totalRelationships){const width=1200,height=760,centerX=width/2,centerY=height/2+28,positions=new Map();nodes.forEach(function(node,index){const angle=index===0?0:(index-1)*Math.PI*2/Math.max(1,nodes.length-1),ring=index===0?0:180+80*Math.floor((index-1)/12);positions.set(node.name,{x:centerX+Math.cos(angle)*ring,y:centerY+Math.sin(angle)*ring,vx:0,vy:0})});for(let iteration=0;iteration<180;iteration++){for(let i=0;i<nodes.length;i++){const a=positions.get(nodes[i].name);for(let j=i+1;j<nodes.length;j++){const b=positions.get(nodes[j].name),dx=a.x-b.x,dy=a.y-b.y,distanceSquared=Math.max(900,dx*dx+dy*dy),force=11000/distanceSquared,distance=Math.sqrt(distanceSquared);a.vx+=dx/distance*force;a.vy+=dy/distance*force;b.vx-=dx/distance*force;b.vy-=dy/distance*force}}relationships.forEach(function(rel){const a=positions.get(String(rel.from)),b=positions.get(String(rel.to));if(!a||!b)return;const dx=b.x-a.x,dy=b.y-a.y,distance=Math.max(1,Math.sqrt(dx*dx+dy*dy)),force=(distance-150)*0.012;a.vx+=dx/distance*force;a.vy+=dy/distance*force;b.vx-=dx/distance*force;b.vy-=dy/distance*force});nodes.forEach(function(node){const point=positions.get(node.name);point.vx+=(centerX-point.x)*0.0015;point.vy+=(centerY-point.y)*0.0015;point.vx*=0.78;point.vy*=0.78;point.x=Math.max(62,Math.min(width-62,point.x+point.vx));point.y=Math.max(92,Math.min(height-62,point.y+point.vy))})}
  const svg=graphSVGElement('svg',{width:'100%',height:'600',role:'img','aria-label':title||'Concept graph',preserveAspectRatio:'xMidYMid meet'});svg.style.cursor='grab';svg.style.userSelect='none';const defs=graphSVGElement('defs'),marker=graphSVGElement('marker',{id:'graph-concept-arrow',viewBox:'0 0 10 10',refX:9,refY:5,markerWidth:6,markerHeight:6,orient:'auto-start-reverse'}),arrow=graphSVGElement('path',{d:'M 0 0 L 10 5 L 0 10 z',fill:'#64748b'});marker.appendChild(arrow);defs.appendChild(marker);svg.appendChild(defs);relationships.forEach(function(rel,index){const from=positions.get(String(rel.from)),to=positions.get(String(rel.to));if(!from||!to)return;const line=graphSVGElement('line',{x1:from.x,y1:from.y,x2:to.x,y2:to.y,stroke:'#94a3b8','stroke-width':Math.max(1,Number(rel.confidence||0)*2),opacity:'0.72','marker-end':'url(#graph-concept-arrow)'});const tooltip=graphSVGElement('title');tooltip.textContent=String(rel.relation_type||'RELATED_TO')+' — '+String(rel.description||'')+' ('+String(rel.document_name||'')+', chunk '+rel.chunk_index+')';line.appendChild(tooltip);svg.appendChild(line);if(index<20){const label=graphSVGElement('text',{x:(from.x+to.x)/2,y:(from.y+to.y)/2-4,'text-anchor':'middle','font-size':9,fill:'#334155',stroke:'#fff','stroke-width':3,'paint-order':'stroke'});label.textContent=graphLabelLines(String(rel.relation_type||'RELATED_TO'),18,1)[0];svg.appendChild(label)}});nodes.forEach(function(node){const point=positions.get(node.name),radius=Math.max(30,Math.min(44,28+Math.sqrt(Math.max(0,node.score))*2)),colors=graphTypeColor(node.type),group=graphSVGElement('g');const circle=graphSVGElement('circle',{cx:point.x,cy:point.y,r:radius,fill:colors[0],stroke:colors[1],'stroke-width':2});group.appendChild(circle);const lines=graphLabelLines(node.name,14,3),label=graphSVGElement('text',{x:point.x,y:point.y-(lines.length-1)*7,'text-anchor':'middle','font-size':11,'font-weight':600,fill:'#0f172a'});lines.forEach(function(line,index){const span=graphSVGElement('tspan',{x:point.x,dy:index===0?0:14});span.textContent=line;label.appendChild(span)});group.appendChild(label);const tooltip=graphSVGElement('title');tooltip.textContent=node.name+' ('+node.type+') — importance '+node.score.toFixed(1);group.appendChild(tooltip);svg.appendChild(group)});const types=Array.from(new Set(nodes.map(function(node){return node.type}))).slice(0,8);types.forEach(function(type,index){const x=22+(index%4)*285,y=24+Math.floor(index/4)*24,colors=graphTypeColor(type);svg.appendChild(graphSVGElement('circle',{cx:x,cy:y,r:7,fill:colors[0],stroke:colors[1]}));const text=graphSVGElement('text',{x:x+12,y:y+4,'font-size':11,fill:'#334155'});text.textContent=graphLabelLines(type,30,1)[0];svg.appendChild(text)});const summary=graphSVGElement('text',{x:width-18,y:height-16,'text-anchor':'end','font-size':11,fill:'#64748b'});summary.textContent='Showing '+nodes.length+' of '+totalNodes+' key concepts and '+relationships.length+' of '+totalRelationships+' relationships';svg.appendChild(summary);preview.appendChild(svg);graphActivateNavigation(svg,width,height)}
function graphRender(result,applyFilters,title,conceptMode){const graph=result.graph||{},allRelationships=graph.relationships||[];let relationships=allRelationships;if(applyFilters){const entityFilter=document.getElementById('graph-filter-entity').value.trim().toLowerCase(),relationFilter=document.getElementById('graph-filter-relation').value.trim().toLowerCase(),documentFilter=document.getElementById('graph-filter-document').value.trim().toLowerCase(),minimumConfidence=Number(document.getElementById('graph-filter-confidence').value||0);relationships=allRelationships.filter(function(rel){return(!entityFilter||String(rel.from_type||'').toLowerCase().includes(entityFilter)||String(rel.to_type||'').toLowerCase().includes(entityFilter))&&(!relationFilter||String(rel.relation_type||'').toLowerCase().includes(relationFilter))&&(!documentFilter||String(rel.document_name||'').toLowerCase().includes(documentFilter))&&Number(rel.confidence||0)>=minimumConfidence})}
  document.getElementById('graph-preview-title').textContent=title||'Graph preview';const preview=document.getElementById('graph-preview');preview.replaceChildren();const nodes=new Map();(graph.entities||[]).forEach(function(entity){if(entity&&entity.name)nodes.set(String(entity.name),{name:String(entity.name),type:String(entity.entity_type||'Unknown'),degree:0})});relationships.forEach(function(rel){if(rel.from&&!nodes.has(String(rel.from)))nodes.set(String(rel.from),{name:String(rel.from),type:String(rel.from_type||'Unknown'),degree:0});if(rel.to&&!nodes.has(String(rel.to)))nodes.set(String(rel.to),{name:String(rel.to),type:String(rel.to_type||'Unknown'),degree:0});if(nodes.has(String(rel.from)))nodes.get(String(rel.from)).degree++;if(nodes.has(String(rel.to)))nodes.get(String(rel.to)).degree++});const ordered=Array.from(nodes.values()).sort(function(a,b){return b.degree-a.degree||a.name.localeCompare(b.name)});if(!ordered.length){graphRenderEvidence([]);const empty=document.createElement('p');empty.style.padding='28px';empty.style.textAlign='center';empty.style.color='#64748b';empty.textContent='No graph entities are available for this result.';preview.appendChild(empty);graphViewState.svg=null;graphViewState.base=null;graphViewState.current=null;document.getElementById('graph-zoom-level').textContent='100%';return}if(conceptMode){const important=graphSelectImportantSubgraph(nodes,relationships,30,45);graphRenderEvidence(important.relationships);document.getElementById('graph-preview-title').textContent=(title||'Concept graph')+' — '+important.nodes.length+' key concepts';graphRenderConceptMap(preview,important.nodes,important.relationships,title,important.totalNodes,important.totalRelationships);return}graphRenderEvidence(relationships);
  const cardWidth=220,cardHeight=118,gapX=64,gapY=64,padding=56;const columns=Math.max(1,Math.ceil(Math.sqrt(ordered.length*1.7)));const rows=Math.ceil(ordered.length/columns);const width=padding*2+columns*cardWidth+(columns-1)*gapX,height=padding*2+rows*cardHeight+(rows-1)*gapY;const svg=graphSVGElement('svg',{width:'100%',height:'520',role:'img','aria-label':title||'Graph preview',preserveAspectRatio:'xMidYMid meet'});svg.style.cursor='grab';svg.style.userSelect='none';const positions=new Map();ordered.forEach(function(node,index){const column=index%columns,row=Math.floor(index/columns);positions.set(node.name,{x:padding+column*(cardWidth+gapX),y:padding+row*(cardHeight+gapY)})});
  relationships.forEach(function(rel){const from=positions.get(String(rel.from)),to=positions.get(String(rel.to));if(!from||!to)return;const line=graphSVGElement('line',{x1:from.x+cardWidth/2,y1:from.y+cardHeight/2,x2:to.x+cardWidth/2,y2:to.y+cardHeight/2,stroke:'#94a3b8','stroke-width':Math.max(1,Math.min(3,Number(rel.confidence||0)*2.5)),opacity:'0.72'});const tooltip=graphSVGElement('title');tooltip.textContent=String(rel.relation_type||'RELATED_TO')+' — '+String(rel.description||'')+' ('+String(rel.document_name||'')+', chunk '+rel.chunk_index+')';line.appendChild(tooltip);svg.appendChild(line)});
  ordered.forEach(function(node){const position=positions.get(node.name),group=graphSVGElement('g');const rect=graphSVGElement('rect',{x:position.x,y:position.y,width:cardWidth,height:cardHeight,rx:12,fill:'#eff6ff',stroke:'#2563eb','stroke-width':1.5});group.appendChild(rect);const lines=graphLabelLines(node.name,24,4);const label=graphSVGElement('text',{x:position.x+cardWidth/2,y:position.y+23,'text-anchor':'middle',fill:'#0f172a','font-size':12,'font-weight':600});lines.forEach(function(line,index){const span=graphSVGElement('tspan',{x:position.x+cardWidth/2,dy:index===0?0:16});span.textContent=line;label.appendChild(span)});group.appendChild(label);const type=graphSVGElement('text',{x:position.x+cardWidth/2,y:position.y+cardHeight-10,'text-anchor':'middle',fill:'#475569','font-size':11});type.textContent=graphLabelLines(node.type,28,1)[0];group.appendChild(type);const tooltip=graphSVGElement('title');tooltip.textContent=node.name+' ('+node.type+')';group.appendChild(tooltip);svg.appendChild(group)});preview.appendChild(svg);graphActivateNavigation(svg,width,height)}
document.getElementById('graph-zoom-out').addEventListener('click',function(){graphZoom(1.25)});document.getElementById('graph-zoom-in').addEventListener('click',function(){graphZoom(0.8)});document.getElementById('graph-fit').addEventListener('click',graphFit);
document.getElementById('graph-test-form').addEventListener('submit',async function(event){event.preventDefault();const debug=document.getElementById('graph-test-debug');debug.textContent='Loading query graph…';try{const response=await fetch('/admin/graph-rag/test',{method:'POST',body:new FormData(event.target)}),payload=await response.json(),result=payload.result||{};debug.textContent=payload.ok?(result.debug||''):(payload.error||'Test failed');document.getElementById('graph-context').textContent=result.context||'';graphRender(result,true,'Query graph preview')}catch(error){debug.textContent='Unable to load graph: '+error.message}});
document.querySelectorAll('.graph-document-preview').forEach(function(button){button.addEventListener('click',async function(){const documentName=button.dataset.documentName||'',formData=new FormData(),csrf=document.querySelector('#graph-test-form input[name="csrf_token"]');formData.set('document_name',documentName);if(csrf)formData.set('csrf_token',csrf.value);button.disabled=true;document.getElementById('graph-test-debug').textContent='Loading ingested graph for '+documentName+'…';try{const response=await fetch('/admin/graph-rag/document-preview',{method:'POST',body:formData}),payload=await response.json(),result=payload.result||{};document.getElementById('graph-test-debug').textContent=payload.ok?(result.debug||''):(payload.error||'Preview failed');document.getElementById('graph-context').textContent='';if(payload.ok){graphRender(result,false,'Ingested document graph: '+documentName);document.getElementById('graph-preview-title').scrollIntoView({behavior:'smooth',block:'start'})}}catch(error){document.getElementById('graph-test-debug').textContent='Unable to load document graph: '+error.message}finally{button.disabled=false}})});
document.getElementById('graph-complete-preview').addEventListener('click',async function(){const button=this,formData=new FormData(),csrf=document.querySelector('#graph-test-form input[name="csrf_token"]');if(csrf)formData.set('csrf_token',csrf.value);button.disabled=true;document.getElementById('graph-test-debug').textContent='Loading the combined graph for all selected documents…';try{const response=await fetch('/admin/graph-rag/complete-preview',{method:'POST',body:formData}),payload=await response.json(),result=payload.result||{};document.getElementById('graph-test-debug').textContent=payload.ok?(result.debug||''):(payload.error||'Preview failed');document.getElementById('graph-context').textContent='';if(payload.ok){graphRender(result,false,'Complete selected-document concept graph',true);document.getElementById('graph-preview-title').scrollIntoView({behavior:'smooth',block:'start'})}}catch(error){document.getElementById('graph-test-debug').textContent='Unable to load the complete graph: '+error.message}finally{button.disabled=false}});
</script>`
}
