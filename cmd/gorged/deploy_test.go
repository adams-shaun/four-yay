package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// deployStub is a stand-in gorged for scripts/deploy-demo.sh: each call
// appends one record line (its kind, then every argument, \x1f-separated) to
// the record file. The -prewarm-art-only call then fails or hangs as the case
// asks; a server call just exits, because the test's own listeners answer
// the script's readiness and format probes on the demo ports.
const deployStub = `#!/usr/bin/env bash
kind=serve
for a in "$@"; do [ "$a" = -prewarm-art-only ] && kind=fill; done
line=$kind
for a in "$@"; do line+=$'\x1f'"$a"; done
printf '%s\n' "$line" >>'@REC@'
if [ "$kind" = fill ]; then
	case '@MODE@' in
	fail) echo 'gorged: ART FILL INCOMPLETE (stub)' >&2; exit 1 ;;
	hang) exec sleep 60 ;;
	esac
fi
exit 0
`

// TestDeployStartsTheServersWhenTheArtFillFails is the opus1 MAJOR's
// deploy-side regression. The deploy used to abort — old servers left
// running, new code never served — whenever the pre-start art fill exited
// non-zero, and nothing bounded how long that fill could run, while the
// post-merge hook waits only 600s for the deploy lock. Art is cosmetic: the
// real script, driven with a stub binary, must start BOTH servers after a
// fill that fails and after one that hangs past the hard ceiling, say so
// loudly, pass the fill a budget the real flag set accepts, and keep that
// budget and the ceiling at or under 240s.
//
// Safety: the listeners are this test's own, bound on free ports in
// 8090-8099 (a successful bind proves no gorged is there for the script's
// port sweep to stop), and every path the script writes — persistence dirs,
// logs, art dir — is overridden into temp dirs, so the live demo on
// 8080/8081 and its /tmp logs are never touched. A probe gorged (the test
// binary re-exec'd from a copy literally named gorged, the reviewer's own
// shape) listens on [::1] of the first port through every subtest: the
// sweep must leave a gorged bound to ANOTHER address of a demo port alone.
func TestDeployStartsTheServersWhenTheArtFillFails(t *testing.T) {
	for _, tool := range []string{"bash", "setsid", "nohup", "ss", "curl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("deploy-demo.sh needs %s: %v", tool, err)
		}
	}
	script, err := filepath.Abs("../../scripts/deploy-demo.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, mode string
		env        []string
		wantExit   string
	}{
		{name: "fill exits non-zero", mode: "fail", wantExit: "exit 1;"},
		// Any non-zero status: GNU timeout says 124, uutils says 125. What
		// must hold under both is that the deploy moved on inside the
		// ceiling instead of waiting out the stub's 60s hang.
		{name: "fill hangs past the hard ceiling", mode: "hang", env: []string{"ART_FILL_CEILING=0.3s"}, wantExit: "exit "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ports := demoTestPorts(t)
			probe := startGorgedProbe(t, ports[0])
			tmp := t.TempDir()
			rec := filepath.Join(tmp, "calls")
			stub := filepath.Join(tmp, "gorged")
			body := strings.NewReplacer("@REC@", rec, "@MODE@", tc.mode).Replace(deployStub)
			if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			artDir := filepath.Join(tmp, "art")
			outPath := filepath.Join(tmp, "deploy.out")
			outFile, err := os.Create(outPath)
			if err != nil {
				t.Fatal(err)
			}
			defer outFile.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", script)
			cmd.Dir = tmp
			cmd.Stdout, cmd.Stderr = outFile, outFile
			cmd.Env = append(os.Environ(),
				// RETIRED_PORTS= keeps the sweep off the live demo's retired
				// :8081 -- its default would stop a real gorged there.
				"BIN="+stub, "DECKS="+filepath.Join(tmp, "decks"), "ART_DIR="+artDir, "SWEEP=ports", "RETIRED_PORTS=",
				fmt.Sprintf("PUB_PORT=%d", ports[0]), fmt.Sprintf("OMNI_PORT=%d", ports[1]),
				"PUB_DIR="+filepath.Join(tmp, "pub"), "OMNI_DIR="+filepath.Join(tmp, "omni"),
				"PUB_LOG="+filepath.Join(tmp, "pub.log"), "OMNI_LOG="+filepath.Join(tmp, "omni.log"))
			cmd.Env = append(cmd.Env, tc.env...)
			start := time.Now()
			runErr := cmd.Run()
			elapsed := time.Since(start)
			out, _ := os.ReadFile(outPath)
			t.Logf("deploy-demo.sh output:\n%s", out)
			if runErr != nil {
				t.Fatalf("deploy-demo.sh: %v — a failed art fill must not fail the deploy", runErr)
			}
			if elapsed > 20*time.Second {
				t.Errorf("deploy took %v: the fill was not cut off at its ceiling", elapsed)
			}
			for _, want := range []string{"art fill INCOMPLETE (" + tc.wantExit, "starting the servers anyway", "ready — spectator"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("deploy output lacks %q", want)
				}
			}
			if tc.env == nil {
				m := regexp.MustCompile(`\(budget (\S+), ceiling (\S+)\)`).FindStringSubmatch(string(out))
				if m == nil {
					t.Fatal("deploy output does not state the fill's budget and ceiling")
				}
				for _, v := range m[1:] {
					if d, err := time.ParseDuration(v); err != nil || d <= 0 || d > 240*time.Second {
						t.Errorf("default fill bound %q: want a duration in (0, 240s] — under the hook's 600s lock wait", v)
					}
				}
			}
			assertGorgedProbeListening(t, probe)

			// The stub servers are started in the background: wait for both.
			var calls [][]string
			deadline := time.Now().Add(10 * time.Second)
			for {
				calls = readDeployCalls(t, rec)
				if len(calls) >= 3 || time.Now().After(deadline) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if len(calls) != 3 || calls[0][0] != "fill" || calls[1][0] != "serve" || calls[2][0] != "serve" {
				t.Fatalf("stub calls = %q, want the fill, then both servers", calls)
			}
			served := strings.Join(append(calls[1][1:], calls[2][1:]...), " ")
			for _, p := range ports {
				if !strings.Contains(served, fmt.Sprintf("127.0.0.1:%d", p)) {
					t.Errorf("no server was started on port %d after the failed fill: %q", p, served)
				}
			}

			// The fill's arguments must be ones the REAL binary accepts:
			// a misspelt flag would exit 2 on every deploy, now silently.
			fs, c := serveFlags()
			fs.Init(fs.Name(), flag.ContinueOnError) // an unknown flag must fail this test, not os.Exit the binary
			fs.SetOutput(io.Discard)
			if err := fs.Parse(calls[0][1:]); err != nil {
				t.Fatalf("real gorged rejects the deploy's fill arguments %q: %v", calls[0][1:], err)
			}
			if !c.prewarmArtOnly || c.artDir != artDir {
				t.Errorf("fill flags = only %v art-dir %q, want the one-shot into %q", c.prewarmArtOnly, c.artDir, artDir)
			}
			if c.prewarmArtBudget <= 0 || c.prewarmArtBudget > 240*time.Second {
				t.Errorf("-prewarm-art-budget = %v, want in (0, 240s]", c.prewarmArtBudget)
			}
			if c.prewarmArtMaxConsecutiveFailures <= 0 {
				t.Errorf("-prewarm-art-max-consecutive-failures = %d, want a positive streak limit", c.prewarmArtMaxConsecutiveFailures)
			}

			// The demo server's arguments too: the real flag set must accept
			// them, and the demo's defaults must be the ones it serves --
			// bot tables omniscient, a person's vs-bot table public.
			fs, c = serveFlags()
			fs.Init(fs.Name(), flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			if err := fs.Parse(calls[1][1:]); err != nil {
				t.Fatalf("real gorged rejects the deploy's serve arguments %q: %v", calls[1][1:], err)
			}
			if c.spectator != "omniscient" || c.vsbotSpectator != "public" || c.botPolicy != "lethal-pressure" || c.pace != 50*time.Millisecond {
				t.Errorf("demo server: spectator %q vsbot-spectator %q bot-policy %q pace %v, want omniscient/public/lethal-pressure/50ms",
					c.spectator, c.vsbotSpectator, c.botPolicy, c.pace)
			}
		})
	}
}

// demoTestPorts reserves every IPv4 candidate while it scans the task range,
// then chooses two and answers the deploy script's /api/tables probes on them
// until the test ends. The first port must also accept a temporary [::1] bind,
// because startGorgedProbe will use that address. Thus a review server already
// on [::1]:8090 is treated as a busy candidate rather than becoming a 10-second
// helper failure. It SKIPS — never fails — when no dual-address first port or
// fewer than two IPv4 ports are free: other sessions use this range by design.
func demoTestPorts(t *testing.T) []int {
	t.Helper()
	type candidate struct {
		port int
		ln   net.Listener
		v6   bool
	}
	var candidates []candidate
	for p := 8090; p <= 8099; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = ln.Close() })
		v6, err := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", p))
		if err == nil {
			_ = v6.Close()
		}
		candidates = append(candidates, candidate{port: p, ln: ln, v6: err == nil})
	}

	first := -1
	for i := range candidates {
		if candidates[i].v6 {
			first = i
			break
		}
	}
	if len(candidates) < 2 || first < 0 {
		var ports []int
		for _, c := range candidates {
			ports = append(ports, c.port)
		}
		t.Skipf("need two IPv4 ports and one dual-address port in 8090-8099 (IPv4 candidates %v); another session is using the range", ports)
	}
	second := 0
	if second == first {
		second++
	}
	selected := []candidate{candidates[first], candidates[second]}
	selectedPorts := map[int]bool{selected[0].port: true, selected[1].port: true}
	for _, c := range candidates {
		if !selectedPorts[c.port] {
			_ = c.ln.Close()
		}
	}
	for _, c := range selected {
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"t1","format":"commander"}]`)
		})}
		go func() { _ = srv.Serve(c.ln) }()
		t.Cleanup(func() { _ = srv.Close() })
	}
	return []int{selected[0].port, selected[1].port}
}

// startGorgedProbe reproduces the reviewer's probe exactly: a process whose
// /proc/<pid>/comm reads gorged (this test binary, copied under that name
// and re-exec'd into its helper) with a LISTENING socket on [::1]:port —
// another address of a port the deploy is about to bind on 127.0.0.1. The
// script's port sweep must never touch it; the caller asserts its socket and
// PID still appear in ss after the deploy. That proves the process is alive
// AND still owns the [::1] listener — unlike signal 0, it cannot mistake an
// unreaped zombie for a live probe. The probe is killed and reaped at cleanup.
type gorgedProbe struct {
	cmd  *exec.Cmd
	port int
	done <-chan error
}

func startGorgedProbe(t *testing.T, port int) *gorgedProbe {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gorged") // the copy's basename is what /proc/<pid>/comm shows
	if err := copyFile(bin, exe); err != nil {
		t.Fatalf("copying the test binary to %s: %v", bin, err)
	}
	ready := filepath.Join(dir, "ready")
	cmd := exec.Command(bin, "-test.run", "^TestGorgedProbeHelper$")
	cmd.Env = append(os.Environ(),
		"GORGE_PROBE_PORT="+fmt.Sprint(port),
		"GORGE_PROBE_READY="+ready)
	log, err := os.Create(filepath.Join(dir, "probe.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		select {
		case <-done:
			return
		default:
		}
		_ = cmd.Process.Kill()
		<-done
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if fileExists(ready) {
			break
		}
		select {
		case err := <-done:
			b, _ := os.ReadFile(log.Name())
			if strings.Contains(string(b), "address already in use") {
				t.Skipf("probe port [::1]:%d was claimed after selection: %v: %s", port, err, b)
			}
			t.Fatalf("probe gorged on [::1]:%d exited before ready: %v: %s", port, err, b)
		default:
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(log.Name())
			t.Fatalf("probe gorged on [::1]:%d never became ready: %s", port, b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Prove the probe really is the shape that used to be swept: a listening
	// socket rendered exactly as [::1]:port in ss -lptn — matched by the old
	// port-only filter, which is how a re-reviewer's probe got killed.
	deadline = time.Now().Add(10 * time.Second)
	for {
		out, err := exec.Command("ss", "-lptn").Output()
		if err != nil {
			t.Fatalf("ss -lptn: %v", err)
		}
		if strings.Contains(string(out), fmt.Sprintf("[::1]:%d", port)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the probe gorged's socket never showed in ss -lptn as [::1]:%d; the probe does not reproduce the reviewer's shape", port)
		}
		time.Sleep(10 * time.Millisecond)
	}
	probe := &gorgedProbe{cmd: cmd, port: port, done: done}
	assertGorgedProbeListening(t, probe)
	return probe
}

// assertGorgedProbeListening proves the exact PID still owns its [::1]
// listener. A killed probe becomes a zombie until Cleanup reaps it, for which
// signal 0 would still succeed; a zombie cannot retain a listening socket.
func assertGorgedProbeListening(t *testing.T, probe *gorgedProbe) {
	t.Helper()
	out, err := exec.Command("ss", "-lptn").Output()
	if err != nil {
		t.Fatalf("ss -lptn: %v", err)
	}
	address := fmt.Sprintf("[::1]:%d", probe.port)
	pid := fmt.Sprintf("pid=%d,", probe.cmd.Process.Pid)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, address) && strings.Contains(line, pid) {
			return
		}
	}
	t.Errorf("the deploy's port sweep killed probe pid %d or closed its [::1]:%d listener; ss -lptn:\n%s", probe.cmd.Process.Pid, probe.port, out)
}

// copyFile copies src to a new file at dst.
func copyFile(dst, src string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	d, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(d, s); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

// TestGorgedProbeHelper is not a test a human runs: it is the body of the
// probe gorged process startGorgedProbe re-execs this binary into (from a
// copy named gorged, so /proc/<pid>/comm matches the sweep's comm check).
// It binds [::1]:$GORGE_PROBE_PORT, signals readiness by creating
// $GORGE_PROBE_READY, and then blocks until killed — the parent asserts the
// deploy's sweep left it running. In an ordinary run of the suite the env is
// unset and this skips in milliseconds.
func TestGorgedProbeHelper(t *testing.T) {
	port := os.Getenv("GORGE_PROBE_PORT")
	if port == "" {
		t.Skip("probe helper: only run through startGorgedProbe")
	}
	ln, err := net.Listen("tcp6", "[::1]:"+port)
	if err != nil {
		t.Fatalf("probe bind [::1]:%s: %v", port, err)
	}
	defer ln.Close()
	if err := os.WriteFile(os.Getenv("GORGE_PROBE_READY"), []byte("listening"), 0o644); err != nil {
		t.Fatalf("probe ready file: %v", err)
	}
	<-make(chan struct{}) // block until the parent's cleanup kills us
}

func readDeployCalls(t *testing.T, path string) [][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if line != "" {
			calls = append(calls, strings.Split(line, "\x1f"))
		}
	}
	return calls
}
