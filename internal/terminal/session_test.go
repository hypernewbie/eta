package terminal

import (
	"strings"
	"testing"
	"time"
)

func TestSessionAcceptsInputAndProducesOutput(t *testing.T) {
	m := NewManager()
	id, e := m.Start(t.TempDir(), 80, 24)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(id)
	s, _ := m.Get(id)
	if e = s.Input([]byte("printf eta-terminal\\nexit\\n")); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		out, _, _ := s.Output(0)
		if strings.Contains(string(out), "eta-terminal") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	out, _, _ := s.Output(0)
	t.Fatalf("output=%q", out)
}
