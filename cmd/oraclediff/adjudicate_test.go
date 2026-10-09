package main

// Canned-cache tests for oraclediff adjudicate (plan P3-2 and P3-3). No
// engine runs here: adjudicate is a join, so every fixture is a t.TempDir
// of committed verdict rows, forge-diff lines and the two caches, and the
// V(F,X) leg is computed by the same CompareOpts the command uses.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/shape"
	"github.com/adams-shaun/gorge/rules"
)

const adjRef = "0123456789abcdef"

// The canned engine states the fixtures reuse. fx20/fx21 are Forge results
// (gorge-vocabulary snapshots, p1.life 20 or 21); x20/x21 the XMage
// results in XMage's vocabulary.
var (
	fx20 = &oraclediff.XResult{Snapshots: []rules.OracleSnapshot{adjGorgeSnap("setup")}}
	fx21 = &oraclediff.XResult{Snapshots: []rules.OracleSnapshot{adjGorgeSnap("setup")}}
	x20  = &oraclediff.XResult{Snapshots: []rules.OracleSnapshot{adjXMageSnap("setup")}}
	x21  = &oraclediff.XResult{Snapshots: []rules.OracleSnapshot{adjXMageSnap("setup")}}
)

func init() { fx21.Snapshots[0].Players[1].Life = 21; x21.Snapshots[0].Players[1].Life = 21 }

// adjGorgeSnap is a gorge-vocabulary snapshot (what Forge's driver emits;
// DESIGN section 6.7).
func adjGorgeSnap(cp string) rules.OracleSnapshot {
	return rules.OracleSnapshot{
		Checkpoint: cp, Turn: 1, Step: "main1",
		Players: []rules.OracleSnapPlayer{
			{Seat: 0, Life: 20, Hand: []string{}, Graveyard: []string{}, LibraryCount: 39, LibraryTop: []string{"Wastes"}},
			{Seat: 1, Life: 20, Hand: []string{}, Graveyard: []string{}, LibraryCount: 38, LibraryTop: []string{"Shock"}},
		},
		Permanents: []rules.OracleSnapPerm{
			{Ref: "p1:Grizzly Bears", Name: "Grizzly Bears", Controller: 1, Owner: 1, PT: "2/2", Types: []string{"Bear", "Creature"}, Colors: "G"},
		},
	}
}

// adjXMageSnap is the same state in XMage's vocabulary.
func adjXMageSnap(cp string) rules.OracleSnapshot {
	s := adjGorgeSnap(cp)
	s.Step = "PRECOMBAT_MAIN"
	s.Permanents = []rules.OracleSnapPerm{
		{Name: "Grizzly Bears", Controller: 1, Owner: 1, PT: "2/2", Types: []string{"Creature", "Bear"}, Colors: "G"},
	}
	return s
}

type adjFixture struct {
	dir      string // run directory (holds forge-diff.jsonl)
	scen     string
	verdicts string
	xcache   string
	fcache   string
	ledger   string
	report   string
	fdPath   string
	shas     map[string]string // card -> scenario sha
}

// adjCase is one scenario's canned three-way inputs: forge is the Forge
// result (the forge cache, keyed by the request sha) and xmage the XMage
// result (the XMage cache, keyed by the scenario sha); gf is the forge-diff
// row's V(G,F) verdict. nil means the cache holds no entry for it.
type adjCase struct {
	it    oraclegen.Item
	forge *oraclediff.XResult
	xmage *oraclediff.XResult
	gf    oraclediff.Verdict
}

// newAdjFixture writes the canned caches and forge-diff rows for the
// scenario table below and returns the paths.
func newAdjFixture(t *testing.T, cases []adjCase) *adjFixture {
	t.Helper()
	dir := t.TempDir()
	f := &adjFixture{
		dir:      dir,
		scen:     filepath.Join(dir, "scen.jsonl"),
		verdicts: filepath.Join(dir, "verdicts"),
		xcache:   filepath.Join(dir, "xmage-cache"),
		fcache:   filepath.Join(dir, "forge-cache"),
		ledger:   filepath.Join(dir, "ledger"),
		report:   filepath.Join(dir, "adjudicate-report.jsonl"),
		fdPath:   filepath.Join(dir, "forge-diff.jsonl"),
		shas:     map[string]string{},
	}
	// fx20/fx21 and x20/x21 are the package-level engine states above.
	forgeCache := oraclediff.Cache{Dir: f.fcache}
	xcache := oraclediff.Cache{Dir: f.xcache}

	var fdRows []ForgeRow
	for _, c := range cases {
		it, gf := c.it, c.gf
		sha := gate.ItemSHA(it)
		f.shas[it.Card] = sha
		if c.forge != nil {
			if err := forgeCache.Put("req-"+it.Card, *c.forge); err != nil {
				t.Fatal(err)
			}
		}
		if c.xmage != nil {
			if err := xcache.Put(sha, *c.xmage); err != nil {
				t.Fatal(err)
			}
		}
		fdRows = append(fdRows, ForgeRow{ID: it.ID, Card: it.Card, Template: it.Template,
			Verdict: gf, RequestSHA: "req-" + it.Card, ScenarioSHA: sha})
	}
	// The scenario file, in table order.
	scen, err := os.Create(f.scen)
	if err != nil {
		t.Fatal(err)
	}
	defer scen.Close()
	for _, c := range cases {
		b, _ := json.Marshal(c.it)
		scen.Write(b)
		scen.WriteString("\n")
	}
	// The forge-diff file, in the same order.
	fd, err := os.Create(f.fdPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	for _, r := range fdRows {
		b, _ := json.Marshal(r)
		fd.Write(b)
		fd.WriteString("\n")
	}
	return f
}

// adjItem is a minimal scenario Item: adjudicate never replays it, it only
// reads Compare/Ignore and hashes the bytes.
func adjItem(id, card, template string) oraclegen.Item {
	return oraclegen.Item{ID: id, Card: card, Template: template}
}

// adjCommitted writes the committed verdict rows the fixture's scenarios
// join against.
func (f *adjFixture) adjCommitted(t *testing.T) {
	t.Helper()
	divergeDetail := "setup p1.life: gorge \"20\", xmage \"21\""
	rows := []compliance.VerdictRow{
		{Card: "Agree Card", Template: "cast-resolve", ID: "agree-a", ScenarioSHA: f.shas["Agree Card"], Status: compliance.StatusAgree},
		{Card: "GF Diff Card", Template: "cast-resolve", ID: "gf-a", ScenarioSHA: f.shas["GF Diff Card"],
			Status: compliance.StatusDiverge, Detail: divergeDetail},
		{Card: "FX Auto Card", Template: "trigger#0", ID: "fx-a", ScenarioSHA: f.shas["FX Auto Card"],
			Status: compliance.StatusXMageWrong, RulingID: "xmage-test-auto", Detail: divergeDetail},
		{Card: "FX Hand Card", Template: "cast-resolve", ID: "fx-h", ScenarioSHA: f.shas["FX Hand Card"],
			Status: compliance.StatusXMageWrong, Ruling: "hand: XMage is wrong per CR 100.1", Detail: divergeDetail},
		{Card: "FX Open Card", Template: "trigger#1", ID: "fxo-a", ScenarioSHA: f.shas["FX Open Card"],
			Status: compliance.StatusDiverge, Detail: divergeDetail},
		{Card: "GF Ruled Card", Template: "cast-resolve", ID: "gfr-a", ScenarioSHA: f.shas["GF Ruled Card"],
			Status: compliance.StatusXMageWrong, RulingID: "xmage-test-auto", Detail: divergeDetail},
		{Card: "GX Agree Card", Template: "cast-resolve", ID: "gx-a", ScenarioSHA: f.shas["GX Agree Card"], Status: compliance.StatusAgree},
		{Card: "Lacks Card", Template: "cast-resolve", ID: "lacks-a", ScenarioSHA: f.shas["Lacks Card"],
			Status: compliance.StatusXMageLacks, Detail: "xmage: Couldn't find a card: Lacks Card"},
		{Card: "XMage Boom Card", Template: "cast-resolve", ID: "boom-a", ScenarioSHA: f.shas["XMage Boom Card"],
			Status: compliance.StatusHarness, Detail: "xmage: driver exploded"},
		{Card: "Forge Miss Card", Template: "cast-resolve", ID: "fmiss-a", ScenarioSHA: f.shas["Forge Miss Card"], Status: compliance.StatusAgree},
		{Card: "Gorge Boom Card", Template: "cast-resolve", ID: "gboom-a", ScenarioSHA: f.shas["Gorge Boom Card"],
			Status: compliance.StatusHarness, Detail: "gorge: no deck list"},
	}
	// The committed Stale row points at another scenario's sha (a first
	// byte flipped), so the join refuses it as stale.
	if sha := f.shas["Stale Card"]; len(sha) >= 4 {
		rows = append(rows, compliance.VerdictRow{Card: "Stale Card", Template: "cast-resolve", ID: "stale-a",
			ScenarioSHA: "0000" + sha[4:], Status: compliance.StatusAgree})
	}
	if err := compliance.ReplaceVerdicts(f.verdicts, rows); err != nil {
		t.Fatal(err)
	}
}

func (f *adjFixture) args(extra ...string) []string {
	out := []string{"-scenarios", f.scen, "-verdicts", f.verdicts, "-forge-diff", f.fdPath,
		"-xmage-cache", f.xcache, "-forge-cache", f.fcache, "-oracle-ref", adjRef,
		"-ledger", f.ledger, "-report", f.report}
	return append(out, extra...)
}

func readReports(t *testing.T, path string) []reportRow {
	t.Helper()
	var out []reportRow
	if err := readLines(path, func(b []byte) error {
		var r reportRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func patternByCard(t *testing.T, dir string) map[string]string {
	t.Helper()
	rows, err := loadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range rows {
		if prev, ok := out[r.Card]; ok {
			t.Errorf("card %q in the ledger twice (%s, %s)", r.Card, prev, r.Pattern)
		}
		out[r.Card] = r.Pattern
	}
	return out
}

// gfDiverge is the V(G,F) verdict a forge-diff row carries when gorge and
// Forge disagree on p1.life.
func gfDiverge() oraclediff.Verdict {
	return oraclediff.Verdict{Status: oraclediff.Diverge, Checkpoint: "setup", Field: "p1.life",
		Gorge: "20", XMage: "21"}
}

func TestAdjudicatePatterns(t *testing.T) {
	gfOK := oraclediff.Verdict{Status: oraclediff.Agree}
	f := newAdjFixture(t, []adjCase{
		// All three agree.
		{it: adjItem("agree-a", "Agree Card", "cast-resolve"), forge: fx20, xmage: x20, gf: gfOK},
		// gorge = Forge, both differ from XMage (the XMage value is the odd one).
		{it: adjItem("gf-a", "GF Diff Card", "cast-resolve"), forge: fx21, xmage: x20, gf: gfOK},
		// An FX|G on an automatic ruling: Forge = XMage, gorge is the odd
		// one out (forge 21, xmage 21, gorge's committed value 20).
		{it: adjItem("fx-a", "FX Auto Card", "trigger#0"), forge: fx21, xmage: x21, gf: gfDiverge()},
		// The same shape on a hand ruling.
		{it: adjItem("fx-h", "FX Hand Card", "cast-resolve"), forge: fx21, xmage: x21, gf: gfDiverge()},
		// An unruled FX|G: the report suggests the gorge_wrong ruling.
		{it: adjItem("fxo-a", "FX Open Card", "trigger#1"), forge: fx21, xmage: x21, gf: gfDiverge()},
		// A GF|X on an already-ruled xmage_wrong row is corroboration
		// (Forge sides with gorge against XMage: forge 21, xmage 20).
		{it: adjItem("gfr-a", "GF Ruled Card", "cast-resolve"), forge: fx21, xmage: x20, gf: gfOK},
		// gorge = XMage, Forge differs: recorded only.
		{it: adjItem("gx-a", "GX Agree Card", "cast-resolve"), forge: fx20, xmage: x21, gf: gfDiverge()},
		// XMage lacks the card; only G/F is compared (no xmage cache entry).
		{it: adjItem("lacks-a", "Lacks Card", "cast-resolve"), forge: fx20, gf: gfOK},
		// The XMage driver harnessed; likewise.
		{it: adjItem("boom-a", "XMage Boom Card", "cast-resolve"), forge: fx20, gf: gfOK},
		// The Forge pass found nothing for the request: F harness.
		{it: adjItem("fmiss-a", "Forge Miss Card", "cast-resolve"), xmage: x20, gf: oraclediff.Verdict{Status: oraclediff.Harness, Engine: "forge", Msg: "no Forge result"}},
		// Gorge itself could not run the scenario: no section 8.1 pattern.
		{it: adjItem("gboom-a", "Gorge Boom Card", "cast-resolve"), forge: fx20, xmage: x20, gf: gfOK},
		// The committed verdict is for an older scenario: skip.
		{it: adjItem("stale-a", "Stale Card", "cast-resolve"), forge: fx20, xmage: x20, gf: gfOK},
	})
	f.adjCommitted(t)
	if err := runAdjudicate(f.args()); err != nil {
		t.Fatal(err)
	}
	got := patternByCard(t, f.ledger)
	want := map[string]string{
		"Agree Card":      "AGREE3",
		"GF Diff Card":    "GF|X",
		"FX Auto Card":    "FX|G",
		"FX Hand Card":    "FX|G",
		"FX Open Card":    "FX|G",
		"GF Ruled Card":   "GF|X",
		"GX Agree Card":   "GX|F",
		"Lacks Card":      "GF|–",
		"XMage Boom Card": "GF|–",
		"Forge Miss Card": "F harness",
	}
	for card, p := range want {
		if got[card] != p {
			t.Errorf("%s: pattern %q, want %q", card, got[card], p)
		}
	}
	// Precondition for the skip assertions: the gorge-harness and stale
	// rows exist as scenarios with committed rows, but classify nothing.
	for _, card := range []string{"Gorge Boom Card", "Stale Card"} {
		if got[card] != "" {
			t.Errorf("%s: classified %q, want no ledger row", card, got[card])
		}
	}
	if len(got) != len(want) {
		t.Errorf("ledger holds %d cards, want %d: %v", len(got), len(want), got)
	}
	// The ledger row carries the ref pin and the F/X first-difference field.
	rows, err := loadLedger(f.ledger)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ForgeRef != adjRef[:12] {
			t.Errorf("%s: forge_ref %q, want %q", r.Card, r.ForgeRef, adjRef[:12])
		}
		if r.ScenarioSHA != f.shas[r.Card] {
			t.Errorf("%s: scenario_sha %q, want %q", r.Card, r.ScenarioSHA, f.shas[r.Card])
		}
		switch r.Card {
		case "GF Diff Card":
			if r.FXField != "p1.life" || r.GFField != "" {
				t.Errorf("GF|X ledger fields: gf %q fx %q", r.GFField, r.FXField)
			}
		case "FX Auto Card":
			if r.GFField != "p1.life" || r.FXField != "" {
				t.Errorf("FX|G ledger fields: gf %q fx %q", r.GFField, r.FXField)
			}
		}
	}

	reps := readReports(t, f.report)
	byCard := map[string]reportRow{}
	for _, r := range reps {
		byCard[r.Card] = r
	}
	// The GF|X diverge row carries the suggested rule command with the
	// @ref12 suffix, and the F/X first-difference field.
	gf := byCard["GF Diff Card"]
	wantRule := `oraclediff rule -card "GF Diff Card" -status xmage_wrong -ruling "<Oracle/CR cite>; forge GF|X @` + adjRef[:12] + `"`
	if gf.Rule != wantRule {
		t.Errorf("GF|X rule command:\n got %q\nwant %q", gf.Rule, wantRule)
	}
	if gf.FXField != "p1.life" || gf.GFField != "" || gf.GXField != "p1.life" {
		t.Errorf("GF|X fields: gx %q gf %q fx %q", gf.GXField, gf.GFField, gf.FXField)
	}
	// The FX|G row that is already ruled is not asked to be re-ruled; its
	// treatment is the review marking, not a command.
	if got := byCard["FX Auto Card"].Rule; got != "" {
		t.Errorf("ruled FX|G row suggests a rule command %q", got)
	}
	// The unruled FX|G row suggests gorge_wrong, and a level-B template is
	// passed on.
	fxo := byCard["FX Open Card"]
	wantRule = `oraclediff rule -card "FX Open Card" -status gorge_wrong -template trigger#1 -ruling "<Oracle cite>; forge FX|G @` + adjRef[:12] + `"`
	if fxo.Rule != wantRule {
		t.Errorf("FX|G rule command:\n got %q\nwant %q", fxo.Rule, wantRule)
	}
	if fxo.GFField != "p1.life" || fxo.FXField != "" || fxo.GXField != "p1.life" {
		t.Errorf("FX|G fields: gx %q gf %q fx %q", fxo.GXField, fxo.GFField, fxo.FXField)
	}
	// No rule command where the flow would change nothing: the AGREE3 row,
	// and the GF|X that corroborates an existing xmage_wrong ruling.
	for _, card := range []string{"Agree Card", "GF Ruled Card"} {
		if got := byCard[card].Rule; got != "" {
			t.Errorf("%s suggests a rule command %q", card, got)
		}
	}
	// The contradictions are listed (section 8.2), and the corroboration
	// is a note, not one.
	if got := byCard["FX Auto Card"].Contradiction; got != "xmage_wrong contradicted by FX|G" {
		t.Errorf("FX Auto Card contradiction %q", got)
	}
	if got := byCard["FX Hand Card"].Contradiction; got != "xmage_wrong contradicted by FX|G" {
		t.Errorf("FX Hand Card contradiction %q", got)
	}
	if got := byCard["GX Agree Card"].Contradiction; got != "agree contradicted by GX|F (recorded only)" {
		t.Errorf("GX Agree Card contradiction %q", got)
	}
	if got := byCard["GF Ruled Card"].Contradiction; got != "" {
		t.Errorf("GF Ruled Card contradiction %q", got)
	}
	if got := byCard["GF Ruled Card"].Note; got != "Forge corroborates the ruling" {
		t.Errorf("GF Ruled Card note %q", got)
	}
	// The skips name their reason.
	skipped := map[string]string{}
	for _, r := range reps {
		if r.Note != "" {
			skipped[r.Card] = r.Note
		}
	}
	if got := skipped["Gorge Boom Card"]; !strings.HasPrefix(got, "the three pairwise verdicts match no section 8.1 row") {
		t.Errorf("gorge harness skip note %q", got)
	}
	if got := skipped["Stale Card"]; !strings.HasPrefix(got, "verdict is for an older scenario") {
		t.Errorf("stale skip note %q", got)
	}
}

func TestAdjudicateD1BLedgerInRunDir(t *testing.T) {
	f := newAdjFixture(t, []adjCase{
		{it: adjItem("agree-a", "Agree Card", "cast-resolve"), forge: fx20, xmage: x20, gf: oraclediff.Verdict{Status: oraclediff.Agree}},
	})
	f.adjCommitted(t)
	// D1=B: no -ledger, so the shards land next to forge-diff.jsonl, and
	// nothing is written under compliance/.
	if err := runAdjudicate(f.args("-d1", "B", "-ledger", "compliance/adjudication")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("compliance/adjudication"); !os.IsNotExist(err) {
		t.Fatalf("D1=B wrote into the repository: %v", err)
	}
	got := patternByCard(t, f.dir)
	if got["Agree Card"] != "AGREE3" {
		t.Errorf("D1=B ledger pattern %q", got["Agree Card"])
	}
}

func TestAdjudicateBadArgs(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"-scenarios", "s", "-forge-diff", "f", "-xmage-cache", "x"},
		{"-scenarios", "s", "-forge-diff", "f", "-xmage-cache", "x", "-oracle-ref", "r", "-d1", "A"},
	} {
		if err := runAdjudicate(args); err == nil {
			t.Errorf("runAdjudicate(%v) accepted", args)
		}
	}
}

// TestAdjudicateVerdictsUntouched: a run without -mark-review writes
// nothing to the committed verdict directory (plan P3-2: "never change a
// verdict status"; the landing check is `git diff -- compliance/verdicts`
// empty after a run).
func TestAdjudicateVerdictsUntouched(t *testing.T) {
	f := newAdjFixture(t, []adjCase{
		{it: adjItem("fx-a", "FX Auto Card", "trigger#0"), forge: fx21, xmage: x21, gf: gfDiverge()},
		{it: adjItem("fx-h", "FX Hand Card", "cast-resolve"), forge: fx21, xmage: x21, gf: gfDiverge()},
	})
	f.adjCommitted(t)
	before := verdictBytes(t, f.verdicts)
	if err := runAdjudicate(f.args()); err != nil {
		t.Fatal(err)
	}
	after := verdictBytes(t, f.verdicts)
	if string(before) != string(after) {
		t.Errorf("the verdict directory changed without -mark-review:\n%s\n---\n%s", before, after)
	}
}

func verdictBytes(t *testing.T, dir string) []byte {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	sortStrings(paths)
	var b []byte
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, raw...)
	}
	return b
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// TestAdjudicateMarkReview: -mark-review under D1=C sets Review: pending --
// and nothing else -- on the section 8.2 contradiction rows, hand ruling or
// automatic; the agree + GX|F contradiction and a corroborated row are not
// touched; under D1=B nothing is written.
func TestAdjudicateMarkReview(t *testing.T) {
	gfOK := oraclediff.Verdict{Status: oraclediff.Agree}
	f := newAdjFixture(t, []adjCase{
		{it: adjItem("fx-a", "FX Auto Card", "trigger#0"), forge: fx21, xmage: x21, gf: gfDiverge()},
		{it: adjItem("fx-h", "FX Hand Card", "cast-resolve"), forge: fx21, xmage: x21, gf: gfDiverge()},
		{it: adjItem("gx-a", "GX Agree Card", "cast-resolve"), forge: fx20, xmage: x21, gf: gfDiverge()},
		{it: adjItem("gfr-a", "GF Ruled Card", "cast-resolve"), forge: fx21, xmage: x21, gf: gfOK},
		{it: adjItem("fmiss-a", "Forge Miss Card", "cast-resolve"), xmage: x20, gf: oraclediff.Verdict{Status: oraclediff.Harness, Engine: "forge", Msg: "no Forge result"}},
	})
	f.adjCommitted(t)
	if err := runAdjudicate(f.args("-mark-review")); err != nil {
		t.Fatal(err)
	}
	all, err := compliance.LoadVerdicts(f.verdicts)
	if err != nil {
		t.Fatal(err)
	}
	get := func(card string) compliance.VerdictRow { return all[card]["trigger#0"] }
	hand := func(card string) compliance.VerdictRow { return all[card]["cast-resolve"] }
	// The automatic xmage_wrong contradiction is now pending review.
	auto := get("FX Auto Card")
	if auto.Review != shape.ReviewPending {
		t.Errorf("FX Auto Card review %q, want %q", auto.Review, shape.ReviewPending)
	}
	if auto.Status != compliance.StatusXMageWrong || auto.RulingID != "xmage-test-auto" {
		t.Errorf("FX Auto Card status/ruling touched: %+v", auto)
	}
	// The hand ruling keeps its status and ruling; only the review reopens.
	h := hand("FX Hand Card")
	if h.Review != shape.ReviewPending {
		t.Errorf("FX Hand Card review %q, want %q", h.Review, shape.ReviewPending)
	}
	if h.Status != compliance.StatusXMageWrong || h.Ruling == "" || h.RulingID != "" {
		t.Errorf("hand ruling touched: %+v", h)
	}
	// The agree + GX|F contradiction is recorded only.
	if r := hand("GX Agree Card"); r.Review != "" || r.Status != compliance.StatusAgree {
		t.Errorf("GX|F agree row marked: %+v", r)
	}
	// A GF|X on an already-ruled xmage_wrong row corroborates it: no
	// reopening (D4: Forge agreement alone never reopens a ruling).
	if r := hand("GF Ruled Card"); r.Review != "" || r.Status != compliance.StatusXMageWrong || r.RulingID != "xmage-test-auto" {
		t.Errorf("corroborated row marked: %+v", r)
	}
	// The F-harness row (agree, no contradiction) is untouched.
	if r := hand("Forge Miss Card"); r.Review != "" {
		t.Errorf("unrelated row marked: %+v", r)
	}

	// D1=B: the same run marks nothing.
	f2 := newAdjFixture(t, []adjCase{
		{it: adjItem("fx-h", "FX Hand Card", "cast-resolve"), forge: fx21, xmage: x21, gf: gfDiverge()},
	})
	f2.adjCommitted(t)
	before := verdictBytes(t, f2.verdicts)
	if err := runAdjudicate(f2.args("-d1", "B", "-ledger", "compliance/adjudication", "-mark-review")); err != nil {
		t.Fatal(err)
	}
	if string(before) != string(verdictBytes(t, f2.verdicts)) {
		t.Errorf("D1=B -mark-review changed the verdict directory")
	}
	if _, err := os.Stat("compliance/adjudication"); !os.IsNotExist(err) {
		t.Fatalf("D1=B wrote into the repository: %v", err)
	}
}
