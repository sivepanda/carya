# Contributing to Carya

Thanks for contributing.

This project is still evolving quickly, so the most helpful contributions are:

- bug fixes that improve reliability in real workflows,
- guardrails around data safety (working tree, refs, chunks),
- tests for behavior that currently has little coverage,
- docs that make contributor intent and architecture clearer.

## Repository map

### `cmd/`

CLI entrypoints and Cobra commands. Keep command handlers thin and move business logic into `internal/*` packages.

### `internal/`

Core systems by package:

- `internal/git`: shadow-repo plumbing, carya refs, conflict prediction.
- `internal/repository`: `.carya` path management and repo helpers.
- `internal/identity`: persisted user identity for team refs.
- `internal/config`: team and configuration helpers.
- `internal/chunk`: chunk model, strategy, manager, reconciliation logic.
- `internal/engine`: orchestrates watcher + chunking + storage.
- `internal/features`: daemon feature abstraction and feature wiring.
- `internal/watcher`: filesystem event ingestion and filtering.
- `internal/store`: SQLite storage for chunks.
- `internal/patch`: compose/apply/commit patch operations.
- `internal/lsp`: diagnostics for team conflict visibility.
- `internal/daemon`: background process lifecycle and signals.
- `internal/tui`: Bubble Tea/Lip Gloss views and shared UI helpers.

## Logging and observability

### Log destination policy

Use the Go `log` package for runtime diagnostics. In normal repo usage, logs are directed to:

- `.carya/carya.log`

Key behavior:

- Root command configures logging in `PersistentPreRunE`.
- For `carya init`, `.carya/` is created before opening the logfile.
- Outside a Carya repo (non-init commands), logger output is discarded.

Contributor guidance:

- Prefer `log.Printf` for debug/runtime tracing.
- Use `fmt.Fprintf(os.Stderr, ...)` for user-facing errors.
- Avoid noisy logging in tight loops unless gated or clearly actionable.

## Workflow conventions

### Chunk lifecycle

- File changes are chunked by `internal/chunk` strategy and periodically flushed.
- `carya compose` builds commits from selected chunks.
- `carya sync` reconciles stored chunks with git state and prunes stale/already-applied entries.
- `carya push` runs `git push` then `carya sync` behavior automatically.

### Team refs

- Team state uses refs under `refs/carya/users/<id>/tree`.
- Prefer isolated remote-tracking fetch behavior for team refs.
- Keep local working refs and fetched remote refs semantically separate.

### Jump safety

- `carya jump` may modify the working tree.
- Preserve local work before destructive transitions (stash/session state patterns).
- For read-only inspection, prefer view flows over checkout flows.

## Code quality expectations

- Keep command files focused on UX and orchestration; move logic into `internal/*`.
- Prefer explicit error messages with context and actionable next steps.
- Avoid hidden side effects across packages.
- Maintain cross-platform-safe git command usage when possible.
- Run formatting and checks before shipping changes.

Recommended local checks:

- `gofmt -w <changed-go-files>`
- `go test ./...`
- `go vet ./...`

## Current gaps where contributions help

- Add unit/integration tests for chunk reconciliation and compose apply behavior.
- Add tests for delete/add edge cases in chunk generation.
- Strengthen docs around daemon behavior and failure recovery.
- Improve housekeeping config ergonomics and autodetection quality.

## PR notes

When opening PRs, include:

- what user workflow changed,
- why the change is needed,
- risk areas (working tree, refs, chunk persistence),
- what validation commands were run.
