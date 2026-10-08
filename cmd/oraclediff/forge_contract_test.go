package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// exportInline exports hand-written scenario lines and returns the requests
// and the request file's path.
func exportInline(t *testing.T, reg *cards.Registry, items ...string) ([]forgeRequest, string) {
	t.Helper()
	dir := t.TempDir()
	scen := filepath.Join(dir, "items.jsonl")
	if err := os.WriteFile(scen, []byte(strings.Join(items, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "forge-req.jsonl")
	if _, err := forgeExport(reg, scen, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return decodeRequests(t, b), out
}

// TestForgeExportCarriesDividedSplit: a spell dividing damage between two
// targets reaches the driver as its own "damage_split" decision (one pick per
// point, pick_idx naming the target), not as a field on the target ask, whose
// "divided" is 0 for a spell cast from hand. This is the answer to the
// driver's question whether the sidecar can carry per-target amounts: it
// already does, in gorge_decisions.
func TestForgeExportCarriesDividedSplit(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	reqs, _ := exportInline(t, reg, `{"id":"Twin Bolt/cast-resolve/v1","card":"Twin Bolt","template":"cast-resolve","name":"x","cr":["601.2"],"why":"hand-written fixture","setup":{"p0":{"hand":["Twin Bolt"]},"p1":{"battlefield":["Grizzly Bears","Serra Angel"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Twin Bolt","mana":"CR","targets":["p1:Grizzly Bears","p1:Serra Angel"]},{"op":"resolve","seat":0}]}`)
	var target, split *int
	for i, d := range reqs[0].GorgeDecisions {
		switch {
		case d.Kind == "target":
			target = &i
		case d.Resume == "damage_split":
			split = &i
		}
	}
	if target == nil {
		t.Fatalf("no target decision: %+v", reqs[0].GorgeDecisions)
	}
	if split == nil {
		t.Fatalf("the divided allocation is missing from gorge_decisions: %+v", reqs[0].GorgeDecisions)
	}
	tg, sp := reqs[0].GorgeDecisions[*target], reqs[0].GorgeDecisions[*split]
	if len(tg.PickRefs) != 2 || tg.PickRefs[0] == tg.PickRefs[1] {
		t.Fatalf("precondition: the target ask must name two distinct targets: %v", tg.PickRefs)
	}
	if tg.Divided != 0 {
		t.Errorf("target ask divided = %d: the contract says the driver sums the split, because a cast spell leaves it 0", tg.Divided)
	}
	if *split <= *target || sp.Kind != "choose_n" || sp.Min != 2 || sp.Max != 2 || len(sp.PickIdx) != 2 {
		t.Errorf("split = %+v, want a later choose_n with min == max == 2 and one pick_idx per point", sp)
	}
	if !reflect.DeepEqual(sp.PickRefs, []string{"p1:Grizzly Bears", "p1:Serra Angel"}) {
		t.Errorf("split pick_refs = %v, want the chosen targets in order, one per point", sp.PickRefs)
	}
}

// echoed adds a request_sha field to a Forge row, as the driver now does.
func echoed(t *testing.T, row []byte, sha string) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(row, &m); err != nil {
		t.Fatal(err)
	}
	m["request_sha"], _ = json.Marshal(sha)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestForgeDiffRequestShaEcho: a Forge row that echoes the sha of the request
// line it answered is used, and one that echoes another sha (an older export
// with the same id) is dropped as stale: it is not compared, not cached, and
// the cache entry for the current sha still answers.
func TestForgeDiffRequestShaEcho(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, reqPath := exportFixture(t, reg)
	lines := readLinesBytes(t, reqPath)
	reqs := decodeRequests(t, joinLines(lines))
	sha := gate.Hash(lines[0])
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")

	good := echoed(t, forgeRowFor(t, reg, reqs[0], nil), sha)
	stale := echoed(t, forgeRowFor(t, reg, reqs[0], func(x *oraclediff.XResult) { x.Snapshots[1].Players[0].Life++ }), gate.Hash([]byte("an older request line")))

	run := func(name string, forge []byte, cache string) ForgeRow {
		t.Helper()
		fp := filepath.Join(tmp, name+"-forge.jsonl")
		writeLines(t, fp, forge)
		rp := filepath.Join(tmp, name+"-req.jsonl")
		writeLines(t, rp, lines[0])
		out := filepath.Join(tmp, name+"-diff.jsonl")
		if _, err := forgeDiff(reg, rp, fp, cache, out); err != nil {
			t.Fatal(err)
		}
		return readRows(t, out)[0]
	}

	// Precondition: without the echo the stale row's mutation is a real
	// divergence, so a pass below cannot come from a row that agrees anyway.
	if v := run("plain", forgeRowFor(t, reg, reqs[0], func(x *oraclediff.XResult) { x.Snapshots[1].Players[0].Life++ }), filepath.Join(tmp, "c0")).Verdict; v.Status != oraclediff.Diverge {
		t.Fatalf("precondition: the mutated row should diverge, got %+v", v)
	}

	if v := run("good", good, cacheDir).Verdict; v.Status != oraclediff.Agree {
		t.Errorf("a row echoing the right request_sha = %+v, want AGREE", v)
	}
	if _, ok := (oraclediff.Cache{Dir: cacheDir}).Get(sha); !ok {
		t.Fatal("the good row was not cached")
	}
	// Stale row, empty cache: dropped, so no verdict and nothing cached.
	emptyCache := filepath.Join(tmp, "cache2")
	v := run("stale", stale, emptyCache).Verdict
	if v.Status != oraclediff.Harness || !strings.Contains(v.Msg, "stale Forge row") {
		t.Errorf("a row echoing another request_sha = %+v, want a stale-row HARNESS", v)
	}
	if _, ok := (oraclediff.Cache{Dir: emptyCache}).Get(sha); ok {
		t.Error("a stale row was cached under the current request sha")
	}
	// Stale row, warm cache: the cached answer for the current sha is used.
	if v := run("stale-warm", stale, cacheDir).Verdict; v.Status != oraclediff.Agree {
		t.Errorf("a stale row with a warm cache = %+v, want the cached AGREE", v)
	}
}

func joinLines(lines [][]byte) []byte {
	var out []byte
	for i, l := range lines {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, l...)
	}
	return out
}

// TestForgeDiffIgnoresLibraryTopOnlyForNamedLibrary: a seat that names
// library cards has a deal-shuffled library gorge and Forge cannot agree on,
// so library_top is not compared; a seat naming only library_top (a
// deterministic top) is still compared.
func TestForgeDiffIgnoresLibraryTopOnlyForNamedLibrary(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const steps = `"steps":[{"op":"cast","seat":0,"card":"p0:Shock","mana":"R","targets":["p1"]},{"op":"resolve","seat":0}]`
	reqs, reqPath := exportInline(t, reg,
		`{"id":"named-library","card":"Shock","template":"cast-resolve","name":"a","cr":["601.2"],"why":"hand-written fixture","setup":{"p0":{"hand":["Shock"],"library":["Forest","Plains"]}},`+steps+`}`,
		`{"id":"named-top","card":"Shock","template":"cast-resolve","name":"b","cr":["601.2"],"why":"hand-written fixture","setup":{"p0":{"hand":["Shock"],"library_top":["Forest","Plains"]}},`+steps+`}`,
		`{"id":"named-library-own-ignore","card":"Shock","template":"cast-resolve","name":"c","cr":["601.2"],"why":"hand-written fixture","ignore":["life"],"setup":{"p0":{"hand":["Shock"],"library":["Forest"]}},`+steps+`}`)
	reorder := func(x *oraclediff.XResult) {
		p := &x.Snapshots[1].Players[0]
		p.LibraryTop = append([]string{"Swamp"}, p.LibraryTop...)
	}
	var forge [][]byte
	for _, rq := range reqs {
		forge = append(forge, forgeRowFor(t, reg, rq, reorder))
	}
	tmp := t.TempDir()
	fp := filepath.Join(tmp, "forge.jsonl")
	writeLines(t, fp, forge...)
	out := filepath.Join(tmp, "diff.jsonl")
	if _, err := forgeDiff(reg, reqPath, fp, filepath.Join(tmp, "c"), out); err != nil {
		t.Fatal(err)
	}
	rows := readRows(t, out)
	if rows[0].Verdict.Status != oraclediff.Agree {
		t.Errorf("a named library with a different library_top = %+v, want AGREE", rows[0].Verdict)
	}
	if v := rows[1].Verdict; v.Status != oraclediff.Diverge || v.Field != "p0.library_top" {
		t.Errorf("a library_top-only seat with a different library_top = %+v, want DIVERGE on p0.library_top", v)
	}
	if rows[2].Verdict.Status != oraclediff.Agree {
		t.Errorf("an item with its own ignore list lost library_top: %+v", rows[2].Verdict)
	}
	if got := forgeIgnore(reqs[2].Item); !reflect.DeepEqual(got, []string{"life", "library_top"}) {
		t.Errorf("forgeIgnore = %v, want the item's own ignore plus library_top", got)
	}
	if got := reqs[2].Item.Ignore; !reflect.DeepEqual(got, []string{"life"}) {
		t.Errorf("forgeIgnore edited the item's ignore list: %v", got)
	}
}

// TestForgeContractStepCastVariant pins what the contract says about costs.
// gen's Step carries cast_mode, a non-default cast option (currently only
// "optionalcost") the driver must select, and still carries no kicked, so
// gorge pays a kick only through a decision it logs. If gen adds kicked, the
// driver must read it from the step and the contract text must change.
func TestForgeContractStepCastVariant(t *testing.T) {
	tags := map[string]bool{}
	st := reflect.TypeOf(oraclegen.Step{})
	for i := 0; i < st.NumField(); i++ {
		tags[strings.Split(st.Field(i).Tag.Get("json"), ",")[0]] = true
	}
	if !tags["ability_index"] || !tags["targets"] {
		t.Fatalf("precondition: expected step tags not found: %v", tags)
	}
	if !tags["cast_mode"] {
		t.Errorf("oraclegen.Step lost cast_mode: the Forge contract (forgeRequest doc) says the driver selects the named cast option")
	}
	if tags["kicked"] {
		t.Errorf("oraclegen.Step gained kicked: the Forge contract (forgeRequest doc) says no step names a kick, so gorge pays one only through a logged optional-cost decision")
	}
}
