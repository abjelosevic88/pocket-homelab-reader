# Configuration

Environment variables; every one has a default. Personal settings (reading, feeds, notifications, connections) live in the UI per person — the `*_URL`/`*_TOKEN`/`OPENAI_*`/`NOTIFY_*` values below are copied once into the first admin's settings.

| Variable | Default | What |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL (links in pushes, the OIDC callback) |
| `DATA_DIR` | `./data` | Database, Core token, backups |
| `DB_PATH` | `$DATA_DIR/pocket-reader.db` | SQLite file |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON request, e.g. an export being imported (64 MB) |
| `TZ` | – | Time zone for the morning digest and the night clean-up |
| `AUTH_MODE` | `local` | `local` (accounts), `proxy` (trust `AUTH_PROXY_HEADER`/`AUTH_PROXY_EMAIL_HEADER`), `none` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | Seed the first admin while the database is empty |
| `SESSION_TTL`, `SECURE_COOKIES` | `720h`, `false` | Set `SECURE_COOKIES=true` behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `NOTIFY_URL`, `NOTIFY_FORMAT` | – | Apprise (`…/notify/key`), ntfy topic URL or JSON webhook; format `apprise`/`ntfy`/`json` |
| `NOTES_URL`, `NOTES_TOKEN` | – | Pocket Notes, direct connection, for highlights (or use Pocket Core) |
| `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` | – | Summaries, catch-up, tags and questions without Pocket AI |
| `FETCH_PRIVATE` | `false` | Let feeds and pages on private networks through (an RSS-Bridge or NAS on your LAN). Off, so accounts cannot make the server fetch internal addresses |
| `FETCH_WORKERS` | `4` | Feeds checked at the same time |
| `USER_AGENT` | `Mozilla/5.0 (compatible; PocketReader/1.0; …)` | How Reader identifies itself to sites |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | – | Pocket Core |
| `TRUST_PROXY`, `METRICS_ENABLED`, `LOG_LEVEL`, `LOG_FORMAT` | `true`, `true`, `info`, `text` | |
| `DEMO_DATA` | `false` | Seed a few weeks of made-up feeds, saved pages and highlights into the owner account; nothing is fetched (screenshots, trying it out) |

Commands: `pocket-reader backup [-o FILE]`, `pocket-reader reset-password [-email] [-password]`, `pocket-reader version`.
