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
	if !f.ShouldInclude("anything/at/all.txt") {
		t.Error("expected all files included when no path args")
	}
}

func TestFilter_ShouldInclude_WithIgnoreFile(t *testing.T) {
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
		{"skills/paper-ingestion/SKILL.md", false},
		{"settings.json", false},
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
	if paths != nil {
		t.Fatalf("expected nil sync paths, got %v", paths)
	}
}
