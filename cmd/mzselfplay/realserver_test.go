package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/mzbridge/mzclient"
	"github.com/adams-shaun/gorge/internal/mzplay"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestSentinelGuardAgainstRealServer runs MageZero's own server.py on a
// model trained from this engine's shards and checks the two guards against
// the server hazard (mzplay.SentinelID): a bag whose every real id is
// unknown to the model is answered because it carries the sentinel, and the
// same bag WITHOUT the sentinel kills the server's worker -- after which the
// leaf falls back to the offline evaluator instead of hanging.
//
// Env-gated (it needs Python, torch and a trained model):
//
//	MZSELFPLAY_REAL_RUN_ROOT  a run root holding models/<deck>/ver1/model.pt.gz
//	MZSELFPLAY_REAL_PYTHON    the MageZero environment's python
//	MZSELFPLAY_REAL_MAGEZERO  the directory holding server.py
//	MZSELFPLAY_REAL_DECK      the model name (default FDN_exp2)
//	MZSELFPLAY_REAL_PORT      the port to serve on (default 50090)
//	MZ_ACTION_VOCAB           the vocabulary the model was trained with
func TestSentinelGuardAgainstRealServer(t *testing.T) {
	root, python, mz := os.Getenv("MZSELFPLAY_REAL_RUN_ROOT"), os.Getenv("MZSELFPLAY_REAL_PYTHON"), os.Getenv("MZSELFPLAY_REAL_MAGEZERO")
	if root == "" || python == "" || mz == "" {
		t.Skip("set MZSELFPLAY_REAL_RUN_ROOT, MZSELFPLAY_REAL_PYTHON and MZSELFPLAY_REAL_MAGEZERO to test against the real server.py")
	}
	deck, port := envOr("MZSELFPLAY_REAL_DECK", "FDN_exp2"), envOr("MZSELFPLAY_REAL_PORT", "50090")
	url := "http://127.0.0.1:" + port

	logPath := filepath.Join(t.TempDir(), "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd := exec.Command(python, "-u", filepath.Join(mz, "server.py"), "--deck", deck, "--version", "1", "--port", port)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CUDA_VISIBLE_DEVICES="+os.Getenv("MZSELFPLAY_REAL_GPU"), "OMP_NUM_THREADS=1")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			if b, err := os.ReadFile(logPath); err == nil {
				t.Logf("server log:\n%s", b)
			}
		}
	}()
	healthy := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil
	}
	deadline := time.Now().Add(5 * time.Minute)
	for healthy() != nil {
		if time.Now().After(deadline) {
			t.Fatal("server.py did not come up")
		}
		time.Sleep(500 * time.Millisecond)
	}

	timeout := 20 * time.Second
	ne := &netEval{url: url, timeout: timeout, client: mzclient.New(url, mzclient.Options{MaxBatch: 1, HTTPClient: &http.Client{Timeout: timeout}})}
	defer ne.client.Close()

	// A position of a real game, and the network leaf on it.
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, "mono-green-stompy")
	if err != nil {
		t.Fatal(err)
	}
	e := rules.New(rules.Config{Seed: 5, Names: []string{"a", "b"}, Decks: [][]*cards.Card{da, da}, Tokens: reg.Tokens})
	e.Advance()
	leaf := mzplay.NewNetLeaf(ne, false)
	v := leaf.Leaf(e, 0)
	if leaf.Fallbacks != 0 || leaf.Calls != 1 || math.IsNaN(v) || v < 0 || v > 1 {
		t.Fatalf("network leaf on a real position: value %v, %d calls, %d fallbacks, last error %v", v, leaf.Calls, leaf.Fallbacks, leaf.LastErr)
	}
	t.Logf("network leaf at the opening position: %.4f (offline evaluator: %.4f)", v, mzplay.OfflineLeaf(e, 0))

	// Guard 1: ids no model knows, plus the sentinel. Answered.
	unknown := []int32{2147480001, 2147480002, 2147480003}
	ev, err := ne.Evaluate(append([]int32{mzplay.SentinelID}, unknown...))
	if err != nil || math.IsNaN(float64(ev.Value)) || ev.Value < -1 || ev.Value > 1 {
		t.Fatalf("a bag of unknown ids WITH the sentinel: value %v, error %v", ev.Value, err)
	}
	// The worker is still alive: an ordinary state is answered after it.
	if leaf.Leaf(e, 0); leaf.Fallbacks != 0 {
		t.Fatalf("the server stopped answering after the sentinel bag: %v", leaf.LastErr)
	}

	// Guard 2: the hazard itself. The same ids WITHOUT the sentinel are an
	// empty bag for the model: the worker thread dies and the request hangs.
	short := &netEval{url: url, timeout: 4 * time.Second, client: mzclient.New(url, mzclient.Options{MaxBatch: 1, HTTPClient: &http.Client{Timeout: 4 * time.Second}})}
	defer short.client.Close()
	if _, err := short.Evaluate(unknown); err == nil {
		t.Fatal("a bag with no id the model knows was answered: the hazard this guard exists for is gone (re-read server.py)")
	}
	if err := healthy(); err != nil {
		t.Fatalf("healthz after the worker died: %v (it is expected to keep answering ok)", err)
	}
	// Every later request now hangs. The leaf falls back to the offline
	// evaluator, and after deadAfter failures in a row stops asking.
	dead := mzplay.NewNetLeaf(short, false)
	want := mzplay.OfflineLeaf(e, 0)
	t0 := time.Now()
	for i := 0; i < deadAfter+3; i++ {
		if got := dead.Leaf(e, 0); got != want {
			t.Fatalf("leaf %d after the server died: %v, want the offline evaluator's %v", i, got, want)
		}
	}
	if dead.Fallbacks != deadAfter+3 || !short.dead.Load() {
		t.Fatalf("%d fallbacks of %d leaves, server marked dead %v", dead.Fallbacks, deadAfter+3, short.dead.Load())
	}
	// One failure was the bare hazard request; the leaves needed deadAfter-1
	// more timeouts and then no request at all.
	if spent := time.Since(t0); spent > time.Duration(deadAfter)*short.timeout {
		t.Fatalf("the leaves waited %v on a dead server", spent)
	}
	if n := short.errs.Load(); n != deadAfter {
		t.Fatalf("%d failed requests, want %d and then none", n, deadAfter)
	}
}
