package rules

// The Rakdos-params brief, gap 7: AddKeyword$ Convoke granted by an Effect.
// Chief Engineer's "Artifact spells you cast have convoke" is an
// S:Mode$ Continuous static whose Affected$ spec carries the wasCast
// predicate and whose AffectedZone$ Stack used to be unread, so the grant
// never reached the spell being cast. The grant now rides the layer-6
// machinery with a zone gate, and wasCast matches a spell on the stack --
// pinned here end to end on the real corpus card.
//
// Deliberate conservatism: the OFFER gate prices convoke from the card's
// own (hand-zone) keyword only -- at offer time the card is not yet a cast
// spell, so wasCast cannot match. The test therefore funds the full mana
// cost and shows the announced contributions still displace it.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func chiefEngineerEngine(t *testing.T, creatures int, engineerOnBattlefield bool) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "Chief Engineer"))
	if engineerOnBattlefield {
		for _, o := range e.G.Zone(state.ZHand, 0) {
			if e.G.Obj(o).Face().Name == "Chief Engineer" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o, From: state.ZHand, To: state.ZBattlefield})
				break
			}
		}
	}
	var ids []state.ObjID
	for i := 0; i < creatures; i++ {
		o := e.G.AddObject(card(t, "Name:Scout\nManaCost:G\nTypes:Creature Scout\nPT:1/1\nOracle:x\n"), 0)
		e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
		ids = append(ids, o.ID)
	}
	// A {2} artifact spell in hand.
	spell := e.G.AddObject(card(t, "Name:Gizmo\nManaCost:2\nTypes:Artifact\nOracle:x\n"), 0)
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), spell.ID))
	return e, spell.ID, ids
}

func TestEffectGrantedConvokeReachesTheCastSpell(t *testing.T) {
	t.Parallel()
	e, spell, creatures := chiefEngineerEngine(t, 2, true)
	addMana(t, e, 0, "GG")
	d := e.Pending()
	opt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			opt = o.Index
		}
	}
	if opt < 0 {
		t.Fatalf("artifact cast not offered: %+v", d.Options)
	}
	submitChoices(t, e, opt)
	// CR 601.2b: the convoke ask announces the tap contributions.
	dc := e.Pending()
	if dc == nil || dc.Kind != decision.KChoose {
		t.Fatalf("no convoke ask: %+v", dc)
	}
	conv := []int{}
	for _, o := range dc.Options {
		if strings.HasPrefix(o.Kind, "convoke_") && (o.Obj == creatures[0] || o.Obj == creatures[1]) {
			conv = append(conv, o.Index)
		}
	}
	if len(conv) != 2 {
		t.Fatalf("convoke options missing the scouts: %+v", dc.Options)
	}
	submitChoices(t, e, conv...)
	passUntilStackEmpty(t, e, 20)
	// Both scouts tapped as payment; the funded {2} generic is untouched.
	for _, id := range creatures {
		if !e.G.Obj(id).Tapped {
			t.Fatalf("scout %d not tapped by the convoke payment", id)
		}
	}
	if pool := e.G.Players[0].Pool; pool.Total() != 2 {
		t.Fatalf("pool=%+v, want the funded {2} untouched (convoke paid the whole cost)", pool)
	}
	if e.G.Obj(spell).Zone != state.ZBattlefield {
		t.Fatalf("Gizmo zone=%s, want battlefield", e.G.Obj(spell).Zone)
	}
}

func TestEffectGrantedConvokeDoesNotReachNonSpellsOrOtherCards(t *testing.T) {
	t.Parallel()
	// Without Chief Engineer on the battlefield, the same cast never poses a
	// convoke ask.
	e, spell, _ := chiefEngineerEngine(t, 0, false)
	addMana(t, e, 0, "GG")
	d := e.Pending()
	opt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			opt = o.Index
		}
	}
	submitChoices(t, e, opt)
	d = e.Pending()
	for _, o := range d.Options {
		if strings.HasPrefix(o.Kind, "convoke_") {
			t.Fatalf("convoke ask without the grant: %+v", d)
		}
	}
	if d != nil && d.Kind != decision.KPriority {
		t.Fatalf("expected the cast to proceed to payment, got %+v", d)
	}
}
