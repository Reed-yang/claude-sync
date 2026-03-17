package sync

import (
	"fmt"
	gosync "sync"
	"sync/atomic"
)

const maxWorkers = 32

type fileResult struct {
	Key string
	Err error
}

func parallelDo[T any](items []T, fn func(T) fileResult) []fileResult {
	results := make([]fileResult, len(items))
	sem := make(chan struct{}, maxWorkers)
	var wg gosync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, it T) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					results[idx] = fileResult{Err: fmt.Errorf("panic: %v", r)}
				}
			}()
			results[idx] = fn(it)
		}(i, item)
	}

	wg.Wait()
	return results
}

type progressCounter struct {
	count atomic.Int64
	total int64
}

func newProgressCounter(total int) *progressCounter {
	return &progressCounter{total: int64(total)}
}

func (p *progressCounter) increment() int64 {
	return p.count.Add(1)
}
