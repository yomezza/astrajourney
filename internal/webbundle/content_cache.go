package webbundle

import (
	"container/list"
	"context"
	"sync"

	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

// Both original and compressed bytes count toward this budget. Old revisions
// may still be requested by open pages, so they participate in the same LRU.
const contentCacheBytes = 16 << 20
const contentCacheEntries = 256

type contentItem struct {
	key     string
	content *httpkit.StaticContent
}

type contentFetch struct {
	done    chan struct{}
	content *httpkit.StaticContent
	err     error
}

type contentCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	pending map[string]*contentFetch
	lru     list.List
	bytes   int
}

func (c *contentCache) get(ctx context.Context, key string, build func() (*httpkit.StaticContent, error)) (*httpkit.StaticContent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if element := c.entries[key]; element != nil {
		c.lru.MoveToFront(element)
		content := element.Value.(contentItem).content
		c.mu.Unlock()
		return content, nil
	}
	if pending := c.pending[key]; pending != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pending.done:
			return pending.content, pending.err
		}
	}
	if c.entries == nil {
		c.entries = make(map[string]*list.Element)
		c.pending = make(map[string]*contentFetch)
	}
	pending := &contentFetch{done: make(chan struct{})}
	c.pending[key] = pending
	c.mu.Unlock()

	// Disk reads and compression do not block unrelated cached files.
	content, err := build()
	c.mu.Lock()
	pending.content, pending.err = content, err
	if err == nil && content.Size() <= contentCacheBytes {
		for c.bytes+content.Size() > contentCacheBytes || c.lru.Len() >= contentCacheEntries {
			oldest := c.lru.Back()
			item := oldest.Value.(contentItem)
			delete(c.entries, item.key)
			c.bytes -= item.content.Size()
			c.lru.Remove(oldest)
		}
		c.entries[key] = c.lru.PushFront(contentItem{key, content})
		c.bytes += content.Size()
	}
	delete(c.pending, key)
	close(pending.done)
	c.mu.Unlock()
	return content, err
}
