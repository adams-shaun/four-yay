package rules

// The comma-delimited WithCountersType$ list, end to end. Forge spells a
// multi-kind entry-counter list as one comma-delimited parameter
// (`WithCountersType$ Hexproof,Indestructible`, Perennation) or as one entry
// in a named SVar sub-ability (`SVar:TrigReturn:DB$ ChangeZone | ...
// WithCountersType$ Vigilance,Lifelink`, Gilraen). The move must place EACH
// kind as its own counter event -- the pre-fix emission wrote one
// `HEXPROOF,INDESTRUCTIBLE` counter that no counter-kind lookup can ever
// read.
//
// This exercises the real card resolution (cast, target, resolve) rather
// than the shared emission helper alone, so a route that skipped the helper
// would fail here.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const perennationSrc = "Name:Perennation\nManaCost:3 W B G\nTypes:Sorcery\n" +
	"A:SP$ ChangeZone | Origin$ Graveyard | Destination$ Battlefield | TgtPrompt$ Choose target permanent card in your graveyard | ValidTgts$ Permanent.YouOwn | WithCountersType$ Hexproof,Indestructible | SpellDescription$ Return target permanent card from your graveyard to the battlefield with a hexproof counter and an indestructible counter on it.\n" +
	"Oracle:x\n"

// TestPerennationReturnPlacesEachCounterKind casts Perennation targeting a
// creature card in its controller's graveyard and asserts the returned
// permanent carries BOTH a Hexproof and an Indestructible counter -- the two
// distinct kinds the comma list names.
func TestPerennationReturnPlacesEachCounterKind(t *testing.T) {
	t.Parallel()
	bearSrc := "Name:Counter Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, find := etbConfig(t, 118, []string{perennationSrc, bearSrc}, nil)
	per := find("Perennation", 0)
	bear := find("Counter Bear", 0)
	// Put the bear in its owner's graveyard (a real, logged move).
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZGraveyard})
	e.pending = nil
	addMana(t, e, 0, "WWBBGG") // {3}{W}{B}{G}
	submitChoices(t, e, castOptionFor(t, e, per).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision for Perennation: %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no graveyard-bear target option: %+v", d.Options)
	}
	if e.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("precondition: bear zone=%s, want graveyard", e.G.Obj(bear).Zone)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(bear)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("returned bear zone=%s, want battlefield", o.Zone)
	}
	if got := o.Counter("Hexproof"); got != 1 {
		t.Fatalf("Hexproof counters = %d, want 1 (all counters=%v)", got, o.Counters)
	}
	if got := o.Counter("Indestructible"); got != 1 {
		t.Fatalf("Indestructible counters = %d, want 1 (all counters=%v)", got, o.Counters)
	}
	replayCheck(t, e, cfg)
}
