# Aux

Aux is a web app that puts an AI in charge of your Spotify account. You chat
with the assistant in a browser; the assistant uses the complete
[spotify-go-wrapper](https://github.com/EmpireForge-ef/spotify-go-wrapper)
API surface — exposed as tools via the
[Anthropic Go SDK](https://github.com/anthropics/anthropic-sdk-go) — to
search the catalog, build and edit playlists, manage your library, follow
artists, and control playback.

## Features

- **AI control of Spotify** — every method of the wrapper (playlists, library,
  player, search, artists, albums, shows, episodes, audiobooks, …) is a tool
  the model can call; responses stream live, including tool activity.
- **Persistent, multi-chat conversations** — a sidebar to start new chats and
  return to old ones; full context (including tool calls) survives restarts.
- **Admin login** — protect the whole app with a password and/or OpenID
  Connect single sign-on (Keycloak-compatible).
- **Runtime settings UI** — set the Spotify credentials, Anthropic API key,
  model, and token cap from the browser; secrets stay masked, changes apply
  without a restart.
- **Model selection** — fetch the current list of models from the Anthropic
  API and pick one (plus a max-tokens cap) to trade quality for cost.
- **Deploy-ready** — non-root container, GitLab CI with versioned publishing,
  and a hardened Helm chart.

## How it works

- **Backend** (Go): a cobra/viper CLI (`aux serve`) that serves the frontend,
  handles the Spotify OAuth authorization-code flow, and runs an agent loop
  against the Anthropic Messages API. Every wrapper method (albums, artists,
  tracks, playlists, player, search, users, shows, episodes, audiobooks,
  chapters, categories, markets, plus the deprecated audio-features and
  recommendations endpoints) is registered as a tool the model can call.
  Responses stream to the browser as server-sent events, including live
  tool-call activity.
- **Frontend** (Vite + TypeScript): a chat UI with a "Connect Spotify"
  button, streaming responses, and a sidebar of persistent conversations —
  start new chats and come back to old ones; the full context (including
  tool calls) survives restarts, so the AI remembers where you left off.

## Requirements

- A [Spotify app](https://developer.spotify.com/dashboard) (client ID +
  secret) with `<public-url>/api/auth/callback` registered as a redirect URI.
  Spotify requires HTTPS redirect URIs; plain HTTP is only allowed on the
  explicit loopback IPs (`http://127.0.0.1:PORT` or `http://[::1]:PORT`) —
  `localhost` is rejected. For local development register
  `http://127.0.0.1:8080/api/auth/callback`.
- An [Anthropic API key](https://platform.claude.com/).
- Spotify Premium for the playback-control tools (everything else works
  without it).

### Spotify API access

The wrapper targets Spotify's **February 2026 development-mode Web API**. For
an app in development mode this means:

- Only up to **5 users** may use it, and each must be added under **User
  Management** in the [Spotify dashboard](https://developer.spotify.com/dashboard)
  — otherwise their requests return `403`. Add every account before going
  live.
- Playlist contents are readable only for playlists the user **owns or
  collaborates on**; Spotify withholds the contents of editorial/algorithmic
  playlists.
- Some endpoints require Spotify's *extended quota* and otherwise return `403`
  (browse/new-releases, categories, artist top tracks, other users' profiles,
  markets) or are deprecated (recommendations, audio features). The AI is told
  about this and falls back to search and the user's own library.

A `403` is an app-level restriction, never a login/scope problem — the app
never asks you to re-authorize to fix one.

## Configuration

Configuration is read from flags, `AUX_`-prefixed environment variables, and
an optional `aux.yaml` file (working directory or `/etc/aux`), in that order
of precedence.

| Key (yaml) | Env var | Default | Description |
|---|---|---|---|
| `addr` | `AUX_ADDR` | `:8080` | Listen address |
| `static_dir` | `AUX_STATIC_DIR` | `frontend/dist` | Built frontend directory |
| `public_url` | `AUX_PUBLIC_URL` | `http://127.0.0.1:8080` | Public base URL; the OAuth redirect URL is derived from it (HTTPS required except on explicit loopback IPs) |
| `token_file` | `AUX_TOKEN_FILE` | `spotify-token.json` | Where the Spotify OAuth token is persisted |
| `spotify.client_id` | `AUX_SPOTIFY_CLIENT_ID` | — | Spotify app client ID |
| `spotify.client_secret` | `AUX_SPOTIFY_CLIENT_SECRET` | — | Spotify app client secret |
| `spotify.redirect_url` | `AUX_SPOTIFY_REDIRECT_URL` | `<public_url>/api/auth/callback` | Override the derived redirect URL |
| `anthropic.api_key` | `AUX_ANTHROPIC_API_KEY` | `$ANTHROPIC_API_KEY` | Anthropic API key |
| `anthropic.model` | `AUX_ANTHROPIC_MODEL` | `claude-opus-4-8` | Model used for the agent (also settable in the admin UI) |
| `anthropic.max_tokens` | `AUX_ANTHROPIC_MAX_TOKENS` | `8192` | Max output tokens per model turn (also settable in the admin UI) |
| `admin.password` | `AUX_ADMIN_PASSWORD` | — | Admin password gating the whole app; empty (and no OIDC) disables auth (dev only) |
| `settings_file` | `AUX_SETTINGS_FILE` | `aux-settings.json` | Where credentials set via the admin UI are persisted (0600) |
| `chats_dir` | `AUX_CHATS_DIR` | `chats` | Directory where conversations are persisted (one JSON file per chat) |
| `oidc.issuer_url` | `AUX_OIDC_ISSUER_URL` | — | OpenID Connect issuer URL; set (with client ID) to enable SSO |
| `oidc.client_id` | `AUX_OIDC_CLIENT_ID` | — | OIDC client ID |
| `oidc.client_secret` | `AUX_OIDC_CLIENT_SECRET` | — | OIDC client secret |
| `oidc.redirect_url` | `AUX_OIDC_REDIRECT_URL` | `<public_url>/api/admin/oidc/callback` | Override the derived OIDC redirect URL |
| `oidc.scopes` | `AUX_OIDC_SCOPES` | `openid profile email` | Space-separated OIDC scopes |
| `oidc.allowed_emails` | `AUX_OIDC_ALLOWED_EMAILS` | — | Comma-separated allowlist of verified emails (empty = any authenticated user) |

## Admin login & runtime settings

The whole app (chat, Spotify connect, settings) sits behind a login screen —
required before exposing Aux publicly. Two login methods, either or both:

- **Password** — set `AUX_ADMIN_PASSWORD`.
- **Single sign-on (OIDC)** — set `AUX_OIDC_ISSUER_URL` and
  `AUX_OIDC_CLIENT_ID` (plus `AUX_OIDC_CLIENT_SECRET` for a confidential
  client) to add a "Sign in with SSO" button, so you can back the login with
  Keycloak or any OIDC provider. Register
  `<public_url>/api/admin/oidc/callback` as a redirect URI in the provider.
  Optionally restrict access with `AUX_OIDC_ALLOWED_EMAILS`. The ID token is
  verified (signature, issuer, audience, nonce) on each login.

If neither is set the app runs **unprotected** (local development only) and
logs a warning.

Once logged in, the ⚙ Settings button opens a panel where you can view and
change at runtime:

- the **Spotify client ID/secret** and the **Anthropic API key** — secrets
  are only ever sent back masked (`••••1234`) and entered through blurred
  password fields;
- the **model** — click **Fetch** to load the current list of models from the
  Anthropic API and pick one, so you automatically get newly released models
  without redeploying;
- the **max output tokens** cap — lower it (or choose a cheaper model like
  Haiku) to save cost.

Values saved there are persisted to `settings_file` (0600), override the
environment, and hot-swap the Spotify/AI clients without a restart — so a
fresh deployment can start with no credentials at all and be configured
entirely from the browser.

## Development

The repo ships a Nix dev shell (`nix develop` or direnv) with Go, Node, and
tooling. Without Nix you need Go ≥ 1.25 and Node ≥ 22.

Copy `.env.example` to `.env` and fill in your credentials; direnv loads it
automatically together with the dev shell (without direnv:
`set -a; source .env; set +a`).

```sh
# backend
go run ./cmd/server serve

# frontend (separate terminal; dev server proxies /api to :8080)
cd frontend && npm ci && npm run dev
```

Open the Vite dev URL, click **Connect Spotify**, and chat — e.g. *"Build me
a focus playlist from my top tracks and remove anything with vocals."*

For a production-style run, `./build.sh` builds both parts (frontend into
`frontend/dist`, backend into `bin/aux` with the version stamped from
`git describe`), then start it with `./bin/aux serve`. The script also has
`frontend`, `backend`, `test`, and `clean` subcommands.

## Docker

The image runs as a non-root user (UID 10001) and needs no privileges — it
works with a read-only root filesystem as long as `/data` is writable.

```sh
docker build -t aux .
docker run -p 8080:8080 -v aux-data:/data \
  --read-only --user 10001:10001 \
  -e AUX_ADMIN_PASSWORD=... \
  -e AUX_SPOTIFY_CLIENT_ID=... \
  -e AUX_SPOTIFY_CLIENT_SECRET=... \
  -e AUX_ANTHROPIC_API_KEY=... \
  -e AUX_PUBLIC_URL=http://127.0.0.1:8080 \
  aux
```

## CI / releases

`.gitlab-ci.yml` runs gofmt/vet/tests, the frontend build, and a Helm lint on
every push. Tagging `vX.Y.Z` publishes:

- the container image to the GitLab registry as `:X.Y.Z` and `:latest`, and
- the Helm chart to the project's GitLab Helm package registry
  (`stable` channel) with matching chart/app version.

## Helm

```sh
helm repo add aux https://gitlab.example.com/api/v4/projects/<id>/packages/helm/stable
helm install aux aux/aux \
  --set image.repository=registry.example.com/you/aux \
  --set config.publicUrl=https://aux.example.com \
  --set ingress.enabled=true --set ingress.host=aux.example.com \
  --set secrets.adminPassword=... \
  --set secrets.spotifyClientId=... \
  --set secrets.spotifyClientSecret=... \
  --set secrets.anthropicApiKey=...
```

The chart persists `/data` (the saved Spotify token, admin settings and
chats) in a small PVC so state survives restarts; set `secrets.existingSecret`
to manage credentials outside the chart. It runs the pod with a hardened
`securityContext` (non-root UID 10001, read-only root filesystem, all
capabilities dropped, `RuntimeDefault` seccomp) — override via
`podSecurityContext` / `containerSecurityContext` if your cluster needs it.

To back the login with Keycloak (or another OIDC provider), enable SSO:

```sh
helm install aux aux/aux \
  ... \
  --set oidc.enabled=true \
  --set oidc.issuerUrl=https://keycloak.example.com/realms/aux \
  --set oidc.clientId=aux \
  --set secrets.oidcClientSecret=... \
  --set oidc.allowedEmails=you@example.com
```

## License

LGPL-3.0 — the same license as the underlying
[spotify-go-wrapper](https://github.com/EmpireForge-ef/spotify-go-wrapper).
See [COPYING](COPYING) and [COPYING.LESSER](COPYING.LESSER).
