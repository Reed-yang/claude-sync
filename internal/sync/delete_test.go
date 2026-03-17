package sync

import (
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

func TestMatchRemoteKeys(t *testing.T) {
	keys := []string{
		"plugins/marketplaces/wakatime/.git/HEAD",
		"plugins/marketplaces/wakatime/.git/config",
		"plugins/marketplaces/wakatime/src/index.ts",
		"plugins/marketplaces/claude-hud/src/index.ts",
		"plugins/cache/superpowers/4.3.1/skills/foo.md",
		"plugins/cache/superpowers/5.0.2/skills/bar.md",
		"projects/-home-siyuan-workspace-Human-Replacement/abc.jsonl",
		"projects/-mnt-novita2-siyuan-workspace/def.jsonl",
		"settings.json",
	}

	tests := []struct {
		name     string
		patterns []string
		expected []string
	}{
		{
			name:     "match .git directories",
			patterns: []string{"plugins/marketplaces/*/.git/**"},
			expected: []string{
				"plugins/marketplaces/wakatime/.git/HEAD",
				"plugins/marketplaces/wakatime/.git/config",
			},
		},
		{
			name:     "match all plugin cache",
			patterns: []string{"plugins/cache/**"},
			expected: []string{
				"plugins/cache/superpowers/4.3.1/skills/foo.md",
				"plugins/cache/superpowers/5.0.2/skills/bar.md",
			},
		},
		{
			name:     "match old-path sessions",
			patterns: []string{"projects/-home-*/**"},
			expected: []string{
				"projects/-home-siyuan-workspace-Human-Replacement/abc.jsonl",
			},
		},
		{
			name:     "multiple patterns union",
			patterns: []string{"plugins/cache/**", "settings.json"},
			expected: []string{
				"plugins/cache/superpowers/4.3.1/skills/foo.md",
				"plugins/cache/superpowers/5.0.2/skills/bar.md",
				"settings.json",
			},
		},
		{
			name:     "no match",
			patterns: []string{"nonexistent/**"},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := matchKeys(keys, tt.patterns)
			if len(matched) != len(tt.expected) {
				t.Errorf("expected %d matches, got %d: %v", len(tt.expected), len(matched), matched)
				return
			}
			for i, key := range matched {
				if key != tt.expected[i] {
					t.Errorf("match[%d]: expected %q, got %q", i, tt.expected[i], key)
				}
			}
		})
	}
}

func TestDoublestarPatterns(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		match   bool
	}{
		{"plugins/marketplaces/*/.git/**", "plugins/marketplaces/wakatime/.git/HEAD", true},
		{"plugins/marketplaces/*/.git/**", "plugins/marketplaces/wakatime/src/index.ts", false},
		{"projects/-home-*/**", "projects/-home-siyuan-workspace/foo.jsonl", true},
		{"projects/-home-*/**", "projects/-mnt-novita2/foo.jsonl", false},
		{"plugins/cache/**", "plugins/cache/a/b/c.md", true},
		{"**/.git/**", "plugins/marketplaces/wakatime/.git/objects/pack/foo.idx", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			matched, err := doublestar.Match(tt.pattern, tt.path)
			if err != nil {
				t.Fatalf("doublestar.Match error: %v", err)
			}
			if matched != tt.match {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.path, matched, tt.match)
			}
		})
	}
}
