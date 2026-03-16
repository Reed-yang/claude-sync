# Selective Push/Pull Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add selective push/pull with CLI path arguments, `.claudesyncignore` support, and `--target` flag for pull.

**Architecture:** New `Filter` type in `internal/sync/filter.go` encapsulates all filtering logic. All public Syncer methods and `GetLocalFiles`/`DetectChanges` accept an optional `*Filter`. CLI commands construct the filter from positional args and `.claudesyncignore`, passing it through. `--target` overrides `claudeDir` in Pull and suppresses state updates.

**Tech Stack:** Go 1.24, `github.com/sabhiram/go-gitignore` for `.claudesyncignore` parsing, cobra for CLI.

---

## Chunk 1: Filter Core + Ignore File

### Task 1: Add go-gitignore dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add dependency**

Run: `cd /tmp/claude-sync && /usr/local/go/bin/go get github.com/sabhiram/go-gitignore@latest`

- [ ] **Step 2: Verify**

Run: `cd /tmp/claude-sync && /usr/local/go/bin/go mod tidy && grep go-gitignore go.mod`
Expected: line showing `github.com/sabhiram/go-gitignore`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add go-gitignore dependency for .claudesyncignore support"
```

### Task 2: Create Filter type with tests

**Files:**
- Create: `internal/sync/filter.go`
- Create: `internal/sync/filter_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/sync/filter_test.go
package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewFilter_NoPaths_NoIgnore(t *testing.T) {
	f := NewFilter(nil, "")
	if f == nil {
		t.Fatal("expected non-nil filter")
	}
	if f.HasPathArgs() {
		t.Error("expected HasPathArgs() == false")
	}
}

func TestNewFilter_WithPaths(t *testing.T) {
	f := NewFilter([]string{"skills/", "settings.json"}, "")
	if !f.HasPathArgs() {
		t.Error("expected HasPathArgs() == true")
	}
}

func TestFilter_ShouldInclude_WithPaths(t *testing.T) {
	f := NewFilter([]string{"skills/", "settings.json"}, "")

	tests := []struct {
		path string
		want bool
	}{
		{"skills/paper-ingestion/SKILL.md", true},
		{"skills/afk/SKILL.md", true},
		{"settings.json", true},
		{"CLAUDE.md", false},
		{"projects/foo/bar.jsonl", false},
		{"plugins/cache/foo.json", false},
	}

	for _, tt := range tests {
		if got := f.ShouldInclude(tt.path); got != tt.want {
			t.Errorf("ShouldInclude(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestFilter_ShouldInclude_NoPaths(t *testing.T) {
	f := NewFilter(nil, "")
	// Without path args, everything is included
	if !f.ShouldInclude("anything/at/all.txt") {
		t.Error("expected all files included when no path args")
	}
}

func TestFilter_ShouldInclude_WithIgnoreFile(t *testing.T) {
	// Create temp .claudesyncignore
	tmpDir := t.TempDir()
	ignoreFile := filepath.Join(tmpDir, ".claudesyncignore")
	err := os.WriteFile(ignoreFile, []byte("settings.local.json\nprojects/\n*.bak\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	f := NewFilter(nil, ignoreFile)

	tests := []struct {
		path string
		want bool
	}{
		{"settings.local.json", false},
		{"settings.json", true},
		{"projects/foo/bar.jsonl", false},
		{"skills/test.md", true},
		{"something.bak", false},
		{"CLAUDE.md", true},
	}

	for _, tt := range tests {
		if got := f.ShouldInclude(tt.path); got != tt.want {
			t.Errorf("ShouldInclude(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestFilter_ShouldInclude_PathsAndIgnore(t *testing.T) {
	tmpDir := t.TempDir()
	ignoreFile := filepath.Join(tmpDir, ".claudesyncignore")
	err := os.WriteFile(ignoreFile, []byte("skills/paper-*\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	f := NewFilter([]string{"skills/"}, ignoreFile)

	tests := []struct {
		path string
		want bool
	}{
		{"skills/afk/SKILL.md", true},
		{"skills/paper-ingestion/SKILL.md", false}, // ignored
		{"settings.json", false},                    // not in path args
	}

	for _, tt := range tests {
		if got := f.ShouldInclude(tt.path); got != tt.want {
			t.Errorf("ShouldInclude(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestFilter_SyncPaths_WithPathArgs(t *testing.T) {
	f := NewFilter([]string{"skills/", "settings.json"}, "")
	paths := f.SyncPaths()
	if len(paths) != 2 {
		t.Fatalf("expected 2 sync paths, got %d", len(paths))
	}
}

func TestFilter_SyncPaths_WithoutPathArgs(t *testing.T) {
	f := NewFilter(nil, "")
	paths := f.SyncPaths()
	// Should return nil (caller uses default SyncPaths)
	if paths != nil {
		t.Fatalf("expected nil sync paths, got %v", paths)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /tmp/claude-sync && /usr/local/go/bin/go test ./internal/sync/ -run TestFilter -v`
Expected: compilation error — `NewFilter` undefined

- [ ] **Step 3: Implement Filter**

```go
// internal/sync/filter.go
package sync

import (
	"os"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// Filter controls which files are included in sync operations.
type Filter struct {
	paths      []string          // CLI path arguments (relative to claudeDir)
	ignorer    *ignore.GitIgnore // .claudesyncignore rules
	hasPathArgs bool
}

// NewFilter creates a filter from CLI path arguments and an optional ignore file path.
// If paths is nil/empty, all files pass the path check.
// If ignoreFile is empty or doesn't exist, no ignore rules are applied.
func NewFilter(paths []string, ignoreFile string) *Filter {
	f := &Filter{
		paths:       normalizePaths(paths),
		hasPathArgs: len(paths) > 0,
	}

	if ignoreFile != "" {
		if _, err := os.Stat(ignoreFile); err == nil {
			f.ignorer, _ = ignore.CompileIgnoreFile(ignoreFile)
		}
	}

	return f
}

// HasPathArgs returns true if CLI path arguments were provided.
func (f *Filter) HasPathArgs() bool {
	return f.hasPathArgs
}

// ShouldInclude returns true if the given relative path passes both
// the path filter and the ignore rules.
func (f *Filter) ShouldInclude(relPath string) bool {
	// Check CLI path args first
	if f.hasPathArgs {
		matched := false
		for _, p := range f.paths {
			if relPath == p || strings.HasPrefix(relPath, p) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check ignore rules
	if f.ignorer != nil {
		if f.ignorer.MatchesPath(relPath) {
			return false
		}
	}

	return true
}

// SyncPaths returns the CLI paths to use as walk roots, or nil if
// no path args were provided (caller should use default SyncPaths).
func (f *Filter) SyncPaths() []string {
	if !f.hasPathArgs {
		return nil
	}
	return f.paths
}

// normalizePaths cleans up path arguments: trims whitespace,
// ensures directory paths end with "/".
func normalizePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	result := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Normalize: remove leading "./" or "/"
		p = strings.TrimPrefix(p, "./")
		p = strings.TrimPrefix(p, "/")
		result = append(result, p)
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /tmp/claude-sync && /usr/local/go/bin/go test ./internal/sync/ -run TestFilter -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sync/filter.go internal/sync/filter_test.go
git commit -m "feat: add Filter type with path args and .claudesyncignore support"
```

## Chunk 2: Wire Filter into Syncer Methods

### Task 3: Update GetLocalFiles and DetectChanges

**Files:**
- Modify: `internal/sync/state.go:146` (`GetLocalFiles`)
- Modify: `internal/sync/state.go:200` (`DetectChanges`)

- [ ] **Step 1: Modify GetLocalFiles to accept and apply Filter**

In `internal/sync/state.go`, change `GetLocalFiles` signature and add filtering:

```go
// GetLocalFiles returns local files matching the given sync paths, filtered by f.
// If f is nil, no additional filtering is applied.
func GetLocalFiles(claudeDir string, syncPaths []string, f *Filter) (map[string]os.FileInfo, error) {
	// If filter has explicit path args, use those as walk roots instead
	if f != nil && f.HasPathArgs() {
		syncPaths = f.SyncPaths()
	}

	files := make(map[string]os.FileInfo)

	for _, syncPath := range syncPaths {
		fullPath := filepath.Join(claudeDir, syncPath)

		info, err := os.Stat(fullPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to stat %s: %w", syncPath, err)
		}

		if info.IsDir() {
			err := filepath.Walk(fullPath, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if fi.IsDir() {
					return nil
				}
				// Skip symlinks
				if fi.Mode()&os.ModeSymlink != 0 {
					return nil
				}

				relPath, _ := filepath.Rel(claudeDir, path)

				// Apply filter
				if f != nil && !f.ShouldInclude(relPath) {
					return nil
				}

				files[relPath] = fi
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("failed to walk %s: %w", syncPath, err)
			}
		} else {
			// Skip symlinks
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			// Apply filter for single files
			if f != nil && !f.ShouldInclude(syncPath) {
				continue
			}
			files[syncPath] = info
		}
	}

	return files, nil
}
```

- [ ] **Step 2: Update DetectChanges to accept and apply Filter**

In `internal/sync/state.go`, change `DetectChanges`:

```go
func (s *SyncState) DetectChanges(claudeDir string, syncPaths []string, f *Filter) ([]FileChange, error) {
	var changes []FileChange

	localFiles, err := GetLocalFiles(claudeDir, syncPaths, f)
	if err != nil {
		return nil, err
	}

	// Check for new or modified files
	for relPath, info := range localFiles {
		fullPath := filepath.Join(claudeDir, relPath)
		hash, err := HashFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to hash %s: %w", relPath, err)
		}

		existing := s.GetFile(relPath)
		if existing == nil {
			changes = append(changes, FileChange{
				Path:      relPath,
				Action:    "add",
				LocalHash: hash,
				LocalSize: info.Size(),
				LocalTime: info.ModTime(),
			})
		} else if existing.Hash != hash {
			changes = append(changes, FileChange{
				Path:      relPath,
				Action:    "modify",
				LocalHash: hash,
				LocalSize: info.Size(),
				LocalTime: info.ModTime(),
			})
		}
	}

	// Check for deleted files — only within filter scope
	for relPath := range s.Files {
		if f != nil && !f.ShouldInclude(relPath) {
			continue // Outside filter scope, don't treat as deleted
		}
		if _, exists := localFiles[relPath]; !exists {
			changes = append(changes, FileChange{
				Path:   relPath,
				Action: "delete",
			})
		}
	}

	return changes, nil
}
```

- [ ] **Step 3: Fix all compilation errors from signature change**

Every caller of `GetLocalFiles` and `DetectChanges` now needs the extra `*Filter` parameter. Pass `nil` for now to maintain existing behavior. Callers:

In `internal/sync/sync.go`:
- Line 109: `s.state.DetectChanges(s.claudeDir, config.SyncPaths)` → add `, nil`
- Line 199: `GetLocalFiles(s.claudeDir, config.SyncPaths)` → add `, nil`
- Line 283: `s.state.DetectChanges(s.claudeDir, config.SyncPaths)` → add `, nil`
- Line 423: `GetLocalFiles(s.claudeDir, config.SyncPaths)` → add `, nil`
- Line 502: `GetLocalFiles(s.claudeDir, config.SyncPaths)` → add `, nil`

In `cmd/claude-sync/main.go`:
- Line 1818: `sync.GetLocalFiles(claudeDir, config.SyncPaths)` → add `, nil`
- Line 1934: `sync.GetLocalFiles(claudeDir, config.SyncPaths)` → add `, nil`

- [ ] **Step 4: Run all tests to verify nothing broke**

Run: `cd /tmp/claude-sync && /usr/local/go/bin/go build ./... && /usr/local/go/bin/go test ./internal/sync/ -v`
Expected: all PASS, no compilation errors

- [ ] **Step 5: Commit**

```bash
git add internal/sync/state.go internal/sync/sync.go cmd/claude-sync/main.go
git commit -m "refactor: add Filter parameter to GetLocalFiles and DetectChanges"
```

### Task 4: Wire Filter into all Syncer public methods

**Files:**
- Modify: `internal/sync/sync.go`

- [ ] **Step 1: Add filter field to Syncer and update all public methods**

Add `filter *Filter` to method signatures. Replace the `nil` placeholders from Task 3 with `s.filter`:

```go
// In Syncer struct, add:
type Syncer struct {
	storage    storage.Storage
	encryptor  *crypto.Encryptor
	state      *SyncState
	claudeDir  string
	quiet      bool
	onProgress ProgressFunc
	filter     *Filter // NEW
}

// Add setter:
func (s *Syncer) SetFilter(f *Filter) {
	s.filter = f
}
```

Then update each method to use `s.filter`:

**Push (line 109):**
```go
changes, err := s.state.DetectChanges(s.claudeDir, config.SyncPaths, s.filter)
```

**Pull (line 199):**
```go
localFiles, err := GetLocalFiles(s.claudeDir, config.SyncPaths, s.filter)
```

Also in Pull, filter the remote file list after building `remoteFiles` map (add after line 196):
```go
// Filter remote files if filter is set
if s.filter != nil {
	for localPath := range remoteFiles {
		if !s.filter.ShouldInclude(localPath) {
			delete(remoteFiles, localPath)
		}
	}
}
```

**Status (line 283):**
```go
return s.state.DetectChanges(s.claudeDir, config.SyncPaths, s.filter)
```

**PreviewPull (line 423):**
```go
localFiles, err := GetLocalFiles(s.claudeDir, config.SyncPaths, s.filter)
```

Also add remote filtering after building `remoteFiles` map in PreviewPull:
```go
if s.filter != nil {
	for localPath := range remoteFiles {
		if !s.filter.ShouldInclude(localPath) {
			delete(remoteFiles, localPath)
		}
	}
}
```

**Diff (line 502):**
```go
localFiles, err := GetLocalFiles(s.claudeDir, config.SyncPaths, s.filter)
```

Also add remote filtering in Diff:
```go
if s.filter != nil {
	for localPath := range remoteFiles {
		if !s.filter.ShouldInclude(localPath) {
			delete(remoteFiles, localPath)
		}
	}
}
```

- [ ] **Step 2: Run all tests**

Run: `cd /tmp/claude-sync && /usr/local/go/bin/go build ./... && /usr/local/go/bin/go test ./internal/sync/ -v`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/sync/sync.go
git commit -m "feat: wire Filter into all Syncer public methods"
```

## Chunk 3: --target Flag and CLI Wiring

### Task 5: Add --target support to Pull

**Files:**
- Modify: `internal/sync/sync.go` (`Pull` method, `downloadFile`)

- [ ] **Step 1: Add PullOptions struct and update Pull signature**

```go
// PullOptions configures pull behavior.
type PullOptions struct {
	TargetDir string // If set, download to this dir instead of claudeDir; skip state updates
}

func (s *Syncer) Pull(ctx context.Context, opts *PullOptions) (*SyncResult, error) {
	result := &SyncResult{}

	targetDir := s.claudeDir
	skipState := false
	if opts != nil && opts.TargetDir != "" {
		targetDir = opts.TargetDir
		skipState = true
	}

	s.progress(ProgressEvent{Action: "scan", Path: "Fetching remote file list..."})

	// List all remote objects
	remoteObjects, err := s.storage.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list remote objects: %w", err)
	}

	if len(remoteObjects) == 0 {
		s.progress(ProgressEvent{Action: "scan", Complete: true})
		return result, nil
	}

	// Build remote file map
	remoteFiles := make(map[string]storage.ObjectInfo)
	for _, obj := range remoteObjects {
		if !strings.HasSuffix(obj.Key, ".age") {
			continue
		}
		localPath := s.localPath(obj.Key)
		remoteFiles[localPath] = obj
	}

	// Filter remote files
	if s.filter != nil {
		for localPath := range remoteFiles {
			if !s.filter.ShouldInclude(localPath) {
				delete(remoteFiles, localPath)
			}
		}
	}

	// Get current local files (from target dir)
	localFiles, err := GetLocalFiles(targetDir, config.SyncPaths, s.filter)
	if err != nil {
		// Target dir may not exist yet, that's fine
		localFiles = make(map[string]os.FileInfo)
	}

	// Build list of files to download
	type downloadTask struct {
		localPath string
		remoteObj storage.ObjectInfo
	}
	var toDownload []downloadTask

	for localPath, remoteObj := range remoteFiles {
		localInfo, localExists := localFiles[localPath]
		stateFile := s.state.GetFile(localPath)

		shouldDownload := false

		if !localExists {
			shouldDownload = true
		} else if stateFile != nil && !skipState {
			if remoteObj.LastModified.After(stateFile.Uploaded) {
				localHash, _ := HashFile(filepath.Join(targetDir, localPath))
				if localHash != stateFile.Hash {
					result.Conflicts = append(result.Conflicts, localPath)
					s.progress(ProgressEvent{Action: "conflict", Path: localPath})
					if err := s.handleConflict(ctx, localPath, remoteObj); err != nil {
						result.Errors = append(result.Errors, err)
					}
					continue
				}
				shouldDownload = true
			}
		} else if localInfo.ModTime().Before(remoteObj.LastModified) {
			shouldDownload = true
		}

		if shouldDownload {
			toDownload = append(toDownload, downloadTask{localPath, remoteObj})
		}
	}

	// Download files with progress
	total := len(toDownload)
	for i, task := range toDownload {
		s.progress(ProgressEvent{
			Action:  "download",
			Path:    task.localPath,
			Size:    task.remoteObj.Size,
			Current: i + 1,
			Total:   total,
		})

		if err := s.downloadFileTo(ctx, task.localPath, task.remoteObj.Key, targetDir, skipState); err != nil {
			s.progress(ProgressEvent{
				Action: "download",
				Path:   task.localPath,
				Error:  err,
			})
			result.Errors = append(result.Errors, fmt.Errorf("%s: %w", task.localPath, err))
			continue
		}
		result.Downloaded = append(result.Downloaded, task.localPath)
	}

	s.progress(ProgressEvent{Action: "download", Complete: true, Total: total})

	if !skipState {
		s.state.LastPull = time.Now()
		s.state.LastSync = time.Now()
		if err := s.state.Save(); err != nil {
			return result, fmt.Errorf("failed to save state: %w", err)
		}
	}

	return result, nil
}
```

- [ ] **Step 2: Add downloadFileTo helper**

```go
// downloadFileTo downloads a file to the specified directory, optionally skipping state updates.
func (s *Syncer) downloadFileTo(ctx context.Context, relativePath, remoteKey, targetDir string, skipState bool) error {
	encrypted, err := s.storage.Download(ctx, remoteKey)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}

	data, err := s.encryptor.Decrypt(encrypted)
	if err != nil {
		return fmt.Errorf("failed to decrypt: %w", err)
	}

	fullPath := filepath.Join(targetDir, relativePath)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if !skipState {
		info, _ := os.Stat(fullPath)
		hash, _ := HashFile(fullPath)
		s.state.UpdateFile(relativePath, info, hash)
		s.state.MarkUploaded(relativePath)
	}

	return nil
}
```

- [ ] **Step 3: Update old downloadFile to delegate to downloadFileTo**

```go
func (s *Syncer) downloadFile(ctx context.Context, relativePath, remoteKey string) error {
	return s.downloadFileTo(ctx, relativePath, remoteKey, s.claudeDir, false)
}
```

- [ ] **Step 4: Fix all callers of Pull — add nil opts**

In `cmd/claude-sync/main.go`, every call to `syncer.Pull(ctx)` → `syncer.Pull(ctx, nil)`. Search for `syncer.Pull(ctx)` and `syncer.Pull(ctx)` patterns.

- [ ] **Step 5: Build and test**

Run: `cd /tmp/claude-sync && /usr/local/go/bin/go build ./... && /usr/local/go/bin/go test ./internal/sync/ -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/sync/sync.go cmd/claude-sync/main.go
git commit -m "feat: add --target support to Pull with state suppression"
```

### Task 6: Wire CLI positional args and --target flag

**Files:**
- Modify: `cmd/claude-sync/main.go` (`pushCmd`, `pullCmd`)

- [ ] **Step 1: Update pushCmd to accept positional args**

In `pushCmd()`, change the `RunE` to construct a filter:

```go
func pushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push [paths...]",
		Short: "Upload local changes to cloud storage",
		Long: `Encrypt and upload changed files from ~/.claude to cloud storage.

Examples:
  claude-sync push                    # Push all changes
  claude-sync push skills/            # Push only skills directory
  claude-sync push skills/ CLAUDE.md  # Push specific paths`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			syncer, err := sync.NewSyncer(cfg, quiet)
			if err != nil {
				return err
			}

			// Build filter from CLI args and .claudesyncignore
			ignoreFile := filepath.Join(config.ClaudeDir(), ".claudesyncignore")
			filter := sync.NewFilter(args, ignoreFile)
			syncer.SetFilter(filter)

			// ... rest unchanged ...
```

- [ ] **Step 2: Update pullCmd to accept positional args and --target**

```go
func pullCmd() *cobra.Command {
	var dryRun, force bool
	var targetDir string

	cmd := &cobra.Command{
		Use:   "pull [paths...]",
		Short: "Download remote changes from cloud storage",
		Long: `Download and decrypt changed files from cloud storage to ~/.claude.

Examples:
  claude-sync pull                         # Pull all changes
  claude-sync pull skills/ CLAUDE.md       # Pull specific paths
  claude-sync pull --target /tmp/backup    # Pull to custom directory
  claude-sync pull --dry-run               # Preview changes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			syncer, err := sync.NewSyncer(cfg, quiet)
			if err != nil {
				return err
			}

			// Build filter
			ignoreFile := filepath.Join(config.ClaudeDir(), ".claudesyncignore")
			filter := sync.NewFilter(args, ignoreFile)
			syncer.SetFilter(filter)

			// Build pull options
			var pullOpts *sync.PullOptions
			if targetDir != "" {
				pullOpts = &sync.PullOptions{TargetDir: targetDir}
			}

			ctx := context.Background()

			// ... first pull check and dry-run logic ...
			// Pass pullOpts to syncer.Pull(ctx, pullOpts)
```

Add the flag registration:
```go
	cmd.Flags().StringVar(&targetDir, "target", "", "Download to custom directory (read-only, does not update state)")
```

- [ ] **Step 3: Build and verify**

Run: `cd /tmp/claude-sync && /usr/local/go/bin/go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add cmd/claude-sync/main.go
git commit -m "feat: wire CLI positional args and --target flag for push/pull"
```

## Chunk 4: Integration Test and Build

### Task 7: Manual integration test

- [ ] **Step 1: Build the binary**

Run: `cd /tmp/claude-sync && /usr/local/go/bin/go build -o bin/claude-sync ./cmd/claude-sync`

- [ ] **Step 2: Test selective push (dry run via status)**

```bash
export PATH="/tmp/claude-sync/bin:$PATH"
# Check that filter works with status
claude-sync status skills/
# Should show only changes under skills/
```

- [ ] **Step 3: Test selective pull with --target**

```bash
claude-sync pull --target /tmp/test-pull skills/
ls /tmp/test-pull/skills/
# Should only contain skills files
```

- [ ] **Step 4: Test .claudesyncignore**

```bash
echo "projects/" > ~/.claude/.claudesyncignore
echo "*.bak" >> ~/.claude/.claudesyncignore
claude-sync status
# Should not list any projects/ files
```

- [ ] **Step 5: Test backward compatibility (no args)**

```bash
claude-sync pull --dry-run
# Should show all files as before
```

- [ ] **Step 6: Install updated binary**

```bash
cp /tmp/claude-sync/bin/claude-sync /home/siyuan/.local/bin/claude-sync
```

- [ ] **Step 7: Final commit with any fixes**

```bash
git add -A
git commit -m "feat: selective push/pull with path args, .claudesyncignore, and --target"
```
