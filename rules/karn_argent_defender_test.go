package rules

// Karn, Argent Defender: "Artifacts and creatures entering the battlefield
// don't cause abilities to trigger." (S:Mode$ DisableTriggers, enforced by
// rules/disable_triggers.go.) Driven with real corpus cards: a creature's own
// ETB, another permanent's "whenever a creature enters" over a creature card
// and over creature TOKENS, and -- the live control -- a land, which Karn
// does not name, still causing landfall.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestKarnArgentDefenderSuppressesArtifactAndCreatureEntryTriggers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{
		lookup(t, reg, "Karn, Argent Defender"),
		lookup(t, reg, "Avatar of Burgeoning Echoes"),
		lookup(t, reg, "Soul Warden"),
		lookup(t, reg, "Arcane Amphisbaena"),
		lookup(t, reg, "Raise the Alarm"),
		lookup(t, reg, "Forest"),
	}, nil)
	moveByName(t, e, 0, "Karn, Argent Defender", state.ZBattlefield)
	moveByName(t, e, 0, "Avatar of Burgeoning Echoes", state.ZBattlefield)
	moveByName(t, e, 0, "Soul Warden", state.ZBattlefield)
	moveByName(t, e, 0, "Arcane Amphisbaena", state.ZHand)
	moveByName(t, e, 0, "Raise the Alarm", state.ZHand)
	moveByName(t, e, 0, "Forest", state.ZHand)
	e.priorityRound()
	life := e.G.Players[0].Life

	// A creature card entering: neither its own ETB (empower Jace 2) nor Soul
	// Warden's "whenever another creature enters" triggers.
	castAmphisbaenaFromHand(t, e)
	if o := e.G.Obj(findByName(e, "Arcane Amphisbaena", 0)); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Arcane Amphisbaena did not enter")
	}
	if toks := jaceTokens(e, 0); len(toks) != 0 {
		t.Fatalf("Arcane Amphisbaena's ETB empowered under Karn: Jace tokens %v", toks)
	}
	if got := e.G.Players[0].Life; got != life {
		t.Fatalf("Soul Warden triggered on a creature entering under Karn: life %d -> %d", life, got)
	}

	// Creature tokens entering are creatures entering too.
	addMana(t, e, 0, "WW")
	castNamed(t, e, "Raise the Alarm")
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Life; got != life {
		t.Fatalf("Soul Warden triggered on creature tokens entering under Karn: life %d -> %d", life, got)
	}

	// Live control: a land is neither an artifact nor a creature, so Avatar's
	// landfall still empowers.
	d := e.Pending()
	played := false
	for _, o := range d.Options {
		if o.Kind == "play_land" && e.G.Obj(o.Obj) != nil && e.G.Obj(o.Obj).Face().Name == "Forest" {
			submitChoices(t, e, o.Index)
			played = true
			break
		}
	}
	if !played {
		t.Fatalf("no play-land option for Forest: %+v", d.Options)
	}
	passUntilStackEmpty(t, e, 40)
	toks := jaceTokens(e, 0)
	if len(toks) != 1 || e.G.Obj(toks[0]).Counter("LOYALTY") != 2 {
		t.Fatalf("landfall under Karn did not empower Jace 2: Jace tokens %v", toks)
	}
	replayCheck(t, e, cfg)
}

// castAmphisbaenaFromHand casts the Arcane Amphisbaena already in seat 0's
// hand and drains the stack.
func castAmphisbaenaFromHand(t *testing.T, e *Engine) {
	t.Helper()
	addMana(t, e, 0, "GG")
	castNamed(t, e, "Arcane Amphisbaena")
	passUntilStackEmpty(t, e, 40)
}
