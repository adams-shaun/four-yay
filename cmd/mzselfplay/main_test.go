package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/adams-shaun/gorge/internal/mzbridge"
	"github.com/adams-shaun/gorge/internal/mzplay"
	"github.com/adams-shaun/gorge/internal/testutil"
)

const cmdVocab = "dim\t64\nA\t0\tPass\nA\t1\tPlay Forest\nA\t2\tPlay Plains\nT\t0\tStop Choosing\nT\t1\tPlayerA\nT\t2\tPlayerB\n"

// fixture lays out a run directory: two .dck decks written from repo decks,
// a pool file, the vocabulary, and a game.yml in the shape draft-zero's loop
// writes. It skips when the corpus is not fetched.
type fixture struct {
	dir, cards, yml string
	outA, outB      string
}

func newFixture(t *testing.T, games, budget int, offlineA, offlineB bool, portA, portB int, edit func(string) string) fixture {
	t.Helper()
	reg := testutil.CorpusRegistry(t) // skips without .cards
	cardsDir, err := filepath.Abs("../../.cards")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var pool []string
	for _, name := range []string{"mono-green-stompy", "mono-white-equipment"} {
		deck, err := testutil.LoadRepoDeck(reg, name)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "NAME:%s\n", name)
		for _, c := range deck {
			fmt.Fprintf(&b, "1 [TST:0] %s\n", c.Faces[0].Name)
		}
		path := filepath.Join(dir, name+".dck")
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		pool = append(pool, path)
	}
	poolFile := filepath.Join(dir, "pool.txt")
	if err := os.WriteFile(poolFile, []byte(strings.Join(pool, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vocab.tsv"), []byte(cmdVocab), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(mzbridge.MZActionVocabEnv, filepath.Join(dir, "vocab.tsv"))
	t.Setenv("MZ_SEED", "")
	f := fixture{dir: dir, cards: cardsDir, yml: filepath.Join(dir, "game.yml"),
		outA: filepath.Join(dir, "out", "session0_A_self.hdf5"), outB: filepath.Join(dir, "out", "session0_B_self.hdf5")}
	player := func(out string, offline bool) string {
		return fmt.Sprintf(`  deckPath: ''
  type: mcts
  priors:
    priority: false
    target: false
    binary: false
    opponent: false
    prior_temperature: 1.5
  noise:
    enabled: false
  mcts:
    search_budget: %d
    timeout_ms: 60000
    td_discount: 0.95
    offline_mode: %v
  gameplay:
    mulligans_enabled: false
    manual_tapping: false
  hiddenInfo:
    see_opponent_hand: false
  deck_pool: %s
  deck_pool_mode: sequential
  output_file: %s
`, budget, offline, poolFile, out)
	}
	yml := "goes_first: random\nplayer_a:\n" + player(f.outA, offlineA) + "player_b:\n" + player(f.outB, offlineB) +
		fmt.Sprintf("training:\n  games: %d\n  threads: 2\n  max_turns: 6\n  max_minutes: 50\nserver:\n  host: localhost\n  port: %d\n  opponent_port: %d\n", games, portA, portB)
	if edit != nil {
		yml = edit(yml)
	}
	if err := os.WriteFile(f.yml, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) run(t *testing.T, extra ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	// The JVM arguments the loop passes come first; the config is last.
	args := append([]string{"-cards", f.cards, "-keep-npy"}, extra...)
	code := run(append(args, f.yml), &out, &errb)
	return code, out.String(), errb.String()
}

func summaries(t *testing.T, out string) []mzplay.Summary {
	t.Helper()
	var got []mzplay.Summary
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, mzplay.SummaryTag)
		if i < 0 {
			continue
		}
		// draft-zero's stats.parse_summaries: the text after the tag, up to
		// the log4j suffix.
		body := line[i+len(mzplay.SummaryTag):]
		if j := strings.LastIndex(body, " =>["); j >= 0 {
			body = body[:j]
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(body), &raw); err != nil {
			t.Fatalf("summary %q: %v", body, err)
		}
		for _, k := range []string{"game", "seed", "deck_a", "deck_b", "first", "winner", "turns", "drawn_a", "drawn_b"} {
			if _, ok := raw[k]; !ok {
				t.Fatalf("summary %q has no %q", body, k)
			}
		}
		var s mzplay.Summary
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	return got
}

func stats(t *testing.T, out string) runStats {
	t.Helper()
	const tag = "MZSELFPLAY_STATS "
	i := strings.Index(out, tag)
	if i < 0 {
		t.Fatalf("no stats line in:\n%s", out)
	}
	body := out[i+len(tag):]
	body = body[:strings.Index(body, " =>[")]
	var st runStats
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func readShard(t *testing.T, out string) map[string][]byte {
	t.Helper()
	got := map[string][]byte{}
	for _, suffix := range []string{mzbridge.IndicesSuffix, mzbridge.OffsetsSuffix, mzbridge.RowSuffix, mzbridge.GameOffsetsSuffix} {
		b, err := os.ReadFile(out + suffix)
		if err != nil {
			t.Fatal(err)
		}
		got[suffix] = b
	}
	return got
}

// TestRunOfflineGames: the command plays the games of a loop-shaped
// game.yml with the offline evaluator, prints one GAME_SUMMARY per game with
// the keys draft-zero parses, writes both shards, and does it all again
// identically from the same game.yml (the run seed is the file's bytes).
func TestRunOfflineGames(t *testing.T) {
	f := newFixture(t, 2, 8, true, true, 50052, 50052, nil)
	code, out, errOut := f.run(t)
	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	sums := summaries(t, out)
	if len(sums) != 2 {
		t.Fatalf("%d GAME_SUMMARY lines, want 2:\n%s", len(sums), out)
	}
	for _, s := range sums {
		// sequential pools: both players read the same list, so game i is a mirror.
		if s.DeckA != s.DeckB || (s.DeckA != "mono-green-stompy" && s.DeckA != "mono-white-equipment") {
			t.Errorf("decks %q vs %q", s.DeckA, s.DeckB)
		}
		if s.Turns != 6 || s.Winner != "draw" {
			t.Errorf("a 6-turn cap ended at turn %d with winner %q", s.Turns, s.Winner)
		}
		if len(s.DrawnA) < 7 || len(s.DrawnB) < 7 {
			t.Errorf("drawn %d / %d cards", len(s.DrawnA), len(s.DrawnB))
		}
	}
	st := stats(t, out)
	if st.Games != 2 || st.Failed != 0 || st.RowsA == 0 || st.RowsB == 0 || st.LeafEvals == 0 || st.NetEvals != 0 {
		t.Fatalf("stats %+v", st)
	}
	a1, b1 := readShard(t, f.outA), readShard(t, f.outB)
	code, out2, _ := f.run(t)
	if code != 0 {
		t.Fatalf("second run: exit %d", code)
	}
	a2, b2 := readShard(t, f.outA), readShard(t, f.outB)
	for k := range a1 {
		if !bytes.Equal(a1[k], a2[k]) || !bytes.Equal(b1[k], b2[k]) {
			t.Fatalf("the same game.yml wrote a different %s the second time", k)
		}
	}
	if s2 := summaries(t, out2); len(s2) != 2 || s2[0].Seed != sums[0].Seed && s2[0].Seed != sums[1].Seed {
		t.Fatalf("second run's seeds differ: %+v vs %+v", s2, sums)
	}
	// A different run seed is a different run.
	if code, _, _ := f.run(t, "-seed", "12345"); code != 0 {
		t.Fatalf("seeded run: exit %d", code)
	}
	if a3 := readShard(t, f.outA); bytes.Equal(a1[mzbridge.RowSuffix], a3[mzbridge.RowSuffix]) {
		t.Fatal("another run seed wrote the same rows")
	}
}

// TestRunRefuses: a configuration the engine does not implement, or cannot
// run, ends with exit status 2 and says why -- before any game is played.
func TestRunRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(string) string
		env  map[string]string
		want string
	}{
		"prior on":       {edit: func(s string) string { return strings.Replace(s, "priority: false", "priority: true", 1) }, want: "priors is switched on"},
		"no vocabulary":  {env: map[string]string{mzbridge.MZActionVocabEnv: ""}, want: "no action vocabulary"},
		"no output file": {edit: func(s string) string { return strings.Replace(s, "output_file: ", "output_file: ''\n  x: ", 1) }, want: "output_file"},
		"missing pool":   {edit: func(s string) string { return strings.Replace(s, "pool.txt", "nope.txt", 1) }, want: "deck pool"},
		"mulligans": {edit: func(s string) string {
			return strings.Replace(s, "mulligans_enabled: false", "mulligans_enabled: true", 1)
		}, want: "mulligans"},
	} {
		f := newFixture(t, 1, 4, true, true, 50052, 50052, tc.edit)
		for k, v := range tc.env {
			t.Setenv(k, v)
		}
		code, out, errOut := f.run(t)
		if code != 2 || !strings.Contains(errOut, tc.want) {
			t.Errorf("%s: exit %d, stderr %q, want exit 2 naming %q", name, code, errOut, tc.want)
		}
		if strings.Contains(out, mzplay.SummaryTag) {
			t.Errorf("%s: a game was played", name)
		}
	}
	// A network player whose server does not answer.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	f := newFixture(t, 1, 4, false, true, port, port, nil)
	if code, _, errOut := f.run(t); code != 2 || !strings.Contains(errOut, "does not answer") {
		t.Errorf("dead server: exit %d, stderr %q", code, errOut)
	}
}

// stub is an inference server: value 0.25 for every bag, and a count of the
// bags that arrived without the sentinel id.
type stub struct {
	*httptest.Server
	bags, noSentinel atomic.Int64
	failAfter        atomic.Int64 // fail every request once this many bags were served; 0 = never
}

func newStub(t *testing.T) *stub {
	s := &stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	mux.HandleFunc("POST /evaluate", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bags, err := mzbridge.DecodeEvaluateRequest(body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if n := s.failAfter.Load(); n > 0 && s.bags.Load() >= n {
			http.Error(w, "worker died", http.StatusInternalServerError)
			return
		}
		evals := make([]mzbridge.Evaluation, len(bags))
		for i, bag := range bags {
			s.bags.Add(1)
			if len(bag) == 0 || bag[0] != mzplay.SentinelID {
				s.noSentinel.Add(1)
			}
			evals[i] = mzbridge.Evaluation{Value: 0.25, PolicyBinary: []float32{0, 0}}
		}
		w.Header().Set("Content-Type", "application/x-msgpack")
		w.Write(mzbridge.AppendEvaluateResponse(nil, evals))
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *stub) port() int { return s.Listener.Addr().(*net.TCPAddr).Port }

// TestRunNetworkLeaf: a player with offline_mode false evaluates its leaves
// through its inference server (player A at server.port, player B at
// server.opponent_port), every bag carries the sentinel id, and the offline
// player asks no server.
func TestRunNetworkLeaf(t *testing.T) {
	a, b := newStub(t), newStub(t)
	f := newFixture(t, 2, 8, false, true, a.port(), b.port(), nil)
	code, out, errOut := f.run(t)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	st := stats(t, out)
	if st.NetEvals == 0 || st.NetErrors != 0 || st.NetFallbacks != 0 || a.bags.Load() != st.NetEvals {
		t.Fatalf("stats %+v, server A saw %d bags", st, a.bags.Load())
	}
	if b.bags.Load() != 0 {
		t.Fatalf("the offline player's port was asked %d times", b.bags.Load())
	}
	if a.noSentinel.Load() != 0 {
		t.Fatalf("%d bags arrived without the sentinel id", a.noSentinel.Load())
	}
	if st.LeafEvals <= st.NetEvals {
		t.Fatalf("leaf evaluations %d, of which network %d: the offline seat evaluated nothing", st.LeafEvals, st.NetEvals)
	}
}

// TestRunServerFailureFallsBack: when the inference server stops answering
// mid-run the games finish on the offline evaluator, the shards are written,
// the failure is reported, the server is not asked again, and the exit
// status is 3.
func TestRunServerFailureFallsBack(t *testing.T) {
	a := newStub(t)
	a.failAfter.Store(20)
	f := newFixture(t, 2, 8, false, false, a.port(), a.port(), nil)
	code, out, errOut := f.run(t)
	if code != 3 {
		t.Fatalf("exit %d, want 3\n%s\n%s", code, out, errOut)
	}
	st := stats(t, out)
	if st.Games != 2 || st.NetFallbacks == 0 || st.NetErrors == 0 || st.NetErrors > deadAfter+2 {
		t.Fatalf("stats %+v: want both games played, fallbacks counted, and at most %d failed requests before the server is left alone", st, deadAfter+2)
	}
	if !strings.Contains(out, "REMOTE EVAL FAILURE") || !strings.Contains(errOut, "fell back to the offline evaluator") {
		t.Fatalf("the failure is not reported:\n%s\n%s", out, errOut)
	}
	if st.RowsA == 0 || st.RowsB == 0 {
		t.Fatalf("no shard rows: %+v", st)
	}
	readShard(t, f.outA)
	readShard(t, f.outB)
}

// TestRunOpponentNodes: -opponent-nodes (and MZ_OPPONENT_NODES=1) reach both
// seats' searches, which the run's stats line shows; without it no tree
// selects at an opponent node.
func TestRunOpponentNodes(t *testing.T) {
	f := newFixture(t, 1, 8, true, true, 50052, 50052, nil)
	t.Setenv("MZ_OPPONENT_NODES", "")
	code, out, errOut := f.run(t)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if st := stats(t, out); st.OppSelections != 0 {
		t.Fatalf("switch off: %d opponent selections", st.OppSelections)
	}
	for _, how := range []string{"flag", "env"} {
		g := newFixture(t, 1, 8, true, true, 50052, 50052, nil)
		var extra []string
		if how == "flag" {
			extra = []string{"-opponent-nodes"}
		} else {
			t.Setenv("MZ_OPPONENT_NODES", "1")
		}
		code, out, errOut := g.run(t, extra...)
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s\n%s", how, code, out, errOut)
		}
		if st := stats(t, out); st.OppSelections == 0 || st.Simulations == 0 {
			t.Fatalf("%s: stats %+v", how, st)
		}
		t.Setenv("MZ_OPPONENT_NODES", "")
	}
}

// TestRunReuseTree: -reuse-tree (and MZ_REUSE_TREE=1) reach both seats'
// searches, which the run's stats line shows; without it no search counts
// a reuse hit or miss.
func TestRunReuseTree(t *testing.T) {
	f := newFixture(t, 1, 8, true, true, 50052, 50052, nil)
	t.Setenv("MZ_REUSE_TREE", "")
	code, out, errOut := f.run(t)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if st := stats(t, out); st.ReuseHits != 0 || st.ReuseMisses != 0 {
		t.Fatalf("switch off: %d hits, %d misses", st.ReuseHits, st.ReuseMisses)
	}
	for _, how := range []string{"flag", "env"} {
		g := newFixture(t, 1, 8, true, true, 50052, 50052, nil)
		var extra []string
		if how == "flag" {
			extra = []string{"-reuse-tree"}
		} else {
			t.Setenv("MZ_REUSE_TREE", "1")
		}
		code, out, errOut := g.run(t, extra...)
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s\n%s", how, code, out, errOut)
		}
		if st := stats(t, out); st.ReuseHits+st.ReuseMisses == 0 || st.Simulations == 0 {
			t.Fatalf("%s: stats %+v", how, st)
		}
		t.Setenv("MZ_REUSE_TREE", "")
	}
}

// TestRunPerSeatCombatSteps: game.yml's mcts.combat_steps under one player
// turns on that seat's per-creature combat searches alone -- no opponent
// nodes, no tree reuse.
func TestRunPerSeatCombatSteps(t *testing.T) {
	for _, k := range []string{"MZ_UPSTREAM_SEARCH", "MZ_OPPONENT_NODES", "MZ_REUSE_TREE"} {
		t.Setenv(k, "")
	}
	edit := func(yml string) string {
		yml = strings.Replace(yml, "max_turns: 6", "max_turns: 14", 1)
		return strings.Replace(yml, "    offline_mode: true\n", "    offline_mode: true\n    combat_steps: true\n", 1)
	}
	f := newFixture(t, 2, 8, true, true, 50052, 50052, edit)
	if b, err := os.ReadFile(f.yml); err != nil || strings.Count(string(b), "combat_steps: true") != 1 {
		t.Fatalf("fixture: combat_steps not set for exactly one player (%v)", err)
	}
	code, out, errOut := f.run(t)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if st := stats(t, out); st.CombatSteps == 0 || st.OppSelections != 0 || st.ReuseHits+st.ReuseMisses != 0 {
		t.Fatalf("stats %+v", st)
	}
}

// TestRunUpstreamSearch: -upstream-search (and MZ_UPSTREAM_SEARCH=1) turn on
// every upstream switch for both seats -- opponent nodes, tree reuse and the
// per-creature combat searches all show on the stats line; without it none
// does.
func TestRunUpstreamSearch(t *testing.T) {
	longer := func(yml string) string { return strings.Replace(yml, "max_turns: 6", "max_turns: 14", 1) }
	for _, k := range []string{"MZ_UPSTREAM_SEARCH", "MZ_OPPONENT_NODES", "MZ_REUSE_TREE"} {
		t.Setenv(k, "")
	}
	f := newFixture(t, 2, 8, true, true, 50052, 50052, longer)
	code, out, errOut := f.run(t)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if st := stats(t, out); st.CombatSteps != 0 || st.OppSelections != 0 || st.ReuseHits+st.ReuseMisses != 0 {
		t.Fatalf("switch off: %+v", st)
	}
	for _, how := range []string{"flag", "env"} {
		g := newFixture(t, 2, 8, true, true, 50052, 50052, longer)
		var extra []string
		if how == "flag" {
			extra = []string{"-upstream-search"}
		} else {
			t.Setenv("MZ_UPSTREAM_SEARCH", "1")
		}
		code, out, errOut := g.run(t, extra...)
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s\n%s", how, code, out, errOut)
		}
		if st := stats(t, out); st.CombatSteps == 0 || st.OppSelections == 0 || st.ReuseHits+st.ReuseMisses == 0 || st.Simulations == 0 {
			t.Fatalf("%s: stats %+v", how, st)
		}
		t.Setenv("MZ_UPSTREAM_SEARCH", "")
	}
}
