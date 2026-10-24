package provider

// Acceptance test harness: when TF_ACC is set, TestMain runs `pulsed init`, starts nats-server with the generated
// nats.conf and `pulsed serve` in the dev profile (free ports, temporary directories), mints an administrator
// personal access token with `pulsed admin token` and points the provider at the loopback admin listener.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/aurimasniekis/terraform-provider-pulse/internal/client"
)

// accEnv describes the Pulse server the acceptance tests run against.
type accEnv struct {
	AdminURL    string // loopback admin listener (http://127.0.0.1:PORT)
	AdminToken  string // administrator personal access token
	ViewerToken string // viewer personal access token (read-only)
	work        string
	childEnv    []string
	pulsedBin   string
}

var acc *accEnv

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"pulse": providerserver.NewProtocol6WithError(New("test")()),
}

func TestMain(m *testing.M) {
	// Use a locally installed Terraform CLI so nothing is downloaded.
	if os.Getenv("TF_ACC_TERRAFORM_PATH") == "" {
		if p, err := exec.LookPath("terraform"); err == nil {
			_ = os.Setenv("TF_ACC_TERRAFORM_PATH", p)
		} else if _, err := os.Stat("/opt/homebrew/bin/terraform"); err == nil {
			_ = os.Setenv("TF_ACC_TERRAFORM_PATH", "/opt/homebrew/bin/terraform")
		}
	}
	_ = os.Setenv("CHECKPOINT_DISABLE", "1")
	if os.Getenv("TF_ACC") == "" {
		os.Exit(m.Run())
	}
	env, stop, err := startPulse()
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance harness:", err)
		stop()
		os.Exit(1)
	}
	acc = env
	_ = os.Setenv(EnvAdminURL, env.AdminURL)
	_ = os.Setenv(EnvToken, env.AdminToken)
	_ = os.Unsetenv(EnvTokenFile)
	code := m.Run()
	if code != 0 {
		dumpLog(filepath.Join(env.work, "pulsed.log"))
		dumpLog(filepath.Join(env.work, "nats.log"))
	}
	stop()
	os.Exit(code)
}

func dumpLog(p string) {
	b, err := os.ReadFile(p) //nolint:gosec // test log
	if err != nil {
		return
	}
	if len(b) > 8000 {
		b = b[len(b)-8000:]
	}
	fmt.Fprintf(os.Stderr, "--- tail of %s\n%s\n", p, b)
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func binary(env, name string) string {
	if p := os.Getenv(env); p != "" {
		return p
	}
	return filepath.Join(repoRoot(), "bin", name)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func freePorts(n int) ([]int, error) {
	out := make([]int, 0, n)
	for range n {
		p, err := freePort()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// startPulse launches nats-server and pulsed and returns the environment and a stop function.
func startPulse() (*accEnv, func(), error) {
	var procs []*exec.Cmd
	work, err := os.MkdirTemp("", "pulse-tfacc-")
	stop := func() {
		for i := len(procs) - 1; i >= 0; i-- {
			if p := procs[i].Process; p != nil {
				_ = p.Signal(os.Interrupt)
				done := make(chan struct{})
				go func() { _, _ = procs[i].Process.Wait(); close(done) }()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					_ = p.Kill()
				}
			}
		}
		if work != "" && os.Getenv("PULSE_ACC_KEEP") == "" {
			_ = os.RemoveAll(work)
		}
	}
	if err != nil {
		return nil, stop, err
	}
	if err := os.Chmod(work, 0o700); err != nil {
		return nil, stop, err
	}
	pulsedBin, natsBin := binary("PULSE_PULSED_BINARY", "pulsed"), binary("PULSE_NATS_SERVER_BINARY", "nats-server")
	for _, b := range []string{pulsedBin, natsBin} {
		if _, err := os.Stat(b); err != nil {
			return nil, stop, fmt.Errorf("%s not found (run `make pulse-bin`, or set PULSE_PULSED_BINARY / PULSE_NATS_SERVER_BINARY)", b)
		}
	}
	ports, err := freePorts(6)
	if err != nil {
		return nil, stop, err
	}
	natsPort, monPort, wsPort, adminPort, metricsPort := ports[0], ports[1], ports[2], ports[3], ports[4]
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, stop, err
	}
	keyFile := filepath.Join(work, "master.key")
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, stop, err
	}
	// pulsed refuses unknown PULSE_* variables, so the provider's own (PULSE_ADMIN_URL, PULSE_ADMIN_TOKEN) are dropped.
	childEnv := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PULSE_") {
			childEnv = append(childEnv, kv)
		}
	}
	childEnv = append(childEnv,
		"PULSE_PROFILE=dev",
		"PULSE_LOG_LEVEL=info",
		"PULSE_MASTER_KEY_FILE="+keyFile,
		"PULSE_DATA_DIR="+filepath.Join(work, "pulsed"),
		"PULSE_NATS_DIR="+filepath.Join(work, "nats"),
		"PULSE_RAW_HOST=localhost",
		fmt.Sprintf("PULSE_NATS_CLIENT_LISTEN=127.0.0.1:%d", natsPort),
		fmt.Sprintf("PULSE_NATS_MONITOR_LISTEN=127.0.0.1:%d", monPort),
		fmt.Sprintf("PULSE_NATS_WEBSOCKET_LISTEN=127.0.0.1:%d", wsPort),
		fmt.Sprintf("PULSE_NATS_URL=tls://127.0.0.1:%d", natsPort),
		"PULSE_NATS_TLS_NAME=nats",
		fmt.Sprintf("PULSE_NATS_WEBSOCKET_URL=http://127.0.0.1:%d", wsPort),
		fmt.Sprintf("PULSE_ADMIN_ADDR=127.0.0.1:%d", adminPort),
		fmt.Sprintf("PULSE_ADMIN_URL=http://127.0.0.1:%d", adminPort),
		fmt.Sprintf("PULSE_METRICS_ADDR=127.0.0.1:%d", metricsPort),
		"PULSE_PUBLIC_ADDR=off",
		"PULSE_EVENTS_MAX_BYTES=256MiB",
		// Outbound webhook tests deliver to httptest servers on loopback.
		"PULSE_OUTBOUND_ALLOW=127.0.0.0/8",
	)
	run := func(args ...string) (string, error) {
		cmd := exec.Command(pulsedBin, args...) //nolint:gosec // test binary
		cmd.Env = childEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("pulsed %s: %w: %s", strings.Join(args, " "), err, out)
		}
		return string(out), nil
	}
	if _, err := run("init"); err != nil {
		return nil, stop, err
	}
	start := func(name string, cmd *exec.Cmd) error {
		logf, err := os.Create(filepath.Join(work, name+".log")) //nolint:gosec // test log
		if err != nil {
			return err
		}
		cmd.Stdout, cmd.Stderr, cmd.Env = logf, logf, childEnv
		if err := cmd.Start(); err != nil {
			return err
		}
		procs = append(procs, cmd)
		return nil
	}
	if err := start("nats", exec.Command(natsBin, "-c", filepath.Join(work, "nats", "nats.conf"))); err != nil { //nolint:gosec // test binary
		return nil, stop, err
	}
	if err := start("pulsed", exec.Command(pulsedBin, "serve")); err != nil { //nolint:gosec // test binary
		return nil, stop, err
	}
	if err := waitReady(fmt.Sprintf("http://127.0.0.1:%d/readyz", metricsPort), 60*time.Second); err != nil {
		dumpLog(filepath.Join(work, "pulsed.log"))
		return nil, stop, err
	}
	env := &accEnv{AdminURL: fmt.Sprintf("http://127.0.0.1:%d", adminPort), work: work, childEnv: childEnv, pulsedBin: pulsedBin}
	if env.AdminToken, err = mintToken(run, "tf-admin@example.com", "administrator"); err != nil {
		return nil, stop, err
	}
	if env.ViewerToken, err = mintToken(run, "tf-viewer@example.com", "viewer"); err != nil {
		return nil, stop, err
	}
	return env, stop, nil
}

var patRE = regexp.MustCompile(`pulse_pat_[A-Za-z0-9_-]+`)

func mintToken(run func(...string) (string, error), email, role string) (string, error) {
	out, err := run("admin", "token", "--email", email, "--create", "--role", role, "--days", "1", "--name", "terraform-acc")
	if err != nil {
		return "", err
	}
	tok := patRE.FindString(out)
	if tok == "" {
		return "", errors.New("pulsed admin token printed no token")
	}
	return tok, nil
}

func waitReady(u string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("pulsed not ready at %s after %s (last error: %w)", u, timeout, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// testAccPreCheck skips acceptance tests unless the harness is running.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if acc == nil {
		t.Skip("acceptance tests need TF_ACC=1 (make testacc)")
	}
}

// adminClient returns an API client with the administrator token (for out-of-band changes and destroy checks).
func adminClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(client.Config{Endpoint: acc.AdminURL, Token: acc.AdminToken})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
