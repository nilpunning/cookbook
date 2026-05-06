# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
# Development (hot reload on .go, .html, .css, config.toml changes)
air
caddy run --config Caddyfile   # separate terminal — adds HTTPS via self-signed cert

# Tests
go test -v ./...
go test -v ./internal/core/...  # single package

# Production build & run
./build.sh
./cookbook -c config.toml

# Utility commands (built into the binary)
./cookbook -k   # generate a random secret key (for SessionSecrets / CSRFKey)
./cookbook -p   # hash a password for FormBasedAuthUsers config
```

`air` reads `.air.toml` and rebuilds to `tmp/main -c config.toml` on changes. The `recipes/` directory and `_test.go` files are excluded from triggering rebuilds.

## Architecture

**Cookbook** is a single Go binary that serves a recipe management web app. Recipes are plain markdown files stored on disk; the binary indexes them into an in-memory Bleve search index at startup and keeps it live via `fsnotify`.

### Key data flow

1. **Startup** (`main.go`): loads config → creates `core.State` (holds the Bleve index, session store, config, and auth strategy) → calls `state.LoadRecipes()` → launches `go state.MonitorRecipesDirectory()` → registers HTTP handlers → starts server.

2. **Recipe indexing** (`internal/core/recipe_files.go`): markdown files in `RecipesPath` are read, converted to HTML via `internal/markdown`, and upserted into the Bleve index. The webpath key is the title-cased, space-stripped recipe name (e.g., `"Apple Cranberry Walnut Salad"` → `"AppleCranberryWalnutSalad"`).

3. **Search** (`internal/search/search.go`): in-memory Bleve index; `name` and `markdown` fields are full-text analyzed, `filename`/`webpath`/`html`/`tags` are keyword fields. `GetRecipesGroupedByTag` groups all recipes by their tags for the home page listing.

4. **Markdown** (`internal/markdown/`): uses goldmark with GFM and a custom `Tags` extension (`tags.go`). A line starting with `tags:` in the markdown is parsed into a tag list and stored in the Bleve index. Recipes with no tags default to the `"Other"` group.

5. **Handlers** (`internal/handlers/`): all handlers are closures over `core.State` (passed by value). `writeResponse` is HTMX-aware — it checks `HX-Request` / `HX-Target` headers and either returns partial template renders or sets `HX-Location` for client-side redirects. Full-page requests get the complete `base.html` layout.

6. **LLM import** (`internal/core/import.go`): when `Server.LLM` is configured, the import flow runs two goroutines concurrently — one asks the LLM if it already knows the recipe from the URL, and one fetches + strips the HTML. If the LLM's crawl knowledge returns a result, the HTTP fetch is cancelled; otherwise the fetched HTML is passed to the LLM.

### Auth

Three modes set in `main.go` based on config presence:
- No config → read-only (no edit/import UI)
- `[FormBasedAuthUsers]` → form login at `/auth/form`
- `[OIDC]` → OIDC flow at `/auth/oidc`

`IsAuthenticated` is checked per-request via the gorilla session store. Edit and import handlers return 401 immediately if not authenticated.

### File storage

Recipes are written directly to `RecipesPath` as `<Name>.md`. Creating a new recipe uses `O_EXCL` to prevent overwrites. Renaming (name change on edit) is: write new file → delete old file. `fsnotify` picks up both events and updates the index.

### Frontend

Templates live in `templates/` and are loaded at startup (not embedded). Static assets (`style.css`, favicons) are in `static/` and served directly. HTMX drives the search box (targets `#recipes`) and form submissions (uses `HX-Location` for redirects).

### Config

`config.toml` (TOML format, see `config-example.toml`). The `CSRFKey` must be a 32-byte hex string; `SessionSecrets` is a list of hex strings. `SecureCookies = true` requires HTTPS (use Caddy in dev). `Language` controls the Bleve text analyzer (default `"en"`).
