package helm

import (
	"errors"
	"k8s.io/client-go/rest"
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
	if !j.Cancel() {
		t.Error("an install that is creating objects couldn't be cancelled (Phase 60)")
	}
	if !j.userCancelled {
		t.Error("cancelling mid-install must be recorded, so the release is rolled back")
	}
	j.mu.Lock()
	j.stage = StageRollingBack
	j.mu.Unlock()
	if j.Cancel() {
		t.Error("a job that is already rolling back was cancellable")
	}
	j.mu.Lock()
	j.stage = StageDone
	j.mu.Unlock()
	j.setStage(StageChecking)
	if j.Snapshot().Stage != StageDone {
		t.Error("a finished job moved back to an earlier stage")
	}
}

// Roadmap Phase 60: a rollback only ever uninstalls a release the job itself created.
func TestRollbackDoesNotUninstallAReleaseItDoesNotOwn(t *testing.T) {
	rollbackSettle = 0
	// an API server that can't be reached: every read fails, which is fine for "best effort"
	cfg := &rest.Config{Host: "http://127.0.0.1:1"}
	var hooked error
	called := false
	j := &InstallJob{
		ID: "j", Request: InstallRequest{Release: "web", Namespace: "ns"}, stage: StageInstalling, userCancelled: true,
		onRollback: func(err error) { called, hooked = true, err },
	}
	// ownsRelease is false: the name was not seen free before the install
	NewJobs().rollback(&Releases{}, cfg, j, true)
	if !called || hooked == nil {
		t.Fatalf("hook called=%v err=%v, want a refusal", called, hooked)
	}
	if j.rolledBack {
		t.Error("claimed a rollback it didn't do")
	}
	if got := j.Snapshot(); got.RolledBack || got.RollbackError == "" {
		t.Errorf("snapshot = %+v", got)
	}
}

func TestRollbackFailedOnlyForItsOwnFailedInstalls(t *testing.T) {
	cases := map[string]*InstallJob{
		"failed while downloading":  {stage: StageFailed, failedAt: StageDownloading, ownsRelease: true},
		"failed but name not owned": {stage: StageFailed, failedAt: StageInstalling},
		"already rolled back":       {stage: StageFailed, failedAt: StageInstalling, ownsRelease: true, rolledBack: true},
		"still running":             {stage: StageInstalling, ownsRelease: true},
		"done":                      {stage: StageDone, ownsRelease: true},
	}
	for name, j := range cases {
		j.Request = InstallRequest{Release: "web", Namespace: "ns"}
		if NewJobs().RollbackFailed(&Releases{}, &rest.Config{Host: "http://127.0.0.1:1"}, j) {
			t.Errorf("%s: a rollback was started", name)
		}
		if j.Snapshot().CanRollBack {
			t.Errorf("%s: offered a rollback", name)
		}
	}
	ok := &InstallJob{stage: StageFailed, failedAt: StageInstalling, ownsRelease: true, Request: InstallRequest{Release: "web", Namespace: "ns"}}
	if !ok.Snapshot().CanRollBack {
		t.Error("a failed install of its own release should offer a rollback")
	}
}
