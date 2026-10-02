package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// saveAZVariants restores the variant table and every az variable a
// -spellbench run writes.
func saveAZVariants(t *testing.T) {
	t.Helper()
	saveAZ(t)
	vs, corpus, label, rec := azVariants, azCorpusPath, azLabel, azRecordArg
	azVariants = nil
	t.Cleanup(func() { azVariants, azCorpusPath, azLabel, azRecordArg = vs, corpus, label, rec })
}

// writeValueCheckpoint writes a small mz checkpoint WITH a value head.
func writeValueCheckpoint(t *testing.T, seed uint64) string {
	t.Helper()
	m := policynet.NewModel(policynet.TableRows, 8, 4, rand.New(rand.NewPCG(seed, 3)))
	m.InitValue(4, rand.New(rand.NewPCG(seed, 5)))
	m.Features = policynet.FeaturesMZ
	path := filepath.Join(t.TempDir(), "v.gpol")
	if err := m.SaveCheckpoint(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseAZVariant(t *testing.T) {
	v, err := parseAZVariant("cur=checkpoint:/a/b.gpol,sims:30,uniform-prior,heuristic-leaf:false,corpus:/c/d.gz")
	if err != nil {
		t.Fatal(err)
	}
	if v.name != "cur" || v.checkpoint != "/a/b.gpol" || v.corpus != "/c/d.gz" || !v.simsSet || v.sims != 30 ||
		!v.uniformSet || !v.uniform || !v.leafSet || v.leaf {
		t.Fatalf("parsed %+v", v)
	}
	if v, err = parseAZVariant("gen0"); err != nil || v.name != "gen0" || v.simsSet || v.leafSet || v.uniformSet {
		t.Fatalf("a bare name inherits everything: %+v %v", v, err)
	}
	for _, bad := range []string{"", "=sims:3", "a+b=sims:3", "a:b", "x=sims:-1", "x=sims:many", "x=bogus:1", "x=heuristic-leaf:maybe", "x=sims:1,sims:2", "x=checkpoint:"} {
		if _, err := parseAZVariant(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	saveAZVariants(t)
	var f azVariantFlag
	if err := f.Set("a=sims:2"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("a=sims:3"); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("a duplicate name: %v", err)
	}
}

func readVisitFile(t *testing.T, path string) []policynet.VisitRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var out []policynet.VisitRecord
	dec := json.NewDecoder(zr)
	for dec.More() {
		var r policynet.VisitRecord
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		r.State, r.Options = policynet.OnPolicyState{}, nil
		out = append(out, r)
	}
	return out
}

// Two variants in one run: each keeps its own checkpoint, leaf and budget,
// and each one's corpus holds only the decisions its own seat searched.
func TestAZVariantsAreIsolated(t *testing.T) {
	dir := corpusDirOrSkip(t)
	saveAZVariants(t)
	ck := writeValueCheckpoint(t, 7)
	tmp := t.TempDir()
	ca, cb := filepath.Join(tmp, "a.jsonl.gz"), filepath.Join(tmp, "b.jsonl.gz")
	var f azVariantFlag
	for _, spec := range []string{
		"heur=heuristic-leaf,sims:2,corpus:" + ca,
		"net=checkpoint:" + ck + ",sims:3,corpus:" + cb,
		"twin=checkpoint:" + ck,
	} {
		if err := f.Set(spec); err != nil {
			t.Fatal(err)
		}
	}
	azWorldArg, azKindsArg, azFlagsGiven = "clairvoyant", "priority,attackers,blockers,target", true
	azCfg.Search.Sims, azCfg.Search.UniformPrior, azCfg.Search.Discount = 4, true, 0.99
	out := filepath.Join(tmp, "out")
	o := sbOpts{bots: "az:heur,az:net", pairs: 1, decks: "FDN01-UBG", out: out, baseSeed: 5, catalog: "fdn", engineVersion: "test"}
	var stdout, stderr bytes.Buffer
	if rc := spellbenchExit(o, dir, 2, 12, 20000, "", &stdout, &stderr); rc != 0 {
		t.Fatalf("rc %d: %s", rc, stderr.String())
	}
	heur, net, twin := azVariantNamed("az:heur"), azVariantNamed("az:net"), azVariantNamed("az:twin")
	if heur.net != nil || !heur.cfg.Search.HeuristicLeaf || heur.cfg.Search.Sims != 2 {
		t.Fatalf("heur resolved to net %v cfg %+v", heur.net, heur.cfg.Search)
	}
	if net.net == nil || !net.net.HasValue() || net.cfg.Search.HeuristicLeaf || net.cfg.Search.Sims != 3 {
		t.Fatalf("net resolved to net %v cfg %+v", net.net, net.cfg.Search)
	}
	if twin.net != net.net || twin.cfg.Search.Sims != 4 {
		t.Fatalf("two variants on one checkpoint file must share one loaded model (%p %p), sims %d", twin.net, net.net, twin.cfg.Search.Sims)
	}
	for _, v := range []*azVariant{heur, net} {
		if !v.cfg.Search.UniformPrior || v.cfg.Search.Discount != 0.99 {
			t.Fatalf("%s did not inherit the global prior/discount: %+v", v.name, v.cfg.Search)
		}
	}
	// games.jsonl says who sat where.
	seats := map[string][2]string{}
	gf, err := os.Open(filepath.Join(out, "games.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer gf.Close()
	sc := bufio.NewScanner(gf)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var row struct {
			GameID string `json:"game_id"`
			P0     string `json:"p0"`
			P1     string `json:"p1"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		seats[row.GameID] = [2]string{row.P0, row.P1}
	}
	if len(seats) != 2 {
		t.Fatalf("%d games", len(seats))
	}
	for _, tc := range []struct {
		path, self, opp string
		sims            int
	}{{ca, "az:heur", "az:net", 2}, {cb, "az:net", "az:heur", 3}} {
		recs := readVisitFile(t, tc.path)
		if len(recs) == 0 {
			t.Fatalf("%s: no record", tc.path)
		}
		games := map[string]bool{}
		for _, r := range recs {
			games[r.GameID] = true
			if seats[r.GameID][r.Seat] != tc.self || r.Opponent != tc.opp || r.Sims != tc.sims {
				t.Fatalf("%s: record of game %s seat %d (sat by %s) opponent %s sims %d; want only %s's own decisions at sims %d",
					tc.path, r.GameID, r.Seat, seats[r.GameID][r.Seat], r.Opponent, r.Sims, tc.self, tc.sims)
			}
		}
		if len(games) != 2 {
			t.Fatalf("%s: records from %d games, want both", tc.path, len(games))
		}
	}
	if !strings.Contains(stdout.String(), "az variant heur visit corpus") {
		t.Fatalf("summary does not name the variant corpora:\n%s", stdout.String())
	}
	raw, _ := os.ReadFile(filepath.Join(out, "run.json"))
	if !bytes.Contains(raw, []byte(`"az_variants"`)) || !bytes.Contains(raw, []byte(`"az-clairvoyant-sims3-net"`)) {
		t.Fatalf("run.json does not record the variants:\n%s", raw)
	}
}

// A variant that overrides nothing is the plain az seat: same game.
func TestAZVariantWithoutOverridesIsPlainAZ(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	saveAZVariants(t)
	var f azVariantFlag
	if err := f.Set("same"); err != nil {
		t.Fatal(err)
	}
	azRegisterVariants()
	azWorldArg, azKindsArg, azFlagsGiven = "clairvoyant", "priority,attackers,blockers,target", true
	azCfg.Search.Sims = 3
	if err := azFrontDoor("az", "", nil, azSeatsFromSpellbench); err != nil {
		t.Fatal(err)
	}
	if err := azResolveVariants(); err != nil {
		t.Fatal(err)
	}
	cat, _ := spellbench.CatalogByID("fdn")
	deck, err := spellbench.Deck(reg, cat.Dir, cat.Pool[0])
	if err != nil {
		t.Fatal(err)
	}
	g := sbGame{id: "t", seed: sbGameSeed(9, 0, 0), deck: cat.Pool[0], seats: [2]string{"az", "bot"}}
	a := sbPlay(g, deck, reg, 12, 20000)
	g.seats[0] = "az:same"
	b := sbPlay(g, deck, reg, 12, 20000)
	if a.err != nil || b.err != nil || a.outcome != b.outcome {
		t.Fatalf("plain az %+v (%v) vs variant %+v (%v)", a.outcome, a.err, b.outcome, b.err)
	}
}

// The loud paths: a checkpoint with no value head, an unknown variant, two
// variants on one corpus file, and a variant flag with no variant seated.
func TestAZVariantFrontDoorRefusals(t *testing.T) {
	dir := corpusDirOrSkip(t)
	run := func(bots string, specs ...string) string {
		saveAZVariants(t)
		var f azVariantFlag
		for _, s := range specs {
			if err := f.Set(s); err != nil {
				t.Fatal(err)
			}
		}
		azWorldArg, azKindsArg, azFlagsGiven = "clairvoyant", "priority", true
		azCfg.Search.Sims = 2
		o := sbOpts{bots: bots, pairs: 1, decks: "FDN01-UBG", out: filepath.Join(t.TempDir(), "o"), baseSeed: 5, catalog: "fdn"}
		var stdout, stderr bytes.Buffer
		if rc := spellbenchExit(o, dir, 1, 4, 2000, "", &stdout, &stderr); rc == 0 {
			t.Fatalf("%s %v: accepted", bots, specs)
		}
		return stderr.String()
	}
	if msg := run("az:pol,bot", "pol=checkpoint:"+writeZeroCheckpoint(t)); !strings.Contains(msg, "no value head") || !strings.Contains(msg, "pol") {
		t.Errorf("a policy-only checkpoint: %s", msg)
	}
	if msg := run("az:nope,bot", "x=sims:2"); !strings.Contains(msg, "az:nope") {
		t.Errorf("an undefined variant: %s", msg)
	}
	shared := filepath.Join(t.TempDir(), "c.gz")
	if msg := run("az:a,az:b", "a=corpus:"+shared, "b=corpus:"+shared); !strings.Contains(msg, "same corpus") {
		t.Errorf("one corpus for two variants: %s", msg)
	}
	if msg := run("az:a,bot", "a=checkpoint:"+filepath.Join(t.TempDir(), "missing.gpol")); !strings.Contains(msg, "missing.gpol") {
		t.Errorf("a missing checkpoint: %s", msg)
	}
}

// With no variant and neither new flag, run.json's az block is the one the
// bench wrote before they existed.
func TestAZDefaultRunRecordUnchanged(t *testing.T) {
	saveAZVariants(t)
	azCfg.Search.Sims, azWorldArg = 7, "clairvoyant"
	raw, err := json.Marshal(azRunRecord())
	if err != nil || string(raw) != `{"sims":7,"world":"clairvoyant","worlds":0}` {
		t.Fatalf("default az run record %s %v", raw, err)
	}
	azCfg.Search.Discount = 0.99
	raw, _ = json.Marshal(azRunRecord())
	if !bytes.Contains(raw, []byte(`"discount":0.99`)) {
		t.Fatalf("a set discount is not recorded: %s", raw)
	}
}

// Two variants on one configuration are one deterministic policy: the seat
// swap replays the same game, which is what -spellbench-first-game-only
// drops.
func TestSpellbenchFirstGameOnly(t *testing.T) {
	dir := corpusDirOrSkip(t)
	saveAZVariants(t)
	var f azVariantFlag
	for _, spec := range []string{"a", "b"} {
		if err := f.Set(spec); err != nil {
			t.Fatal(err)
		}
	}
	azWorldArg, azKindsArg, azFlagsGiven = "clairvoyant", "priority,attackers,blockers,target", true
	azCfg.Search.Sims = 2
	play := func(firstOnly bool) []string {
		out := filepath.Join(t.TempDir(), "o")
		o := sbOpts{bots: "az:a,az:b", pairs: 1, decks: "FDN01-UBG,FDN02-WG", out: out, baseSeed: 5, catalog: "fdn", firstOnly: firstOnly}
		var stdout, stderr bytes.Buffer
		if rc := spellbenchExit(o, dir, 2, 12, 20000, "", &stdout, &stderr); rc != 0 {
			t.Fatalf("rc %d: %s", rc, stderr.String())
		}
		raw, err := os.ReadFile(filepath.Join(out, "games.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		var rows []string
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var row struct {
				GameID  string `json:"game_id"`
				Result  string `json:"result"`
				Turns   int    `json:"turns"`
				Intents int    `json:"intents"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, fmt.Sprintf("%s %s %d %d", row.GameID, row.Result, row.Turns, row.Intents))
		}
		return rows
	}
	both, first := play(false), play(true)
	if len(both) != 4 || len(first) != 2 || first[0] != both[0] || first[1] != both[2] {
		t.Fatalf("first-game-only %v of %v", first, both)
	}
	for p := 0; p < 2; p++ {
		if strings.TrimPrefix(both[2*p], "m0000p000"+fmt.Sprint(p)+"g0") != strings.TrimPrefix(both[2*p+1], "m0000p000"+fmt.Sprint(p)+"g1") {
			t.Fatalf("pair %d: identical seats, yet the swap is another game: %q vs %q", p, both[2*p], both[2*p+1])
		}
	}
}
