package rules

// Corpus-carrier census for the three oracle fix mechanisms
// (cli-20261005T035645Z-da38b1fc). Each repair was structural -- the
// triggered-cost LKI capture serves every `Sacrificed$...` reader, the
// cost-time Remembered binding serves every `TriggeredCard$...` read inside a
// trigger Cost$, and the graveyard player scan serves every zone-qualified
// Valid graveyard selector -- so the affected class is wider than the one
// card the hand Oracle scenario named. This census records that class so a
// corpus-pin bump that adds or removes a carrier is named rather than
// silently joining an untested set.
//
// It is a data census in the established per-card ratchet shape
// (rules/acceptance_test.go's knownUnsupported, rules/paramcensus_test.go):
// one file per card under rules/testdata/<table>/, loaded with the shared
// loadPerCardLabels, and compared in BOTH directions -- a new carrier is a
// regression to classify, a stale entry is a card the corpus no longer
// carries and must be deleted.
//
// Deliberately NOT included: a label per SVar name or consuming API. The
// three tables each hold the one constant mechanism label because the walk's
// question is which CARDS depend on the fixed path, and the path is the whole
// label.

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// Oracle-fix census labels. Each names the mechanism, not a card.
const (
	censusLabelSacrificedPower   = "count:Sacrificed$CardPower"
	censusLabelTurnEntered       = "filter:ThisTurnEnteredFrom_Battlefield"
	censusLabelTriggeredCardMana = "trigger-cost:TriggeredCard$CastTotalManaSpent"
)

// oracleFixCarriers walks the compiled corpus and returns, per census label,
// the set of card names carrying that label. It is derived, never
// hand-listed, so a new carrier card is measured rather than missed.
func oracleFixCarriers(reg *cards.Registry) map[string]map[string]bool {
	out := map[string]map[string]bool{
		censusLabelSacrificedPower:   {},
		censusLabelTurnEntered:       {},
		censusLabelTriggeredCardMana: {},
	}
	add := func(label, card string) {
		if out[label] == nil {
			out[label] = map[string]bool{}
		}
		out[label][card] = true
	}
	faceHasHead := func(f *cards.Face, head string) bool {
		for _, body := range f.SVars {
			if strings.Contains(body, head) {
				return true
			}
		}
		return false
	}
	// chainHasCostX reports whether any SA in the chain carries a Cost$ that
	// names X -- the shape whose X the trigger-cost window evaluates while the
	// referent must be bound.
	chainHasCostX := func(sa *cards.SA) bool {
		for ; sa != nil; sa = sa.Sub {
			if strings.Contains(sa.Params["Cost"], "X") {
				return true
			}
		}
		return false
	}
	// chainHasTurnEntered reports whether any SA in the chain carries a
	// parameter naming the turn-entered-from-battlefield predicate.
	chainHasTurnEntered := func(sa *cards.SA) bool {
		for ; sa != nil; sa = sa.Sub {
			for _, v := range sa.Params {
				if strings.Contains(v, "ThisTurnEnteredFrom_Battlefield") {
					return true
				}
			}
		}
		return false
	}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			if faceHasHead(f, "Sacrificed$CardPower") {
				add(censusLabelSacrificedPower, f.Name)
			}
			if faceHasHead(f, "TriggeredCard$CastTotalManaSpent") {
				for _, tr := range f.Triggers {
					if chainHasCostX(tr.Effect) {
						add(censusLabelTriggeredCardMana, f.Name)
					}
				}
				for _, sa := range f.Abilities {
					if chainHasCostX(sa) {
						add(censusLabelTriggeredCardMana, f.Name)
					}
				}
			}
			for _, body := range f.SVars {
				if strings.Contains(body, "ThisTurnEnteredFrom_Battlefield") {
					add(censusLabelTurnEntered, f.Name)
					break
				}
			}
			for _, tr := range f.Triggers {
				if chainHasTurnEntered(tr.Effect) {
					add(censusLabelTurnEntered, f.Name)
				}
			}
			for _, sa := range f.Abilities {
				if chainHasTurnEntered(sa) {
					add(censusLabelTurnEntered, f.Name)
				}
			}
		}
	}
	return out
}

// loadCensusLabels loads one census directory with the shared per-card
// loader.
func loadCensusLabels(t *testing.T, dir string) map[string][]string {
	t.Helper()
	return loadPerCardLabels(t, filepath.Join("testdata", dir))
}

// checkCensus compares the measured carrier set for one label against the
// per-card table in both directions, with a loud precondition that the walk
// actually reached the class.
func checkCensus(t *testing.T, label, dir, mustCarry string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	got := oracleFixCarriers(reg)[label]
	want := loadCensusLabels(t, dir)

	// Precondition: the walk reached the class and the card the fix was
	// reported for is a member. A registry that failed to load, or a walk
	// whose chaining stopped resolving, must fail rather than pass vacuously.
	if len(got) == 0 {
		t.Fatalf("%s: census found no carriers at all; the corpus walk is broken", label)
	}
	if mustCarry != "" && !got[mustCarry] {
		t.Fatalf("%s: precondition: %q is not a measured carrier, got %v", label, mustCarry, sortedCensusNames(got))
	}

	var gotNames, wantNames []string
	for n := range got {
		gotNames = append(gotNames, n)
	}
	for n := range want {
		wantNames = append(wantNames, n)
	}
	sort.Strings(gotNames)
	sort.Strings(wantNames)
	if strings.Join(gotNames, "; ") != strings.Join(wantNames, "; ") {
		t.Fatalf("%s carrier cards changed:\n got: %v\nwant: %v", label, gotNames, wantNames)
	}
	// Every table file must name exactly this label (a file carrying the
	// wrong label is a copy-paste defect the set comparison cannot see).
	for name, labels := range want {
		if len(labels) != 1 || labels[0] != label {
			t.Errorf("%s: %s labels = %v, want [%s]", label, name, labels, label)
		}
	}
}

func sortedCensusNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestCorpusCensusFilesWellFormed holds the three census directories to the
// per-card schema without a corpus.
func TestCorpusCensusFilesWellFormed(t *testing.T) {
	t.Parallel()
	loadCensusLabels(t, "corpus-sacrificed-power")
	loadCensusLabels(t, "corpus-turn-entered-filter")
	loadCensusLabels(t, "corpus-triggered-card-mana")
}

// TestCorpusSacrificedPowerCensus is the ratchet for the count head whose
// triggered-cost read the LKI capture feeds.
func TestCorpusSacrificedPowerCensus(t *testing.T) {
	checkCensus(t, censusLabelSacrificedPower, "corpus-sacrificed-power", "Rhovanion Rampager")
}

// TestCorpusTurnEnteredCensus is the ratchet for the turn-entered-from-
// battlefield predicate the graveyard player scan serves.
func TestCorpusTurnEnteredCensus(t *testing.T) {
	checkCensus(t, censusLabelTurnEntered, "corpus-turn-entered-filter", "Supper for Spiders")
}

// TestCorpusTriggeredCardManaCensus is the ratchet for the cost-time
// TriggeredCard referent binding.
func TestCorpusTriggeredCardManaCensus(t *testing.T) {
	checkCensus(t, censusLabelTriggeredCardMana, "corpus-triggered-card-mana", "Uncover the Moon-Letters")
}
