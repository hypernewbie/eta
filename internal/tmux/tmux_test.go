package tmux

import "testing"

func TestParseSessionsReadsListFormat(t *testing.T) {
	sessions := parseSessions("work\t3\t1\t1754006400\nscratch\t1\t0\t1754010000\n")
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	if sessions[0].Name != "work" || sessions[0].Windows != 3 || !sessions[0].Attached {
		t.Errorf("first = %+v", sessions[0])
	}
	if sessions[1].Attached {
		t.Errorf("second should be detached: %+v", sessions[1])
	}
	if sessions[0].Created.IsZero() {
		t.Error("created time was not parsed")
	}
}

func TestParseSessionsToleratesJunk(t *testing.T) {
	if got := parseSessions(""); len(got) != 0 {
		t.Errorf("empty output = %+v, want none", got)
	}
	if got := parseSessions("broken-line\n"); len(got) != 0 {
		t.Errorf("short line = %+v, want none", got)
	}
}

// Names reach a command line, so anything that could be read as another
// argument, a tmux target, or a shell construct must be refused.
func TestValidNameRejectsUnsafeInput(t *testing.T) {
	for _, name := range []string{
		"", "-x", "a b", "a;rm -rf /", "a:1", "a.1", "a$(id)", "a/b", "a\tb",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		if ValidName(name) {
			t.Errorf("ValidName(%q) = true, want false", name)
		}
	}
	for _, name := range []string{"work", "build-2", "a", "A_b-9"} {
		if !ValidName(name) {
			t.Errorf("ValidName(%q) = false, want true", name)
		}
	}
}

func TestAttachArgvRefusesUnsafeNames(t *testing.T) {
	if _, err := AttachArgv("a;id"); err == nil {
		t.Fatal("unsafe name accepted")
	}
	argv, err := AttachArgv("work")
	if err != nil {
		t.Fatal(err)
	}
	// -A so a session that vanished between listing and opening is
	// created rather than erroring.
	want := []string{"tmux", "new-session", "-A", "-s", "work"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v", argv)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}
