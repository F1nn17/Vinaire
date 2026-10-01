package provision

import (
	"context"
	"errors"
	"fmt"
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

func (K3s) Preflight(ctx context.Context, node Runner, serverURL string, log LogFunc) (NodeFacts, error) {
	var facts NodeFacts
	var failure string
	err := node.Run(ctx, fmt.Sprintf(preflightScript, shQuote(serverURL)), func(line string) {
		if kv, ok := strings.CutPrefix(line, "FACT:"); ok {
			k, v, _ := strings.Cut(kv, "=")
			switch k {
			case "hostname":
				facts.Hostname = strings.ToLower(strings.TrimSpace(v))
			case "arch":
				facts.Arch = v
			case "os":
				facts.OS = v
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
func (K3s) PrepareNode(_ context.Context, _ Runner, log LogFunc) error {
	log("nothing to prepare — the k3s installer handles its own prerequisites")
	return nil
}

func (K3s) InstallControlPlane(context.Context, Runner, LogFunc) error {
	return fmt.Errorf("k3s control-plane install: %w (Roadmap Phase 34)", ErrNotImplemented)
}

func (K3s) FetchKubeconfig(context.Context, Runner) ([]byte, error) {
	return nil, fmt.Errorf("k3s kubeconfig fetch: %w (Roadmap Phase 34)", ErrNotImplemented)
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
