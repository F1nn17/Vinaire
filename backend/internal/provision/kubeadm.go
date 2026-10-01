package provision

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Kubeadm provisions upstream Kubernetes with kubeadm on Debian/Ubuntu
// (Roadmap Phase 35). Everything it may install — Kubernetes minors, CNI
// manifests and their pinned versions — comes from its KubeadmConfig, never
// "latest".
type Kubeadm struct {
	Config KubeadmConfig
}

func (Kubeadm) Name() string { return "kubeadm" }

const kubeadmAdminConf = "/etc/kubernetes/admin.conf"

// joinCommandRe is the one shape of join credential a worker accepts: exactly
// what `kubeadm token create --print-join-command` prints. A control-plane join
// (--control-plane, --certificate-key) is deliberately not accepted — this is
// about workers, and a single control plane.
var joinCommandRe = regexp.MustCompile(
	`^kubeadm join (\[[0-9a-fA-F:]+\]|[A-Za-z0-9][A-Za-z0-9.\-]*):([0-9]{1,5}) ` +
		`--token ([a-z0-9]{6}\.[a-z0-9]{16}) ` +
		`--discovery-token-ca-cert-hash (sha256:[a-f0-9]{64})$`)

var (
	endpointRe      = regexp.MustCompile(`^(\[[0-9a-fA-F:]+\]|[A-Za-z0-9][A-Za-z0-9.\-]*)(:[0-9]{1,5})?$`)
	preflightNameRe = regexp.MustCompile(`^[A-Za-z0-9]{2,40}$`)
)

func (Kubeadm) ValidateJoinToken(token Secret) error {
	if !joinCommandRe.MatchString(strings.TrimSpace(token.Reveal())) {
		return errors.New("the join command must look like: kubeadm join <host>:6443 --token <id>.<secret> --discovery-token-ca-cert-hash sha256:<hash> (as printed by `kubeadm token create --print-join-command`)")
	}
	return nil
}

// JoinEndpoint reads the API address out of the join command itself — the
// command is the source of truth for where a worker must connect.
func (k Kubeadm) JoinEndpoint(_ string, token Secret) (string, error) {
	m := joinCommandRe.FindStringSubmatch(strings.TrimSpace(token.Reveal()))
	if m == nil {
		return "", k.ValidateJoinToken(token)
	}
	return "https://" + m[1] + ":" + m[2], nil
}

// --- preflight ---

// kubeadmBase is the read-only part both preflights share: OS, resources,
// leftovers of a previous install, reachability of what the install downloads,
// and clock sanity. FACT:/ERROR: lines are parsed by runPreflight.
const kubeadmBase = `set -e
[ "$(uname -s)" = "Linux" ] || { echo "ERROR: this isn't a Linux machine"; exit 1; }
. /etc/os-release 2>/dev/null || true
case " ${ID:-} ${ID_LIKE:-} " in
  *" debian "*|*" ubuntu "*) ;;
  *) echo "ERROR: kubeadm provisioning supports Debian and Ubuntu only (this is ${PRETTY_NAME:-an unknown OS}); RHEL-family systems aren't supported yet"; exit 1;;
esac
CPUS=$(nproc)
MEMKB=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo)
echo "FACT:hostname=$(hostname)"
echo "FACT:arch=$(uname -m)"
echo "FACT:os=${PRETTY_NAME:-}"
echo "FACT:cpus=$CPUS"
echo "FACT:memkb=$MEMKB"
echo "FACT:product_uuid=$(cat /sys/class/dmi/id/product_uuid 2>/dev/null || true)"
echo "FACT:epoch=$(date +%s)"
[ "$CPUS" -ge 2 ] || { echo "ERROR: kubeadm needs at least 2 CPUs (this machine has $CPUS)"; exit 1; }
[ "$MEMKB" -ge 1700000 ] || { echo "ERROR: kubeadm needs at least about 2 GB of RAM (this machine has $((MEMKB / 1024)) MB)"; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "ERROR: curl is required"; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "ERROR: systemd is required"; exit 1; }
if [ -e /etc/kubernetes/admin.conf ] || [ -e /etc/kubernetes/kubelet.conf ]; then
  echo "ERROR: this machine already belongs to a Kubernetes cluster (/etc/kubernetes has a kubeconfig). If an earlier install failed partway, use Reset on it first"
  exit 1
fi
if [ "$(awk 'NR>1' /proc/swaps | wc -l)" -gt 0 ]; then
  echo "swap is on — it will be turned off while preparing the machine"
fi
port_busy() {
  if command -v ss >/dev/null 2>&1; then ss -ltn 2>/dev/null | grep -Eq "[:.]$1[[:space:]]"; return $?; fi
  if command -v netstat >/dev/null 2>&1; then netstat -ltn 2>/dev/null | grep -Eq "[:.]$1[[:space:]]"; return $?; fi
  HEX=$(printf '%04X' "$1")
  grep -qi ":$HEX [0-9A-F:]* 0A " /proc/net/tcp /proc/net/tcp6 2>/dev/null
}
reach() { CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$1" 2>/dev/null || echo 000); [ "$CODE" != "000" ]; }
reachk() { CODE=$(curl -sk -o /dev/null -w '%{http_code}' --max-time 10 "$1" 2>/dev/null || echo 000); [ "$CODE" != "000" ]; }
reach https://pkgs.k8s.io/ || { echo "ERROR: can't reach pkgs.k8s.io, which the kubeadm, kubelet and kubectl packages come from"; exit 1; }
reach https://registry.k8s.io/v2/ || { echo "ERROR: can't reach registry.k8s.io, which the Kubernetes images are pulled from"; exit 1; }
if command -v timedatectl >/dev/null 2>&1 && [ "$(timedatectl show -p NTPSynchronized --value 2>/dev/null)" = "no" ]; then
  echo "warning: this machine's clock isn't NTP-synchronized — clock drift between nodes breaks joining and certificates"
fi
`

const kubeadmControlPlanePorts = `for P in 6443 2379 2380 10250 10251 10252 10253 10254 10255 10256 10257 10258 10259; do
  if port_busy "$P"; then echo "ERROR: port $P is already in use on this machine — a control plane needs 6443, 2379-2380 and 10250-10259 free"; exit 1; fi
done
echo "preflight ok: a clean machine for a new control plane"
`

const kubeadmWorkerChecks = `if port_busy 10250; then echo "ERROR: port 10250 is already in use on this machine"; exit 1; fi
reachk "@API@/livez" || { echo "ERROR: this machine can't reach the cluster API at @API@"; exit 1; }
echo "preflight ok: reached @API@"
`

func (k Kubeadm) Preflight(ctx context.Context, node Runner, join JoinInfo, log LogFunc) (NodeFacts, error) {
	script := kubeadmBase + strings.ReplaceAll(kubeadmWorkerChecks, "@API@", shQuoteInner(join.ServerURL))
	return runPreflight(ctx, node, script, log)
}

func (k Kubeadm) PreflightControlPlane(ctx context.Context, node Runner, log LogFunc) (NodeFacts, error) {
	return runPreflight(ctx, node, kubeadmBase+kubeadmControlPlanePorts, log)
}

// shQuoteInner escapes a validated URL for use inside the double-quoted strings
// of the fixed scripts above (the URL already matched serverURLRe, so this is
// belt and braces).
func shQuoteInner(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(s)
}

// --- prepare ---

// kubeadmPrepare turns a Debian/Ubuntu machine into one kubeadm can run on:
// swap off, kernel modules and sysctls, containerd with the systemd cgroup
// driver, and the pinned-minor Kubernetes packages held against upgrades.
const kubeadmPrepare = `set -e
export DEBIAN_FRONTEND=noninteractive
MINOR=@MINOR@

echo "turning swap off"
swapoff -a 2>/dev/null || true
sed -ri '/[[:space:]]swap[[:space:]]/ s/^([^#])/#\1/' /etc/fstab 2>/dev/null || true
if [ "$(awk 'NR>1' /proc/swaps | wc -l)" -gt 0 ]; then
  echo "ERROR: swap is still on and couldn't be turned off — kubelet won't start with swap enabled"; exit 1
fi

echo "enabling kernel modules and sysctls"
printf 'overlay\nbr_netfilter\n' > /etc/modules-load.d/k8s.conf
if command -v modprobe >/dev/null 2>&1; then
  modprobe overlay 2>/dev/null || true
  modprobe br_netfilter 2>/dev/null || true
fi
[ -d /proc/sys/net/bridge ] || { echo "ERROR: the br_netfilter kernel module isn't available on this machine"; exit 1; }
grep -qw overlay /proc/filesystems || { echo "ERROR: overlay filesystem support is missing on this machine"; exit 1; }
cat > /etc/sysctl.d/k8s.conf <<'EOSYSCTL'
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOSYSCTL
sysctl -p /etc/sysctl.d/k8s.conf >/dev/null

echo "installing containerd"
apt-get update -qq
apt-get install -y -qq apt-transport-https ca-certificates curl gpg containerd >/dev/null
mkdir -p /etc/containerd
containerd config default > /etc/containerd/config.toml
sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
grep -q 'SystemdCgroup = true' /etc/containerd/config.toml || { echo "ERROR: couldn't set the systemd cgroup driver in containerd's config"; exit 1; }
systemctl enable containerd >/dev/null 2>&1
systemctl restart containerd

echo "adding the Kubernetes v$MINOR package repository"
mkdir -p -m 755 /etc/apt/keyrings
KEYTMP=$(mktemp)
trap 'rm -f "$KEYTMP"' EXIT
curl -fsSL "https://pkgs.k8s.io/core:/stable:/v$MINOR/deb/Release.key" -o "$KEYTMP" || { echo "ERROR: couldn't download the Kubernetes v$MINOR repository key"; exit 1; }
gpg --dearmor --yes -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg "$KEYTMP"
echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v$MINOR/deb/ /" > /etc/apt/sources.list.d/kubernetes.list
apt-get update -qq

echo "installing kubelet, kubeadm and kubectl"
apt-get install -y -qq kubelet kubeadm kubectl >/dev/null
apt-mark hold kubelet kubeadm kubectl >/dev/null
systemctl enable kubelet >/dev/null 2>&1
echo "machine prepared for Kubernetes v$MINOR"
`

func (k Kubeadm) PrepareNode(ctx context.Context, node Runner, opts PrepareOptions, log LogFunc) error {
	minor := opts.K8sMinor
	if minor == "" {
		minor = k.Config.DefaultMinor()
	}
	if !k.Config.HasMinor(minor) {
		return fmt.Errorf("Kubernetes %s isn't in this Vinaire's supported list", minor)
	}
	return node.Run(ctx, strings.ReplaceAll(kubeadmPrepare, "@MINOR@", minor), log)
}

// --- scrubbing ---

// kubeadm prints its own join credentials as it goes: the bootstrap token
// ("Using token: abcdef.0123456789abcdef"), and at the end a ready-to-paste
// join command with the token and the CA hash. Those let anyone with a route to
// the API add a node for the next 24 hours, so they never reach the job log —
// which the UI shows — even though the run itself is fine with them.
var (
	bootstrapTokenRe = regexp.MustCompile(`\b[a-z0-9]{6}\.[a-z0-9]{16}\b`)
	caHashRe         = regexp.MustCompile(`sha256:[a-f0-9]{64}`)
	certKeyRe        = regexp.MustCompile(`(--certificate-key\s+)[a-f0-9]{16,}`)
)

// scrubKubeadm masks kubeadm's join credentials in a line of its output.
func scrubKubeadm(line string) string {
	line = bootstrapTokenRe.ReplaceAllString(line, redacted)
	line = caHashRe.ReplaceAllString(line, "sha256:"+redacted)
	return certKeyRe.ReplaceAllString(line, "${1}"+redacted)
}

func scrubbed(log LogFunc) LogFunc {
	return func(line string) { log(scrubKubeadm(line)) }
}

// --- control plane ---

// installControlPlane builds the kubeadm init script. Every interpolated value
// was validated and is single-quoted.
func (k Kubeadm) installControlPlaneScript(o ControlPlaneOptions, cni KubeadmCNI) string {
	var args []string
	args = append(args, "--pod-network-cidr="+shQuote(cni.PodCIDR))
	args = append(args, "--apiserver-cert-extra-sans="+shQuote(o.TLSSAN))
	if o.Kubeadm.ControlPlaneEndpoint != "" {
		args = append(args, "--control-plane-endpoint="+shQuote(o.Kubeadm.ControlPlaneEndpoint))
	}
	if len(o.Kubeadm.IgnorePreflightErrors) > 0 {
		args = append(args, "--ignore-preflight-errors="+shQuote(strings.Join(o.Kubeadm.IgnorePreflightErrors, ",")))
	}

	var b strings.Builder
	b.WriteString("set -e\nexport KUBECONFIG=" + kubeadmAdminConf + "\n")
	b.WriteString(`echo "running kubeadm init (this pulls the control plane images — a few minutes)"
if ! kubeadm init ` + strings.Join(args, " ") + `; then
  echo "ERROR: kubeadm init failed; recent kubelet log:"
  journalctl -u kubelet -n 30 --no-pager 2>&1 || true
  exit 1
fi
`)
	b.WriteString("CNITMP=$(mktemp)\ntrap 'rm -f \"$CNITMP\"' EXIT\n")
	b.WriteString(`echo "installing the ` + cni.Label + ` pod network (` + cni.Version + `)"
curl -fsSL ` + shQuote(cni.Manifest) + ` -o "$CNITMP" || { echo "ERROR: couldn't download the ` + cni.Label + ` manifest"; exit 1; }
kubectl apply -f "$CNITMP"
`)
	if o.Kubeadm.SingleNode {
		b.WriteString(`echo "single-node: allowing workloads on the control plane"
kubectl taint nodes --all node-role.kubernetes.io/control-plane- 2>/dev/null || true
`)
	}
	b.WriteString(`echo "waiting for the node to become Ready"
kubectl wait --for=condition=Ready node --all --timeout=300s || { echo "ERROR: the node didn't become Ready in time"; kubectl get pods -A 2>&1 || true; exit 1; }
echo "waiting for CoreDNS"
kubectl -n kube-system rollout status deployment/coredns --timeout=300s || { echo "ERROR: CoreDNS didn't become ready in time"; exit 1; }
echo "kubernetes control plane is ready"
`)
	return b.String()
}

func (k Kubeadm) InstallControlPlane(ctx context.Context, node Runner, opts ControlPlaneOptions, log LogFunc) error {
	if !hostRe.MatchString(opts.TLSSAN) && net.ParseIP(opts.TLSSAN) == nil {
		return errors.New("the certificate address (--apiserver-cert-extra-sans) must be an IP address or a hostname")
	}
	if !k.Config.HasMinor(opts.Kubeadm.Minor) {
		return fmt.Errorf("Kubernetes %q isn't in this Vinaire's supported list", opts.Kubeadm.Minor)
	}
	cni, ok := k.Config.CNI(opts.Kubeadm.CNI)
	if !ok {
		return fmt.Errorf("unknown pod network %q", opts.Kubeadm.CNI)
	}
	if ep := opts.Kubeadm.ControlPlaneEndpoint; ep != "" && !endpointRe.MatchString(ep) {
		return errors.New("the control-plane endpoint must be a host name or IP, optionally with :port")
	}
	for _, n := range opts.Kubeadm.IgnorePreflightErrors {
		if !preflightNameRe.MatchString(n) {
			return fmt.Errorf("%q isn't a valid kubeadm preflight check name", n)
		}
	}
	return node.Run(ctx, k.installControlPlaneScript(opts, cni), scrubbed(log))
}

func (Kubeadm) FetchKubeconfig(ctx context.Context, controlPlane Runner) (Secret, error) {
	out, err := controlPlane.Capture(ctx, "cat "+kubeadmAdminConf+"\n")
	if err != nil {
		return "", fmt.Errorf("reading %s (is this a kubeadm control plane, and can this user read it as root/sudo?): %w", kubeadmAdminConf, err)
	}
	if !strings.Contains(out, "clusters:") {
		return "", errors.New("the file at " + kubeadmAdminConf + " doesn't look like a kubeconfig")
	}
	return Secret(out), nil
}

// --- workers ---

// JoinTokenFromControlPlane mints a fresh bootstrap token and returns the whole
// join command (token + CA hash). Capture keeps it out of every log.
func (k Kubeadm) JoinTokenFromControlPlane(ctx context.Context, controlPlane Runner) (Secret, error) {
	out, err := controlPlane.Capture(ctx, "kubeadm token create --print-join-command\n")
	if err != nil {
		return "", fmt.Errorf("creating a join command on the control plane (is it a kubeadm control plane?): %w", err)
	}
	cmd := Secret(strings.TrimSpace(out))
	if err := k.ValidateJoinToken(cmd); err != nil {
		return "", errors.New("the control plane returned something that doesn't look like a kubeadm join command")
	}
	return cmd, nil
}

func (k Kubeadm) JoinWorker(ctx context.Context, node Runner, join JoinInfo, log LogFunc) error {
	if err := k.ValidateJoinToken(join.Token); err != nil {
		return err
	}
	if err := ValidateNodeName(join.NodeName); err != nil {
		return err
	}
	// Rebuilt from the parsed parts, each single-quoted, rather than run as
	// the text we were handed.
	m := joinCommandRe.FindStringSubmatch(strings.TrimSpace(join.Token.Reveal()))
	cmd := "kubeadm join " + shQuote(m[1]+":"+m[2]) + " --token " + shQuote(m[3]) + " --discovery-token-ca-cert-hash " + shQuote(m[4])
	if join.NodeName != "" {
		cmd += " --node-name " + shQuote(join.NodeName)
	}
	script := "set -e\necho \"joining the cluster with kubeadm\"\nif ! " + cmd + `; then
  echo "ERROR: kubeadm join failed; recent kubelet log:"
  journalctl -u kubelet -n 30 --no-pager 2>&1 || true
  exit 1
fi
systemctl is-active --quiet kubelet || { echo "ERROR: kubelet isn't running after the join"; exit 1; }
echo "joined the cluster"
`
	return node.Run(ctx, script, scrubbed(log))
}

// --- reset ---

const kubeadmReset = `set +e
echo "running kubeadm reset"
if command -v kubeadm >/dev/null 2>&1; then kubeadm reset -f; else echo "kubeadm isn't installed here; cleaning up what's left"; fi
systemctl stop kubelet 2>/dev/null
echo "removing CNI configuration and leftover state"
rm -rf /etc/cni/net.d /var/lib/cni /var/lib/kubelet/pki /root/.kube
echo "flushing iptables rules"
for T in "" "-t nat" "-t mangle" "-t raw"; do
  iptables $T -F 2>/dev/null; iptables $T -X 2>/dev/null
  ip6tables $T -F 2>/dev/null; ip6tables $T -X 2>/dev/null
done
if command -v ipvsadm >/dev/null 2>&1; then ipvsadm --clear 2>/dev/null; fi
ip link delete cni0 2>/dev/null
ip link delete flannel.1 2>/dev/null
echo "reset complete — this machine can be set up again from scratch"
`

func (Kubeadm) Reset(ctx context.Context, node Runner, log LogFunc) error {
	return node.Run(ctx, kubeadmReset, log)
}
