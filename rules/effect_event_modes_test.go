package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The DamageDone trigger mode is the Azra Oddsmaker Effect shape. The
// deliberately simple Execute body makes each separate firing observable.
func TestEffectDamageDoneTriggerRepeatsWithinTurn(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:OddsmakerPromise\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigDamage\n"+
		"SVar:TrigDamage:Mode$ DamageDone | ValidTarget$ Player | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	e.G.Players[0].Pool[state.MU] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if src := e.G.Obj(e.G.Zone(state.ZGraveyard, 0)[0]); src == nil || src.Face().Name != "OddsmakerPromise" {
		t.Fatalf("precondition: Effect spell did not reach the graveyard: %+v", src)
	}
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "DamageDone" || !e.G.Delayed[0].EffectRepeat {
		t.Fatalf("precondition: Effect did not arm a repeatable DamageDone trigger: %+v", e.G.Delayed)
	}
	before := e.G.Players[0].Life
	// Direct noncombat damage to an opponent exercises ValidTarget$ Player;
	// the triggered body must lose life for the Effect's controller twice.
	for n := 1; n <= 2; n++ {
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
		e.putTriggersOnStack()
		passUntilStackEmpty(t, e, 8)
		if got := e.G.Players[0].Life; got != before-int32(2*n) {
			t.Fatalf("after %d hits, controller life = %d, want %d", n, got, before-int32(2*n))
		}
	}
}

// An Effect's `Triggers$` SpellCast body is an ordinary REPEATABLE trigger for
// the Effect's lifetime ("whenever you cast a spell this turn, ..."), not a
// one-shot CR 603.7 promise: the registration must survive its own firing and
// fire again for the next matching cast.
func TestEffectSpellCastTriggerRepeatsWithinTurn(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:CastPromise\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigCast\n"+
		"SVar:TrigCast:Mode$ SpellCast | ValidActivatingPlayer$ You | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	filler := card(t, "Name:Filler\nManaCost:U\nTypes:Sorcery\nA:SP$ Note | Text$ filler\nOracle:x\n")
	e := handEngine(t, promise, filler, filler)
	e.G.Players[0].Pool[state.MU] = 3
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "SpellCast" {
		t.Fatalf("precondition: Effect did not arm a SpellCast trigger: %+v", e.G.Delayed)
	}
	if len(e.G.Zone(state.ZHand, 0)) != 2 {
		t.Fatalf("precondition: want two castable fillers in hand, got %d", len(e.G.Zone(state.ZHand, 0)))
	}
	before := e.G.Players[0].Life
	for n := 1; n <= 2; n++ {
		e.askPriority(0)
		castFirst(t, e, "cast")
		passUntilStackEmpty(t, e, 8)
		if got := e.G.Players[0].Life; got != before-int32(2*n) {
			t.Fatalf("after cast %d, controller life = %d, want %d (the registration was consumed by its first firing)",
				n, got, before-int32(2*n))
		}
	}
}

// The same repeat contract for an Effect's `Triggers$` ChangesZone body: a
// resolving sorcery's Stack -> Graveyard move is a matching zone change, and
// the second one must fire the Effect too.
func TestEffectChangesZoneTriggerRepeatsWithinTurn(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:ZonePromise\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigZone\n"+
		"SVar:TrigZone:Mode$ ChangesZone | Origin$ Stack | Destination$ Graveyard | ValidCard$ Card | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	filler := card(t, "Name:Filler\nManaCost:U\nTypes:Sorcery\nA:SP$ Note | Text$ filler\nOracle:x\n")
	e := handEngine(t, promise, filler, filler)
	e.G.Players[0].Pool[state.MU] = 3
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "ChangesZone" {
		t.Fatalf("precondition: Effect did not arm a ChangesZone trigger: %+v", e.G.Delayed)
	}
	if len(e.G.Zone(state.ZHand, 0)) != 2 {
		t.Fatalf("precondition: want two castable fillers in hand, got %d", len(e.G.Zone(state.ZHand, 0)))
	}
	before := e.G.Players[0].Life
	for n := 1; n <= 2; n++ {
		e.askPriority(0)
		castFirst(t, e, "cast")
		passUntilStackEmpty(t, e, 8)
		if got := e.G.Players[0].Life; got != before-int32(2*n) {
			t.Fatalf("after filler %d resolved to the graveyard, controller life = %d, want %d (the registration was consumed by its first firing)",
				n, got, before-int32(2*n))
		}
	}
}
