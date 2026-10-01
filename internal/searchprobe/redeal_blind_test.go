package searchprobe

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Every skipped redeal probe in this test binary is still run and checked
// (redeal_blind.go): a hidden-blind board whose world changed the
// observation panics.
func init() { redealProbeVerify = true }

// faceBlind is the card-text half of observationBlind: a face whose
// offer-time text can read a hidden zone is refused, one whose hidden-zone
// words are resolution payload is admitted.
func TestRedealFaceBlind(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name  string
		blind bool
		why   string
	}{
		{"Grizzly Bears", true, "vanilla"},
		{"Evolving Wilds", true, "an untargeted library search is resolution payload"},
		{"Bushwhack", true, "a charm mode's library search is resolution payload"},
		{"Squad Rallier", true, "a dig's destinations are resolution payload"},
		{"Quilled Greatwurm", true, "a graveyard MayPlay names its public zone"},
		{"Zul Ashur, Lich Lord", true, "an Effect's installed MayPlay static exists only once it resolves"},
		{"Abyssal Harvester", true, "a graveyard target's own ThisTurnEntered is public"},
		{"Uncharted Voyage", true, "library positions are resolution payload"},
		{"Leyline Axe", true, "the opening-hand keyword acts in the pregame only"},
		{"Daze", true, "a static's Description$ is display text"},
		{"Land Grant", false, "an alternative cost that reveals and counts a hand"},
		{"Lion's Eye Diamond", false, "a mana ability paid by discarding a hand"},
		{"Oracle of Mul Daya", false, "MayLookAt/MayPlay at the library top"},
		{"Courser of Kruphix", false, "MayLookAt/MayPlay at the library top"},
		{"Future Sight", false, "MayLookAt/MayPlay at the library top"},
		{"Elvish Spirit Guide", false, "a mana ability exiled from a hand"},
		{"Maro", false, "a CDA counting a hand"},
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s is not in the corpus", tc.name)
		}
		got := true
		for _, f := range c.Faces {
			if f != nil && !faceBlindScan(f) {
				got = false
			}
		}
		if got != tc.blind {
			t.Errorf("%s: faceBlind %v, want %v (%s)", tc.name, got, tc.blind, tc.why)
		}
	}
}

// A registered may-play or look grant, or a hidden word in any registered
// effect's values, refuses the skip.
func TestRedealContinuousBlind(t *testing.T) {
	var plain state.ContinuousEffect
	plain.Affects, plain.AddPower = "Creature.YouCtrl", 1
	if !continuousBlind(&plain) {
		t.Fatal("a plain pump is not blind")
	}
	for name, ce := range map[string]state.ContinuousEffect{
		"MayPlay":   {MayPlay: true, AffectedZone: "Exile"},
		"MayLookAt": {MayLookAt: true},
		"hand zone": {AffectedZone: "Hand"},
		"svar":      {Affects: "Creature", AddPowerExpr: "X", SVars: map[string]string{"X": "Count$InYourHand"}},
	} {
		if continuousBlind(&ce) {
			t.Errorf("%s: blind", name)
		}
	}
}

// The bench fixture's boundary is hidden-blind, so every deal skips its
// probe -- and under this binary's verify mode each skipped probe still runs
// and must agree.
func TestRedealSkipsTheProbeOnABlindBoard(t *testing.T) {
	f := benchRoot(t)
	h := f.h
	known, err := ProjectKnownCards(h)
	if err != nil {
		t.Fatal(err)
	}
	r, why := NewRedealer(f.setup, h, known, RedealBase{Engine: f.engine, Observer: f.collector})
	if why != "" {
		t.Fatal(why)
	}
	if !r.blind {
		t.Fatal("the bench fixture's boundary is not hidden-blind: the skip (and its verify) would never run")
	}
	for i := 0; i < 64; i++ {
		w, reason := r.Deal([2]uint64{uint64(i) + 1, uint64(i) * 7}, nil)
		if reason != "" {
			t.Fatalf("deal %d: %s", i, reason)
		}
		w.Release()
	}
}
