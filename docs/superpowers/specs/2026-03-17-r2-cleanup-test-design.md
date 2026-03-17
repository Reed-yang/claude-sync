# R2 Storage Cleanup with Test Environment Validation

## Overview

Clean up redundant content on Cloudflare R2 storage used by claude-sync, using a fresh GPU node 2 test environment to validate each deletion step. The cleanup requires three new features (delete command, parallel I/O, session path remapping) and a four-round progressive deletion strategy.

## Motivation

Current R2 storage contains ~428 MB across ~2092 files. Analysis shows ~99% is redundant:

| Category | Files | Size | Status |
|----------|-------|------|--------|
| Old-path sessions (`projects/-home-*`) | 843 | ~419 MB | Remap to `-mnt-` then delete old |
| Plugin `.git/` directories | 124 | ~3.6 MB | Redundant — auto-cloned on install |
| Plugin cache (all versions) | 367 | ~3.3 MB | Redundant — auto-rebuilt on startup |
| Plugin marketplace source | 240 | ~2.6 MB | To be validated — may or may not be needed |

**Target end state on R2:** Only configuration, custom skills, and remapped session history.

## Feature 1: `delete` Subcommand

### Interface

```bash
claude-sync delete <glob-patterns...> [--dry-run]
```

### Examples

```bash
claude-sync delete "plugins/marketplaces/*/.git/**" --dry-run
claude-sync delete "projects/-home-*/**"
claude-sync delete "plugins/cache/**"
```

### Behavior

1. List all remote files via `storage.List("")` (full enumeration)
2. Match each remote key against provided glob patterns using `doublestar.Match()` from the [doublestar](https://github.com/bmatcuk/doublestar) library (supports `**` recursive matching, unlike `path.Match`)
3. In `--dry-run` mode: print matched files and total size, exit
4. In normal mode: display matched list, prompt user for `yes` confirmation
5. If matched count exceeds 50% of total remote files, show extra warning
6. Execute deletion via `storage.DeleteBatch()` in batches of 1000
7. Update local `state.json` — remove entries for deleted keys
8. Print summary: files deleted (matched count minus error count), space freed

**Note:** This is a new glob matching path independent from `Filter.ShouldInclude()` (which uses prefix matching for selective push/pull). The `delete` command does not reuse `Filter` — it operates directly on remote keys with glob semantics.

### Safety

- Always requires explicit `yes` confirmation (no `--force` flag)
- `--dry-run` is the recommended first step
- >50% match triggers additional warning
- state.json is updated atomically after successful deletion

## Feature 2: Parallel Push/Pull/Delete

### Design

Add a worker pool to all I/O-bound operations in `sync.go`. Uses Go goroutines with a semaphore channel.

```
const maxWorkers = 32
```

### Affected Operations

| Operation | Current | After |
|-----------|---------|-------|
| Push (upload) | Sequential | 32 concurrent uploads |
| Pull (download) | Sequential | 32 concurrent downloads |
| Delete (batch) | Sequential batches of 1000 | Keep sequential (already efficient at ~3 batches for 2K files) |

### Implementation Approach

```go
sem := make(chan struct{}, maxWorkers)
var wg sync.WaitGroup
var mu sync.Mutex
var errs []error

for _, file := range files {
    wg.Add(1)
    sem <- struct{}{}
    go func(f File) {
        defer wg.Done()
        defer func() { <-sem }()
        if err := process(ctx, f); err != nil {
            mu.Lock()
            errs = append(errs, err)
            mu.Unlock()
        }
    }(file)
}
wg.Wait()
```

### State Update Strategy

The current `uploadFile()` / `downloadFile()` calls `s.state.UpdateFile()` and `s.state.MarkUploaded()` inline after each file. With parallel workers, this creates a race condition on the shared `map[string]*FileState`.

**Solution: Deferred batch state update.**

Each goroutine returns a result struct (key, hash, timestamp, error) via a results channel. After `wg.Wait()`, the main goroutine iterates results and applies all state updates sequentially, then calls `state.Save()` once.

```go
type fileResult struct {
    Key       string
    Hash      string
    Timestamp time.Time
    Err       error
}

results := make([]fileResult, 0, len(files))
var mu sync.Mutex

// ... workers append to results with mu.Lock() ...

wg.Wait()
for _, r := range results {
    if r.Err == nil {
        s.state.UpdateFile(r.Key, r.Hash, r.Timestamp)
        s.state.MarkUploaded(r.Key)
    }
}
s.state.Save()
```

### Constraints

- R2/S3 supports high concurrency; 32 workers is conservative and safe
- Error collection via mutex; partial failures are reported but don't abort other workers
- Progress output uses a shared atomic counter (`atomic.Int64`)
- state.json is updated in bulk after all workers complete (no per-file state writes from goroutines)

## Feature 3: Session Path Remapping

### Mapping Table

| R2 Old Path | Local New Path |
|-------------|---------------|
| `-home-siyuan-workspace-Human-Replacement` | `-mnt-novita2-siyuan-workspace-Human-Replacement` |
| `-home-siyuan-workspace-Preference-Data-Annotation-Platform` | `-mnt-novita2-siyuan-workspace-Preference-Data-Annotation-Platform` |
| `-home-siyuan-workspace-research-utils` | `-mnt-novita2-siyuan-workspace-research-utils` |
| `-home-siyuan-workspace-hws` | `-mnt-novita2-siyuan-workspace-hws` |
| `-home-siyuan-workspace-data` | `-mnt-novita2-siyuan-workspace-data` |
| `-home-siyuan-workspace-agent-readings` | `-mnt-novita2-siyuan-workspace-agent-readings` |
| `-home-siyuan-workspace` | `-mnt-novita2-siyuan-workspace` |
| `-home-siyuan` | Keep as-is (not a workspace path) |
| `*--claude-worktrees-*` | Keep as-is (temp worktree paths, no local equivalent) |

### Workflow

1. `claude-sync pull --target /tmp/r2-remap "projects/-home-siyuan-workspace-*/**"` — download old sessions to temp dir
2. Rename directories according to mapping table
3. Copy into `~/.claude/projects/` — skip files that already exist locally (local version is newer)
4. `claude-sync push projects/` — upload remapped sessions
5. `claude-sync delete "projects/-home-siyuan-workspace-*/**"` — remove old-path copies from R2

### Conflict Handling

- 6 project directories were already remapped locally in a previous session
- For those: R2 copies are older, skip on conflict (don't overwrite local)
- For files only on R2 (subagent records, tool-results): copy into local directory

## Test Environment: GPU Node 2

### Setup

1. SSH to GPU node 2 as siyuan user
2. Install latest Claude Code: `npm install -g @anthropic-ai/claude-code`
3. Install claude-sync binary (scp from local or build from source)
4. `claude-sync init` — configure same R2 bucket + passphrase
5. `claude-sync pull` — pull all remote config
6. Launch `claude` — verify baseline

### Verification Checklist (run after each deletion round)

```
[ ] claude-sync pull completes without errors
[ ] claude starts without errors
[ ] All plugins in settings.json enabledPlugins auto-install
[ ] A skill works (e.g., type /brainstorm and verify it loads)
[ ] Custom skills in skills/ directory are present and callable
```

## Progressive Deletion Rounds

### Round 0: Remap + Delete Old Sessions

**Action:**
1. Execute session path remapping workflow (Feature 3)
2. `claude-sync delete "projects/-home-siyuan-workspace-*/**"`

**Expected:** ~419 MB freed. Session history accessible under new paths.

**Backup:** `pull --target /tmp/r2-backup-round0 "projects/-home-*/**"` before deletion.

### Round 1: Delete Plugin `.git/` Directories

**Action:**
```bash
claude-sync delete "plugins/marketplaces/*/.git/**" --dry-run
claude-sync delete "plugins/marketplaces/*/.git/**"
```

**Expected:** ~3.6 MB freed. Plugins still auto-install from marketplace.

**Backup:** `pull --target /tmp/r2-backup-round1 "plugins/marketplaces/*/.git/**"`

### Round 2: Delete All Plugin Cache

**Action:**
```bash
claude-sync delete "plugins/cache/**" --dry-run
claude-sync delete "plugins/cache/**"
```

**Expected:** ~3.3 MB freed. Claude Code rebuilds cache on first startup.

**Backup:** `pull --target /tmp/r2-backup-round2 "plugins/cache/**"`

### Round 3: Delete Marketplace Source Repos

**Action:**
```bash
claude-sync delete "plugins/marketplaces/wakatime/**" --dry-run
claude-sync delete "plugins/marketplaces/OthmanAdi-planning-with-files/**" --dry-run
claude-sync delete "plugins/marketplaces/wakatime/**"
claude-sync delete "plugins/marketplaces/OthmanAdi-planning-with-files/**"
```

**Expected:** ~2.5 MB freed. Claude Code auto-clones from marketplace registry.

**Risk:** Medium — if Claude Code needs local marketplace files to bootstrap plugin installation, this will fail. In that case: rollback and keep marketplace source, only delete `.git/` (already done in Round 1).

**Backup:** `pull --target /tmp/r2-backup-round3 "plugins/marketplaces/wakatime/**" "plugins/marketplaces/OthmanAdi-*/**"`

### Rollback Procedure (any round)

```bash
# Copy backup files back into ~/.claude, then push
cp -rn /tmp/r2-backup-roundN/* ~/.claude/
claude-sync push <paths-that-were-deleted>
```

**Note:** `pull --target` exports to a directory mirroring `~/.claude` structure. To rollback, copy files back into `~/.claude` (using `cp -rn` to avoid overwriting existing files), then push. Direct push from a target dir is not supported without `--claude-dir` override.

## Post-Cleanup: .claudesyncignore Update

After all rounds pass validation, update `~/.claude/.claudesyncignore`:

```gitignore
# Large vendored forks
skills/paper-ingestion/mineru-fork/

# Plugin git repos (auto-cloned on install)
plugins/marketplaces/*/.git/

# Plugin cache (auto-rebuilt on startup)
plugins/cache/

# Node modules
**/node_modules/
```

Push the updated ignore file:
```bash
claude-sync push .claudesyncignore
```

## Final R2 State

| Content | Kept | Reason |
|---------|------|--------|
| `settings.json` | Yes | Core config with enabledPlugins |
| `settings.local.json` | Yes | Machine-specific config |
| `CLAUDE.md` | Yes | Behavior rules |
| `.claudesyncignore` | Yes | Sync exclusion rules |
| `skills/*` (excl. mineru-fork) | Yes | Custom skills |
| `projects/-mnt-*` | Yes | Remapped session history + memory |
| `plugins/install-counts-cache.json` | Yes | Install count cache |
| `plugins/marketplaces/*` (excl. .git, maybe excl. wakatime/OthmanAdi) | Conditional | Depends on Round 3 result |
| Everything else | No | Redundant or auto-rebuilt |

**Expected final size:** ~10-15 MB (down from ~428 MB), depending on Round 3 outcome.

## Development Order

1. **`delete` subcommand** (Feature 1) — implement and test correctness first (simpler without concurrency)
2. **Parallel I/O** (Feature 2) — add concurrency to Push/Pull; not a blocker for delete (DeleteBatch is already efficient). Primary motivation: Round 0 needs to push ~843 remapped session files, which is slow sequentially.
3. **Session remapping** (Feature 3) — can be a shell script, doesn't need to be in claude-sync core
4. **Test environment setup** — can be done in parallel with 1-3
5. **Progressive deletion rounds 0-3** — sequential, each verified on test env
