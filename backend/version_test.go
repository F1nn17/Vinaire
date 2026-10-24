package main

import "testing"

func TestTheVersionIsTheChartsTagOrDev(t *testing.T) {
	for in, want := range map[string]string{
		"1.3.0":                     "1.3.0",
		"1.3.0-rc.1":                "1.3.0-rc.1",
		"latest":                    "latest",
		"":                          "dev",
		"<script>alert(1)</script>": "dev",
		"1.0 0":                     "dev",
		"a-very-long-version-a-very-long-version-a-very-long-version-a-very-long": "dev",
	} {
		t.Setenv("VINAIRE_VERSION", in)
		if got := versionFromEnv(); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
