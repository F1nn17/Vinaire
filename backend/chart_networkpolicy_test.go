package main

import (
	"os/exec"
	"strings"
	"testing"
)

// Roadmap Phase 103: the chart's NetworkPolicy lets Vinaire out to a Keycloak on plain http - only when both switches are
// on, only the three private ranges, only port 80. Skipped where helm is not installed (CI renders the same cases).
func renderChart(t *testing.T, set ...string) string {
	t.Helper()
	args := []string{"template", "v", "../charts/vinaire", "-n", "v", "-s", "templates/networkpolicy.yaml"}
	for _, s := range set {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "could not find template") {
			return "" // the policy is off: the template renders nothing
		}
		t.Fatalf("helm %v: %v: %s", args, err, out)
	}
	return string(out)
}

func TestTheNetworkPolicyOpensPort80ToPrivateRangesOnlyForAnHTTPKeycloak(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed")
	}
	const rule = "port: 80,"
	cases := []struct {
		name string
		set  []string
		want bool
	}{
		{"policy on, plain http Keycloak allowed", []string{"networkPolicy.enabled=true", "keycloak.allowHttpPrivateNetwork=true"}, true},
		{"policy on, http not allowed", []string{"networkPolicy.enabled=true"}, false},
		{"http allowed, policy off", []string{"keycloak.allowHttpPrivateNetwork=true"}, false},
		{"opted out of the automatic rule", []string{"networkPolicy.enabled=true", "keycloak.allowHttpPrivateNetwork=true", "networkPolicy.httpKeycloakEgress=false"}, false},
	}
	for _, c := range cases {
		got := renderChart(t, c.set...)
		if has := strings.Contains(got, rule); has != c.want {
			t.Errorf("%s: port 80 rule present=%v, want %v", c.name, has, c.want)
		}
		if strings.Contains(got, "0.0.0.0/0") && strings.Contains(got, rule) {
			// the 443 rule uses 0.0.0.0/0; the port 80 rule must not share a block with it
			for _, block := range strings.Split(got, "    - to:") {
				if strings.Contains(block, rule) && strings.Contains(block, "0.0.0.0/0") {
					t.Errorf("%s: port 80 is open to every address", c.name)
				}
			}
		}
	}
	on := renderChart(t, "networkPolicy.enabled=true", "keycloak.allowHttpPrivateNetwork=true")
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		if !strings.Contains(on, cidr) {
			t.Errorf("the private range %s is missing", cidr)
		}
	}
	for _, never := range []string{"100.64.0.0/10", "169.254.0.0/16"} {
		// 169.254.0.0/16 appears only as the "except" of the 443 rule
		if never == "100.64.0.0/10" && strings.Contains(on, never) {
			t.Errorf("%s was opened without being asked for", never)
		}
	}
	for _, block := range strings.Split(on, "    - to:") {
		if strings.Contains(block, "port: 80,") && strings.Contains(block, "169.254") {
			t.Error("the link-local range is in the port 80 rule")
		}
	}
}
