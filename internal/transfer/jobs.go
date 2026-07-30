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
type Job struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Completed int       `json:"completed"`
	Total     int       `json:"total"`
	Error     string    `json:"error,omitempty"`
	Done      bool      `json:"done"`
	Started   time.Time `json:"started"`
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
	for _, job := range loaded {
		if job.ID == "" {
			continue
		}
		if !job.Done {
			job.Done = true
			job.Error = "interrupted by Eta restart"
		}
		jobs.values[job.ID] = job
	}
	jobs.mu.Lock()
	err = jobs.saveLocked()
	jobs.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (j *Jobs) Start(total int) Job { return j.StartNamed(total, "") }
func (j *Jobs) StartNamed(total int, name string) Job {
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	job := Job{ID: hex.EncodeToString(raw[:]), Name: name, Total: total, Started: time.Now()}
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
