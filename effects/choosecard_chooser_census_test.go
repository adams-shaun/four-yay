package effects

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestChooseCardDefinedChoosersResolve is the class census for a ChooseCard
// whose Defined$ names the chooser (Struggle for Sanity's "that player
// exiles a card": Defined$ Player.IsRemembered). Every distinct Defined$
// spelling a corpus ChooseCard carries must be a selector the shared
// resolver models (knownDefinedTargets ok), so no chooser silently falls
// through to nobody; and the remembered-player spellings must name the
// remembered PLAYER -- not the caster, and not a remembered card's
// controller -- when the resolution remembers both a player and cards (the
// Struggle shape: RememberTargets$ + RememberRevealed$).
func TestChooseCardDefinedChoosersResolve(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	specs := map[string]int{}
	carriers := 0
	// One node per line: a top-level ability itself, and every SVar's own
	// head (sub-abilities are SVars, so walking Sub would count them twice).
	visit := func(sa *cards.SA) {
		if sa == nil || sa.API != "ChooseCard" {
			return
		}
		if spec := strings.TrimSpace(sa.ParamStr(cards.PKDefined)); spec != "" {
			specs[spec]++
			carriers++
		}
	}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			for _, sa := range f.Abilities {
				visit(sa)
			}
			names := make([]string, 0, len(f.SVars))
			for name := range f.SVars {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if strings.Contains(f.SVars[name], "ChooseCard") {
					visit(cards.ResolveSVar(f.SVars, name))
				}
			}
		}
	}
	if carriers < 150 {
		t.Fatalf("census found %d ChooseCard Defined$ carriers: the scan is not reading the corpus", carriers)
	}

	h, c := fixtureHost(t)
	// The Struggle shape: the resolution remembers the targeted opponent AND
	// the revealed cards of their hand (a card owned by seat 1).
	revealed := h.g.AddObject(mkCard(t, "Name:Revealed\nTypes:Instant\nOracle:x\n"), 1)
	c.Remembered = []state.Target{{Player: 1, IsPlayer: true}, {Obj: revealed.ID}}
	c.Targets = []state.Target{{Player: 1, IsPlayer: true}}
	var bad []string
	keys := make([]string, 0, len(specs))
	for spec := range specs {
		keys = append(keys, spec)
	}
	sort.Strings(keys)
	for _, spec := range keys {
		if _, ok := knownDefinedTargets(h, c, spec); !ok {
			bad = append(bad, spec)
		}
	}
	for _, spec := range bad {
		t.Errorf("ChooseCard Defined$ %q (%d carriers) is not a modelled selector: its chooser falls through", spec, specs[spec])
	}
	for _, spec := range []string{"Player.IsRemembered", "Remembered", "RememberedPlayer"} {
		sa := &cards.SA{API: "ChooseCard", Params: map[string]string{"Defined": spec}}
		got := chooseCardChoosers(h, c, sa)
		if len(got) != 1 || got[0] != 1 {
			t.Errorf("ChooseCard Defined$ %s asks %v, want exactly the remembered player [1]", spec, got)
		}
	}
	t.Logf("%d ChooseCard Defined$ carriers, %d distinct selectors", carriers, len(specs))
}
