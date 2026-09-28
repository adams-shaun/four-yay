package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func modeOptionContaining(t *testing.T, d *decision.Decision, text string) int {
	t.Helper()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("next decision = %+v, want modes", d)
	}
	for _, opt := range d.Options {
		if strings.Contains(opt.Label, text) {
			return opt.Index
		}
	}
	t.Fatalf("mode containing %q absent from options %+v", text, d.Options)
	return 0
}

// CR 733.1 returns an aborted proposal to the state before it began. Azorius
// Charm really reaches the CR 601.2b mode ask, then its mana disappears before
// that answer is submitted; the failed payment must reverse both the stack move
// and the mode cache maintained beside ModeChosen.
func TestAbortedModalCastRestoresItsUnchosenState(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "ur-delver", "Azorius Charm")
	id := crAbortMove(t, e, 0, "Azorius Charm", state.ZHand)
	beforeModes := append([]string(nil), e.G.Obj(id).ChosenModes...)
	for _, color := range []string{"W", "U"} {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: color, Amount: 1})
	}
	e.askPriority(0)
	start := len(e.L.Events)
	crAbortAnswer(t, e, "Azorius Charm", crAbortOption(t, e, "Azorius Charm", "cast", id))

	d := e.Pending()
	draw := modeOptionContaining(t, d, "Draw a card")
	// This defensive-path mutation models the resources changing while the
	// proposal is open. The cast was legal when offered, but cannot complete.
	e.emit(events.Event{Kind: events.ManaClear, Player: 0})
	crAbortAnswer(t, e, "Azorius Charm", draw)

	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("aborted Azorius Charm zone = %s, want hand", got)
	}
	if got := e.G.Obj(id).ChosenModes; !slices.Equal(got, beforeModes) {
		t.Fatalf("aborted Azorius Charm retained announced modes %v, want pre-proposal %v", got, beforeModes)
	}
	modeEvents := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.ModeChosen && ev.Obj == id {
			modeEvents++
		}
	}
	if modeEvents != 2 {
		t.Fatalf("ModeChosen events after aborted proposal = %d, want forward announcement and reverse marker", modeEvents)
	}
}

// A mode with an unsatisfied mandatory target requirement is not a legal
// announcement. Warping Wail on an empty battlefield must therefore omit its
// creature and sorcery modes and offer the untargeted token mode, rather than
// let a bot repeatedly propose and reverse an impossible cast.
func TestModalCastOmitsModesWithNoLegalTargets(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "eldrazi-stompy")
	id := crAbortMove(t, e, 0, "Warping Wail", state.ZHand)
	for range 2 {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	}
	e.askPriority(0)
	crAbortAnswer(t, e, "Warping Wail", crAbortOption(t, e, "Warping Wail", "cast", id))

	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || len(d.Options) != 1 ||
		!strings.Contains(d.Options[0].Label, "Eldrazi Scion") {
		t.Fatalf("Warping Wail modes on an empty board = %+v, want only its untargeted token mode", d)
	}
	crAbortAnswer(t, e, "Warping Wail", 0)
	if got := e.G.Obj(id).Zone; got != state.ZStack {
		t.Fatalf("Warping Wail zone after legal token-mode announcement = %s, want stack", got)
	}
}

// Forge stores a Charm mode's ValidTgts$ on the selected SVar, not the outer
// Charm SA. Choosing Boros Charm's player-damage mode must therefore offer
// players, while choosing its creature mode must offer the creature and no
// player. This pins that CR 601.2c reads the already-announced mode.
func TestModalCastTargetsComeFromTheAnnouncedMode(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name       string
		modeText   string
		wantPlayer bool
	}{
		{name: "damage", modeText: "deals 4 damage", wantPlayer: true},
		{name: "double_strike", modeText: "double strike", wantPlayer: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := crAbortEngine(t, reg, "ur-delver", "Boros Charm")
			id := crAbortMove(t, e, 0, "Boros Charm", state.ZHand)
			creature := crAbortMove(t, e, 1, "Thalia, Guardian of Thraben", state.ZBattlefield)
			// Thalia supplies the creature target and taxes the noncreature spell.
			for _, color := range []string{"W", "R", "C"} {
				e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: color, Amount: 1})
			}
			e.askPriority(0)
			crAbortAnswer(t, e, "Boros Charm", crAbortOption(t, e, "Boros Charm", "cast", id))
			crAbortAnswer(t, e, "Boros Charm", modeOptionContaining(t, e.Pending(), tc.modeText))

			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("after announcing %s mode, next decision = %+v, want target", tc.name, d)
			}
			hasPlayer, hasCreature := false, false
			for _, opt := range d.Options {
				hasPlayer = hasPlayer || opt.Kind == "player"
				hasCreature = hasCreature || opt.Obj == creature
			}
			if hasPlayer != tc.wantPlayer || hasCreature == tc.wantPlayer {
				t.Fatalf("%s target options: hasPlayer=%t hasThalia=%t, want player=%t creature=%t; options=%+v",
					tc.name, hasPlayer, hasCreature, tc.wantPlayer, !tc.wantPlayer, d.Options)
			}
		})
	}
}
