package transfer

import (
	"bytes"
	"testing"
)

func TestManifestVerifiesVariableFinalChunk(t *testing.T) {
	m, e := BuildManifest(bytes.NewBufferString("eta transfer"), 4)
	if e != nil {
		t.Fatal(e)
	}
	if len(m.Chunks) != 3 || m.Size != 12 {
		t.Fatalf("%+v", m)
	}
	if e := m.Verify(2, []byte("sfer")); e != nil {
		t.Fatal(e)
	}
	if e := m.Verify(1, []byte("nope")); e == nil {
		t.Fatal("accepted corrupt chunk")
	}
}
