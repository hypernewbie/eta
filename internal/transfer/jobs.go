package transfer

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Job is a snapshot of an asynchronous outbound transfer.
type Job struct {
	ID        string    `json:"id"`
	Completed int       `json:"completed"`
	Total     int       `json:"total"`
	Error     string    `json:"error,omitempty"`
	Done      bool      `json:"done"`
	Started   time.Time `json:"started"`
}

type Jobs struct {
	mu     sync.Mutex
	values map[string]Job
}

func NewJobs() *Jobs { return &Jobs{values: map[string]Job{}} }
func (j *Jobs) Start(total int) Job {
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	job := Job{ID: hex.EncodeToString(raw[:]), Total: total, Started: time.Now()}
	j.mu.Lock()
	j.values[job.ID] = job
	j.mu.Unlock()
	return job
}
func (j *Jobs) Progress(id string, completed int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.values[id]
	if !ok {
		return
	}
	job.Completed = completed
	j.values[id] = job
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
}
func (j *Jobs) Get(id string) (Job, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.values[id]
	return job, ok
}
