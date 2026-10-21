package backup

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Roadmap Phase 78: what reaches a Job's environment or command line from a request or from the
// operator's file. None of it may ever carry a shell metacharacter, a path separator or a line break.

const unsafeChars = " \t\r\n;$`'\"\\|&<>()*?{}!#~"

func FuzzRequestValues(f *testing.F) {
	for _, s := range []string{
		"", "0 3 * * *", "*/15 * * * mon", "0 3 * * *; rm -rf /", "$(id) * * * *", "5Gi", "0Gi", "5Gi\nx",
		"db-postgres-20261021T084848Z.dump", "../x.dump", "a b", "x;rm", "my-secret", "Bad_Name",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if ValidFile(s) && strings.ContainsAny(s, unsafeChars+"/") {
			t.Fatalf("file name %q accepted", s)
		}
		if ValidSize(s) && strings.ContainsAny(s, unsafeChars+"/") {
			t.Fatalf("size %q accepted", s)
		}
		// a cron expression is the CronJob's schedule field, never part of a script: it may hold spaces
		// between its fields and * / , - — nothing a shell would act on
		if ValidSchedule(s) && strings.ContainsAny(strings.NewReplacer(" ", "", "*", "", "?", "").Replace(s), unsafeChars) {
			t.Fatalf("schedule %q accepted", s)
		}
		if ValidSchedule(s) && len(strings.Fields(s)) != 5 {
			t.Fatalf("schedule %q does not have five fields", s)
		}
		if nameRe.MatchString(s) && strings.ContainsAny(s, unsafeChars+"/.") {
			t.Fatalf("name %q accepted", s)
		}
	})
}

// The operator's configuration is trusted less than it looks: it is a file, and an S3 endpoint in it must
// not become a way to reach Vinaire's own network.
func FuzzLoadConfig(f *testing.F) {
	for _, s := range []string{
		`{}`, `{"targets":[]}`,
		`{"targets":[{"name":"m","endpoint":"https://s3.example.com","bucket":"my-bucket","prefix":"a/b"}]}`,
		`{"targets":[{"name":"m","endpoint":"http://s3.example.com","bucket":"my-bucket"}]}`,
		`{"targets":[{"name":"m","endpoint":"https://127.0.0.1","bucket":"my-bucket"}]}`,
		`{"targets":[{"name":"m","endpoint":"https://u:p@h","bucket":"my-bucket"}]}`,
		`{"targets":[{"name":"m","endpoint":"https://h","bucket":"b;rm","prefix":"../x"}]}`,
		`not json`, `{"uploadImage":"evil;x"}`,
	} {
		f.Add(s)
	}
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, body string) {
		p := filepath.Join(dir, "c.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := LoadConfig(p)
		if err != nil {
			return
		}
		for _, tg := range c.Targets {
			u, perr := url.Parse(tg.Endpoint)
			if perr != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" {
				t.Fatalf("accepted endpoint %q", tg.Endpoint)
			}
			switch strings.ToLower(u.Hostname()) {
			case "localhost", "127.0.0.1", "::1", "0.0.0.0", "169.254.169.254":
				t.Fatalf("accepted endpoint %q", tg.Endpoint)
			}
			for _, v := range []string{tg.Name, tg.Bucket, tg.Prefix, tg.Region} {
				if strings.ContainsAny(v, unsafeChars) || strings.Contains(v, "..") {
					t.Fatalf("accepted value %q in target %q", v, tg.Name)
				}
			}
		}
		if c.UploadImage != "" && strings.ContainsAny(c.UploadImage, unsafeChars) {
			t.Fatalf("accepted image %q", c.UploadImage)
		}
	})
}
