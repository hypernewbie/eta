package rangecache

import "testing"

func TestLRUEvictsAndCopies(t *testing.T) {
	c := New(4)
	c.Put("a", []byte("ab"))
	c.Put("b", []byte("cd"))
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a missing")
	}
	c.Put("c", []byte("ef"))
	if _, ok := c.Get("b"); ok {
		t.Fatal("b not evicted")
	}
	v, _ := c.Get("a")
	v[0] = 'x'
	v, _ = c.Get("a")
	if string(v) != "ab" {
		t.Fatal("cache leaked body")
	}
}
