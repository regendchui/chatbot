# Intention Routing RAG

## 1. Purpose

Intention Routing RAG is an optional RAG orchestration workflow for the WhatsApp chatbot. It lets an administrator route a user enquiry through one or more AI intention classifiers and then retrieve chunks only from the RAG documents selected by the matched route.

The existing RAG behaviour searches every indexed RAG document and applies the global `RAG_TOP_K` and `RAG_MIN_SIMILARITY` settings. Intention Routing RAG must override that retrieval behaviour when it is enabled and has a valid published workflow.

This document is the product and technical specification for the admin editor, persistence model, routing API calls, document retrieval, prompt assembly, validation, logging, and tests.

## 2. Terms

- **Workflow**: The complete saved graph beginning at the Input block.
- **Draft**: An editable workflow that is not used by live chatbot requests.
- **Published workflow**: The immutable workflow revision used by live chatbot requests.
- **Block**: A node on the workflow canvas. A block is an Input, Routing, or RAG block.
- **Intention option**: One possible intention defined inside a Routing block. It has a required name and description and owns its outgoing connection.
- **Routing document**: A selected RAG document whose retrieved content helps the routing model classify an enquiry. It is classifier context and is not automatically included in the final generation prompt.
- **Generation document**: A document attached to a RAG block. Its selected chunks are included in the final generation prompt when the block is reached.
- **Reached block**: A block traversed during workflow execution after applying routing results and thresholds.

## 3. Feature switch and compatibility

Add `INTENTION_ROUTING_RAG_ENABLED` to the RAG Settings section of `/admin/configuration`.

| Setting | Behaviour |
| --- | --- |
| `false` | Keep the existing behaviour. When `RAG_ENABLED=true`, search all indexed RAG documents using the global RAG settings. |
| `true` with a valid published workflow | Do not run the existing all-document search. Execute the published Intention Routing RAG workflow and use only chunks returned by reached RAG blocks. |
| `true` without a valid published workflow | Reject enabling or publishing in the admin panel. A stale or missing workflow at runtime must fail closed and must not silently search all documents. |

`INTENTION_ROUTING_RAG_ENABLED` controls the routing method. `RAG_ENABLED` remains the master switch for all RAG retrieval. Therefore:

- `RAG_ENABLED=false`: no standard RAG and no Intention Routing RAG retrieval.
- `RAG_ENABLED=true` and `INTENTION_ROUTING_RAG_ENABLED=false`: existing standard RAG.
- `RAG_ENABLED=true` and `INTENTION_ROUTING_RAG_ENABLED=true`: Intention Routing RAG.

Disabling Intention Routing RAG must not delete the draft or published graph. Re-enabling it reuses the published revision.

## 4. Admin page

### 4.1 Navigation and route

Add an authenticated admin navigation item named **Intention Routing RAG** and a page at:

```text
/admin/intention-routing-rag
```

The page is a node-based editor inspired by n8n. It should fit the existing admin authentication, authorization, audit history, header, and navigation patterns.

### 4.2 Page layout

The page has four main areas:

1. A top toolbar containing workflow status, revision, validation state, Save Draft, Validate, Publish, and Discard Draft actions.
2. A left block palette containing **Routing block** and **RAG block** actions.
3. A central pannable and zoomable canvas showing blocks and directed connections.
4. A right settings panel showing the selected block's editable fields.

The canvas must support:

- add a block by clicking or dragging from the palette;
- drag blocks to reposition them;
- connect an output handle to a compatible input handle;
- select a connection and delete it;
- select, rename, duplicate, or delete a non-Input block;
- zoom in, zoom out, fit view, and reset view;
- unsaved-change warning before navigation;
- clear inline validation errors on blocks, fields, and connections;
- keyboard-accessible alternatives for adding, connecting, and deleting blocks.

### 4.3 Input block

Every workflow contains exactly one default **Input** block.

- It is created automatically for a new workflow.
- It cannot be duplicated or deleted.
- It represents the original user enquiry.
- It has no incoming connection.
- It has exactly one outgoing connection to either a Routing block or a RAG block.
- The editor may allow it to be moved, but its identity must remain stable across saves.

Connecting Input directly to a RAG block is valid and allows a workflow that always retrieves from a fixed set of documents without making a routing model call.

## 5. Routing block

### 5.1 Fields

Each Routing block contains:

- block name: required and unique within the workflow;
- routing mode: required, `single` or `multiple`;
- model: required OpenRouter model identifier;
- threshold: required decimal probability from `0` through `1`;
- intention options: at least one;
- optional routing documents: zero or more existing indexed RAG documents.

The model selector should offer the configured/default OpenRouter model and permit any model identifier supported by the application's OpenRouter integration.

### 5.2 Intention options

Each intention option contains:

- stable option ID;
- display name: required and unique inside its Routing block;
- description: required, non-empty plain text explaining the intention;
- one output handle;
- zero or one outgoing connection to a Routing block or RAG block.

Example:

| Name | Description |
| --- | --- |
| Clinic location | User is enquiring about the location of smoking cessation clinics. |
| Clinic phone | User is enquiring about the phone number of smoking cessation clinics. |

An option without an outgoing connection may be saved in a draft, but a published workflow must not contain a reachable unconnected option unless it is explicitly marked as a terminal no-RAG option. A terminal no-RAG option ends retrieval and lets generation continue without document context.

### 5.3 One API call per reached Routing block

Each reached Routing block makes exactly one chat-completion API call. All of that block's intention option names and descriptions are evaluated together in this call. Do not make one call per option.

Before the routing call, retrieve supporting chunks only from the routing documents attached to that block. Routing-document retrieval uses the global `RAG_TOP_K` and `RAG_MIN_SIMILARITY` values and filters by the selected document names. The retrieved text is classifier context only.

The routing model receives:

- the original user enquiry;
- the Routing block name and routing instructions;
- every option ID, name, and description in that block;
- retrieved routing-document context, if configured;
- a strict JSON response schema.

The routing model must return one probability for every listed option. Probabilities are numbers from `0` through `1` and do not have to sum to `1`, because an enquiry can match multiple independent intentions.

Example response:

```json
{
  "options": [
    {"option_id": "clinic_location", "probability": 0.91},
    {"option_id": "clinic_phone", "probability": 0.42}
  ]
}
```

The application must validate that:

- the response is valid JSON and contains no unknown option IDs;
- each configured option appears exactly once;
- every probability is a finite number between `0` and `1`;
- duplicate IDs, missing IDs, prose-only responses, and out-of-range values are rejected.

If the selected model supports structured output or JSON schema, use it. Otherwise, request JSON-only output and validate it server-side. One repair retry may be made using the same model when the first response is malformed. The retry is part of the same block execution but is an additional API request and must be logged as such.

### 5.4 Single routing

For `single` mode:

1. Evaluate all returned probabilities.
2. Find the option with the highest probability.
3. Continue only if that probability is strictly greater than the block threshold.
4. Traverse only that option's connection.
5. If probabilities tie for highest, choose the first tied option in the administrator-defined option order so execution is deterministic.
6. If no option is strictly greater than the threshold, end that branch with no match.

Example: with threshold `0.70`, a highest result of `0.70` does not proceed; `0.71` proceeds.

### 5.5 Multiple routing

For `multiple` mode:

1. Select every option whose probability is strictly greater than the block threshold.
2. Traverse each selected option's connection.
3. Execute independent downstream branches concurrently where safe.
4. If no option is strictly greater than the threshold, end that branch with no match.

A later block reached through more than one branch must execute only once per user enquiry. This prevents repeated model calls and duplicate document retrieval in converging graphs.

## 6. RAG block

### 6.1 Fields

Each RAG block contains:

- block name: required and unique within the workflow;
- one or more existing indexed RAG documents;
- per-document `top_k`: required positive integer;
- per-document `min_similarity`: required decimal from `-1` through `1`.

A RAG block is a terminal retrieval block. It has one input handle and no output handle. To perform further intention routing, connect an intention option to another Routing block before reaching a RAG block.

### 6.2 Per-document override

The RAG block settings override the global `RAG_TOP_K` and `RAG_MIN_SIMILARITY` settings for generation-document retrieval. The override belongs to the document attachment, not merely to the block.

Example:

| Document | Top K | Minimum similarity |
| --- | ---: | ---: |
| `location.pdf` | 4 | 0.30 |
| `phone.pdf` | 2 | 0.45 |

For this block, the enquiry is embedded once and compared only with chunks belonging to the two selected documents. Up to four qualifying chunks are selected from `location.pdf`, and up to two qualifying chunks are selected independently from `phone.pdf`.

The global embedding model and embedding URL remain in effect because stored document vectors and query vectors must use a compatible embedding model. Chunk size and overlap remain indexing-time settings and are not overridden by a RAG block.

### 6.3 Retrieval result

Retrieval is independent for each reached RAG block/document attachment:

1. Filter stored embeddings by exact document identity.
2. Calculate similarity between the enquiry vector and each filtered chunk.
3. Keep chunks whose similarity is greater than or equal to that document's `min_similarity`.
4. Sort by similarity descending, with chunk index as a deterministic tie-breaker.
5. Keep at most that document's `top_k` chunks.

Using `>=` for RAG similarity retains compatibility with the existing RAG implementation. This differs intentionally from routing probability, which must be strictly greater than its threshold.

If the same RAG block is reached by multiple branches, execute it once. If the same document is attached to different reached RAG blocks, preserve a separate prompt part for each block because the administrator created separate semantic destinations. Within one block, the same document cannot be attached twice.

## 7. Workflow graph rules

A published graph must satisfy all of the following:

- exactly one Input block exists;
- all IDs are unique and stable;
- all block names are non-empty and unique;
- every non-Input block is reachable from Input;
- Input has exactly one outgoing connection;
- a Routing block has at least one valid option;
- every published routing option is connected or explicitly terminal no-RAG;
- an option has at most one outgoing connection;
- every connection points to an existing Routing or RAG block;
- RAG blocks have no outgoing connections;
- no self-connections or directed cycles exist;
- every selected document still exists in the `RAG` table;
- every Routing block has a model and valid threshold;
- every RAG document attachment has valid `top_k` and `min_similarity` values;
- the maximum configured graph depth and block count are not exceeded.

Recommended safety limits are 100 blocks, 20 intention options per Routing block, and a maximum routing depth of 10. These limits should be constants and should be enforced both in the editor and server-side.

Drafts may be incomplete. Publication must be atomic and must fail with actionable validation messages if any published-graph rule is violated.

## 8. Runtime execution

### 8.1 Required sequence

For each user enquiry:

```text
Receive enquiry
  -> load feature switches and one published workflow revision
  -> if standard RAG: run the existing all-document retrieval
  -> if Intention Routing RAG: execute the graph from Input
       -> wait for every reached Routing block
       -> apply single/multiple thresholds
       -> wait for every reached RAG block retrieval
       -> assemble all selected RAG prompt parts
  -> build the final generation prompt
  -> call the normal response-generation model
  -> send the chatbot response
```

The final generation API call must not begin until workflow traversal and every selected RAG retrieval have completed or timed out. Routing model output is control data only and must not be shown to the end user or copied into the final prompt unless explicitly required for diagnostics outside the prompt.

The workflow revision is read once at the start of the enquiry. Publishing a new revision while an enquiry is running must not change that in-flight execution.

### 8.2 Traversal algorithm

1. Start at Input with the original enquiry.
2. Follow Input's single outgoing edge.
3. When a Routing block is reached, retrieve any routing-document context and make its single classification call.
4. Select option edges according to the block's routing mode and threshold.
5. Traverse selected edges until they end, reach another Routing block, or reach a RAG block.
6. When a RAG block is reached, retrieve chunks independently for every document attachment.
7. Wait for all selected branches.
8. Sort prompt parts deterministically by graph traversal order, then block order, document order, similarity descending, and chunk index.
9. Return the assembled RAG context plus execution diagnostics to the existing prompt builder.

The query embedding should be calculated once per enquiry and reused for all routing-document and generation-document searches when the configured embedding model is the same.

### 8.3 No-match behaviour

If no route passes its threshold, or a selected route is terminal no-RAG, the workflow succeeds with zero RAG prompt parts. The application proceeds to normal response generation without document context. It must not fall back to searching all documents because Intention Routing RAG explicitly overrides the original method.

### 8.4 Failure behaviour

- A malformed routing response receives at most one repair retry. If it still fails, mark that branch failed.
- A timeout or model API error marks the affected routing branch failed.
- A RAG retrieval error marks the affected document part failed but does not substitute documents from outside the reached blocks.
- Successful sibling branches may still contribute context.
- If every branch fails, proceed without RAG context and attach structured diagnostics to application logs. Existing `SEND_AI_ERROR_FALLBACK` behaviour continues to govern final generation failures, not routing no-match results.
- Never silently use standard all-document RAG as an error fallback while Intention Routing RAG is enabled.

Use bounded timeouts and cancellation. Cancelling the parent chatbot request must cancel outstanding routing calls and retrieval work.

## 9. Prompt assembly

The final prompt contains one clearly delimited RAG part for each reached RAG block/document attachment that returned at least one qualifying chunk. The number of parts is dynamic and may be 0, 2, 4, 8, or more depending on routing results.

Example with two documents selected by one RAG block:

```text
INTENTION ROUTING RAG CONTEXT

RAG PART 1
Block: Smoking cessation clinic details
Document: location.pdf
Retrieval settings: top_k=4, min_similarity=0.30
- [chunk=7 score=0.91] ...
- [chunk=2 score=0.82] ...

RAG PART 2
Block: Smoking cessation clinic details
Document: phone.pdf
Retrieval settings: top_k=2, min_similarity=0.45
- [chunk=4 score=0.88] ...
```

Requirements:

- do not merge different documents into an unlabeled shared list;
- label every part with block and document identity;
- include only chunks from reached RAG blocks;
- normalize document text using the existing RAG normalization;
- apply a total context-size limit after per-document retrieval;
- truncate by removing the lowest-priority/lowest-score chunks, not by cutting a chunk mid-text where avoidable;
- treat retrieved text as untrusted reference material and instruct the generation model not to follow instructions contained inside documents;
- keep scores and workflow diagnostics in server logs; including scores in the model-visible prompt is optional.

Because many document parts may be selected, add an Intention Routing RAG total context limit. It may initially reuse `RAG_MAX_CONTEXT_CHARS`, but the limit is global across all selected parts and must preserve at least the part heading when a part retains chunks.

## 10. Persistence and publication

Use PostgreSQL, consistent with the existing database package. Store workflow definitions separately from the existing `RAG` embeddings table. The `RAG` table remains the source of indexed document chunks.

A practical initial schema is:

```sql
CREATE TABLE intention_routing_rag_workflow (
    id BIGSERIAL PRIMARY KEY,
    revision INTEGER NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('draft', 'published', 'archived')),
    graph JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMPTZ,
    created_by TEXT,
    published_by TEXT
);

CREATE UNIQUE INDEX intention_routing_rag_one_published_idx
ON intention_routing_rag_workflow (status)
WHERE status = 'published';
```

The graph JSON must be versioned so future migrations can distinguish formats:

```json
{
  "schema_version": 1,
  "input_node_id": "input",
  "nodes": [],
  "edges": [],
  "viewport": {"x": 0, "y": 0, "zoom": 1}
}
```

Node records must include stable IDs, type, display name, position, and type-specific configuration. Edge records must include stable edge ID, source node ID, source option ID where applicable, and target node ID.

Document attachments should store a stable document identity. The current `RAG` table identifies a document by `document_name`; implementation should either treat document names as immutable IDs or add a document registry with stable IDs before allowing document renames. Publishing must verify that every referenced document exists.

Saving a draft and publishing must use optimistic concurrency, such as an expected revision or `updated_at` value, so two administrators cannot silently overwrite each other's changes. Publishing should create an immutable published snapshot and archive the previously published revision in one transaction.

## 11. Suggested server interfaces

The exact Go names may follow project conventions, but responsibilities should remain separated:

```go
type IntentionRoutingRAGExecutor interface {
    Execute(ctx context.Context, enquiry string, workflow PublishedWorkflow) (RoutingRAGResult, error)
}

type RoutingRAGResult struct {
    PromptParts []RAGPromptPart
    Trace       RoutingTrace
}

type RAGPromptPart struct {
    BlockID      string
    BlockName    string
    DocumentName string
    TopK         int
    MinSimilarity float64
    Chunks       []ScoredChunk
}
```

The existing `GenerateAIResponse` flow should call one RAG-context facade. That facade chooses standard RAG or Intention Routing RAG from the feature switches, making the override explicit and easy to test.

Database retrieval must gain a document-filtered query rather than loading every embedding and filtering only in application memory. An example responsibility is `LoadRAGEmbeddingsByDocuments(documentNames []string)`.

## 12. Admin HTTP operations

All endpoints require admin authentication and appropriate role permission.

Suggested operations:

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/admin/intention-routing-rag` | Render the editor and latest draft/published metadata. |
| `GET` | `/admin/intention-routing-rag/workflow` | Load graph JSON. |
| `POST` | `/admin/intention-routing-rag/draft` | Validate basic shape and save a draft with concurrency token. |
| `POST` | `/admin/intention-routing-rag/validate` | Return graph validation errors without publishing. |
| `POST` | `/admin/intention-routing-rag/publish` | Fully validate and atomically publish a new revision. |
| `POST` | `/admin/intention-routing-rag/discard-draft` | Replace the draft with the current published graph after confirmation. |
| `POST` | `/admin/intention-routing-rag/test` | Run an enquiry against the draft without changing live traffic. |

The test action should display probabilities for every reached Routing block, selected options, thresholds, reached RAG blocks, selected chunks, prompt-part preview, timing, and errors. It must clearly state that it does not call or alter the published workflow unless the administrator explicitly chooses to run final-generation preview.

## 13. Security and operational requirements

- Apply CSRF protection to every state-changing admin operation.
- Enforce server-side payload-size, block-count, option-count, and string-length limits.
- Escape all names, descriptions, model identifiers, document names, and validation messages rendered into HTML.
- Do not expose API keys to browser JavaScript.
- Do not trust client-side graph validation; repeat it on the server.
- Use allowlisted outbound model endpoints from existing configuration.
- Treat RAG documents as untrusted text and defend routing and generation prompts against prompt injection.
- Rate-limit the test endpoint and prevent it from being used as an unauthenticated model proxy.
- Record who saved, validated, published, enabled, disabled, or discarded a workflow.
- Avoid logging full user enquiries and document chunks when production privacy settings prohibit content logging.

## 14. Observability

Create one execution/trace ID per enquiry and log structured fields for:

- published workflow revision;
- feature-switch state;
- block ID, block type, and execution order;
- selected routing model;
- routing latency, retry count, and API status;
- probability for each option;
- threshold and selected option IDs;
- reached RAG block/document attachments;
- per-document top K, minimum similarity, candidates, matches, and selected chunk IDs;
- number of prompt parts and total context characters;
- timeout, no-match, validation, and retrieval errors;
- total routing workflow latency before generation begins.

Do not render internal probabilities or trace data to chatbot users. The admin test view and server logs are the intended diagnostic surfaces.

## 15. Acceptance criteria

### Configuration and compatibility

- When Intention Routing RAG is disabled, the existing all-document RAG output is unchanged.
- When it is enabled, the all-document RAG method is never called for that enquiry.
- The feature cannot be enabled without a valid published workflow.
- Disabling the feature preserves workflow data.

### Editor

- A new workflow begins with exactly one non-deletable Input block.
- Administrators can add, configure, move, connect, duplicate, and delete Routing and RAG blocks.
- Routing options have required descriptions and individual output connections.
- Invalid cycles, unreachable blocks, missing documents, and invalid thresholds prevent publication.
- Draft edits do not affect live chatbot requests until published.

### Routing

- A reached Routing block makes one classification call containing all of its options.
- Every option receives a validated probability.
- Single routing selects only the highest option when it is strictly above threshold.
- Multiple routing selects every option strictly above threshold.
- No match produces no downstream retrieval and never triggers all-document fallback.
- Routing documents affect classification context but are not automatically added to the generation prompt.

### Retrieval and prompt generation

- A reached RAG block searches only its configured documents.
- Each document uses its own top K and minimum similarity.
- Two selected documents create two separately labeled RAG prompt parts.
- Multiple selected branches can create 4, 8, or more parts in deterministic order.
- The final generation call waits for the routing and selected retrieval work to finish.
- A converged block executes once per enquiry.
- Total RAG context respects the configured maximum size.

### Reliability and audit

- Runtime failures do not cause silent all-document fallback.
- Save and publish operations are protected from concurrent overwrite.
- Published revisions are immutable and auditable.
- The test view explains routing selections and retrieval results without changing live traffic.

## 16. Test plan

### Unit tests

- graph validation: valid graph, cycle, orphan, duplicate ID/name, missing edge, missing document;
- single mode: above threshold, equal to threshold, below threshold, tie;
- multiple mode: none, one, and many above threshold;
- strict routing-response JSON validation and one repair retry;
- per-document filtering, top K, similarity, and deterministic ordering;
- converging branches execute a downstream block once;
- prompt-part assembly and total-size trimming;
- feature-switch matrix and no-fallback rule.

### Integration tests

- draft save, validation, publish, revision conflict, and discard;
- role and CSRF enforcement;
- Routing block with and without routing documents;
- nested Routing blocks followed by multiple RAG blocks;
- document deletion after draft save prevents publish;
- document deletion after publication produces a traceable partial failure without searching other documents;
- final generation does not start before workflow completion;
- standard RAG remains unchanged while the feature is disabled.

### UI tests

- create blocks from the palette and drag them on the canvas;
- connect each intention option to a downstream block;
- edit model, mode, threshold, descriptions, documents, top K, and minimum similarity;
- show errors on the responsible block and field;
- warn about unsaved changes;
- save/reload positions and viewport;
- keyboard-only graph editing;
- test-run trace clearly identifies chosen and rejected options.

## 17. Implementation sequence

1. Add persistence, graph types, validation, immutable publication, and audit records.
2. Add document-filtered RAG database queries and reusable query embedding support.
3. Add the routing model client with strict response validation.
4. Add the graph executor, concurrency control, de-duplication, timeouts, and trace result.
5. Add prompt-part assembly and the standard-versus-routing RAG facade.
6. Integrate the facade before final prompt generation.
7. Add the feature switch to Configuration and enforce publish-before-enable.
8. Add authenticated admin routes, permissions, and the node editor.
9. Add draft test execution and diagnostics.
10. Complete unit, integration, UI, load, and regression testing before enabling in production.

## 18. Decisions requiring product confirmation

The specification uses the following safe defaults where the original request did not define behaviour:

- routing probability must be strictly greater than threshold;
- RAG similarity remains greater than or equal to its threshold for compatibility;
- probability values are independent and need not sum to `1`;
- a no-match continues generation without RAG and never searches all documents;
- routing documents use global retrieval settings and are classifier-only context;
- RAG blocks are terminal;
- an option has a single downstream connection;
- a malformed routing response receives one repair retry;
- successful sibling branches may still supply context when another branch fails;
- repeated arrival at one block executes that block once per enquiry;
- separately configured RAG blocks produce separate prompt parts even if they refer to the same document.

Any change to these decisions should be made explicitly before implementation because it affects graph validation, runtime semantics, tests, and administrator expectations.
