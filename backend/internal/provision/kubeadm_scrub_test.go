package provision

import (
	"strings"
	"testing"
)

func TestScrubKubeadmMasksJoinCredentials(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	cases := map[string]string{
		"[bootstrap-token] Using token: gftt5q.x88y4p4dtjlymjor":        "[bootstrap-token] Using token: [REDACTED]",
		"kubeadm join 10.0.0.5:6443 --token gftt5q.x88y4p4dtjlymjor \\": "kubeadm join 10.0.0.5:6443 --token [REDACTED] \\",
		"\t--discovery-token-ca-cert-hash sha256:" + hash:               "\t--discovery-token-ca-cert-hash sha256:[REDACTED]",
		"--control-plane --certificate-key " + strings.Repeat("0f", 32): "--control-plane --certificate-key [REDACTED]",
	}
	for in, want := range cases {
		if got := scrubKubeadm(in); got != want {
			t.Errorf("scrubKubeadm(%q)\n got  %q\n want %q", in, got, want)
		}
	}
	// ordinary output is left alone
	for _, line := range []string{
		"[init] Using Kubernetes version: v1.37.1",
		"[certs] Generating \"apiserver\" certificate and key",
		"kubelet is healthy after 501.5ms",
		"your Kubernetes control-plane has initialized successfully!",
	} {
		if got := scrubKubeadm(line); got != line {
			t.Errorf("ordinary line was changed: %q -> %q", line, got)
		}
	}
}

func TestKubeadmRunsAreScrubbed(t *testing.T) {
	f := &fakeRunner{output: []string{"[bootstrap-token] Using token: gftt5q.x88y4p4dtjlymjor", "done"}}
	var logged []string
	k := Kubeadm{Config: DefaultKubeadmConfig()}
	err := k.InstallControlPlane(t.Context(), f, ControlPlaneOptions{TLSSAN: "10.0.0.5", Kubeadm: KubeadmOptions{Minor: k.Config.DefaultMinor()}}, func(l string) { logged = append(logged, l) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(logged, "\n"), "gftt5q") {
		t.Errorf("the bootstrap token reached the log: %v", logged)
	}
}
