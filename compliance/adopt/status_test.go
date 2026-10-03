package adopt

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/gate"
)

func TestBucket(t *testing.T) {
	for reason, want := range map[string]string{
		"no printed list (compliance/printed/G00.json): ...": BucketNoPrintedList,
		"not in the corpus":                                                  BucketNotInCorpus,
		"unsupported [kw:Bargain]":                                           BucketUnsupported,
		"gorge_wrong verdict (cast-resolve): levels":                         BucketGorgeWrong,
		"XMage does not implement it; needs a hand-authored oracle scenario": BucketXMageLacks,
		"no generated scenario (no fixture gorge can cast) and no hand":      BucketTemplateGap,
		"no verdict for cast-resolve-v1/Shock":                               BucketNoVerdict,
		"verdict diverge (cast-resolve): step 1":                             BucketDiverge,
		"verdict harness (cast-resolve) [ruling r]: xmage: boom":             BucketHarness,
		"automatic ruling r sampled for review; check it":                    BucketReview,
		"verdict is for an older scenario (x); re-run the XMage pass":        BucketStale,
		"level B has no templates yet":                                       BucketLevel,
		"frozen p0.life changed: want 17, have 20 (x)":                       BucketExpectation,
	} {
		if got := Bucket(gate.Problem{Card: "c", Reason: reason}); got != want {
			t.Errorf("Bucket(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestRollupsLabelCompleteFormats(t *testing.T) {
	cs := &Census{Config: &Config{Formats: []Format{{Name: "Standard", Sets: []string{"A", "B"}}, {Name: "All", Tournament: true}}}}
	sets := []SetStatus{
		{Set: "A", Formats: []string{"Standard", "All"}, Level: "A", Declared: "A", Cards: 10},
		{Set: "B", Formats: []string{"Standard", "All"}, Level: "A", Cards: 5, Outstanding: 3, NonTournament: 1,
			Buckets: map[string]int{BucketUnsupported: 3}},
	}
	r := cs.Rollups(sets)
	if r[0].Label != "Standard: 1/2 sets at A, 3 of 15 entries outstanding" || r[0].Complete() {
		t.Errorf("Standard rollup %+v", r[0])
	}
	if r[1].Outstanding != 2 {
		t.Errorf("All leaves out non-tournament problems: %+v", r[1])
	}
	sets[1].Declared, sets[1].Outstanding, sets[1].NonTournament = "A", 0, 0
	if r := cs.Rollups(sets); r[0].Label != "Standard:A" || !r[0].Complete() {
		t.Errorf("all declared: %+v", r[0])
	}
}

func TestCheckRatchet(t *testing.T) {
	sets := []SetStatus{{Set: "FRA", Outstanding: 0}, {Set: "FDN", Outstanding: 7}, {Set: "M10", Outstanding: 190}}
	declared := compliance.Declared{"FRA": "A"}
	ratchet := map[string]RatchetEntry{"FRA": {Level: "A"}, "FDN": {Outstanding: 6}, "M10": {Outstanding: 198}}
	committed := []string{"FDN", "FRA", "M10"}
	fails, slack := CheckRatchet(committed, sets, declared, ratchet)
	if len(fails) != 1 || !strings.Contains(fails[0], "FDN: 7 outstanding, ratchet 6") {
		t.Errorf("fails %v, want FDN's growth only", fails)
	}
	if !reflect.DeepEqual(slack, []string{"M10: 198 -> 190"}) {
		t.Errorf("slack %v", slack)
	}
	// Dropping a declaration, or declaring without recording the floor,
	// both fail; so does a set the ratchet does not know.
	fails, _ = CheckRatchet(append(committed, "NEW"), sets, compliance.Declared{"FDN": "A"},
		map[string]RatchetEntry{"FRA": {Level: "A"}, "FDN": {Outstanding: 7}, "M10": {Outstanding: 190}})
	want := []string{"FRA: was declared at A", "NEW: a committed set missing", "FDN: declared A but the ratchet's floor"}
	if len(fails) != len(want) {
		t.Fatalf("fails %v", fails)
	}
	for i, w := range want {
		if !strings.HasPrefix(fails[i], w) {
			t.Errorf("fail %d = %q, want prefix %q", i, fails[i], w)
		}
	}
	round := MarshalRatchet(ratchet)
	if !strings.Contains(string(round), "\"FDN\": {\"outstanding\":6},\n") {
		t.Errorf("one set per line: %s", round)
	}
}
