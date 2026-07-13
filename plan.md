# Roadmap — deferred feature ideas

Larger features that came out of using the agent, deferred because each is a
project on its own. Rough notes so they're actionable later; not committed to
any order.

## 1. Real recommendation engine

**Why:** Spotify removed `get_recommendations`, `get_related_artists`, and the
audio-features endpoints for development-mode apps, so "make a vibe playlist"
or "songs like X" has no first-class backend. Today the AI assembles these
from genre/year search plus the user's top/saved tracks — workable but shallow.

**Idea:** a `recommend_tracks` tool that builds a candidate pool server-side
and returns a deduped, slimmed list:

- Seeds: 1–5 artist IDs / track IDs / genres, plus optional targets (era,
  mood, energy expressed as search terms).
- Gather: seed artists' albums → tracks; genre/year searches; the user's top
  tracks and saved tracks that match the seed artists' genres.
- Rank/dedupe: drop duplicates by URI, exclude the seeds themselves, optionally
  weight by overlap with the user's library.
- Optional upgrade: ISRC- or embedding-based similarity over the user's saved
  songs for genuinely "similar" results instead of catalog + genre matching.

Reuse the existing `Slim()` projections for the output.

## 2. Undo / snapshot-based restore for destructive edits

**Why:** destructive actions are now confirmed, but still irreversible. An undo
would build real trust in an AI agent.

**Idea:** before a destructive playlist edit (`replace_playlist_items`,
`remove_playlist_items`), capture the playlist's current items + snapshot ID;
store the last N snapshots per chat (or globally). Add an `undo_last_change`
tool / UI affordance that restores from the captured snapshot. Library
removals and unfollows can be undone by re-saving / re-following the captured
IDs.

## 3. Extended-quota application + graceful-degradation messaging

**Why:** many endpoints (new releases, categories, artist top tracks, other
users' data, recommendations) are locked in development mode. Extended quota
unlocks them but requires a registered business with ~250k MAU — likely out of
reach, so this is mostly about honest UX.

**Idea:** surface a clear one-time note in the UI/README about what's locked
and why, and — if quota is ever granted — re-enable the removed tools behind a
config flag rather than deleting them permanently.

## 4. Auto-generated cover art for AI playlists

**Why:** `set_playlist_cover_image` exists but the app never generates a cover;
a nice detail for AI-made playlists.

**Idea:** generate a cover server-side — either a simple deterministic
gradient + playlist-name render (no external deps), or an image-generation
model for something richer — and upload it via the existing tool when the AI
creates a playlist.

## 5. Smaller polish

- **"Why this song?" surfacing in the UI** — the AI already explains in text;
  could render the rationale inline per track.
- **Context signals beyond time** — day-part presets, "after the gym", etc.,
  possibly wired to real signals (last activity) rather than manual hints.
- **Multi-modal** — let the model *see* cover art it uploads (vision), for
  feedback loops on generated covers.

## 6. Weather-aware recommendations

**Why:** the AI already gets the current time (and timezone) each turn, so
"something for a Monday morning" works. The current weather is the natural next
context signal — rainy vs. sunny, hot vs. freezing meaningfully changes what
fits ("rainy-day lo-fi", "sunny drive playlist"), and users shouldn't have to
spell it out.

**Idea:** wire a weather API (e.g. Open-Meteo — free, no API key, or
OpenWeatherMap with a key) and fold the current conditions into the per-turn
context block alongside the time, so the model can factor it in without a tool
call.

- **Location:** the API needs coordinates. Options, cheapest first: a
  configurable `AUX_LOCATION` / settings field (lat,lon, or a city name
  geocoded once); or, later, an optional browser-geolocation prompt passed with
  the chat request. Keep it opt-in — no location, no weather, no behavior
  change.
- **Fetch + cache:** call the weather API server-side and cache the result for
  ~15–30 min (weather barely moves and it keeps the turn cheap); reuse the
  persisted-store pattern if it should survive restarts. Degrade silently on
  API failure — just omit the weather line, never block the turn.
- **Inject:** add one short line to `turnContext` ("Current weather in Berlin:
  12°C, light rain") next to the time, gated on the feature being configured —
  mirrors how the timezone-aware clock is already injected.
- **Config:** `AUX_WEATHER_ENABLED` + provider/location settings, surfaced in
  the admin settings UI like the timezone picker.
- **Prompt:** a guideline nudging the AI to *lightly* weight weather into vibe
  selections and mention it when relevant ("some rainy-evening jazz"), without
  overfitting every request to the forecast.
