package botobs

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/view"
)

// designDocFacts are the six facts the design doc's §6 names by hand. They are
// a floor, not the whole checklist: the ticket's "Done means" requires at
// least these, so the ratchet asserts they are present by id rather than
// leaving the checklist free to shrink to whatever probes happen to exist.
var designDocFacts = []string{
	"own_library_list",
	"own_deck_archetype_prior",
	"opp_archetype_posterior",
	"own_curve",
	"per_zone_counts",
	"decision_routing_arms",
}

// kindConsts maps every decision.Kind string value to the Go constant name the
// routing probe greps for. It must cover decision.Kinds exactly: a new kind
// added there without a row here fails probeDecisionRoutingArms loudly (the
// table is the mapping, and the probe checks its completeness), which is the
// same fail-closed behaviour a new unhandled kind deserves.
var kindConsts = map[decision.Kind]string{
	decision.KPriority:        "KPriority",
	decision.KTarget:          "KTarget",
	decision.KAttackers:       "KAttackers",
	decision.KBlockers:        "KBlockers",
	decision.KMulligan:        "KMulligan",
	decision.KModes:           "KModes",
	decision.KTriggerOrder:    "KTriggerOrder",
	decision.KTriggerOptional: "KTriggerOptional",
	decision.KCommanderZone:   "KCommanderZone",
	decision.KChoose:          "KChoose",
	decision.KReplacement:     "KReplacement",
	decision.KArrange:         "KArrange",
	decision.KStartingPlayer:  "KStartingPlayer",
}

// probes is the measurement table. Each entry returns whether a bot's
// observation path CARRIES the fact today -- derived from the real types
// (view.PlayerView, view.View, deck.Manifest) or, for the router, from the
// policy source -- never from the checklist itself. A probe and a checklist
// entry must match one-to-one: an unprobed fact fails as loudly as a probed
// fact with no entry, so a newly exposed carrier cannot be added silently.
var probes = map[string]func(repoRoot string) bool{
	"own_library_list":         probeOwnLibraryList,
	"own_deck_archetype_prior": probeOwnDeckArchetypePrior,
	"opp_archetype_posterior":  probeOppArchetypePosterior,
	"own_curve":                probeOwnCurve,
	"per_zone_counts":          probePerZoneCounts,
	"decision_routing_arms":    probeDecisionRoutingArms,
	"own_hand_contents":        probeOwnHandContents,
	"own_decklist":             probeOwnDecklist,
	"public_life_totals":       probePublicLifeTotals,
	"mana_pool":                probeManaPool,
}

func probeOwnLibraryList(_ string) bool {
	// A full library as an unordered list is a slice of CardView on the view.
	// LibrarySize (an int count) and LibraryTop (one *CardView) are not it.
	return hasCardSliceField(reflect.TypeOf(view.View{}), "Library") ||
		hasCardSliceField(reflect.TypeOf(view.PlayerView{}), "Library")
}

func probeOwnDeckArchetypePrior(_ string) bool {
	// The prior rides the manifest the view already projects as OwnDeck, so
	// the carrier is a string Archetype field on deck.Manifest.
	return hasFieldName(reflect.TypeOf(deck.Manifest{}), "Archetype", func(f reflect.StructField) bool {
		return f.Type.Kind() == reflect.String
	})
}

func probeOppArchetypePosterior(_ string) bool {
	// A posterior is per-opponent, so it must be per-seat: a field containing
	// "Archetype" on the per-seat projection.
	return hasFieldName(reflect.TypeOf(view.PlayerView{}), "Archetype", func(reflect.StructField) bool { return true })
}

func probeOwnCurve(_ string) bool {
	// The curve is a property of the decklist the bot already holds; a carrier
	// would be a Curve field on the manifest (or the view).
	return hasFieldName(reflect.TypeOf(deck.Manifest{}), "Curve", func(reflect.StructField) bool { return true }) ||
		hasFieldName(reflect.TypeOf(view.View{}), "Curve", func(reflect.StructField) bool { return true })
}

func probePerZoneCounts(_ string) bool {
	// Counts the view already carries for every seat. All three must be
	// present for the fact to hold, so removing any one regresses it.
	t := reflect.TypeOf(view.PlayerView{})
	return hasIntField(t, "LibrarySize") && hasIntField(t, "HandSize") && hasIntField(t, "GraveyardSize")
}

func probeDecisionRoutingArms(repoRoot string) bool {
	// The router must name every kind decision.Kinds lists, at a routing site
	// (`case decision.<K>` or `d.Kind == decision.<K>`) in botpolicy's
	// non-test source. A kind absent from kindConsts is a new kind the probe
	// cannot look for: fail closed.
	src, err := policySources(repoRoot)
	if err != nil {
		// A missing source tree must fail, not silently pass.
		panic(err)
	}
	for _, k := range decision.Kinds {
		name, ok := kindConsts[k]
		if !ok {
			return false
		}
		re := regexp.MustCompile(`(case decision\.` + regexp.QuoteMeta(name) + `\b|d\.Kind\s*==\s*decision\.` + regexp.QuoteMeta(name) + `\b)`)
		if !re.Match(src) {
			return false
		}
	}
	return true
}

func probeOwnHandContents(_ string) bool {
	return hasCardSliceField(reflect.TypeOf(view.PlayerView{}), "Hand")
}

func probeOwnDecklist(_ string) bool {
	// OwnDeck is the genesis manifest, projected for the viewer's own seat.
	f, ok := reflect.TypeOf(view.View{}).FieldByName("OwnDeck")
	return ok && f.Type == reflect.TypeOf((*deck.Manifest)(nil))
}

func probePublicLifeTotals(_ string) bool {
	return hasFieldName(reflect.TypeOf(view.PlayerView{}), "Life", func(f reflect.StructField) bool {
		return f.Type.Kind() == reflect.Int32
	})
}

func probeManaPool(_ string) bool {
	return hasFieldName(reflect.TypeOf(view.PlayerView{}), "Pool", func(f reflect.StructField) bool {
		return f.Type == reflect.TypeOf(map[string]int32{})
	})
}

// ---- reflection helpers -------------------------------------------------

func hasIntField(t reflect.Type, name string) bool {
	f, ok := t.FieldByName(name)
	return ok && f.Type.Kind() == reflect.Int
}

// hasFieldName reports whether t has a field whose name contains substr and
// satisfies pred.
func hasFieldName(t reflect.Type, substr string, pred func(reflect.StructField) bool) bool {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if len(f.Name) >= len(substr) && contains(f.Name, substr) && pred(f) {
			return true
		}
	}
	return false
}

// hasCardSliceField reports whether t has a field whose name contains substr
// and is a []view.CardView.
func hasCardSliceField(t reflect.Type, substr string) bool {
	cardSlice := reflect.TypeOf([]view.CardView{})
	return hasFieldName(t, substr, func(f reflect.StructField) bool { return f.Type == cardSlice })
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---- source access ------------------------------------------------------

// repoRootFromTest locates the repo root relative to this package's
// directory (internal/botobs), which is the working directory `go test`
// runs the binary in, so the routing probe reads the source the test binary
// was built from. runtime.Caller is not used: under -trimpath it returns a
// module-relative path that does not exist on disk.
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("cannot locate the repo root for the routing probe: %v", err)
	}
	return root
}

// policySources concatenates botpolicy's non-test .go sources.
func policySources(repoRoot string) ([]byte, error) {
	dir := filepath.Join(repoRoot, "botpolicy")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || filepath.Ext(n) != ".go" || len(n) > 8 && n[len(n)-8:] == "_test.go" {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names) // deterministic concatenation order
	var out []byte
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
		out = append(out, '\n')
	}
	return out, nil
}

// ---- the ratchet --------------------------------------------------------

// TestDesignDocFactsArePresent pins the §6 floor: the checklist cannot shrink
// below the six facts the design doc names.
func TestDesignDocFactsArePresent(t *testing.T) {
	c, err := Load("checklist.json")
	if err != nil {
		t.Fatalf("load checklist: %v", err)
	}
	have := make(map[string]bool, len(c.Facts))
	for _, f := range c.Facts {
		have[f.ID] = true
	}
	for _, id := range designDocFacts {
		if !have[id] {
			t.Errorf("design doc §6 fact %q is missing from checklist.json", id)
		}
	}
}

// TestChecklistMatchesMeasuredExposure is the ratchet: it measures each fact
// from the observation path and fails if a recorded flag disagrees in either
// direction. A recorded true that regresses to false fails; a recorded false
// that becomes exposed (and is therefore unrecorded) fails too. It also
// asserts every checklist fact has a probe and every probe has a fact, so a
// newly exposed carrier cannot be added without a checklist row, which is the
// "newly exposed fact that is not recorded fails" half of the contract.
func TestChecklistMatchesMeasuredExposure(t *testing.T) {
	c, err := Load("checklist.json")
	if err != nil {
		t.Fatalf("load checklist: %v", err)
	}
	// A vacuous checklist (no facts) must fail loudly, not pass an empty loop.
	if len(c.Facts) == 0 {
		t.Fatal("checklist.json carries no facts; the obs axis would have a zero denominator")
	}
	root := repoRootFromTest(t)

	recorded := make(map[string]Fact, len(c.Facts))
	var dupes []string
	for _, f := range c.Facts {
		if f.ID == "" {
			t.Errorf("a checklist fact has an empty id: %#v", f)
			continue
		}
		if _, ok := recorded[f.ID]; ok {
			dupes = append(dupes, f.ID)
		}
		recorded[f.ID] = f
	}
	if len(dupes) > 0 {
		t.Errorf("checklist.json repeats fact id(s) %v; each id must appear once", dupes)
	}

	// Every recorded fact must be probed; every probe must be recorded.
	for id := range recorded {
		if _, ok := probes[id]; !ok {
			t.Errorf("recorded fact %q has no measurement probe; add one so its exposure is a ratchet, not a claim", id)
		}
	}
	for id := range probes {
		if _, ok := recorded[id]; !ok {
			t.Errorf("probe %q has no checklist entry; a newly exposed fact must be recorded", id)
		}
	}

	var recordedExposed, measuredExposed int
	for _, id := range sortedKeys(recorded) {
		f := recorded[id]
		probe, ok := probes[id]
		if !ok {
			continue
		}
		measured := probe(root)
		if f.Exposed != measured {
			t.Errorf("fact %q: recorded exposed=%v but measured exposed=%v (a false->true is a newly exposed fact not recorded; a true->false is a regression)",
				id, f.Exposed, measured)
		}
		if f.Exposed {
			recordedExposed++
		}
		if measured {
			measuredExposed++
		}
	}
	if recordedExposed != measuredExposed {
		t.Errorf("exposed count: recorded %d, measured %d; the checklist and the observation path disagree", recordedExposed, measuredExposed)
	}
	// Cross-check the number scripts/reward_collect.py will sum: it counts
	// f.get("exposed") truthiness over the facts list, which is recordedExposed.
	if recordedExposed == 0 {
		t.Errorf("no fact is recorded exposed; the obs axis would be 0/%d and the ratchet proves nothing", len(c.Facts))
	}
}

// TestRoutingProbeCoversEveryKind guards the probe's own mapping: if
// decision.Kinds grows, kindConsts is stale and the routing probe would look
// for nothing. Failing here names the new kind instead of letting the probe
// silently miss it.
func TestRoutingProbeCoversEveryKind(t *testing.T) {
	for _, k := range decision.Kinds {
		if _, ok := kindConsts[k]; !ok {
			t.Errorf("decision.Kinds carries %q with no kindConsts row; add the constant name so the routing probe checks it", k)
		}
	}
	if len(kindConsts) != len(decision.Kinds) {
		t.Errorf("kindConsts has %d rows but decision.Kinds has %d; keep them in step", len(kindConsts), len(decision.Kinds))
	}
	// The probe must also be able to read the source tree it greps.
	if _, err := policySources(repoRootFromTest(t)); err != nil {
		t.Fatalf("cannot read botpolicy sources: %v", err)
	}
}

// TestRoutingProbeReflectsSource proves the routing probe reads the source
// rather than always answering true: against a copy of botpolicy with the one
// KArrange routing site deleted, the probe must flip to false. Without this a
// probe that ignored its input would let the ratchet pass for every kind.
func TestRoutingProbeReflectsSource(t *testing.T) {
	src, err := policySources(repoRootFromTest(t))
	if err != nil {
		t.Fatalf("read botpolicy sources: %v", err)
	}
	if !regexp.MustCompile(`decision\.KArrange\b`).Match(src) {
		t.Fatal("precondition: the real botpolicy source has no KArrange routing site, so this test cannot observe a removal")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "botpolicy")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// One file is enough: the probe reads the concatenation.
	stripped := regexp.MustCompile(`(?m)^.*decision\.KArrange.*$`).ReplaceAll(src, []byte("// removed by TestRoutingProbeReflectsSource"))
	if err := os.WriteFile(filepath.Join(sub, "policy.go"), stripped, 0o644); err != nil {
		t.Fatal(err)
	}
	if probeDecisionRoutingArms(dir) {
		t.Error("routing probe returned true with the KArrange site removed; it is not reading the source")
	}
}

func sortedKeys(m map[string]Fact) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Compile-time pin: the facts the probes walk are the real observation-path
// types, so renaming or moving one is an error here rather than a silent
// probe returning false.
var (
	_ = reflect.TypeOf(view.View{})
	_ = reflect.TypeOf(view.PlayerView{})
	_ = reflect.TypeOf(deck.Manifest{})
	_ = botpolicy.Board{}
)
