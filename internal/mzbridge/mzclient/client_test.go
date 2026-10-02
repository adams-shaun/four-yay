package mzclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/internal/mzbridge"
)

// stubServer answers /evaluate like server.py: value = first id / 1000 and
// policy_player = [number of ids] per bag, a bare map for a single bag. It
// records the bag count of every request.
type stubServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []int
	fail     bool
}

func newStubServer(t *testing.T) *stubServer {
	s := &stubServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	mux.HandleFunc("POST /evaluate", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bags, err := mzbridge.DecodeEvaluateRequest(body)
		s.mu.Lock()
		failing := s.fail
		s.requests = append(s.requests, len(bags))
		s.mu.Unlock()
		if err != nil || failing || r.Header.Get("Content-Type") != "application/x-msgpack" {
			http.Error(w, "bad request", http.StatusInternalServerError)
			return
		}
		evals := make([]mzbridge.Evaluation, len(bags))
		for i, bag := range bags {
			evals[i] = mzbridge.Evaluation{PolicyPlayer: []float32{float32(len(bag))}, PolicyBinary: []float32{0, 1}}
			if len(bag) > 0 {
				evals[i].Value = float32(bag[0]) / 1000
			}
		}
		w.Header().Set("Content-Type", "application/x-msgpack")
		w.Write(mzbridge.AppendEvaluateResponse(nil, evals))
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *stubServer) seen() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// evalMany runs n concurrent Evaluate calls, caller i sending the bag
// [i, i, ...] of length i+1, and checks each got its own answer back.
func evalMany(t *testing.T, c *Client, n int) {
	t.Helper()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bag := make([]int32, i+1)
			for j := range bag {
				bag[j] = int32(i)
			}
			e, err := c.Evaluate(context.Background(), bag)
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
				return
			}
			if e.Value != float32(i)/1000 || len(e.PolicyPlayer) != 1 || e.PolicyPlayer[0] != float32(i+1) {
				t.Errorf("caller %d got another caller's answer: %+v", i, e)
			}
		}()
	}
	wg.Wait()
}

func TestClientBatchesOnSize(t *testing.T) {
	s := newStubServer(t)
	// the timer never fires: only a full batch is sent
	c := New(s.URL, Options{MaxBatch: 4, FlushInterval: time.Hour})
	defer c.Close()
	if err := c.Healthz(context.Background()); err != nil {
		t.Fatal(err)
	}
	evalMany(t, c, 12)
	if got := s.seen(); !slices.Equal(got, []int{4, 4, 4}) {
		t.Fatalf("requests carried %v bags, want three of 4", got)
	}
}

func TestClientFlushesOnTimer(t *testing.T) {
	s := newStubServer(t)
	c := New(s.URL, Options{MaxBatch: 64, FlushInterval: 150 * time.Millisecond})
	defer c.Close()
	evalMany(t, c, 3) // fewer than a batch: only the timer can send them
	got := s.seen()
	total := 0
	for _, n := range got {
		total += n
	}
	if total != 3 || len(got) > 3 {
		t.Fatalf("requests carried %v bags", got)
	}
	// a lone caller gets the single-bag (bare map) response form
	e, err := c.Evaluate(context.Background(), []int32{500})
	if err != nil || e.Value != 0.5 {
		t.Fatalf("lone caller: %+v %v", e, err)
	}
	if got := s.seen(); got[len(got)-1] != 1 {
		t.Fatalf("lone caller's request carried %d bags", got[len(got)-1])
	}
}

func TestClientDirectBatch(t *testing.T) {
	s := newStubServer(t)
	c := New(s.URL, Options{})
	defer c.Close()
	evals, err := c.EvaluateBatch(context.Background(), [][]int32{{7}, {8}, {9, 9}})
	if err != nil || len(evals) != 3 || evals[0].Value != 0.007 || evals[1].PolicyPlayer[0] != 1 || evals[2].PolicyPlayer[0] != 2 {
		t.Fatalf("%+v %v", evals, err)
	}
	// an empty bag is refused before anything is sent
	before := len(s.seen())
	if _, err := c.EvaluateBatch(context.Background(), [][]int32{{7}, {}}); !errors.Is(err, ErrEmptyBag) {
		t.Fatalf("empty bag in a batch: %v", err)
	}
	if _, err := c.Evaluate(context.Background(), nil); !errors.Is(err, ErrEmptyBag) {
		t.Fatalf("empty bag: %v", err)
	}
	if len(s.seen()) != before {
		t.Fatal("a request with an empty bag reached the server")
	}
	if evals, err := c.EvaluateBatch(context.Background(), nil); err != nil || evals != nil {
		t.Fatalf("empty batch: %v %v", evals, err)
	}
}

func TestClientErrors(t *testing.T) {
	s := newStubServer(t)
	c := New(s.URL, Options{MaxBatch: 2, FlushInterval: time.Hour})
	s.mu.Lock()
	s.fail = true
	s.mu.Unlock()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := c.Evaluate(context.Background(), []int32{1})
			errs <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err == nil {
			t.Error("a failed request returned no error to its caller")
		}
	}
	// a caller that gives up does not wait for a batch that never fills
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Evaluate(ctx, []int32{1}); err != context.DeadlineExceeded {
		t.Fatalf("cancelled caller: %v", err)
	}
	// Close fails a caller still waiting to be batched, and later callers
	waiting := make(chan error, 1)
	go func() {
		_, err := c.Evaluate(context.Background(), []int32{1})
		waiting <- err
	}()
	time.Sleep(20 * time.Millisecond)
	c.Close()
	// the abandoned bag and the waiting one may have filled a batch (the
	// server is failing) or the close reached it first: an error either way
	if err := <-waiting; err == nil {
		t.Fatal("caller waiting at Close got no error")
	}
	if _, err := c.Evaluate(context.Background(), []int32{1}); err != ErrClosed {
		t.Fatalf("after Close: %v", err)
	}
	c.Close() // twice is fine

	// NaN from the model is an error, not a value
	nan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(mzbridge.AppendEvaluateResponse(nil, []mzbridge.Evaluation{{Value: float32(math.NaN())}}))
	}))
	defer nan.Close()
	c2 := New(nan.URL, Options{})
	defer c2.Close()
	if _, err := c2.EvaluateBatch(context.Background(), [][]int32{{1}}); err == nil {
		t.Fatal("NaN value accepted")
	}
	if err := New("http://127.0.0.1:1", Options{}).Healthz(context.Background()); err == nil {
		t.Fatal("healthz against nothing succeeded")
	}
}

// TestClientAgainstMageZeroServer is the real round trip. Start server.py by
// hand (CPU, a port in 8090-8099), then run with
//
//	MZBRIDGE_SERVER_URL=http://127.0.0.1:<port>
//	MZBRIDGE_PYTHON=<venv>/bin/python  MZBRIDGE_MAGEZERO=<MageZero>/src/magezero
//	MZBRIDGE_RUN_ROOT=<dir holding models/ and data/>  MZBRIDGE_DECK=<deck>
//	MZ_ACTION_VOCAB=<the vocabulary the model was trained with>
//
// It sends real bags from the run's testing shard through the micro-batcher
// and compares every output with the same checkpoint evaluated directly in
// Python.
func TestClientAgainstMageZeroServer(t *testing.T) {
	url, py, mz := os.Getenv("MZBRIDGE_SERVER_URL"), os.Getenv("MZBRIDGE_PYTHON"), os.Getenv("MZBRIDGE_MAGEZERO")
	root, deck := os.Getenv("MZBRIDGE_RUN_ROOT"), os.Getenv("MZBRIDGE_DECK")
	if url == "" || py == "" || mz == "" || root == "" || deck == "" {
		t.Skip("MZBRIDGE_SERVER_URL / _PYTHON / _MAGEZERO / _RUN_ROOT / _DECK unset: the real-server round trip did not run")
	}
	const script = `
import json, sys
sys.path.insert(0, sys.argv[1])
import torch
from dataset import H5Indexed
from model import NetTransformer, load_model
from vocab import FeatureVocab
deck = sys.argv[2]
ckpt = load_model(f"models/{deck}/ver1/model.pt.gz")
vocab = FeatureVocab.from_state_dict(ckpt["feature_vocab"])
model = NetTransformer(len(vocab)).eval()
model.load_state_dict(ckpt["model_state_dict"])
raw = H5Indexed(f"data/{deck}/ver1/testing")
out = []
for k in range(12):
    ids = raw[k][0].tolist()
    if k == 5:
        ids = ids[:3] + [5, 6, 7]          # ids the model has never seen are dropped
    rows, off = vocab.map_bags(ids, [0])
    with torch.no_grad():
        pA, pB, tgt, b2, val = model(torch.tensor(rows, dtype=torch.long), torch.tensor(off, dtype=torch.long))
    out.append({"ids": ids, "player": pA[0].tolist(), "opponent": pB[0].tolist(), "target": tgt[0].tolist(),
                "binary": b2[0].tolist(), "value": val[0].item()})
print(json.dumps(out))
`
	cmd := exec.Command(py, "-c", script, mz, deck)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CUDA_VISIBLE_DEVICES=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("reference evaluation: %v\n%s", err, stderr.String())
	}
	var ref []struct {
		IDs      []int32   `json:"ids"`
		Player   []float32 `json:"player"`
		Opponent []float32 `json:"opponent"`
		Target   []float32 `json:"target"`
		Binary   []float32 `json:"binary"`
		Value    float32   `json:"value"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}

	c := New(url, Options{})
	defer c.Close()
	if err := c.Healthz(context.Background()); err != nil {
		t.Fatal(err)
	}
	near := func(what string, i int, got, want []float32) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("bag %d %s: %d values, reference %d", i, what, len(got), len(want))
			return
		}
		for j := range got {
			if d := got[j] - want[j]; d > 2e-3 || d < -2e-3 {
				t.Errorf("bag %d %s[%d] = %v, reference %v", i, what, j, got[j], want[j])
				return
			}
		}
	}
	check := func(i int, e mzbridge.Evaluation) {
		t.Helper()
		near("policy_player", i, e.PolicyPlayer, ref[i].Player)
		near("policy_opponent", i, e.PolicyOpponent, ref[i].Opponent)
		near("policy_target", i, e.PolicyTarget, ref[i].Target)
		near("policy_binary", i, e.PolicyBinary, ref[i].Binary)
		near("value", i, []float32{e.Value}, []float32{ref[i].Value})
		if len(e.PolicyPlayer) != 1024 || len(e.PolicyTarget) != 1024 || len(e.PolicyBinary) != 2 {
			t.Errorf("bag %d: head widths %d %d %d", i, len(e.PolicyPlayer), len(e.PolicyTarget), len(e.PolicyBinary))
		}
	}
	// concurrently, through the micro-batcher (array responses)
	var wg sync.WaitGroup
	for i := range ref {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := c.Evaluate(context.Background(), ref[i].IDs)
			if err != nil {
				t.Errorf("bag %d: %v", i, err)
				return
			}
			check(i, e)
		}()
	}
	wg.Wait()
	// one at a time (the bare-map response)
	e, err := c.Evaluate(context.Background(), ref[0].IDs)
	if err != nil {
		t.Fatal(err)
	}
	check(0, e)
	// and one explicit request of all bags
	bags := make([][]int32, len(ref))
	for i := range ref {
		bags[i] = ref[i].IDs
	}
	evals, err := c.EvaluateBatch(context.Background(), bags)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range evals {
		check(i, e)
	}
	// NOT probed here: a request whose bags hold no id the model knows.
	// server.py's worker thread dies on it (see EvaluateBatch) and the
	// server then answers nothing until it is restarted.
}
