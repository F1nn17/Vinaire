// Roadmap Phase 78: what usually breaks in a real installation, checked against a real cluster.
// The manifests are rendered by the "Install from an image" wizard's own code
// (frontend/scripts/render-image-fixtures.ts), so the test installs what the wizard installs and
// cannot drift from it. Needs LIFECYCLE_FIXTURES (the directory the script wrote to), kubectl, and
// a backend running with AUTH_MODE=disabled against the same cluster; skipped without them.
//
//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	Preset, Name, Namespace, User, Database, Password string
}

func loadFixture(t *testing.T, name string) fixture {
	t.Helper()
	dir := os.Getenv("LIFECYCLE_FIXTURES")
	if dir == "" {
		t.Skip("LIFECYCLE_FIXTURES is not set")
	}
	b, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// kubectl runs kubectl and returns its combined output.
func kubectl(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("kubectl", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func install(t *testing.T, name string) fixture {
	t.Helper()
	f := loadFixture(t, name)
	b, err := os.ReadFile(filepath.Join(os.Getenv("LIFECYCLE_FIXTURES"), name+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	kubectl(t, string(b), "apply", "-f", "-")
	waitReady(t, f)
	return f
}

func waitReady(t *testing.T, f fixture) {
	t.Helper()
	// the pod may not exist yet right after an apply or a delete
	eventually(t, 3*time.Minute, func() bool {
		out, err := exec.Command("kubectl", "-n", f.Namespace, "wait", "--for=condition=ready", "pod/"+f.Name+"-0", "--timeout=5s").CombinedOutput()
		_ = out
		return err == nil
	})
}

func exec1(t *testing.T, f fixture, args ...string) string {
	t.Helper()
	return kubectl(t, "", append([]string{"-n", f.Namespace, "exec", f.Name + "-0", "--"}, args...)...)
}

func psql(t *testing.T, f fixture, sql string) string {
	t.Helper()
	// a freshly Ready postgres may still be restarting once after init; retry the first statements
	var out string
	eventually(t, 60*time.Second, func() bool {
		b, err := exec.Command("kubectl", "-n", f.Namespace, "exec", f.Name+"-0", "--",
			"psql", "-U", f.User, "-d", f.Database, "-tA", "-c", sql).CombinedOutput()
		out = string(b)
		return err == nil
	})
	return strings.TrimSpace(out)
}

func apiDo(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	var payload []byte
	if r != nil {
		payload, _ = io.ReadAll(r)
	}
	// Writes are rate-limited per identity (Phase 17) and this suite writes in bursts: wait the
	// limiter out rather than fail on it. Any other status is the answer.
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(method, baseURL()+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 40 {
			time.Sleep(3 * time.Second)
			continue
		}
		return resp.StatusCode, b
	}
}

type backupItem struct {
	Kind, Name, State, Role string
}

func backupState(t *testing.T, f fixture, name string) string {
	t.Helper()
	code, b := apiDo(t, "GET", "/api/v1/instances/"+f.Namespace+"/"+f.Name+"/backups", nil)
	if code != 200 {
		t.Fatalf("list backups: %d %s", code, b)
	}
	var res struct{ Backups []backupItem }
	_ = json.Unmarshal(b, &res)
	for _, x := range res.Backups {
		if x.Name == name {
			return x.State
		}
	}
	return ""
}

func waitJob(t *testing.T, f fixture, job string) {
	t.Helper()
	eventually(t, 4*time.Minute, func() bool {
		s := backupState(t, f, job)
		if s == "failed" {
			logs, _ := exec.Command("kubectl", "-n", f.Namespace, "logs", "job/"+job, "--all-containers").CombinedOutput()
			t.Fatalf("job %s failed:\n%s", job, logs)
		}
		return s == "succeeded"
	})
}

func created(t *testing.T, b []byte) string {
	t.Helper()
	var res struct{ Created []string }
	if err := json.Unmarshal(b, &res); err != nil || len(res.Created) == 0 {
		t.Fatalf("no object created: %s", b)
	}
	return res.Created[0]
}

type instanceRow struct {
	Namespace, Name, Origin string
	Parts                   []struct{ Kind, Name, Fate string }
}

func instance(t *testing.T, ns, name string) *instanceRow {
	t.Helper()
	var res struct{ Instances []instanceRow }
	get(t, "/api/v1/instances", &res)
	for i := range res.Instances {
		if res.Instances[i].Namespace == ns && res.Instances[i].Name == name {
			return &res.Instances[i]
		}
	}
	return nil
}

func TestPostgresInstallRecreateBackupRestoreDelete(t *testing.T) {
	pg := loadFixture(t, "pgsrc") // skips when the fixtures are not there
	kubectl(t, "", "create", "namespace", pg.Namespace)
	t.Cleanup(func() { _ = exec.Command("kubectl", "delete", "namespace", pg.Namespace, "--wait=false").Run() })

	src := install(t, "pgsrc")

	t.Run("the instance is found with its volume claim", func(t *testing.T) {
		in := instance(t, src.Namespace, src.Name)
		if in == nil || in.Origin != "image" {
			t.Fatalf("instance not listed: %+v", in)
		}
		var claim bool
		for _, p := range in.Parts {
			if p.Kind == "PersistentVolumeClaim" && p.Fate == "stays" {
				claim = true
			}
		}
		if !claim {
			t.Errorf("the StatefulSet's volume claim must be part of the instance and stay: %+v", in.Parts)
		}
	})

	psql(t, src, "create table t(x int); insert into t values (42),(43);")

	t.Run("data survives the pod being recreated", func(t *testing.T) {
		kubectl(t, "", "-n", src.Namespace, "delete", "pod", src.Name+"-0")
		waitReady(t, src)
		if got := psql(t, src, "select string_agg(x::text, ',' order by x) from t"); got != "42,43" {
			t.Fatalf("data lost after the pod was recreated: %q", got)
		}
	})

	var job string
	t.Run("a dump to a volume claim succeeds", func(t *testing.T) {
		code, b := apiDo(t, "POST", "/api/v1/instances/"+src.Namespace+"/"+src.Name+"/backups",
			map[string]any{"kind": "dump", "target": "pvc", "size": "1Gi"})
		if code != 200 {
			t.Fatalf("create dump: %d %s", code, b)
		}
		job = created(t, b)
		waitJob(t, src, job)
	})

	dst := install(t, "pgdst")

	t.Run("the dump loads into a new instance", func(t *testing.T) {
		code, b := apiDo(t, "POST", "/api/v1/instances/"+dst.Namespace+"/"+dst.Name+"/restore",
			map[string]any{"source": src.Name, "confirmName": dst.Name})
		if code != 200 {
			t.Fatalf("restore: %d %s", code, b)
		}
		waitJob(t, dst, created(t, b))
		if got := psql(t, dst, "select string_agg(x::text, ',' order by x) from t"); got != "42,43" {
			t.Fatalf("restored data differs: %q", got)
		}
	})

	t.Run("a restore never goes over its own source", func(t *testing.T) {
		code, _ := apiDo(t, "POST", "/api/v1/instances/"+src.Namespace+"/"+src.Name+"/restore",
			map[string]any{"source": src.Name, "confirmName": src.Name})
		if code != 400 {
			t.Fatalf("want 400, got %d", code)
		}
	})

	t.Run("a version change is checked", func(t *testing.T) {
		path := "/api/v1/instances/" + src.Namespace + "/" + src.Name + "/upgrade"
		for name, body := range map[string]map[string]any{
			"major without acknowledgement": {"tag": "99", "confirmName": src.Name},
			"lower major":                   {"tag": "1", "acceptMajor": true, "confirmName": src.Name},
			"wrong confirmation":            {"tag": "17.9", "confirmName": "other"},
			"a repository in the tag":       {"tag": "evil/postgres:17", "acceptMajor": true, "confirmName": src.Name},
		} {
			if code, b := apiDo(t, "POST", path, body); code != 400 {
				t.Errorf("%s: want 400, got %d %s", name, code, b)
			}
		}
	})

	t.Run("deleting the instance keeps its volume claim and its backups", func(t *testing.T) {
		if code, b := apiDo(t, "DELETE", "/api/v1/statefulsets/"+src.Namespace+"/"+src.Name, nil); code != 200 {
			t.Fatalf("delete: %d %s", code, b)
		}
		eventually(t, time.Minute, func() bool { return instance(t, src.Namespace, src.Name) == nil })
		out := kubectl(t, "", "-n", src.Namespace, "get", "pvc", "-o", "name")
		if !strings.Contains(out, "data-"+src.Name+"-0") || !strings.Contains(out, src.Name+"-backups") {
			t.Errorf("the data claim and the backup claim must stay:\n%s", out)
		}
	})
}

func TestRedisKeepsDataAcrossRecreateAndCanBeDumped(t *testing.T) {
	f := loadFixture(t, "rd")
	kubectl(t, "", "create", "namespace", f.Namespace)
	t.Cleanup(func() { _ = exec.Command("kubectl", "delete", "namespace", f.Namespace, "--wait=false").Run() })
	install(t, "rd")

	cli := func(args ...string) string {
		return strings.TrimSpace(exec1(t, f, append([]string{"redis-cli", "-a", f.Password, "--no-auth-warning"}, args...)...))
	}
	if got := cli("set", "greeting", "hello"); got != "OK" {
		t.Fatalf("set: %q", got)
	}
	// the append-only file is flushed every second
	time.Sleep(2 * time.Second)

	kubectl(t, "", "-n", f.Namespace, "delete", "pod", f.Name+"-0")
	waitReady(t, f)
	eventually(t, time.Minute, func() bool {
		out, err := exec.Command("kubectl", "-n", f.Namespace, "exec", f.Name+"-0", "--", "redis-cli", "-a", f.Password, "--no-auth-warning", "get", "greeting").CombinedOutput()
		return err == nil && strings.TrimSpace(string(out)) == "hello"
	})

	code, b := apiDo(t, "POST", "/api/v1/instances/"+f.Namespace+"/"+f.Name+"/backups",
		map[string]any{"kind": "dump", "target": "pvc", "size": "1Gi"})
	if code != 200 {
		t.Fatalf("create dump: %d %s", code, b)
	}
	waitJob(t, f, created(t, b))
}
