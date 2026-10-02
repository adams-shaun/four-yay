package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSacrificedSourceCountHeadsReadLKI: an ability that sacrifices its own
// source as a cost and then counts something on "this artifact" (Ravenous
// Amulet's Count$CardCounters.SOUL; 35 corpus scripts pair a
// Sac<1/CARDNAME> cost with a Count$CardCounters read: Shrine of Burning
// Rage, Golden Urn, Time Bomb, Lotus Blossom, ...) must read the source's
// last known information (CR 608.2h, 113.7a). The card in the graveyard has
// no counters; the head used to read it there and answer 0.
func TestSacrificedSourceCountHeadsReadLKI(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	amulet := onBoardCard(t, e, 0, corpusCard(t, "Ravenous Amulet"))
	e.emit(events.Event{Kind: events.CounterChange, Obj: amulet, Counter: "SOUL", Amount: 2})
	bear := onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})

	amuletLKI := effects.SacrificedLKI(e, amulet)
	bearLKI := effects.SacrificedLKI(e, bear)
	e.emit(events.Sacrifice(amulet))
	e.emit(events.Sacrifice(bear))
	if o := e.G.Obj(amulet); o.Zone == state.ZBattlefield || o.Counter("SOUL") != 0 {
		t.Fatalf("precondition: the sacrificed Amulet is off the battlefield with no live counters (zone %s, SOUL %d)", o.Zone, o.Counter("SOUL"))
	}

	eval := func(source state.ObjID, sac []state.SacrificedInfo, body string) int32 {
		t.Helper()
		ctx := &effects.Ctx{Controller: 0, Source: source, Sacrificed: sac}
		n, ok := effects.EvalCountOK(e, ctx, body)
		if !ok {
			t.Fatalf("%s not evaluated", body)
		}
		return n
	}
	if n := eval(amulet, []state.SacrificedInfo{amuletLKI}, "Count$CardCounters.SOUL"); n != 2 {
		t.Fatalf("Count$CardCounters.SOUL after the Amulet was sacrificed as a cost = %d, want 2 (LKI)", n)
	}
	if n := eval(amulet, []state.SacrificedInfo{amuletLKI}, "Count$CardCounters.ALL"); n != 2 {
		t.Fatalf("Count$CardCounters.ALL = %d, want 2 (LKI)", n)
	}
	if n := eval(amulet, nil, "Count$CardCounters.SOUL"); n != 0 {
		t.Fatalf("an ability that did NOT sacrifice its source reads it live: %d, want 0", n)
	}
	if n := eval(bear, []state.SacrificedInfo{bearLKI}, "Count$CardPower"); n != 3 {
		t.Fatalf("Count$CardPower of a sacrificed 2/2 with a +1/+1 counter = %d, want 3 (LKI)", n)
	}
	if n := eval(bear, []state.SacrificedInfo{bearLKI}, "Count$CardToughness"); n != 3 {
		t.Fatalf("Count$CardToughness = %d, want 3 (LKI)", n)
	}

	// A snapshot taken AFTER the sacrifice (the mana-ability path binds
	// Ctx.Sacrificed once the cost is paid) rebuilds the counters from the
	// log's departure record instead of the cleared graveyard card.
	late := effects.SacrificedLKI(e, amulet)
	if n := eval(amulet, []state.SacrificedInfo{late}, "Count$CardCounters.SOUL"); n != 2 {
		t.Fatalf("post-departure snapshot: Count$CardCounters.SOUL = %d, want 2", n)
	}
	if late := effects.SacrificedLKI(e, bear); late.Power != 3 || late.Toughness != 3 {
		t.Fatalf("post-departure bear snapshot P/T = %d/%d, want 3/3", late.Power, late.Toughness)
	}
}

// TestLotusBlossomSacrificeAddsPetalCountMana is the mana-ability half of the
// class: "{T}, Sacrifice Lotus Blossom: Add X mana of any one color, where X
// is the number of petal counters on Lotus Blossom." The mana path pays the
// sacrifice before it evaluates Amount$, so it must still see the three
// petal counters the Blossom left the battlefield with.
func TestLotusBlossomSacrificeAddsPetalCountMana(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	blossom := corpusCommander(t, reg, "Lotus Blossom")
	e, cfg := eachColorGame(t, 403, []*cards.Card{blossom})
	id := moveToBattlefieldByName(t, e, 0, "Lotus Blossom")
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "PETAL", Amount: 3})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.priorityRound()
	before := poolTotal(e.G.Players[0].Pool)
	activateMana(t, e, id)
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Lotus Blossom zone = %v, want graveyard (cost paid)", o.Zone)
	}
	if got := poolTotal(e.G.Players[0].Pool) - before; got != 3 {
		t.Fatalf("Lotus Blossom with 3 petal counters added %d mana, want 3", got)
	}
	replayCheck(t, e, cfg)
}
