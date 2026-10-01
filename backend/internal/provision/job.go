package provision

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// A provisioning run is a Job: it keeps running if the browser tab closes (a
// half-installed node is worse than a finished one), keeps a bounded log that
// a reconnecting client can replay, and is forgotten a while after it ends.
// The job holds only progress text — never credentials (those live only in the
// goroutine running it, see Run).

type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

type EventKind string

const (
	EventLog    EventKind = "log"    // a line of output
	EventStep   EventKind = "step"   // a phase boundary ("Connecting…", "Installing…")
	EventStatus EventKind = "status" // final result
)

// Event is one entry of a job's log.
type Event struct {
	Seq    int       `json:"seq"`
	Time   time.Time `json:"time"`
	Kind   EventKind `json:"kind"`
	Text   string    `json:"text"`
	Status Status    `json:"status,omitempty"` // set on EventStatus
}

const (
	maxEvents       = 3000
	subscriberQueue = 1024
	finishedTTL     = 15 * time.Minute
	maxRunningJobs  = 2
)

// Job is one provisioning run.
type Job struct {
	ID       string
	Owner    string
	Provider string
	Host     string

	mu       sync.Mutex
	status   Status
	err      string
	nodeName string
	events   []Event
	nextSeq  int
	subs     map[chan Event]struct{}
	finished time.Time
}

// Snapshot is a point-in-time copy of a job, safe to hand to JSON.
type Snapshot struct {
	ID       string  `json:"id"`
	Provider string  `json:"provider"`
	Host     string  `json:"host"`
	Status   Status  `json:"status"`
	Error    string  `json:"error,omitempty"`
	NodeName string  `json:"nodeName,omitempty"`
	Events   []Event `json:"events"`
}

func (j *Job) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Snapshot{
		ID: j.ID, Provider: j.Provider, Host: j.Host,
		Status: j.status, Error: j.err, NodeName: j.nodeName,
		Events: append([]Event(nil), j.events...),
	}
}

func (j *Job) appendLocked(kind EventKind, text string, status Status) {
	ev := Event{Seq: j.nextSeq, Time: time.Now(), Kind: kind, Text: text, Status: status}
	j.nextSeq++
	j.events = append(j.events, ev)
	if len(j.events) > maxEvents {
		j.events = j.events[len(j.events)-maxEvents:]
	}
	for ch := range j.subs {
		select {
		case ch <- ev:
		default:
			// A subscriber that can't keep up is dropped; it reconnects and
			// replays from the log.
			close(ch)
			delete(j.subs, ch)
		}
	}
}

// Log records one line of output (already redacted by the caller).
func (j *Job) Log(text string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.appendLocked(EventLog, text, "")
}

// Step records a phase boundary.
func (j *Job) Step(text string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.appendLocked(EventStep, text, "")
}

func (j *Job) finish(status Status, errText, nodeName string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status, j.err, j.nodeName, j.finished = status, errText, nodeName, time.Now()
	msg := "finished"
	if status == StatusFailed {
		msg = errText
	}
	j.appendLocked(EventStatus, msg, status)
	for ch := range j.subs {
		close(ch)
	}
	j.subs = nil
}

// Subscribe returns the events with Seq > afterSeq so far plus a channel of
// later ones. The channel closes when the job ends or the subscriber falls
// behind. Call cancel when done listening.
func (j *Job) Subscribe(afterSeq int) (replay []Event, live <-chan Event, cancel func()) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, ev := range j.events {
		if ev.Seq > afterSeq {
			replay = append(replay, ev)
		}
	}
	if j.status != StatusRunning {
		ch := make(chan Event)
		close(ch)
		return replay, ch, func() {}
	}
	ch := make(chan Event, subscriberQueue)
	if j.subs == nil {
		j.subs = map[chan Event]struct{}{}
	}
	j.subs[ch] = struct{}{}
	return replay, ch, func() {
		j.mu.Lock()
		defer j.mu.Unlock()
		if _, ok := j.subs[ch]; ok {
			delete(j.subs, ch)
			close(ch)
		}
	}
}

// Registry tracks jobs in memory.
type Registry struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewRegistry() *Registry { return &Registry{jobs: map[string]*Job{}} }

var ErrTooManyJobs = errors.New("too many provisioning runs in progress — wait for one to finish")

// New registers a running job for owner.
func (r *Registry) New(owner, provider, host string) (*Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	running := 0
	for id, j := range r.jobs {
		j.mu.Lock()
		done, finished := j.status != StatusRunning, j.finished
		j.mu.Unlock()
		switch {
		case !done:
			running++
		case time.Since(finished) > finishedTTL:
			delete(r.jobs, id)
		}
	}
	if running >= maxRunningJobs {
		return nil, ErrTooManyJobs
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	j := &Job{ID: hex.EncodeToString(b), Owner: owner, Provider: provider, Host: host, status: StatusRunning}
	r.jobs[j.ID] = j
	return j, nil
}

// Get returns a job only to the identity that started it.
func (r *Registry) Get(id, owner string) (*Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok || j.Owner != owner {
		return nil, false
	}
	return j, true
}
