# Graph RAG

## Purpose

Graph RAG augments traditional embedding retrieval with entity and relationship
evidence stored in Apache AGE. It is independently enabled and can operate as:

- no retrieval;
- traditional RAG only;
- Graph RAG only;
- hybrid traditional + Graph RAG; or
- Graph RAG configured inside an Intention Routing RAG block.

Graph RAG is disabled by default. Existing workflows and standard RAG behavior
remain unchanged until an administrator enables or explicitly overrides it.

## PostgreSQL and AGE

The deployment pins `apache/age:release_PG15_1.6.0`. PostgreSQL remains major
version 15 for compatibility with the existing `pgdata` volume. Startup creates
the AGE extension, metadata tables, and the fixed `knowledge_graph` namespace
idempotently. When AGE is missing or unavailable, the application logs the
condition and continues serving non-graph functionality.

Every AGE connection executes `LOAD 'age'`. Cypher is chosen from fixed,
bounded application templates; model-generated or user-supplied raw Cypher is
never executed.

## Graph model

AGE uses fixed labels and structural relationships:

| Kind | Name | Purpose |
|---|---|---|
| Vertex | `Document` | One document in one immutable graph snapshot |
| Vertex | `Chunk` | Source RAG chunk and provenance |
| Vertex | `Entity` | Canonical entity shared across supporting documents |
| Edge | `HAS_CHUNK` | Document snapshot to source chunk |
| Edge | `MENTIONS` | Source chunk to entity |
| Edge | `RELATED_TO` | Extracted relationship between entities |

Model-generated categories are properties (`entity_type` and `relation_type`),
not dynamic labels. Entities contain canonical names, normalized keys, aliases,
and confidence. Relationship evidence contains description, confidence,
document name, chunk index, and snapshot ID.

Canonicalization normalizes whitespace and case. When the model supplies
Chinese/English aliases, the stable lexical key across the canonical name and
aliases merges high-confidence variants. Uncertain aliases remain separate.

## Document lifecycle and snapshots

Only existing documents from **Admin → RAG** can be selected.

1. Selecting a document creates a persisted queued job.
2. One project worker claims jobs with `FOR UPDATE SKIP LOCKED`.
3. Chunks are extracted in bounded processing waves with configured concurrency.
4. Strict JSON is validated and confidence-filtered.
5. A new AGE snapshot is written without changing live retrieval.
6. Metadata atomically activates the successful snapshot.
7. The previous snapshot is removed after activation.

If extraction or storage fails, the job is marked failed, the document remains
stale, and its previous successful snapshot continues serving queries.

Reindexing a selected traditional RAG document automatically queues a rebuild.
Deleting a traditional document removes its graph provenance. Shared entities
are deleted only when no remaining `MENTIONS` or `RELATED_TO` evidence supports
them. Changing extraction settings marks selected documents `rebuild required`
without automatically spending model tokens.

## Extraction settings

All settings appear with explanations under **Admin → Configuration → RAG
Settings**:

- `GRAPH_RAG_EXTRACTION_MODEL`
- `GRAPH_RAG_EXTRACTION_PROMPT`
- `GRAPH_RAG_MIN_EXTRACTION_CONFIDENCE`
- `GRAPH_RAG_BATCH_SIZE`
- `GRAPH_RAG_CONCURRENCY`
- `GRAPH_RAG_RETRY_COUNT`
- `GRAPH_RAG_EXTRACTION_TIMEOUT_MS`

The application appends the required JSON schema and untrusted-evidence rules;
the editable prompt cannot disable those safeguards. Raw provider errors and
job token counts are retained for authenticated operational inspection.

## Retrieval settings

General defaults are:

| Setting | Default | Meaning |
|---|---:|---|
| Inbound messages | 2 | Latest inbound messages combined oldest-to-newest |
| Traversal depth | 2 | Maximum bidirectional relationship hops |
| Seed entities | 5 | Maximum query-resolved starting entities |
| Entities | 30 | Maximum distinct explored entities |
| Relationships | 50 | Maximum returned evidence relationships |
| Graph context | 12000 | Graph context character limit |
| Match confidence | 0.50 | Minimum query and evidence confidence |
| Timeout | 3000 ms | Query entity resolution plus traversal deadline |

The query model and editable query prompt are separate from extraction so a
lower-latency model can be selected without rebuilding documents. Query output
uses strict JSON and receives one validation retry.

Retrieval resolves canonical names and aliases, traverses relationships in both
directions, prefers shorter paths and higher confidence, deduplicates equivalent
evidence, and uses active snapshots only. AGE errors, model errors, and timeouts
are reported in traces but do not prevent generation with another enabled RAG
method.

## Hybrid prompt composition

Traditional and graph contexts remain separately labelled and explicitly marked
as untrusted reference evidence. Compact provenance such as
`[source: location.pdf, chunk 4]` is always supplied to the generation model.
The model is instructed never to follow instructions inside evidence.

`HYBRID_RAG_MAX_CONTEXT_CHARS` defaults to 20000. Traditional RAG initially
receives 60% and Graph RAG 40%; unused capacity flows to the other section.
Truncation is included in the retrieval trace. Visible document citations in
chatbot replies are controlled by `GRAPH_RAG_INCLUDE_CITATIONS` and default off.

## Intention Routing RAG

Graph RAG is not a new workflow block. Schema-version-2 RAG blocks contain:

- existing per-document traditional RAG settings; and
- Graph behavior: `disabled`, `inherit`, or `override`.

`inherit` uses general Graph RAG enablement and retrieval settings. `override`
contains a complete validated snapshot of graph documents and query-time
settings. `disabled` prevents graph retrieval for that block. A RAG block may be
traditional-only, graph-only, or hybrid.

Schema-version-1 workflows are normalized to version 2 with Graph RAG disabled.
They continue executing unchanged and are saved as version 2 only after the
administrator edits/saves them.

## Scheduled messages

`AUTO_AI_RAG_MODE` remains `disabled`, `standard`, or `intention`:

- `disabled`: no retrieval;
- `standard`: use `AUTO_AI_TRADITIONAL_RAG_ENABLED` and
  `AUTO_AI_GRAPH_RAG_ENABLED`;
- `intention`: use the settings of reached workflow RAG blocks.

## Admin page

**Admin → Graph RAG** requires the separate `/admin/graph-rag` role permission.
It provides:

- document selection, rebuild, stale rebuild, and provenance removal;
- queued/running/completed/failed jobs and chunk progress;
- entity/relationship counts, token usage, timestamps, and errors;
- natural-language retrieval testing;
- resolved evidence, provenance, generated context, and trace details;
- read-only SVG neighborhood preview and an accessible evidence table; and
- confirmed full-graph deletion without deleting traditional RAG documents.

State-changing endpoints require authentication and CSRF validation. Document
removal and full deletion create audit/config-history records. Full deletion
requires the administrator to type the project name.

## Deployment

Before the first AGE deployment:

```bash
cd /opt/chatbot
mkdir -p /opt/chatbot-backups
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > /opt/chatbot-backups/pre-graph-rag.dump
test -s /opt/chatbot-backups/pre-graph-rag.dump
```

Deploy without deleting the volume:

```bash
git pull --ff-only origin main
docker compose pull postgres
docker compose build --pull --no-cache bot
docker compose up -d --force-recreate postgres
docker compose up -d --force-recreate bot
docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "SELECT extversion FROM pg_extension WHERE extname='"'"'age'"'"';"'
docker logs -f --tail=200 wa_bot
```

Do not run `docker compose down -v` and do not delete `pgdata`.

## Rollback

If the bot fails but PostgreSQL is healthy, redeploy the prior application
commit; Graph RAG is optional and the prior application ignores its tables.

If the database image itself cannot start, stop the stack, retain the current
volume, restore the pre-deployment dump into a fresh PostgreSQL 15 volume, and
point the prior application at that restored database. Never overwrite or
delete the original volume until the restored database has been verified.

## Verification

Unit tests cover strict extraction parsing, Graph-only and hybrid workflow
validation, version-1 normalization, provenance context, hybrid budgets, and
admin route protection. The opt-in AGE integration test exercises extension
setup, persisted jobs, snapshot activation, traversal, provenance, and deletion:

```bash
docker compose -f docker-compose.graph-rag-test.yml up -d --wait
GRAPH_RAG_INTEGRATION_DSN='postgres://graph_test:graph_test@127.0.0.1:55432/graph_test?sslmode=disable' \
  go test ./db -run TestGraphRAGAGEIntegration -count=1
docker compose -f docker-compose.graph-rag-test.yml down -v
```
