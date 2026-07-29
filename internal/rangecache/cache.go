// Package rangecache provides a bounded in-memory LRU for hot remote ranges.
package rangecache

import (
	"container/list"
	"sync"
)

type item struct {
	key  string
	body []byte
}
type Cache struct {
	mu          sync.Mutex
	limit, used int64
	order       *list.List
	items       map[string]*list.Element
}

func New(limit int64) *Cache {
	return &Cache{limit: limit, order: list.New(), items: map[string]*list.Element{}}
}
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e)
	return append([]byte(nil), e.Value.(item).body...), true
}
func (c *Cache) Put(key string, body []byte) {
	if int64(len(body)) > c.limit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[key]; ok {
		old := e.Value.(item)
		c.used -= int64(len(old.body))
		e.Value = item{key, append([]byte(nil), body...)}
		c.used += int64(len(body))
		c.order.MoveToFront(e)
	} else {
		c.items[key] = c.order.PushFront(item{key, append([]byte(nil), body...)})
		c.used += int64(len(body))
	}
	for c.used > c.limit {
		e := c.order.Back()
		old := e.Value.(item)
		delete(c.items, old.key)
		c.used -= int64(len(old.body))
		c.order.Remove(e)
	}
}
