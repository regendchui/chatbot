# WhatsApp chatbot (Whatsmeow + PostgreSQL + OpenRouter)

Go application that pairs with WhatsApp via [whatsmeow](https://github.com/tulir/whatsmeow), stores conversation and participant data in PostgreSQL, serves baseline and follow-up surveys over HTTPS, and uses OpenRouter for replies after a participant completes the baseline survey. An optional admin panel runs on a separate HTTPS port for operations, configuration, and reporting.

The project is intended to run with **Docker Compose** (app + database). Survey definitions and most tunable settings are loaded from the filesystem on **first** startup, then **persisted and edited** through the `project_setting` table and the admin **Configuration** page.

The author welcomes any research collaboration. For research collaboration, please contact: regend@connect.hku.hk

Link for JSON Configurator: https://regendchui.github.io/chatbot-JSON-configurator/

## Feature highlights

This application is designed as a production-oriented WhatsApp intervention platform with configurable workflows, document-grounded AI responses, and an operational admin console.

- **Dynamic survey engine (baseline + follow-up)**
  - Surveys are JSON-driven (`survey-config.json`) and can be managed centrally through `project_setting.json_variables`.
  - Supports multiple question types (text, numeric, multiple-choice, multiple-select, date picker, slider) with optional conditional visibility logic.
  - Baseline/follow-up responses are stored in PostgreSQL, with automatic table/column evolution for new questions and follow-up cycles.

- **RAG (Retrieval-Augmented Generation) for grounded AI replies**
  - Upload and ingest external knowledge documents (including PDF, DOCX, CSV support in the app flow) to embedding storage.
  - At reply time, the bot retrieves relevant chunks and injects them into the model prompt, reducing guesswork and improving factual consistency.
  - Includes chunk controls (size/overlap/top-k/similarity/context limits) and slice-protect markers for preserving critical spans during chunking.

- **Admin panel for operations and governance**
  - Browser-based admin interface for enrollment, verification, role/permission management, blacklist, conversation history, survey responses, auto-message schedules, and project settings.
  - Session timeout and forced global admin logout controls for tighter access control.
  - Built-in audit visibility through login history and configuration update history pages.
  - Configuration values (AI, RAG, behavior, throttling, messaging controls) are persisted in DB and editable without code redeploy.

- **All-in-one VPS deployment with Docker**
  - Docker Compose bundles app + PostgreSQL in a reproducible deployment model suitable for a single VPS.
  - README includes practical Hostinger-style VPS setup: DNS, Nginx reverse proxy, HTTPS (Let’s Encrypt), and operations commands.
  - Supports “single stack” hosting where survey endpoints, admin panel, bot runtime, and DB run together under one server footprint.

- **Security controls adopted**
  - **Data protection at rest:** encrypted storage paths for sensitive fields (phone numbers and admin/role credentials).
  - **Access control:** admin + role accounts, path-level permission checks, session cookies, secure-cookie support behind HTTPS, login protection/lockout.
  - **Input/data safety:** SQL identifier validation for dynamic survey DDL paths and HTML escaping in admin/survey rendering.
  - **Operational safeguards:** audit logs for auth/config events, blacklist enforcement, verification gates, and replay/duplicate inbound protections.

## What’s included

- **Inbound/outbound messaging** with optional boot message, verification gate, intervention end (one-time message, then no further AI replies for that participant), and phone blacklist.
- **Unified conversation history** in a `conversation` table (with a `nature` field for message type), used for AI context and admin views.
- **Surveys** at `/survey/{slug}` driven by JSON (mirrored in `project_setting.json_variables`); cron-driven auto AI and follow-up messages with shared outbound throttling.
- **Admin panel** at `/admin/home/` (login at `/admin/login`): tables, CSV export, enrollment, verification, roles, blacklist, WhatsApp status/QR, raw project-setting debug, and DB-backed env-style settings.
- **Emergency CLI** to reset the primary admin password without the old password.

## Repository layout (main packages)

```text
.
├─ main/              # Entrypoint, WhatsApp handlers, collective response, message slice, AI initiation
├─ db/                # PostgreSQL: init/bootstrap, conversation/meta/surveys/project_setting, logs, RAG, roles, and blacklist management
├─ survey/            # Survey config, HTTPS forms, invitations, schema migration helpers
├─ admin_panel/       # Admin HTTPS server, auth/sessions, timeout/countdown, config, logs, RAG + table pages
├─ AI/                # OpenRouter chat + embeddings integration, memory assembly, RAG retrieval/chunking
├─ cron_task/         # Scheduled auto AI/follow-up/manual sends and retry helpers
├─ messaging/         # Outbound WhatsApp send helpers + queued cron throttle worker
├─ common/            # Shared models/utilities/encryption helpers
├─ survey-config.json # Default survey JSON (seeded into project_setting.json_variables)
├─ .env               # First-boot secrets/defaults (copied into project_setting on first run)
├─ Dockerfile
└─ docker-compose.yml
```

## File and module reference

This section describes the **purpose of each first-party file** in the application (excluding vendored or tool caches such as `.gomodcache/`). Use it as a map when changing behavior or onboarding.

### Root (project)

| File | Role |
|------|------|
| `README.md` | Project documentation (this file). |
| `.env` | Local / first-boot environment variables: database, keys, listen addresses, defaults copied into `project_setting` on initial deploy. Not a substitute for secrets management in production. |
| `go.mod` / `go.sum` | Go module definition and dependency checksums. |
| `Dockerfile` | Container image build for the bot (compile and runtime layout). |
| `docker-compose.yml` | Orchestrates the app container and PostgreSQL; defines service names, ports, volumes. |
| `.dockerignore` | Files excluded from the Docker build context (reduces image noise; helps avoid macOS `._*` metadata issues). |
| `survey-config.json` | Default survey definition (baseline, follow-ups, project metadata). On first startup it is loaded and mirrored into `project_setting.json_variables`; later edits typically go through the admin Configuration page. |
| `survey_configurator.html` | Standalone HTML tool to **design** survey JSON visually in the browser. It is **not** served by the Go app; open the file locally if you use it. |

### `main/` — process entry and WhatsApp wiring

| File | Role |
|------|------|
| `main.go` | Application entry: database and `project_setting` bootstrap, survey init, cron workers, WhatsApp client lifecycle, HTTPS servers for surveys and admin, CLI hooks (e.g. emergency admin password reset). |
| `handler.go` | Incoming WhatsApp message handler: blacklist/verification/intervention checks, conversation persistence, baseline invitation flow, AI reply orchestration. |
| `collective_response.go` | Buffers rapid inbound messages per participant and merges them into one AI prompt after a configurable delay. |
| `message_slice.go` | Outbound AI message slicing pipeline (paragraph-based chunks), configurable delay, and chunk send retries. |
| `ai_initiation.go` | Sends AI-generated initiation messages after baseline completion and/or admin verification events. |
| `sender.go` | Outbound message helpers used from the main package (delegates to shared messaging where appropriate). |
| `message_struct.go` | Shared message struct(s) used across handlers, DB, and AI layers. |
| `utils.go` | General helpers (e.g. JID/phone normalization utilities used in the WhatsApp path). |
| `encryption.go` | Phone encryption helpers in the `main` package (parallel to `common/encryption.go`). Packages under `db/` and `survey/` use **`common/encryption.go`** because they cannot import `main`. |
| `intervention.go` | Intervention window logic: whether the participant is past `intervention_period` after baseline completion (used to stop AI replies while cron may continue per design). |
| `whatsapp_admin_state.go` | In-memory WhatsApp connection state for the admin panel (connected flag, last QR string, errors); thread-safe updates from the WhatsApp client. |

### `db/` — PostgreSQL access

| File | Role |
|------|------|
| `init_db.go` | Database pool bootstrap and centralized startup table initialization. |
| `conversation_db.go` | `conversation` table DDL/migration from legacy names and conversation insert/query/delete helpers used by messaging/admin/AI context. |
| `meta_db.go` | `meta` table (participant profile): first contact, baseline/follow-up flags, participant name, message interval, verification, end-message flag, enrollment helpers, deletes. |
| `project_setting_db.go` | Singleton `project_setting` row: load/save `env_variables` and `json_variables`, admin password encryption, merge defaults from `.env` + `survey-config.json`, admin credential verification, JSON fetch-by-URL for config import. |
| `auto_message_db.go` | Scheduled auto AI and follow-up outbound tasks: insert due rows, mark sent, delete pending follow-up schedules; shared by cron packages. |
| `blacklist_db.go` | `blacklist` table: add/remove/list encrypted phone rows; lookup for handler and cron skip. |
| `role_db.go` | `role` table: secondary admin users, encrypted passwords, permitted page paths, permission checks. |
| `rag_db.go` | RAG embedding storage schema and related persistence helpers. |
| `login_history_db.go` | Login attempt audit table and recent login history queries for admin logs page. |
| `config_update_history_db.go` | Configuration change audit table and list queries for admin logs page. |

### `survey/` — surveys, HTTPS forms, schema

| File | Role |
|------|------|
| `survey_config.go` | Go structs for `survey-config.json`, loading from `project_setting`, slug resolution, baseline system-field ordering, project/phases/schedule types. |
| `survey_web.go` | HTTPS server for `/survey/{slug}`: GET form HTML, POST validation and INSERT into response tables, phone field rules (`SURVEY_PHONE_DIGITS`), client-side validation script for visibility and numeric fields. |
| `survey_table.go` | Creates baseline and follow-up **response** tables from JSON; column validation; migration-friendly `ADD COLUMN` for new questions. |
| `migration.go` | Shared `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` helper for survey table evolution. |
| `add_FU_meta.go` | Ensures per-follow-up columns on `meta` (`fu_<id>_timestamp`, `fu_<id>_completed`) and related naming helpers. |
| `baseline_invitation.go` | Builds/sends baseline survey invitation text and link (uses `SURVEY_PUBLIC_BASE_URL` + baseline slug). |
| `followup_invitation.go` | Sends a follow-up **WhatsApp** invitation (`invitation_text` + public survey URL) and records the outbound message with the appropriate conversation `nature` (for cron-driven flows). |
| `hooks.go` | `SchedulingHooks` indirection so `survey` can call cron scheduling without an import cycle (`main` registers db/cron functions at startup). |

### `admin_panel/` — admin HTTPS UI and auth

| File | Role |
|------|------|
| `admin_panel.go` | Registers all `/admin/...` routes and starts the admin HTTPS server. |
| `admin_panel_auth.go` | Login, logout, signed session cookie, root admin vs role user auth, path-based permission checks, forwarded-proto secure cookie behavior. |
| `admin_panel_timeout.go` | Session timeout policy and remaining-time countdown UI helpers for admin pages. |
| `admin_panel_login_protection.go` | In-memory rate limiting / lockout for failed admin logins (by IP and username). |
| `admin_panel_page_general.go` | Shared HTML chrome: styles, navigation (permission-filtered), timestamp formatting with `ADMIN_PANEL_UTC_OFFSET_HOURS`, small form helpers. |
| `admin_panel_page_config.go` | **Configuration** page: edit DB-backed env-like settings, verification message patch into JSON, cron delay, intervention message, admin credential change, JSON replace (text/URL/file). |
| `admin_panel_page_log.go` | Admin audit log page (login history + config update history). |
| `admin_panel_page_export_csv.go` | CSV export handlers for table pages and filtered survey exports. |
| `admin_panel_page_survey_responses.go` | Survey response listing (baseline + follow-ups), survey links, phone filter, per-row and orphan deletes with safeguards. |
| `admin_panel_page_enrollment.go` | Manual participant enrollment, duplicate checks, delete participant and related survey rows, phone filter. |
| `admin_panel_page_blacklist.go` | Blacklist add/remove/list, phone filter. |
| `admin_panel_page_role.go` | Role users: create, delete, permission edits, password reset. |
| `admin_panel_page_verification.go` | Lists unverified participants; approve verification flag. |
| `admin_panel_page_whatsapp.go` | Connection status, QR display, refresh, WhatsApp logout. |
| `admin_panel_page_clientinfo.go` | Per-participant summary (including name) and WhatsApp-style chat history preview; optional manual send. |
| `admin_panel_page_table_conversation.go` | **Conversation history** table (formerly “AI memory”); CSV export. |
| `admin_panel_page_table_meta.go` | `meta` table browser; CSV export. |
| `admin_panel_page_table_auto_messages.go` | Auto-message schedule table; CSV export. |
| `admin_panel_page_table_db_tables.go` | Generic database table listing/exploration; CSV export. |
| `admin_panel_page_table_project_setting.go` | Raw `project_setting` row view for debugging (sensitive; warning in UI). |
| `admin_panel_page_rag.go` | RAG document upload/reindex/delete controls and related operations. |
| `admin_panel_page_table_embedding.go` | RAG embedding table view for inspection/debugging. |

### `AI/` — OpenRouter

| File | Role |
|------|------|
| `AI.go` | Calls OpenRouter chat completions API; builds prompt from latest input + memory + survey + phase + RAG context. |
| `AI_memory.go` | Loads recent `conversation` rows and formatted survey/phase context for prompts (participant name, completion state, etc.). |
| `rag.go` | RAG ingestion and retrieval: parse docs, chunk text (with slice-protect markers), generate embeddings, similarity search, and context assembly. |

### `cron_task/` — scheduled outbound messages

| File | Role |
|------|------|
| `auto_AI_message_cron.go` | Schedules and sends periodic **auto AI** messages; respects blacklist; uses shared outbound worker/throttle; marks tasks sent in `auto_message_db`. |
| `auto_followup_message_cron.go` | Schedules and sends **follow-up prompt** messages; same blacklist and shared throttle behavior. |
| `auto_manual_message_cron.go` | Schedules and sends manual/admin-defined outbound cron messages. |
| `retry_past_auto_message.go` | Retry helper for failed/past-due auto-message tasks from admin actions. |

### `messaging/` — outbound WhatsApp send

| File | Role |
|------|------|
| `sender.go` | Low-level send helpers to the WhatsApp client (used from `main` and cron paths) so send logic stays consistent. |

### `common/` — shared utilities

| File | Role |
|------|------|
| `encryption.go` | AES-GCM (or related) helpers for phone encryption at rest and other crypto used by `db` and handlers. |
| `models_utils.go` | Shared types/helpers (e.g. `Message` struct, digit normalization, SQL identifier validation, follow-up column name builders) reused across packages. |

## Configuration overview

1. **`.env` in the project root** supplies database credentials, encryption keys, API keys, listen addresses, and defaults for values that are copied into the database on first run.
2. On startup the app ensures a singleton row exists in **`project_setting`**: `env_variables` (string map, including encrypted admin password) and `json_variables` (full survey-config object).
3. **Runtime reads** for AI prompts, limits, cron delays, survey JSON, etc. come from **`project_setting`** once initialized. Changing `.env` alone does not override values already stored in the database (except where the code still reads `os.Getenv` directly for bootstrap-only items).

Edit sensitive values in production via **Admin → Configuration** (and JSON upload/text/URL there) rather than committing real secrets to git.

## Environment variables (first boot and runtime)

Set these in `.env` before the first `docker compose up`. Many are also editable later under **Configuration** in the admin panel (stored in `project_setting.env_variables`).

| Variable | Purpose |
|----------|---------|
| `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` | PostgreSQL connection |
| `PHONE_ENCRYPTION_KEY` | 32-byte secret for phone encryption at rest |
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `OPENROUTER_MODEL` | Optional; if unset, code uses a built-in Gemini model on OpenRouter |
| `OPENROUTER_URL` | Optional; override OpenRouter endpoint (default chat completions URL) |
| `OPENROUTER_SITE_URL` | Optional; sent as OpenRouter `HTTP-Referer` header |
| `OPENROUTER_APP_NAME` | Optional; sent as OpenRouter `X-Title` header |
| `SURVEY_CONFIG_PATH` | Path to initial `survey-config.json` (default `survey-config.json`) |
| `SURVEY_HTTP_ADDR` | Survey listen address (default `:8080`) |
| `SURVEY_PUBLIC_BASE_URL` | Public base URL for survey links in WhatsApp (no trailing slash) |
| `SURVEY_PHONE_DIGITS` | `0` = allow 8–15 digit phone on survey forms; otherwise exact digit count |
| `ADMIN_PANEL_HTTP_ADDR` | Admin panel listen address (default `:8081`) |
| `ADMIN_PANEL_USERNAME`, `ADMIN_PANEL_PASSWORD` | Initial admin login (password is stored encrypted in DB after seed) |
| `ADMIN_PW_ENCRYPTION_KEY` | Key used to encrypt admin and role passwords at rest |
| `ADMIN_PANEL_COOKIE_SECURE` | `true` when admin is served over HTTPS (see reverse proxy note) |
| `ADMIN_PANEL_UTC_OFFSET_HOURS` | Display-only offset for admin timestamps and CSV exports |
| `AI_SYSTEM_PROMPT`, `AI_MEMORY_MESSAGE_LIMIT` | AI behavior and memory window size |
| `REQUIRE_VERIFICATION` | Gate unverified participants (also see `verification_message` in survey JSON) |
| `INBOUND_REPLAY_GRACE_WINDOW_SECONDS` | Limits replay of old inbound events on reconnect |
| `CRON_SEND_MIN_DELAY_SECONDS`, `CRON_SEND_MAX_DELAY_SECONDS` | Random delay between cron outbound sends |
| `INTERVENTION_END_MESSAGE` | One-time text when intervention period ends (overridable in survey project settings where applicable) |
| `TARGET_JID`, `BOOT_MESSAGE`, `ENABLE_BOOT_MESSAGE` | Optional startup send |
| `SEND_AI_ERROR_FALLBACK` | Whether to send a fallback line on AI errors |

### Admin panel behind HTTPS reverse proxy

Terminate TLS at your reverse proxy and forward requests to the admin listen port. Set:

- `ADMIN_PANEL_COOKIE_SECURE=true` so session cookies are marked secure.

The proxy should forward `X-Forwarded-Proto: https` when the client used HTTPS so the app can treat the request as secure. Admin home URL is **`/admin/home/`** (login remains **`/admin/login`**).

**Important:**


If you use **`SURVEY_PHONE_DIGITS`** (e.g. `11`), update the phone-related strings so participants know the exact length you require; the app enforces length in the input, but the label should match your policy.

## 2.0) Prepare `.env` and survey JSON before VPS setup

Do this before you deploy to the VPS so first boot seeds the correct values.

### A) Edit `.env` (required)

1. Copy the example file to `.env` (if not already present).
2. Set required secrets and connection values:
   - `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`
   - `PHONE_ENCRYPTION_KEY` (32 bytes)
   - `ADMIN_PW_ENCRYPTION_KEY`
   - `OPENROUTER_API_KEY`
3. Set public URLs and security flags for your domain:
   - `SURVEY_PUBLIC_BASE_URL=https://yourdomain.com`
   - `ADMIN_PANEL_COOKIE_SECURE=true` (when using HTTPS)

4. Please update the docker-compse.yml file if you change the DB_PASSWORD. Please make sure the setting of the yml file matches the setting in the .env file

### B) Edit survey JSON (`survey-config.json`)

The base JSON should come from the repository README workflow/template (the tracked `survey-config.json`, or one generated from `survey_configurator.html` and then saved as `survey-config.json`).

Link for JSON Configurator: https://regendchui.github.io/chatbot-JSON-configurator/

Keep the file path aligned with:
- `SURVEY_CONFIG_PATH=survey-config.json`

On first startup, this JSON is loaded and mirrored into `project_setting.json_variables`. After that, ongoing edits are usually done in **Admin -> Configuration**.

## 2.1) Linux VPS Deployment (Hostinger example)

This section shows a practical production setup on a Hostinger Ubuntu VPS.

### A) Create and access VPS

1. Create a VPS in Hostinger (for example Ubuntu 22.04 LTS).
2. In Hostinger panel, copy the server public IP.
3. SSH to the VPS:

```bash
ssh root@YOUR_VPS_PUBLIC_IP
```

### B) Install Docker + Compose plugin

Run on VPS (Ubuntu):

```bash
apt-get update
apt-get install -y ca-certificates curl gnupg lsb-release
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo \"$VERSION_CODENAME\") stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
docker --version
docker compose version
```

Optional firewall baseline:

```bash
ufw allow OpenSSH
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable
```

### C) Upload project files with FileZilla (SFTP)

1. Open FileZilla -> Site Manager -> New Site.
2. Protocol: `SFTP - SSH File Transfer Protocol`
3. Host: `YOUR_VPS_PUBLIC_IP`
4. Port: `22`
5. Logon Type: password or key file.
6. Username: `root` (or your sudo user).
7. Upload project folder to `/opt/chatbot`.

On VPS:

```bash
cd /opt/chatbot
ls -la
```

### D) Bind your domain to VPS (Hostinger DNS)

In Hostinger domain DNS zone:

- Add `A` record: `@` -> `YOUR_VPS_PUBLIC_IP`
- Add `A` record: `www` -> `YOUR_VPS_PUBLIC_IP` (optional)

Wait for DNS propagation (usually a few minutes, up to 24h).

### E) Configure app env for production

Edit `/opt/chatbot/.env`:

- Use strong secrets (do not reuse local/testing values).
- Set production URL and secure admin cookie:

```env
SURVEY_PUBLIC_BASE_URL=https://yourdomain.com
ADMIN_PANEL_COOKIE_SECURE=true
```

Then start containers:

```bash
cd /opt/chatbot
docker compose up --build
docker compose logs -f wa_bot
```

### F) Put app behind Nginx reverse proxy + HTTPS

Install Nginx + Certbot:

```bash
apt-get update
apt-get install -y nginx certbot python3-certbot-nginx
```

Create `/etc/nginx/sites-available/chatbot` with Nano:

1) Open the file in Nano:

```bash
sudo nano /etc/nginx/sites-available/chatbot
```

2) Paste this config into Nano:

```nginx
server {
    listen 80;
    server_name yourdomain.com www.yourdomain.com;

    location /survey/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /admin/ {
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

3) Save and exit Nano:

- Press `Ctrl + O` (write out/save)
- Press `Enter` (confirm filename)
- Press `Ctrl + X` (exit)

4) Enable and apply Nginx config:

```bash
sudo ln -s /etc/nginx/sites-available/chatbot /etc/nginx/sites-enabled/chatbot
sudo nginx -t
sudo systemctl reload nginx
```

If the symlink already exists, run this instead:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

Issue TLS certificate:

```bash
certbot --nginx -d yourdomain.com -d www.yourdomain.com
```

Verify certificate status:

```bash
certbot certificates

```
Enter email address (used for urgent renewal and security notices)
 (Enter 'c' to cancel): your-email@example.com


### H) SSL renewal (Let's Encrypt)

Most Ubuntu installations of Certbot create automatic renewal timers, but you should verify it.

Check timer/service:

```bash
systemctl status certbot.timer
systemctl list-timers | grep certbot
```

Run a dry-run renewal test:

```bash
certbot renew --dry-run
```

Recommended deploy hook (reload Nginx after successful renewal):

```bash
certbot renew --deploy-hook "systemctl reload nginx"
```

Optional cron fallback (if timer is unavailable):

```bash
crontab -e
```

Add:

```cron
0 3 * * * certbot renew --quiet --deploy-hook "systemctl reload nginx"
```

### I) Why `X-Forwarded-Proto` is important

This app uses forwarded protocol detection to decide whether to set secure admin cookies.
If Nginx sets:

```nginx
proxy_set_header X-Forwarded-Proto $scheme;
```

then HTTPS requests are recognized as `https` in app logic.
Combined with `ADMIN_PANEL_COOKIE_SECURE=true`, admin session cookies are protected for production HTTPS usage.

## Build and run (local or VPS)

After `.env` is ready and (on production) DNS and proxy are configured:

```bash
cd /opt/chatbot   # or your project directory
docker compose up --build
```

Follow container logs for the WhatsApp QR code on first pairing. Session data is stored in PostgreSQL for subsequent runs.

### Troubleshooting: `failed to xattr ... /._... operation not permitted`

On macOS (especially external drives), AppleDouble files such as `._README.md` can break Docker BuildKit.

```bash
dot_clean -m .
docker compose up -d --build
```

If it still fails:

```bash
xattr -rc .
dot_clean -m .
docker compose up -d --build
```

The repo’s `.dockerignore` excludes `._*` where possible; cleaning the tree avoids stubborn cases.

## First-time WhatsApp pairing (QR)

1. Open WhatsApp on the phone → **Linked devices** → **Link a device**.
2. Scan the QR from the container logs, or encode the logged string as a QR (e.g. an online text QR generator).

After pairing, keys live in the database; reuse the same volume unless you intend a full reset.

## Reset PostgreSQL data (fresh database)

If Postgres uses a Docker volume (default in this compose file):

```bash
docker compose down -v
docker compose up -d --build
```

This wipes DB data; you will need to scan the QR again and re-seed configuration as on first boot.

## Emergency admin password reset

Use when you cannot log in and need to set a new primary admin password from the shell (bypasses old password check).

```text
force-reset-admin-password <new_password> [new_username]
```

**Docker Compose:**

```bash
docker compose run --rm wa_bot force-reset-admin-password "NewStrongPassword123!"
docker compose run --rm wa_bot force-reset-admin-password "NewStrongPassword123!" "admin"
```

**Binary:**

```bash
./your-binary-name force-reset-admin-password "NewStrongPassword123!"
```

The new password is stored encrypted with `ADMIN_PW_ENCRYPTION_KEY`. The command exits after updating credentials; it does not start the full bot.

## Compile a 3-file runtime bundle

If you want a minimal deploy folder with only:

- application binary
- `.env`
- `survey-config.json`

use the commands below.

### Build bundle into `output/`

Run from repository root:

```bash
cd "/Volumes/Crucial 2TB/chatbot"
mkdir -p output
go build -o output/whatsapp-bot ./main
cp .env output/.env
cp survey-config.json output/survey-config.json
```

After this, `output/` should contain exactly:

- `whatsapp-bot`
- `.env`
- `survey-config.json`

### Run from the bundle folder

```bash
cd output
docker compose up --build
```

Notes:

- The app still needs a reachable PostgreSQL database (from values in `.env`).
- `cron_task_time` in `survey-config.json` is interpreted in UTC.
- If you edit `.env` or `survey-config.json`, restart the binary to pick up changes.

## Runtime behavior (summary)

- **Surveys**: `{SURVEY_PUBLIC_BASE_URL}/survey/{link_slug}` — forms include `respondent_phone` (digits); length rules follow `SURVEY_PHONE_DIGITS` when set.
- **Baseline**: until baseline is complete, the bot focuses on invitation / survey completion flow; AI replies follow project rules after completion.
- **Conversation**: messages are stored in `conversation` with a `nature` (e.g. client vs bot/cron); AI context uses recent rows up to the configured limit.
- **Cron**: `cron_task_time` and schedule config in survey JSON are interpreted in **UTC**; auto AI and follow-up sends share throttling between sends.
- **Blacklist**: blocked numbers skip inbound handling and cron scheduling for that participant.
- **Roles**: optional admin users with per-path permissions (see **Role** in the admin panel).

## Quick URL reference

Replace host and slugs with yours (`survey-config.json` defines `link_slug` per survey).

| Area | Example (local) | Example (production) |
|------|-----------------|----------------------|
| Survey | `http://localhost/survey/baseline` | `https://yourdomain.com/survey/baseline` |
| Admin login | `http://localhost/admin/login` | `https://yourdomain.com/admin/login` |
| Admin home | `http://localhost/admin/home/` | `https://yourdomain.com/admin/home/` |

Further admin routes live under `/admin/...` (tables, configuration, enrollment, etc.); use the in-app navigation after login.

## Disclaimer

This project is primarily developed for research and educational purposes. To accelerate development and prototyping, generative AI tools were used to assist with coding, debugging, documentation, and software design. Users who have concerns regarding AI-assisted code generation, human authorship, or originality should carefully evaluate whether this project is appropriate for their intended use.

While reasonable efforts have been made to implement basic security measures and operational safeguards, this software is provided “as is” without any warranty of any kind, express or implied. The author makes no guarantees regarding the security, reliability, stability, accuracy, legal compliance, or fitness of this project for any specific purpose.

Users are solely responsible for:

- Verifying the security and safety of their deployment environment
- Ensuring compliance with local laws, regulations, institutional policies, and ethical requirements
- Protecting participant privacy, confidential data, and API credentials
- Conducting independent testing, validation, and security review before production use

The author shall not be held liable for any direct, indirect, incidental, consequential, legal, financial, operational, or data-related damages arising from the use, misuse, modification, deployment, or distribution of this project.

This project may integrate with third-party services and APIs, including WhatsApp, OpenRouter, PostgreSQL, Docker, and other external platforms. The availability, behavior, privacy practices, and terms of these third-party services are outside the author’s control.

By using this project, you acknowledge and accept all associated risks and responsibilities.
