package transfer

import (
	"path/filepath"
	"testing"
)

func TestPersistentJobsSurviveRestartAndMarkInFlightInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	jobs, err := NewPersistentJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	active := jobs.StartNamed(4, "active.txt")
	jobs.Progress(active.ID, 2)
	finished := jobs.StartNamed(1, "finished.txt")
	jobs.Finish(finished.ID, nil)

	reloaded, err := NewPersistentJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	gotActive, found := reloaded.Get(active.ID)
	if !found || !gotActive.Done || gotActive.Error != "interrupted by Eta restart" || gotActive.Completed != 2 {
		t.Fatalf("active=%#v found=%v", gotActive, found)
	}
	gotFinished, found := reloaded.Get(finished.ID)
	if !found || !gotFinished.Done || gotFinished.Error != "" {
		t.Fatalf("finished=%#v found=%v", gotFinished, found)
	}
	if listed := reloaded.List(); len(listed) != 2 {
		t.Fatalf("listed=%#v", listed)
	}
}
