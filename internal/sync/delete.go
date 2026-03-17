package sync

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/tawanorg/claude-sync/internal/storage"
)

type DeleteRemoteOptions struct {
	DryRun bool
}

type DeleteRemoteResult struct {
	Matched []storage.ObjectInfo
	Deleted int
	Errors  []error
}

func matchKeys(keys []string, patterns []string) []string {
	var matched []string
	seen := make(map[string]bool)
	for _, key := range keys {
		for _, pattern := range patterns {
			ok, err := doublestar.Match(pattern, key)
			if err != nil {
				continue
			}
			if ok && !seen[key] {
				matched = append(matched, key)
				seen[key] = true
				break
			}
		}
	}
	return matched
}

func (s *Syncer) DeleteRemote(ctx context.Context, patterns []string, opts DeleteRemoteOptions) (*DeleteRemoteResult, error) {
	objects, err := s.storage.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list remote files: %w", err)
	}

	// Only process .age files (matching Pull behavior)
	keys := make([]string, 0, len(objects))
	objMap := make(map[string]storage.ObjectInfo)
	for _, obj := range objects {
		if !strings.HasSuffix(obj.Key, ".age") {
			continue
		}
		key := strings.TrimSuffix(obj.Key, ".age")
		keys = append(keys, key)
		objMap[key] = obj
	}
	sort.Strings(keys)

	matchedKeys := matchKeys(keys, patterns)

	result := &DeleteRemoteResult{}
	for _, key := range matchedKeys {
		result.Matched = append(result.Matched, objMap[key])
	}

	if opts.DryRun || len(matchedKeys) == 0 {
		return result, nil
	}

	remoteKeys := make([]string, 0, len(matchedKeys))
	for _, key := range matchedKeys {
		remoteKeys = append(remoteKeys, objMap[key].Key)
	}

	const batchSize = 1000
	for i := 0; i < len(remoteKeys); i += batchSize {
		end := i + batchSize
		if end > len(remoteKeys) {
			end = len(remoteKeys)
		}
		batch := remoteKeys[i:end]
		if err := s.storage.DeleteBatch(ctx, batch); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("batch delete failed at offset %d: %w", i, err))
		} else {
			result.Deleted += len(batch)
		}
	}

	for _, key := range matchedKeys {
		s.state.RemoveFile(key)
	}
	if err := s.state.Save(); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("failed to save state: %w", err))
	}

	return result, nil
}
