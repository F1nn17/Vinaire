package helm

import (
	"errors"
	"testing"
	"time"
)

func TestJobsAreScopedToTheirOwnerAndLimited(t *testing.T) {
	js := NewJobs()
	if _, err := js.StartInstall(&Releases{}, nil, "user:a", InstallRequest{Release: "Bad_Name", Namespace: "ns"}, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("an invalid release name started a job: %v", err)
	}

	// Jobs added directly, so no cluster is needed.
	mk := func(id, owner, stage string) *InstallJob {
		j := &InstallJob{ID: id, Owner: owner, stage: stage, StartedAt: time.Now(), total: -1}
		js.jobs[id] = j
		return j
	}
	j := mk("one", "user:a", StageDownloading)
	if _, ok := js.Get("one", "user:b"); ok {
		t.Error("another identity could see the job")
	}
	if got, ok := js.Get("one", "user:a"); !ok || got != j {
		t.Error("the owner couldn't see their job")
	}
	for i := 0; i < maxJobsPerKey-1; i++ {
		mk("x"+string(rune('a'+i)), "user:a", StageInstalling)
	}
	if _, err := js.StartInstall(&Releases{}, nil, "user:a", InstallRequest{Release: "ok", Namespace: "ns"}, nil); !errors.Is(err, ErrTooManyJobs) {
		t.Errorf("per-identity limit not enforced: %v", err)
	}
}

func TestJobProgressSpeedAndCancelRules(t *testing.T) {
	j := &InstallJob{ID: "j", StartedAt: time.Now().Add(-2 * time.Second), stage: StageDownloading, total: -1}
	j.progress(0, 1000)
	j.dlStarted = time.Now().Add(-2 * time.Second)
	j.progress(500, 1000)
	s := j.Snapshot()
	if s.BytesDone != 500 || s.BytesTotal != 1000 || s.BytesPerSec < 200 || s.BytesPerSec > 300 {
		t.Errorf("snapshot = %+v, want ~250 B/s", s)
	}
	if !j.Cancel() {
		t.Error("a downloading job couldn't be cancelled")
	}
	j.setStage(StageInstalling)
	if j.Cancel() {
		t.Error("an install that is creating objects was cancellable")
	}
	j.mu.Lock()
	j.stage = StageDone
	j.mu.Unlock()
	j.setStage(StageChecking)
	if j.Snapshot().Stage != StageDone {
		t.Error("a finished job moved back to an earlier stage")
	}
}
