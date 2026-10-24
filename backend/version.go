package main

import (
	"os"
	"regexp"
)

// versionRe is what a release looks like (1.3.0, 1.3.0-rc.1, or a tag such as "latest" or "dev"): nothing that
// could be more than a name is passed on to the UI.
var versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// versionFromEnv is the release this Vinaire is: the chart sets VINAIRE_VERSION to the image tag. Not set, or not
// shaped like a version, it is "dev".
func versionFromEnv() string {
	if v := os.Getenv("VINAIRE_VERSION"); versionRe.MatchString(v) {
		return v
	}
	return "dev"
}
