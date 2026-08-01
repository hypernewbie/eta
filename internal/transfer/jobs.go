package transfer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Job is a durable snapshot of an asynchronous outbound transfer.
// SourcePeer / SourcePath and DestinationPeer / DestinationPath carry
// the routing needed for the server to auto-resume the transfer after a
// process restart. Empty peer means a local endpoint. Jobs whose
// fields are populated and are not Done are left in that state on
// restart so the resume goroutine can pick them up; jobs without
// routing info are too old to resume and are marked interrupted.
type Job struct {
	ID              string    `json:"id"`
	Name            string    `json:"name,omitempty"`
	Completed       int       `json:"completed"`
	Total           int       `json:"total"`
	Error           string    `json:"error,omitempty"`
	Done            bool      `json:"done"`
	Started         time.Time `json:"started"`
	SourcePeer      string    `json:"sourcePeer,omitempty"`
	SourceRoot      int       `json:"sourceRoot,omitempty"`
	SourcePath      string    `json:"sourcePath,omitempty"`
	DestinationPeer string    `json:"destinationPeer,omitempty"`
	DestinationRoot int       `json:"destinationRoot,omitempty"`
	DestinationPath string    `json:"destinationPath,omitempty"`
}

// JobSpec bundles the routing and size hints required to start a Job.
// It is intentionally separate from Job's runtime fields so that
// StartWith has no overlap with the polled-progress mutation path.
type JobSpec struct {
	Name            string
	Total           int
	SourcePeer      string
	SourceRoot      int
	SourcePath      string
	DestinationPeer string
	DestinationRoot int
	DestinationPath string
}

// Jobs keeps small control-plane records. The optional path persists snapshots
// atomically; a process restart marks in-flight work interrupted rather than
// pretending the sender goroutine can resume by itself.
type Jobs struct {
	mu     sync.Mutex
	values map[string]Job
	path   string
}

func NewJobs() *Jobs { return &Jobs{values: map[string]Job{}} }

func NewPersistentJobs(path string) (*Jobs, error) {
	jobs := NewJobs()
	jobs.path = path
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return jobs, nil
	}
	if err != nil {
		return nil, err
	}
	var loaded []Job
	if err := json.Unmarshal(body, &loaded); err != nil {
		return nil, err
	}
	marked := 0
	for _, job := range loaded {
		if job.ID == "" {
			continue
		}
		// Jobs with full routing info are left in pre-restart state so
		// resumePendingJobs can attempt a real retry on startup. Jobs
		// without routing info (older versions or jobs the server
		// never recorded) cannot be reconstructed, so we mark them
		// interrupted with the previous journal wording.
		if !job.Done && (job.SourcePath == "" || job.DestinationPath == "") {
			job.Done = true
			job.Error = "interrupted by Eta restart"
			marked++
		}
		jobs.values[job.ID] = job
	}
	jobs.mu.Lock()
	if marked > 0 {
		_ = jobs.saveLocked()
	}
	jobs.mu.Unlock()
	return jobs, nil
}

func (j *Jobs) Start(total int) Job {
	return j.StartWith(JobSpec{Total: total})
}

func (j *Jobs) StartNamed(total int, name string) Job {
	return j.StartWith(JobSpec{Total: total, Name: name})
}

// StartWith records a new in-flight Job with the given routing. The
// returned Job is also persisted; callers Progress/Finish to update it.
func (j *Jobs) StartWith(spec JobSpec) Job {
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	job := Job{
		ID:              hex.EncodeToString(raw[:]),
		Name:            spec.Name,
		Total:           spec.Total,
		Started:         time.Now().UTC(),
		SourcePeer:      spec.SourcePeer,
		SourceRoot:      spec.SourceRoot,
		SourcePath:      spec.SourcePath,
		DestinationPeer: spec.DestinationPeer,
		DestinationRoot: spec.DestinationRoot,
		DestinationPath: spec.DestinationPath,
	}
	j.mu.Lock()
	j.values[job.ID] = job
	_ = j.saveLocked()
	j.mu.Unlock()
	return job
}
func (j *Jobs) Progress(id string, completed int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.values[id]
	if !ok || job.Done {
		return
	}
	job.Completed = completed
	j.values[id] = job
	_ = j.saveLocked()
}
func (j *Jobs) Finish(id string, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.values[id]
	if !ok {
		return
	}
	job.Done = true
	if err != nil {
		job.Error = err.Error()
	}
	j.values[id] = job
	_ = j.saveLocked()
}
func (j *Jobs) Get(id string) (Job, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.values[id]
	return job, ok
}
func (j *Jobs) List() []Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	jobs := make([]Job, 0, len(j.values))
	for _, job := range j.values {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, k int) bool { return jobs[i].Started.After(jobs[k].Started) })
	return jobs
}
func (j *Jobs) saveLocked() error {
	if j.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return err
	}
	jobs := make([]Job, 0, len(j.values))
	for _, job := range j.values {
		jobs = append(jobs, job)
	}
	body, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(j.path), ".transfer-jobs-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(append(body, '\n'))
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, j.path)
}
