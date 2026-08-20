package corsproxy

import (
	"container/list"
	"crypto/tls"
	"sync"
)

type certificateCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*list.Element
	lru      list.List
}

type certificateCacheEntry struct {
	hostname    string
	certificate *tls.Certificate
}

func newCertificateCache(capacity int) *certificateCache {
	return &certificateCache{
		capacity: capacity,
		entries:  make(map[string]*list.Element, capacity),
	}
}

func (c *certificateCache) Fetch(hostname string, generate func() (*tls.Certificate, error)) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[hostname]; ok {
		c.lru.MoveToFront(element)
		return element.Value.(certificateCacheEntry).certificate, nil
	}
	certificate, err := generate()
	if err != nil {
		return nil, err
	}
	element := c.lru.PushFront(certificateCacheEntry{hostname: hostname, certificate: certificate})
	c.entries[hostname] = element
	if c.lru.Len() > c.capacity {
		oldest := c.lru.Back()
		entry := oldest.Value.(certificateCacheEntry)
		delete(c.entries, entry.hostname)
		c.lru.Remove(oldest)
	}
	return certificate, nil
}
