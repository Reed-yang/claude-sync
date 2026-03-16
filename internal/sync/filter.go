package sync

import (
	"os"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// Filter controls which files are included in sync operations.
type Filter struct {
	paths       []string
	ignorer     *ignore.GitIgnore
	hasPathArgs bool
}

// NewFilter creates a filter from CLI path arguments and an optional ignore file path.
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
		p = strings.TrimPrefix(p, "./")
		p = strings.TrimPrefix(p, "/")
		result = append(result, p)
	}
	return result
}
