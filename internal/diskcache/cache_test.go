package diskcache

import (
	"testing"
	"time"
)

func TestPutGetAndLRU(t *testing.T) {
	c, e := New(t.TempDir(), 4)
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Put("a", []byte("aa")); e != nil {
		t.Fatal(e)
	}
	time.Sleep(time.Millisecond)
	if e = c.Put("b", []byte("bb")); e != nil {
		t.Fatal(e)
	}
	if _, ok, e := c.Get("a"); e != nil || !ok {
		t.Fatal("missing a")
	}
	time.Sleep(time.Millisecond)
	if e = c.Put("c", []byte("cc")); e != nil {
		t.Fatal(e)
	}
	if _, ok, _ := c.Get("b"); ok {
		t.Fatal("oldest entry retained")
	}
	if got, ok, e := c.Get("a"); e != nil || !ok || string(got) != "aa" {
		t.Fatal("a missing")
	}
}
