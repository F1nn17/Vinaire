package helm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/rest"
)

// An install job runs an install in the background so the UI can show what is
// happening — downloading the chart (bytes, speed), checking the caller's rights,
// installing, waiting for pods — instead of one spinner on a request that may
// last minutes. It is the same install as Releases.Install, run as the same
// caller's *rest.Config; only the delivery differs. A job is visible only to
// the identity that started it (Owner), and is forgotten shortly after it ends.

// Stages of an install job, in order.
const (
	StageDownloading = "downloading"
	StageChecking    = "checking"
	StageInstalling  = "installing"
	StageDone        = "done"
	StageFailed      = "failed"
	StageCancelled   = "cancelled"
)

const (
	jobRetention  = 15 * time.Minute
	maxJobs       = 64
	maxJobsPerKey = 4
)

// ErrTooManyJobs is returned when the caller (or the server) has too many
// installs in flight.
var ErrTooManyJobs = errors.New("too many installs are running — wait for one to finish")

// InstallJob is one background install.
type InstallJob struct {
	ID        string
	Owner     string
	Request   InstallRequest
	StartedAt time.Time

	mu         sync.Mutex
	stage      string
	failedAt   string // the stage that was running when the job failed or was cancelled
	done       int64
	total      int64 // -1 = the server didn't say
	dlStarted  time.Time
	finishedAt time.Time
	err        error
	missing    []Needed
	unchecked  []string
	result     *InstallResult
	cancel     context.CancelFunc
}

// JobSnapshot is a point-in-time, JSON-friendly view of a job.
type JobSnapshot struct {
	ID          string         `json:"id"`
	Stage       string         `json:"stage"`
	FailedStage string         `json:"failedStage,omitempty"`
	Chart       string         `json:"chart"`
	Release     string         `json:"release"`
	Namespace   string         `json:"namespace"`
	BytesDone   int64          `json:"bytesDone"`
	BytesTotal  int64          `json:"bytesTotal"` // -1 = unknown
	BytesPerSec int64          `json:"bytesPerSec"`
	ElapsedSec  float64        `json:"elapsedSec"`
	Error       string         `json:"error,omitempty"`
	Forbidden   bool           `json:"forbidden,omitempty"`
	Missing     []Needed       `json:"missing,omitempty"`
	Unchecked   []string       `json:"unchecked,omitempty"`
	Result      *InstallResult `json:"result,omitempty"`
}

// Finished reports whether the job has ended, one way or another.
func (j *InstallJob) Finished() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.finishedLocked()
}

func (j *InstallJob) finishedLocked() bool {
	return j.stage == StageDone || j.stage == StageFailed || j.stage == StageCancelled
}

// Snapshot returns the job's current state.
func (j *InstallJob) Snapshot() JobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	end := time.Now()
	if !j.finishedAt.IsZero() {
		end = j.finishedAt
	}
	s := JobSnapshot{
		ID: j.ID, Stage: j.stage, FailedStage: j.failedAt, Chart: j.Request.Chart, Release: j.Request.Release, Namespace: j.Request.Namespace,
		BytesDone: j.done, BytesTotal: j.total, ElapsedSec: end.Sub(j.StartedAt).Seconds(),
		Missing: j.missing, Unchecked: j.unchecked, Result: j.result,
	}
	if !j.dlStarted.IsZero() && j.done > 0 {
		if secs := end.Sub(j.dlStarted).Seconds(); secs > 0 {
			s.BytesPerSec = int64(float64(j.done) / secs)
		}
	}
	if j.err != nil {
		s.Error = j.err.Error()
		s.Forbidden = errors.Is(j.err, ErrForbidden)
	}
	return s
}

func (j *InstallJob) setStage(stage string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.finishedLocked() {
		j.stage = stage
	}
}

func (j *InstallJob) progress(done, total int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.dlStarted.IsZero() {
		j.dlStarted = time.Now()
	}
	j.done, j.total = done, total
}

// Cancel stops a job that hasn't reached the install step; once Helm is creating
// objects, stopping it halfway would only leave a partial release behind, so the
// call is refused and false is returned.
func (j *InstallJob) Cancel() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.stage != StageDownloading && j.stage != StageChecking {
		return false
	}
	if j.cancel != nil {
		j.cancel()
	}
	return true
}

// Jobs is the registry of install jobs.
type Jobs struct {
	mu   sync.Mutex
	jobs map[string]*InstallJob
}

// NewJobs returns an empty registry.
func NewJobs() *Jobs { return &Jobs{jobs: map[string]*InstallJob{}} }

func newJobID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Get returns a job, but only to the identity that started it.
func (js *Jobs) Get(id, owner string) (*InstallJob, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()
	j, ok := js.jobs[id]
	if !ok || j.Owner != owner {
		return nil, false
	}
	return j, true
}

func (js *Jobs) sweepLocked() {
	for id, j := range js.jobs {
		j.mu.Lock()
		old := j.finishedLocked() && time.Since(j.finishedAt) > jobRetention
		j.mu.Unlock()
		if old {
			delete(js.jobs, id)
		}
	}
}

// OnFinish is called once, after the job ends, with the outcome (the audit hook).
type OnFinish func(res *InstallResult, err error)

// StartInstall begins an install in the background as the caller's own config.
func (js *Jobs) StartInstall(rl *Releases, cfg *rest.Config, owner string, req InstallRequest, onFinish OnFinish) (*InstallJob, error) {
	js.mu.Lock()
	js.sweepLocked()
	running, mine := 0, 0
	for _, j := range js.jobs {
		if !j.Finished() {
			running++
			if j.Owner == owner {
				mine++
			}
		}
	}
	if running >= maxJobs || mine >= maxJobsPerKey {
		js.mu.Unlock()
		return nil, ErrTooManyJobs
	}
	// A validation error should come back to the request, not later from a job.
	if !releaseNameRe.MatchString(req.Release) || !dnsLabelRe.MatchString(req.Namespace) {
		js.mu.Unlock()
		return nil, errInvalidName
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout+2*time.Minute)
	j := &InstallJob{
		ID: newJobID(), Owner: owner, Request: req, StartedAt: time.Now(),
		stage: StageDownloading, total: -1, cancel: cancel,
	}
	js.jobs[j.ID] = j
	js.mu.Unlock()

	go func() {
		defer cancel()
		res, err := js.run(ctx, rl, cfg, j)
		j.mu.Lock()
		j.finishedAt = time.Now()
		switch {
		case err == nil:
			j.stage, j.result = StageDone, res
		case errors.Is(err, context.Canceled):
			j.failedAt, j.stage, j.err = j.stage, StageCancelled, errors.New("cancelled")
		default:
			j.failedAt, j.stage, j.err = j.stage, StageFailed, err
		}
		j.mu.Unlock()
		if onFinish != nil {
			onFinish(res, err)
		}
	}()
	return j, nil
}

var errInvalidName = fmt.Errorf("%w: release name must be lowercase letters, digits and dashes (max 53 characters) and the namespace a valid name", ErrInvalid)

func (js *Jobs) run(ctx context.Context, rl *Releases, cfg *rest.Config, j *InstallJob) (*InstallResult, error) {
	req := j.Request
	// 1. download (cached for the next two steps)
	dctx := WithProgress(ctx, j.progress)
	if _, err := rl.Catalog.LoadChart(dctx, req.Repo, req.Chart, req.Version); err != nil {
		return nil, err
	}
	// 2. may the caller do what this chart needs?
	j.setStage(StageChecking)
	pf, err := rl.Preflight(ctx, cfg, req)
	if errors.Is(err, ErrRender) {
		pf, err = &PreflightResult{Missing: []Needed{}}, nil // let the install itself judge the chart
	}
	if err != nil {
		return nil, err
	}
	if !pf.OK() {
		j.mu.Lock()
		j.missing, j.unchecked = pf.Missing, pf.Unchecked
		j.mu.Unlock()
		return nil, fmt.Errorf("%w: you don't have the permissions this chart needs: %s", ErrForbidden, pf.String())
	}
	j.mu.Lock()
	j.unchecked = pf.Unchecked
	j.mu.Unlock()
	// 3. install (Wait, if asked for, keeps this step open until pods are ready)
	j.setStage(StageInstalling)
	return rl.Install(ctx, cfg, req)
}
