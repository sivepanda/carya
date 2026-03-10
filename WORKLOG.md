# cmd
Contains external executables, so pretty much just the main method + cobra commands (init, daemon, publish, team, jump, pull, checkout, housekeeping, compose, view, lsp, completion)

# internal
Contains business logic, each system is in its own package.

## internal/git
Core git plumbing for state sharing.
- `plumbing.go` — ShadowRepo: manages `.carya/shadow/` to snapshot the working tree without touching the main index. Mutex-serialized.
- `refs.go` — RefManager: manages `refs/carya/users/<id>/tree` refs locally and on origin. Handles push/fetch of carya refs.
- `conflict.go` — ConflictPredictor: uses `git merge-tree` to predict conflicts and `git diff-tree` for file-level diffs between trees.

## internal/repository
Repository struct — finds .carya dir, exposes root/carya/db paths, Exists() check.

## internal/identity
Persists a user ID to `.carya/identity`.

## internal/config
Team-level config (remotes, etc.).

## internal/chunk
Defines what chunks are, their schema, what needs to be saved and tracked, etc. Includes chunking strategy, manager, unified diff handling.

## internal/engine
Main chunk engine — coordinates chunk detection and storage.

## internal/features
Plugin-style feature system for the daemon. Defines a Feature interface + FeatureManager, with chunk engine and file watcher implemented as features.

## internal/watcher
Watches filesystem for changes; fires events consumed by the chunk engine.

## internal/store
Local SQLite store for chunks and state.

## internal/patch
Patch apply/generate helpers.

## internal/lsp
Full LSP server that surfaces team conflicts as editor diagnostics. Reads team refs and converts conflicts to diagnostics in real time.

## internal/daemon
Background daemon: runs watcher + feature manager, handles signals and PID file.

## internal/tui
Shared TUI components (Bubble Tea) — styles, keybindings, screen models (diff viewer, housekeeping, compose, etc.).

## Housekeeping
Delegated to external package `github.com/sivepanda/mycelia`. `pull` and `checkout` load `carya.json` and run post-pull/post-checkout tasks through it.


# TODO

1. Improve housekeeping engine - presently all configs need to be registered, make it smarter
2. fix patching, review git patch formatting, make sure everything works
3. dependencies
4. corrupted lines?
