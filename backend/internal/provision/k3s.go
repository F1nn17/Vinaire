package provision

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// K3s provisions nodes with the official installer (https://get.k3s.io).
type K3s struct{}

func (K3s) Name() string { return "k3s" }

const k3sNodeTokenPath = "/var/lib/rancher/k3s/server/node-token"

// preflightScript only reads. Machine facts come back as FACT:key=value lines.
// %s is the single-quoted server URL.
const preflightScript = `set -e
[ "$(uname -s)" = "Linux" ] || { echo "ERROR: this isn't a Linux machine"; exit 1; }
echo "FACT:hostname=$(hostname)"
echo "FACT:arch=$(uname -m)"
echo "FACT:os=$( . /etc/os-release 2>/dev/null && echo "$PRETTY_NAME" )"
command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 || { echo "ERROR: curl or wget is required"; exit 1; }
command -v systemctl >/dev/null 2>&1 || command -v rc-service >/dev/null 2>&1 || { echo "ERROR: systemd or OpenRC is required"; exit 1; }
if [ -e /usr/local/bin/k3s ] || [ -e /etc/rancher/k3s ]; then
  echo "ERROR: k3s is already installed here (run k3s-agent-uninstall.sh first to start clean)"; exit 1
fi
fetch() { if command -v curl >/dev/null 2>&1; then curl -sk --max-time 8 "$1"; else wget -qO- --no-check-certificate --timeout=8 "$1"; fi; }
SERVER=%s
fetch "$SERVER/ping" | grep -q pong || { echo "ERROR: this machine can't reach the cluster API at $SERVER"; exit 1; }
echo "preflight ok: reached $SERVER"
`

func (K3s) Preflight(ctx context.Context, node Runner, join JoinInfo, log LogFunc) (NodeFacts, error) {
	return runPreflight(ctx, node, fmt.Sprintf(preflightScript, shQuote(join.ServerURL)), log)
}

func (K3s) ValidateJoinToken(token Secret) error { return ValidateToken(token) }

// JoinEndpoint: k3s workers are told the API address explicitly.
func (K3s) JoinEndpoint(serverURL string, _ Secret) (string, error) {
	if serverURL == "" {
		return "", errors.New("the cluster server URL is required")
	}
	if err := ValidateServerURL(serverURL); err != nil {
		return "", err
	}
	return serverURL, nil
}

// runPreflight runs a preflight script and parses its FACT:/ERROR: lines.
func runPreflight(ctx context.Context, node Runner, script string, log LogFunc) (NodeFacts, error) {
	var facts NodeFacts
	var failure string
	err := node.Run(ctx, script, func(line string) {
		if kv, ok := strings.CutPrefix(line, "FACT:"); ok {
			k, v, _ := strings.Cut(kv, "=")
			switch k {
			case "hostname":
				facts.Hostname = strings.ToLower(strings.TrimSpace(v))
			case "arch":
				facts.Arch = v
			case "os":
				facts.OS = v
			case "cpus":
				facts.CPUs, _ = strconv.Atoi(strings.TrimSpace(v))
			case "memkb":
				facts.MemKB, _ = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			case "product_uuid":
				facts.ProductUUID = strings.ToLower(strings.TrimSpace(v))
			case "epoch":
				facts.Epoch, _ = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			}
			return
		}
		if msg, ok := strings.CutPrefix(line, "ERROR: "); ok {
			failure = msg
		}
		log(line)
	})
	if err != nil {
		if failure != "" {
			return facts, errors.New(failure)
		}
		return facts, err
	}
	log(fmt.Sprintf("machine: %s (%s, %s)", facts.Hostname, facts.OS, facts.Arch))
	return facts, nil
}

// PrepareNode: the k3s installer sets up what it needs itself, so there is
// nothing to change on the machine beforehand.
func (K3s) PrepareNode(_ context.Context, _ Runner, _ PrepareOptions, log LogFunc) error {
	log("nothing to prepare — the k3s installer handles its own prerequisites")
	return nil
}

const k3sKubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

// controlPlanePreflight only reads. A machine that already has k3s (or
// anything bound to the API port) is refused untouched.
const controlPlanePreflight = `set -e
[ "$(uname -s)" = "Linux" ] || { echo "ERROR: this isn't a Linux machine"; exit 1; }
echo "FACT:hostname=$(hostname)"
echo "FACT:arch=$(uname -m)"
echo "FACT:os=$( . /etc/os-release 2>/dev/null && echo "$PRETTY_NAME" )"
command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 || { echo "ERROR: curl or wget is required"; exit 1; }
command -v systemctl >/dev/null 2>&1 || command -v rc-service >/dev/null 2>&1 || { echo "ERROR: systemd or OpenRC is required"; exit 1; }
if [ -e /etc/rancher/k3s ] || [ -e /usr/local/bin/k3s ]; then
  echo "ERROR: k3s is already installed on this machine (/etc/rancher/k3s exists) — refusing to overwrite it. To add the cluster that's already here, use Import existing instead."
  exit 1
fi
port_in_use() {
  if command -v ss >/dev/null 2>&1; then ss -ltn 2>/dev/null | grep -Eq '[:.]6443[[:space:]]'; return $?; fi
  if command -v netstat >/dev/null 2>&1; then netstat -ltn 2>/dev/null | grep -Eq '[:.]6443[[:space:]]'; return $?; fi
  grep -qi ':192B [0-9A-F:]* 0A ' /proc/net/tcp /proc/net/tcp6 2>/dev/null
}
if port_in_use; then
  echo "ERROR: port 6443 is already in use on this machine — something else is listening there, so a new cluster can't start"
  exit 1
fi
echo "preflight ok: a clean machine"
`

func (K3s) PreflightControlPlane(ctx context.Context, node Runner, log LogFunc) (NodeFacts, error) {
	return runPreflight(ctx, node, controlPlanePreflight, log)
}

// installControlPlaneScript installs a single-server k3s and waits for its API.
// The version and SAN are validated and single-quoted.
func installControlPlaneScript(o ControlPlaneOptions) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	if o.Version != "" {
		fmt.Fprintf(&b, "export INSTALL_K3S_VERSION=%s\n", shQuote(o.Version))
	}
	fmt.Fprintf(&b, "TLS_SAN=%s\n", shQuote(o.TLSSAN))
	b.WriteString(`tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
echo "downloading the k3s installer from get.k3s.io"
if command -v curl >/dev/null 2>&1; then curl -sfL https://get.k3s.io -o "$tmp"; else wget -qO "$tmp" https://get.k3s.io; fi
[ -s "$tmp" ] || { echo "ERROR: couldn't download the k3s installer"; exit 1; }
sh "$tmp" server --tls-san "$TLS_SAN" --write-kubeconfig-mode 600
echo "waiting for the API server to become ready"
i=0
until k3s kubectl get --raw /readyz >/dev/null 2>&1; do
  i=$((i+1))
  if [ "$i" -ge 90 ]; then
    echo "ERROR: the API server didn't become ready in time; recent log:"
    journalctl -u k3s -n 25 --no-pager 2>&1 || true
    exit 1
  fi
  sleep 2
done
echo "k3s server is ready"
`)
	return b.String()
}

func (K3s) InstallControlPlane(ctx context.Context, node Runner, opts ControlPlaneOptions, log LogFunc) error {
	if !hostRe.MatchString(opts.TLSSAN) && net.ParseIP(opts.TLSSAN) == nil {
		return errors.New("the certificate address (--tls-san) must be an IP address or a hostname")
	}
	if err := ValidateVersion(opts.Version); err != nil {
		return err
	}
	return node.Run(ctx, installControlPlaneScript(opts), log)
}

// FetchKubeconfig reads k3s's admin kubeconfig. Capture keeps it out of every
// log; it comes back as a Secret.
func (K3s) FetchKubeconfig(ctx context.Context, controlPlane Runner) (Secret, error) {
	out, err := controlPlane.Capture(ctx, "cat "+k3sKubeconfigPath+"\n")
	if err != nil {
		return "", fmt.Errorf("reading %s (is this a k3s server, and can this user read it as root/sudo?): %w", k3sKubeconfigPath, err)
	}
	if !strings.Contains(out, "clusters:") {
		return "", errors.New("the file at " + k3sKubeconfigPath + " doesn't look like a kubeconfig")
	}
	return Secret(out), nil
}

// JoinTokenFromControlPlane reads the node token off the server. It uses
// Capture so the value goes nowhere but back to the caller.
func (K3s) JoinTokenFromControlPlane(ctx context.Context, controlPlane Runner) (Secret, error) {
	out, err := controlPlane.Capture(ctx, "cat "+k3sNodeTokenPath+"\n")
	if err != nil {
		return "", fmt.Errorf("reading the join token from the control plane (is it a k3s server?): %w", err)
	}
	token := Secret(strings.TrimSpace(out))
	if err := ValidateToken(token); err != nil {
		return "", errors.New("the control plane returned something that doesn't look like a k3s node token")
	}
	return token, nil
}

// joinScript installs the agent. Values are single-quoted AND were validated
// against strict patterns before getting here. The installer is downloaded to
// a file first so a failed download can't masquerade as success (piping an
// empty script into sh exits 0).
func joinScript(j JoinInfo) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "export K3S_URL=%s\n", shQuote(j.ServerURL))
	fmt.Fprintf(&b, "export K3S_TOKEN=%s\n", shQuote(j.Token.Reveal()))
	if j.Version != "" {
		fmt.Fprintf(&b, "export INSTALL_K3S_VERSION=%s\n", shQuote(j.Version))
	}
	if j.NodeName != "" {
		fmt.Fprintf(&b, "export K3S_NODE_NAME=%s\n", shQuote(j.NodeName))
	}
	b.WriteString(`tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
echo "downloading the k3s installer from get.k3s.io"
if command -v curl >/dev/null 2>&1; then curl -sfL https://get.k3s.io -o "$tmp"; else wget -qO "$tmp" https://get.k3s.io; fi
[ -s "$tmp" ] || { echo "ERROR: couldn't download the k3s installer"; exit 1; }
sh "$tmp" agent
if command -v systemctl >/dev/null 2>&1; then
  if ! systemctl is-active --quiet k3s-agent; then
    echo "ERROR: the k3s-agent service isn't running; recent log:"
    journalctl -u k3s-agent -n 25 --no-pager 2>&1 || true
    exit 1
  fi
fi
echo "k3s agent installed and running"
`)
	return b.String()
}

func (K3s) JoinWorker(ctx context.Context, node Runner, join JoinInfo, log LogFunc) error {
	if err := ValidateServerURL(join.ServerURL); err != nil {
		return err
	}
	if err := ValidateToken(join.Token); err != nil {
		return err
	}
	if err := ValidateVersion(join.Version); err != nil {
		return err
	}
	if err := ValidateNodeName(join.NodeName); err != nil {
		return err
	}
	return node.Run(ctx, joinScript(join), log)
}

const k3sReset = `set +e
if [ -x /usr/local/bin/k3s-agent-uninstall.sh ]; then
  echo "removing the k3s agent"
  /usr/local/bin/k3s-agent-uninstall.sh
elif [ -x /usr/local/bin/k3s-uninstall.sh ]; then
  echo "removing k3s (server)"
  /usr/local/bin/k3s-uninstall.sh
else
  echo "ERROR: no k3s installation was found on this machine"
  exit 1
fi
echo "reset complete — this machine can be set up again from scratch"
`

// Reset runs k3s's own uninstall script — destructive.
func (K3s) Reset(ctx context.Context, node Runner, log LogFunc) error {
	return node.Run(ctx, k3sReset, log)
}
