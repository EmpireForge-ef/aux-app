# Aux

Aux is a web app that puts an AI in charge of your Spotify account. You chat
with the assistant in a browser; the assistant drives the
[spotify-go-wrapper](https://github.com/EmpireForge-ef/spotify-go-wrapper)
API — exposed as tools via the
[Anthropic Go SDK](https://github.com/anthropics/anthropic-sdk-go) — to
search the catalog, build and edit playlists, manage your library, follow
artists, and control playback.

## Features

- **AI control of Spotify** — every working method of the wrapper (playlists,
  library, player, search, artists, albums, shows, episodes, audiobooks, …) is
  a tool the model can call; responses stream live, including tool activity.
  Tool results are slimmed to the essentials (name, artist, uri, …) to stay
  fast and token-cheap. Destructive actions ask for your confirmation first.
- **Personalised & context-aware** — the AI keeps a small cross-chat memory of
  your music preferences (favourite genres, no-gos, era) and knows the current
  time, so "a playlist like last time" or "something for a Monday morning"
  just works.
- **Persistent, multi-chat conversations** — a sidebar to start new chats,
  return to old ones, and rename or delete them; full context (including tool
  calls) survives restarts. Each message has a copy button.
- **Works on mobile** — responsive layout with an off-canvas chat drawer, so
  it's usable on a phone as well as a desktop.
- **Admin login** — protect the whole app with a password and/or OpenID
  Connect single sign-on (Keycloak-compatible).
- **Runtime settings UI** — set the Spotify credentials, Anthropic API key,
  model, and token cap from the browser; secrets stay masked, changes apply
  without a restart.
- **Model selection** — fetch the current list of models from the Anthropic
  API and pick one (plus a max-tokens cap) to trade quality for cost.
- **Deploy-ready** — non-root container, GitHub Actions with versioned GHCR
  publishing, and a hardened Helm chart.

## How it works

- **Backend** (Go): a cobra/viper CLI (`aux serve`) that serves the frontend,
  handles the Spotify OAuth authorization-code flow, and runs an agent loop
  against the Anthropic Messages API. Every working wrapper method (albums,
  artists, tracks, playlists, player, search, users, shows, episodes,
  audiobooks, chapters) is registered as a tool the model can call. Responses
  stream to the browser as server-sent events, including live tool-call
  activity. Destructive tools (removing saved items, clearing/removing
  playlist items, unfollowing) are gated behind a user confirmation before
  they run.
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
of precedence. Each setting lists its environment variable, the equivalent
`aux.yaml` key, and its default.

**Server**

- **`AUX_ADDR`** (`addr`, default `:8080`) — listen address.
- **`AUX_PUBLIC_URL`** (`public_url`, default `http://127.0.0.1:8080`) —
  public base URL; the Spotify and OIDC redirect URLs are derived from it.
  HTTPS is required except on the loopback IPs (`127.0.0.1` / `[::1]`).
- **`AUX_STATIC_DIR`** (`static_dir`, default `frontend/dist`) — directory of
  the built frontend to serve.

**Spotify**

- **`AUX_SPOTIFY_CLIENT_ID`** (`spotify.client_id`) — Spotify app client ID.
- **`AUX_SPOTIFY_CLIENT_SECRET`** (`spotify.client_secret`) — Spotify app
  client secret.
- **`AUX_SPOTIFY_REDIRECT_URL`** (`spotify.redirect_url`) — override the
  redirect URL (default `<public_url>/api/auth/callback`).

**Anthropic**

- **`AUX_ANTHROPIC_API_KEY`** (`anthropic.api_key`, default
  `$ANTHROPIC_API_KEY`) — Anthropic API key.
- **`AUX_ANTHROPIC_MODEL`** (`anthropic.model`, default `claude-opus-4-8`) —
  model used for the agent. Also settable in the admin UI.
- **`AUX_ANTHROPIC_MAX_TOKENS`** (`anthropic.max_tokens`, default `8192`) —
  max output tokens per model turn. Also settable in the admin UI.

**Admin login**

- **`AUX_ADMIN_PASSWORD`** (`admin.password`) — password gating the whole
  app. Empty (and no OIDC) disables auth — local development only.
- **`AUX_OIDC_ISSUER_URL`** (`oidc.issuer_url`) — OpenID Connect issuer URL;
  set together with the client ID to enable SSO.
- **`AUX_OIDC_CLIENT_ID`** (`oidc.client_id`) — OIDC client ID.
- **`AUX_OIDC_CLIENT_SECRET`** (`oidc.client_secret`) — OIDC client secret.
- **`AUX_OIDC_REDIRECT_URL`** (`oidc.redirect_url`) — override the OIDC
  redirect URL (default `<public_url>/api/admin/oidc/callback`).
- **`AUX_OIDC_SCOPES`** (`oidc.scopes`, default `openid profile email`) —
  space-separated OIDC scopes.
- **`AUX_OIDC_ALLOWED_EMAILS`** (`oidc.allowed_emails`) — comma-separated
  allowlist of verified emails; empty means any authenticated user is allowed.

**Storage** (persist these on a volume in production)

- **`AUX_TOKEN_FILE`** (`token_file`, default `spotify-token.json`) — where
  the Spotify OAuth token is persisted.
- **`AUX_SETTINGS_FILE`** (`settings_file`, default `aux-settings.json`) —
  where credentials and model choice set via the admin UI are persisted
  (mode 0600).
- **`AUX_CHATS_DIR`** (`chats_dir`, default `chats`) — directory of persisted
  conversations (one JSON file per chat).
- **`AUX_PREFERENCES_FILE`** (`preferences_file`, default
  `aux-preferences.json`) — where the user's saved music preferences (the AI's
  cross-chat memory) are persisted.

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
Released images are published to `ghcr.io/empireforge-ef/aux-app`; build
locally with `docker build -t aux .` for development.

```sh
docker run -p 8080:8080 -v aux-data:/data \
  --read-only --user 10001:10001 \
  -e AUX_ADMIN_PASSWORD=... \
  -e AUX_SPOTIFY_CLIENT_ID=... \
  -e AUX_SPOTIFY_CLIENT_SECRET=... \
  -e AUX_ANTHROPIC_API_KEY=... \
  -e AUX_PUBLIC_URL=http://127.0.0.1:8080 \
  ghcr.io/empireforge-ef/aux-app:latest
```

## CI / releases

CI runs on **GitHub Actions** (`.github/workflows/ci.yml`); pipelines are
visible under the repository's **Actions** tab. Every push and pull request
runs gofmt, `go vet`, `go test`, the frontend build, and a Helm lint.

Pushing a `vX.Y.Z` tag additionally publishes to the GitHub Container
Registry (GHCR):

- the container image at
  [`ghcr.io/empireforge-ef/aux-app`](https://github.com/EmpireForge-ef/aux-app/pkgs/container/aux-app),
  tagged `:X.Y.Z` and `:latest`, and
- the Helm chart as an OCI artifact at `oci://ghcr.io/empireforge-ef/charts/aux`,
  with matching chart/app version.

Both are pushed with the workflow's built-in `GITHUB_TOKEN` — no extra
secrets to configure. GHCR packages start **private**: make them public in
the package settings, or add `imagePullSecrets` (and a registry credential)
for a private pull.

## Helm

The chart is an OCI artifact on GHCR, so no `helm repo add` is needed —
install straight from the registry (the image repository already defaults to
`ghcr.io/empireforge-ef/aux-app`):

```sh
helm install aux oci://ghcr.io/empireforge-ef/charts/aux --version X.Y.Z \
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
helm install aux oci://ghcr.io/empireforge-ef/charts/aux --version X.Y.Z \
  ... \
  --set oidc.enabled=true \
  --set oidc.issuerUrl=https://keycloak.example.com/realms/aux \
  --set oidc.clientId=aux \
  --set secrets.oidcClientSecret=... \
  --set oidc.allowedEmails=you@example.com
```

## Roadmap

Larger deferred ideas (a real recommendation engine, snapshot-based undo,
auto-generated cover art, …) are tracked in [plan.md](plan.md).

## License

LGPL-3.0 — the same license as the underlying
[spotify-go-wrapper](https://github.com/EmpireForge-ef/spotify-go-wrapper).
See [COPYING](COPYING) and [COPYING.LESSER](COPYING.LESSER).
