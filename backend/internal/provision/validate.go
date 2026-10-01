package provision

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Everything the user types ends up either in an SSH dial or inside a shell
// script that runs as root on a remote machine. So every field is validated
// against a strict shape here, and values that go into the script are
// single-quoted as well (shQuote) — injection has to get past both.

var (
	hostRe      = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.\-]*[A-Za-z0-9])?$`)
	usernameRe  = regexp.MustCompile(`^[a-z_][a-z0-9_\-.]{0,31}$`)
	nodeNameRe  = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	versionRe   = regexp.MustCompile(`^v[0-9][A-Za-z0-9.+_\-]{0,40}$`)
	tokenRe     = regexp.MustCompile(`^[A-Za-z0-9:_.\-]{8,512}$`)
	serverURLRe = regexp.MustCompile(`^https://[A-Za-z0-9]([A-Za-z0-9.\-]*[A-Za-z0-9])?(:[0-9]{1,5})?$`)
)

// ValidateTarget checks an SSH host/port/username and rejects destinations
// that would turn this into a way to poke at the Vinaire server itself:
// loopback, link-local (cloud metadata lives at 169.254.169.254), unspecified
// and multicast addresses. Private ranges are allowed — that's where real
// nodes live. It resolves the host so a hostname can't smuggle a forbidden IP
// past the check; the actual dial uses the returned address, so the answer
// can't change in between (no DNS rebinding window).
func ValidateTarget(ctx context.Context, host string, port int, username string) (resolved string, err error) {
	if !hostRe.MatchString(host) || len(host) > 253 {
		return "", errors.New("host must be an IP address or a hostname")
	}
	if port < 1 || port > 65535 {
		return "", errors.New("port must be between 1 and 65535")
	}
	if username != "" && !usernameRe.MatchString(username) {
		return "", errors.New("username contains characters that aren't allowed")
	}

	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
		if lookupErr != nil || len(addrs) == 0 {
			return "", fmt.Errorf("couldn't resolve host %q", host)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return "", fmt.Errorf("address %s isn't allowed as a provisioning target (loopback, link-local, unspecified and multicast addresses are refused)", ip)
		}
	}
	return ips[0].String(), nil
}

func ValidateServerURL(u string) error {
	if !serverURLRe.MatchString(u) {
		return errors.New("server URL must look like https://host:6443")
	}
	return nil
}

func ValidateToken(t Secret) error {
	if !tokenRe.MatchString(t.Reveal()) {
		return errors.New("join token has an unexpected format")
	}
	return nil
}

func ValidateNodeName(n string) error {
	if n != "" && !nodeNameRe.MatchString(n) {
		return errors.New("node name must be a lowercase DNS name")
	}
	return nil
}

func ValidateVersion(v string) error {
	if v != "" && !versionRe.MatchString(v) {
		return errors.New("version must look like v1.35.5+k3s1")
	}
	return nil
}

// shQuote single-quotes s for a POSIX shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
