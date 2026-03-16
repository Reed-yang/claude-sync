# Selective Push/Pull for claude-sync

**Date:** 2026-03-16
**Status:** Approved

## Problem

claude-sync currently syncs all files matching hard-coded `SyncPaths` on every push/pull. There is no way to:
- Push/pull only specific files or directories
- Persistently exclude files from sync (e.g., machine-specific configs, large caches)
- Pull files to a custom directory for inspection before merging

## Design

### Filtering Pipeline

Priority from high to low:

```
CLI path arguments (highest — when specified, only sync these paths)
    ↓ when not specified
Hard-coded SyncPaths (default — full sync)
    ↓ both subject to
.claudesyncignore (global exclude — always applied)
```

### CLI Interface

```bash
# Existing behavior (unchanged)
claude-sync push
claude-sync pull [--dry-run] [--force]

# New: positional path arguments
claude-sync push skills/ settings.json
claude-sync pull --force skills/ CLAUDE.md

# New: --target flag for pull (read-only export, does NOT update state)
claude-sync pull --target /tmp/backup
claude-sync pull --target /tmp/backup skills/ CLAUDE.md
```

**Path argument behavior:**
- Paths are relative to `~/.claude/`
- Directories are recursive (include all files underneath)
- Multiple paths separated by spaces
- No arguments = full sync (backward compatible)
- `.claudesyncignore` still applies even when paths are specified

**`--target` behavior:**
- Downloads files to the specified directory instead of `~/.claude/`
- Does NOT update `~/.claude-sync/state.json` (read-only export)
- Can be combined with path arguments

### `.claudesyncignore` File

**Location:** `~/.claude/.claudesyncignore` (inside synced directory, so it can itself be synced)

**Format:** `.gitignore` syntax:

```gitignore
# Machine-specific config
settings.local.json

# Session recordings
projects/

# Large caches
plugins/cache/

# Temp files
*.bak
*.conflict.*
```

**Matching rules:**
- Paths relative to `~/.claude/`
- Trailing `/` matches directories
- Supports `*`, `**`, `!` (negate)
- Empty lines and `#` comments ignored

**Implementation:** Use an existing Go gitignore parsing library (e.g., `go-gitignore`) rather than writing a custom parser.

## Code Changes

### New Files

| File | Purpose |
|------|---------|
| `internal/sync/filter.go` | Filter pipeline: load `.claudesyncignore`, match CLI paths, filter file lists |
| `internal/sync/filter_test.go` | Unit tests for filter logic |

### Modified Files

| File | Change |
|------|--------|
| `cmd/claude-sync/main.go` | `pushCmd`/`pullCmd`: parse positional args; `pullCmd`: add `--target` flag; pass filter and targetDir to Syncer. Update direct `GetLocalFiles()` calls at ~lines 1818 and 1934 to use filter. |
| `internal/sync/sync.go` | `Push()`/`Pull()`/`Status()`/`PreviewPull()`/`Diff()`: accept `*Filter` parameter; `Pull()` accepts optional `targetDir`. When `targetDir` is set, write files there and skip all `s.state` updates (both in `Pull()` and `downloadFile()`). |
| `internal/sync/state.go` | `GetLocalFiles()`: accept `*Filter` parameter. When CLI paths are provided, they **replace** `syncPaths` as the walk roots; when absent, `syncPaths` (hard-coded default) is used. `.claudesyncignore` is applied during walk in both cases. `DetectChanges()`: apply filter to deletion detection — files outside filter scope are not treated as deletions. |
| `internal/sync/sync_test.go` | Update tests for Push/Pull/Status/Diff to cover filtered scenarios |
| `internal/sync/state_test.go` | Update tests for GetLocalFiles/DetectChanges with filter |

### Unchanged

- `internal/crypto/` — encryption logic unaffected
- `internal/storage/` — storage layer unaffected
- `internal/config/config.go` — `SyncPaths` stays as default, no new config fields
- `config.yaml` format — no changes

### Estimated Scope

~450 lines added/modified.

## Behavior Matrix

| CLI args | .claudesyncignore | Result |
|----------|-------------------|--------|
| None | None | Full sync (current behavior) |
| None | Present | Full sync minus ignored files |
| `skills/` | None | Only sync skills/ |
| `skills/` | Has `skills/paper-*` | Sync skills/ except paper-* |
| `settings.json skills/` | Present | Sync only those two, minus any ignored |

## Edge Cases

- **Path doesn't exist locally (push):** Skip with warning, not an error
- **Path doesn't exist remotely (pull):** Skip with warning, not an error
- **`--target` with `--dry-run`:** Show what would be downloaded to target dir
- **`--target` with `--force`:** `--force` is ignored (no local state to conflict with)
- **`.claudesyncignore` itself:** Not ignored by default (can be synced between devices)
- **`.claudesyncignore` loaded once:** Rules are read at operation start and not re-evaluated mid-operation, avoiding chicken-and-egg issues if the ignore file itself is synced
- **Selective push and deletions:** When CLI paths are specified, only files within the filter scope are candidates for deletion detection. Files outside the scope are untouched.
- **Remote-side filtering (pull):** After fetching the full remote object list via `storage.List()`, filter the list client-side before downloading. No changes to storage interface needed.
- **Backward compatibility:** No args = identical to current behavior
