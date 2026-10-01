package provision

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const dialTimeout = 15 * time.Second

// HostKeyInfo is what the user is asked to confirm before any credentials are
// sent: the server's SSH host key fingerprint (OpenSSH's SHA256:... format,
// the same string `ssh-keygen -lf` prints).
type HostKeyInfo struct {
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"keyType"`
}

var errHostKeyCaptured = errors.New("host key captured")

// FetchHostKey connects, performs only the key exchange, records the server's
// host key and hangs up. No authentication is attempted, so no credentials
// are involved at this step.
func FetchHostKey(ctx context.Context, address string) (HostKeyInfo, error) {
	var info HostKeyInfo
	cfg := &ssh.ClientConfig{
		User: "vinaire-hostkey-probe",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			info = HostKeyInfo{Fingerprint: ssh.FingerprintSHA256(key), KeyType: key.Type()}
			return errHostKeyCaptured
		},
		Timeout: dialTimeout,
	}
	conn, err := dialTCP(ctx, address)
	if err != nil {
		return HostKeyInfo{}, err
	}
	defer conn.Close()
	_, _, _, err = ssh.NewClientConn(conn, address, cfg)
	if info.Fingerprint != "" {
		return info, nil
	}
	if err == nil {
		err = errors.New("no host key received")
	}
	return HostKeyInfo{}, fmt.Errorf("SSH handshake with %s failed: %w", address, err)
}

func dialTCP(ctx context.Context, address string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to %s: %w", address, err)
	}
	return conn, nil
}

// Credentials are one SSH login. Exactly one of Password / PrivateKey is used
// (key wins if both are somehow set). They live only as long as the run.
type Credentials struct {
	Username   string
	Password   Secret
	PrivateKey Secret
	Passphrase Secret
}

func (c Credentials) authMethods() ([]ssh.AuthMethod, error) {
	if c.PrivateKey != "" {
		key := []byte(c.PrivateKey.Reveal())
		signer, err := ssh.ParsePrivateKey(key)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			if c.Passphrase == "" {
				return nil, errors.New("the private key is passphrase-protected — enter its passphrase")
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(c.Passphrase.Reveal()))
		}
		if err != nil {
			return nil, errors.New("couldn't read the private key (wrong passphrase, or not a valid OpenSSH/PEM private key)")
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	if c.Password == "" {
		return nil, errors.New("a password or a private key is required")
	}
	pw := c.Password.Reveal()
	return []ssh.AuthMethod{
		ssh.Password(pw),
		// Some servers only offer keyboard-interactive for password logins.
		ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = pw
			}
			return answers, nil
		}),
	}, nil
}

// Runner runs scripts on a remote machine as root.
type Runner interface {
	// Run executes script (sent over stdin, so nothing secret lands in a
	// process listing) and streams its combined output line by line.
	Run(ctx context.Context, script string, onLine func(string)) error
	// Capture executes script and returns its stdout without forwarding it
	// anywhere — for fetching a secret such as the join token.
	Capture(ctx context.Context, script string) (string, error)
	Close() error
}

type sshRunner struct {
	client *ssh.Client
	creds  Credentials
}

// Dial opens an SSH connection that only proceeds if the server's host key
// fingerprint equals the one the user confirmed (ssh.HostKeyCallback, not
// InsecureIgnoreHostKey). A mismatch aborts during the handshake, before any
// credential is sent.
func Dial(ctx context.Context, address string, creds Credentials, pinnedFingerprint string) (Runner, error) {
	auth, err := creds.authMethods()
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User: creds.Username,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if got := ssh.FingerprintSHA256(key); got != pinnedFingerprint {
				return fmt.Errorf("host key mismatch: expected %s but the server presented %s — possible man-in-the-middle, aborted before sending credentials", pinnedFingerprint, got)
			}
			return nil
		},
		Timeout: dialTimeout,
	}
	conn, err := dialTCP(ctx, address)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, address, cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH login to %s failed: %w", address, err)
	}
	return &sshRunner{client: ssh.NewClient(c, chans, reqs), creds: creds}, nil
}

func (r *sshRunner) Close() error { return r.client.Close() }

// command picks how the script gets root. A non-root user goes through sudo:
// with a password login the password is fed to `sudo -S` on stdin (never as an
// argument); with a key login sudo must be passwordless (`-n`).
func (r *sshRunner) command() (cmd, stdinPrefix string) {
	if r.creds.Username == "root" {
		return "sh -s", ""
	}
	if r.creds.PrivateKey == "" && r.creds.Password != "" {
		return "sudo -S -p '' sh -s", r.creds.Password.Reveal() + "\n"
	}
	return "sudo -n sh -s", ""
}

// lineWriter turns a byte stream into lines.
type lineWriter struct {
	mu     sync.Mutex
	buf    []byte
	onLine func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			if len(w.buf) > 8192 {
				w.emit(w.buf)
				w.buf = nil
			}
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *lineWriter) emit(b []byte) {
	if line := strings.TrimRight(string(b), "\r"); line != "" {
		w.onLine(line)
	}
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = nil
	}
}

func (r *sshRunner) exec(ctx context.Context, script string, stdout, stderr io.Writer) error {
	sess, err := r.client.NewSession()
	if err != nil {
		return fmt.Errorf("opening SSH session: %w", err)
	}
	defer sess.Close()

	cmd, prefix := r.command()
	sess.Stdin = strings.NewReader(prefix + script)
	sess.Stdout = stdout
	sess.Stderr = stderr

	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case err := <-done:
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("command failed (exit status %d)", exit.ExitStatus())
		}
		return err
	case <-ctx.Done():
		_ = sess.Close()
		return ctx.Err()
	}
}

func (r *sshRunner) Run(ctx context.Context, script string, onLine func(string)) error {
	w := &lineWriter{onLine: onLine}
	err := r.exec(ctx, script, w, w)
	w.flush()
	return err
}

func (r *sshRunner) Capture(ctx context.Context, script string) (string, error) {
	var out bytes.Buffer
	var errOut lineWriter
	var lastErr string
	errOut.onLine = func(l string) { lastErr = l }
	if err := r.exec(ctx, script, &out, &errOut); err != nil {
		errOut.flush()
		if lastErr != "" {
			return "", fmt.Errorf("%w: %s", err, strconv.Quote(lastErr))
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
