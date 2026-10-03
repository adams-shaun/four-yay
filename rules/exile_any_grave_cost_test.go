package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// exileAnyGraveBoard builds a two-seat game over real corpus cards with the
// named graveyard fodder and an optional battlefield resident, all through
// logged events so replayCheck holds. The same shape cardname_cost_test.go's
// selfSacrificeBoard uses.
func exileAnyGraveBoard(t *testing.T, reg *cards.Registry, top, fodder, decoy, bearer string) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	ids := map[string]*cards.Card{}
	for name, want := range map[string]bool{top: true, fodder: true, decoy: decoy != "", bearer: bearer != ""} {
		if !want {
			continue
		}
		c := mustCorpusCard(t, reg, name)
		if ids[name] != nil {
			t.Fatalf("duplicate corpus name %q", name)
		}
		ids[name] = c
	}
	deck := []*cards.Card{}
	names := []string{}
	for _, name := range []string{top, bearer} {
		if ids[name] != nil {
			deck = append(deck, ids[name])
			names = append(names, name)
		}
	}
	if ids[fodder] != nil {
		deck = append(deck, ids[fodder])
		names = append(names, fodder)
	}
	if ids[decoy] != nil {
		deck = append(deck, ids[decoy])
		names = append(names, decoy)
	}
	cfg := Config{Seed: 12, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append(deck, mountainDeck(t, 37)...), mountainDeck(t, 40)}}
	e := New(cfg)
	out := map[string]state.ObjID{}
	byCard := map[*cards.Card]string{}
	for name, c := range ids {
		byCard[c] = name
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || o.Card == nil {
			continue
		}
		name, ok := byCard[o.Card]
		if !ok {
			continue
		}
		if _, seen := out[name]; seen {
			continue
		}
		out[name] = o.ID
	}
	// top and bearer to the battlefield; fodder and decoy to the graveyard.
	for _, name := range []string{top, bearer} {
		if id, ok := out[name]; ok && e.G.Obj(id).Zone != state.ZBattlefield {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
		}
	}
	for _, name := range []string{fodder, decoy} {
		if id, ok := out[name]; ok && e.G.Obj(id).Zone != state.ZGraveyard {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZGraveyard})
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg, out
}

// TestExileAnyGraveParsesToTheGraveyardExilePart (exg1): the verb lands on
// the same Cost.Exile part ExileFromGrave builds, with no phantom generic and
// no Unknown entry -- Thelon of Havenwood's `{B}{G}, Exile a Fungus card from
// a graveyard` must not price an extra {1}, and the malformed fallback stays
// reserved for a non-literal amount.
func TestExileAnyGraveParsesToTheGraveyardExilePart(t *testing.T) {
	t.Parallel()
	c := ParseCost("B G ExileAnyGrave<1/Fungus>")
	if c.Generic != 0 || c.Colored[state.ManaIndex('B')] != 1 || c.Colored[state.ManaIndex('G')] != 1 {
		t.Fatalf("ExileAnyGrave parse priced the mana half wrong: %+v", c)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("ExileAnyGrave wrongly reported unknown: %v", c.Unknown)
	}
	if len(c.Exile) != 1 || c.Exile[0].N != 1 || c.Exile[0].Spec != "Fungus" || c.Exile[0].Zone != state.ZGraveyard {
		t.Fatalf("ExileAnyGrave part = %+v", c.Exile)
	}
	grave := ParseCost("ExileAnyGrave<1/Card.TriggeredNewCard>")
	if len(grave.Exile) != 1 || grave.Exile[0].Spec != "Card.TriggeredNewCard" || grave.Exile[0].Zone != state.ZGraveyard {
		t.Fatalf("trigger-referent ExileAnyGrave part = %+v", grave.Exile)
	}
	malformed := ParseCost("ExileAnyGrave<X/Fungus>")
	if len(malformed.Exile) != 0 || len(malformed.Unknown) == 0 || malformed.Generic != 1 {
		t.Fatalf("malformed ExileAnyGrave kept the fallback: %+v unknown=%v", malformed, malformed.Unknown)
	}
}

// TestExileAnyGraveThelonOfHavenwoodAbilityPaysAndExiles (exg1 class A): the
// activated ability is offered at exactly {B}{G} plus a real graveyard exile
// -- the exile ask offers only Fungus cards in a graveyard (never the
// non-Fungus decoy), answering it exiles the chosen card, and the ability's
// effect resolves (a SPORE counter on the battlefield Fungus).
func TestExileAnyGraveThelonOfHavenwoodAbilityPaysAndExiles(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	th := mustCorpusCard(t, reg, "Thelon of Havenwood")
	fungus := mustCorpusCard(t, reg, "Vitaspore Thallid")
	decoy := mustCorpusCard(t, reg, "Bear Cub")
	// {1} in the pool so the unpatched engine's phantom generic does not mask
	// the defect as a withheld offer: on main the ability IS offered (the
	// brief measured this), at the degraded {1}{B}{G}, and never exiles
	// anything. The patch must make the ask real instead.
	cfg := Config{Seed: 7, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{th, fungus, fungus, decoy},
			mountainDeck(t, 37)...), mountainDeck(t, 40)}}
	e := New(cfg)
	var thelonID, bearerID, fodderID, decoyID state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || o.Card == nil {
			continue
		}
		switch {
		case o.Card == th:
			thelonID = o.ID
		case o.Card == fungus && bearerID == 0:
			bearerID = o.ID
		case o.Card == fungus:
			fodderID = o.ID
		case o.Card == decoy:
			decoyID = o.ID
		}
	}
	if thelonID == 0 || bearerID == 0 || fodderID == 0 || decoyID == 0 {
		t.Fatalf("deck cards not found: %+v", map[string]state.ObjID{"thelon": thelonID, "bearer": bearerID, "fodder": fodderID, "decoy": decoyID})
	}
	for _, id := range []state.ObjID{thelonID, bearerID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
	}
	for _, id := range []state.ObjID{fodderID, decoyID} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZGraveyard})
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "G", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	idx := -1
	for i, sa := range th.Faces[0].Abilities {
		if sa.Kind == "AB" && sa.API != "Mana" && strings.Contains(sa.Params["Cost"], "ExileAnyGrave") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("missing compiled ExileAnyGrave activation")
	}
	e.Advance()
	found := false
	for _, opt := range e.legalActions(0) {
		if opt.Kind == "ability" && opt.Obj == thelonID && opt.Ability == idx {
			found = true
		}
	}
	if !found {
		t.Fatal("legalActions did not offer the ExileAnyGrave ability")
	}
	opt := abilityOption(t, e, thelonID, idx)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no exile-cost ask after activating Thelon: %+v", d)
	}
	var picks []int
	for _, o := range d.Options {
		if o.Kind == "exilecost" && o.Obj == fodderID {
			picks = append(picks, o.Index)
		}
	}
	if len(picks) != 1 || len(d.Options) != 1 {
		t.Fatalf("exile ask did not offer exactly the graveyard Fungus (decoy must not appear): %+v", d.Options)
	}
	submitChoices(t, e, picks[0])
	if e.G.Obj(fodderID).Zone != state.ZExile {
		t.Fatalf("paying the cost did not exile the chosen Fungus: %v", e.G.Obj(fodderID).Zone)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bearerID).Counter("SPORE") != 1 {
		t.Fatalf("ability resolved without its SPORE counter: %d", e.G.Obj(bearerID).Counter("SPORE"))
	}
	replayCheck(t, e, cfg)
}

// waitForWindow drains priority-only decisions until the triggered-cost
// window's pay ask (or a decline-only one) is pending.
func waitForWindow(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while waiting for the triggered-cost window")
		}
		if d.Kind == decision.KChoose && len(d.Options) > 0 &&
			(d.Options[0].Kind == "trigger_cost_pay" || d.Options[0].Kind == "trigger_cost_decline") {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision waiting for the window: %+v", d)
		}
		if len(e.G.Stack) == 0 {
			t.Fatal("stack empty but the triggered-cost window never opened")
		}
		e.resolveTop()
	}
	t.Fatal("the triggered-cost window never opened")
	return nil
}

// TestTriggeredNewCardCostIsUnpayableWithoutATriggerContext (exg1 guard): an
// ORDINARY activated ability whose cost carries the bare trigger referent is
// never offered, even with eligible-looking cards in the zone -- the
// fail-closed default means the offer path never invents a referent.
func TestTriggeredNewCardCostIsUnpayableWithoutATriggerContext(t *testing.T) {
	t.Parallel()
	src := card(t, "Name:Referent Payer\nManaCost:1\nTypes:Artifact\n"+
		"A:AB$ GainLife | Cost$ ExileAnyGrave<1/Card.TriggeredNewCard> | ActivationZone$ Graveyard | Defined$ You | LifeAmount$ 1\n"+
		"Oracle:x\n")
	e := handEngine(t, src)
	o := e.G.Obj(e.G.Zone(state.ZHand, 0)[0])
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand, To: state.ZGraveyard})
	mtn := e.G.Zone(state.ZLibrary, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: mtn, From: state.ZLibrary, To: state.ZGraveyard})
	e.Advance()
	for _, opt := range e.legalActions(0) {
		if opt.Kind == "ability" && opt.Obj == o.ID {
			t.Fatalf("the referent-bearing cost was offered without a trigger context: %+v", opt)
		}
	}
}
