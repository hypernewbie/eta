package transfer

import (
	"path/filepath"
	"testing"
)

// TestPersistentJobsLeavesRoutingInfoJobsUntouchedForResume covers the
// newer load semantics: a non-done Job that recorded both source and
// destination routing on Start must NOT be coerced into the
// "interrupted by Eta restart" error state at load time, because
// resumePendingJobs is expected to retry it. A Job missing the
// routing fields (older versions, or jobs created via StartNamed
// without calling the new StartWith) keeps the previous
// mark-interrupted behavior.
func TestPersistentJobsLeavesRoutingInfoJobsUntouchedForResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	jobs, err := NewPersistentJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	routed := jobs.StartWith(JobSpec{
		Name:            "src/file.md",
		Total:           8,
		SourceRoot:      0,
		SourcePath:      "src/file.md",
		DestinationPeer: "http://127.0.0.1:17081",
		DestinationRoot: 0,
		DestinationPath: "dst/file.md",
	})
	jobs.Progress(routed.ID, 3)
	legacy := jobs.StartNamed(2, "legacy-only-name")
	jobs.Progress(legacy.ID, 1)

	reloaded, err := NewPersistentJobs(path)
	if err != nil {
		t.Fatal(err)
	}

	gotRouted, found := reloaded.Get(routed.ID)
	if !found {
		t.Fatalf("routed job missing after reload")
	}
	if gotRouted.Done {
		t.Fatalf("routed job must not be marked done; got %#v", gotRouted)
	}
	if gotRouted.Completed != 3 {
		t.Fatalf("routed completion lost on reload; got %d", gotRouted.Completed)
	}
	if gotRouted.SourcePath != "src/file.md" || gotRouted.DestinationPeer == "" {
		t.Fatalf("routing lost; got %#v", gotRouted)
	}

	gotLegacy, found := reloaded.Get(legacy.ID)
	if !found || !gotLegacy.Done || gotLegacy.Error != "interrupted by Eta restart" {
		t.Fatalf("legacy job should be interrupted; got %#v found=%v", gotLegacy, found)
	}
}

// TestStartWithPersistsRoutingFields verifies that StartWith stores the
// spec fields in the Job and that they round-trip through the
// on-disk JSON file.
func TestStartWithPersistsRoutingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	jobs, err := NewPersistentJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	job := jobs.StartWith(JobSpec{
		Name:            "src/top.md",
		Total:           4,
		SourcePeer:      "", // local source
		SourceRoot:      0,
		SourcePath:      "src/top.md",
		DestinationPeer: "http://127.0.0.1:17081",
		DestinationRoot: 0,
		DestinationPath: "dst/top.md",
	})
	if job.SourcePeer != "" {
		t.Fatalf("SourcePeer should be empty; got %q", job.SourcePeer)
	}
	if job.DestinationPeer == "" || job.SourcePath == "" || job.DestinationPath == "" {
		t.Fatalf("routing fields not stored: %#v", job)
	}

	reloaded, err := NewPersistentJobs(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.Get(job.ID)
	if got.DestinationPeer != job.DestinationPeer ||
		got.SourcePath != job.SourcePath ||
		got.DestinationPath != job.DestinationPath {
		t.Fatalf("routing did not round-trip through disk: %#v", got)
	}
}
