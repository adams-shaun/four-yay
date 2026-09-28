package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// fdnScenario supplies real corpus spells and inline, license-safe board fixtures.
const fdnBear = "Name:FDN Audit Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
const fdnGiant = "Name:FDN Audit Giant\nManaCost:3 G\nTypes:Creature Giant\nPT:6/6\nOracle:x\n"

func fdnSetup(t *testing.T, reg *cards.Registry, name string, own, opposing []*cards.Card) (*Engine, state.ObjID) {
	t.Helper()
	own = append([]*cards.Card{lookup(t, reg, name)}, own...)
	e, _ := corpusEngineCfg(t, reg, own, opposing)
	id := moveByName(t, e, 0, name, state.ZHand)
	if e.G.Obj(id).Zone != state.ZHand {
		t.Fatal("spell not in hand")
	}
	return e, id
}

func fdnOffer(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("cast not offered for %s (%d): %+v", e.G.Obj(id).Face().Name, id, d.Options)
}

func fdnTarget(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected target ask, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == id {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("target %d not available: %+v", id, d.Options)
}

// Answer cost/mode/selection asks, then let both players pass priority until resolution.
func fdnResolve(t *testing.T, e *Engine) {
	t.Helper()
	for n := 0; n < 80; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("missing pending decision")
		}
		if len(e.G.Stack) == 0 && d.Kind == decision.KPriority {
			return
		}
		if len(d.Options) == 0 {
			t.Fatalf("empty options: %+v", d)
		}
		idx := d.Options[0].Index
		if d.Kind == decision.KPriority {
			idx = -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				t.Fatalf("no pass: %+v", d)
			}
		}
		submitChoices(t, e, idx)
	}
	t.Fatal("resolution did not finish")
}

// fdnResolveCost selects a specified card when a discard/sacrifice ask follows announcement.
func fdnResolveCost(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected cost ask: %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == id {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("cost card %d not offered: %+v", id, d.Options)
}

func TestFDNCastability(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	bear := card(t, fdnBear)
	giant := card(t, fdnGiant)
	t.Run("Eaten Alive sacrifice", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Eaten Alive", []*cards.Card{bear}, []*cards.Card{bear})
		fodder := moveByName(t, e, 0, bear.Faces[0].Name, state.ZBattlefield)
		victim := moveByName(t, e, 1, bear.Faces[0].Name, state.ZBattlefield)
		addMana(t, e, 0, "B")
		fdnOffer(t, e, id)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "altaddcost" {
			t.Fatalf("missing additional-cost ask: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
		fdnResolveCost(t, e, fodder)
		fdnTarget(t, e, victim)
		fdnResolve(t, e)
		if e.G.Obj(victim).Zone != state.ZExile || e.G.Obj(fodder).Zone != state.ZGraveyard {
			t.Fatalf("victim %s fodder %s", e.G.Obj(victim).Zone, e.G.Obj(fodder).Zone)
		}
	})
	t.Run("Eaten Alive pay mana", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Eaten Alive", nil, []*cards.Card{bear})
		victim := moveByName(t, e, 1, bear.Faces[0].Name, state.ZBattlefield)
		addMana(t, e, 0, "BBBBB") // printed {B} plus additional {3}{B}
		fdnOffer(t, e, id)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "altaddcost" {
			t.Fatalf("missing additional-cost ask: %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
		fdnTarget(t, e, victim)
		fdnResolve(t, e)
		if e.G.Obj(victim).Zone != state.ZExile {
			t.Fatalf("victim %s", e.G.Obj(victim).Zone)
		}
	})
	t.Run("Tolarian Terror", func(t *testing.T) {
		extras := make([]*cards.Card, 5)
		for i := range extras {
			extras[i] = lookup(t, reg, "Opt")
		}
		e, id := fdnSetup(t, reg, "Tolarian Terror", extras, nil)
		for range extras {
			moveByName(t, e, 0, "Opt", state.ZGraveyard)
		}
		if len(e.G.Zone(state.ZGraveyard, 0)) < 5 {
			t.Fatal("graveyard not seeded")
		}
		addMana(t, e, 0, "UU")
		fdnOffer(t, e, id)
		fdnResolve(t, e)
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("terror %s", e.G.Obj(id).Zone)
		}
	})
	t.Run("Luminous Rebuke", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Luminous Rebuke", nil, []*cards.Card{bear})
		victim := moveByName(t, e, 1, bear.Faces[0].Name, state.ZBattlefield)
		e.emit(events.Event{Kind: events.Tap, Obj: victim})
		if !e.G.Obj(victim).Tapped {
			t.Fatal("target not tapped")
		}
		addMana(t, e, 0, "WW")
		fdnOffer(t, e, id)
		fdnTarget(t, e, victim)
		fdnResolve(t, e)
		if e.G.Obj(victim).Zone != state.ZGraveyard {
			t.Fatalf("victim %s", e.G.Obj(victim).Zone)
		}
	})
	t.Run("Thrill of Possibility", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Thrill of Possibility", []*cards.Card{bear}, nil)
		discard := moveByName(t, e, 0, bear.Faces[0].Name, state.ZHand)
		before := len(e.G.Zone(state.ZHand, 0))
		addMana(t, e, 0, "RR")
		fdnOffer(t, e, id)
		fdnResolveCost(t, e, discard)
		fdnResolve(t, e)
		if e.G.Obj(discard).Zone != state.ZGraveyard || len(e.G.Zone(state.ZHand, 0)) != before {
			t.Fatalf("discard %s hand %d before %d", e.G.Obj(discard).Zone, len(e.G.Zone(state.ZHand, 0)), before)
		}
	})
	t.Run("Wildwood Scourge", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Wildwood Scourge", nil, nil)
		addMana(t, e, 0, "GGG")
		fdnOffer(t, e, id)
		announceXCast(t, e, id, 2)
		fdnResolve(t, e)
		if e.G.Obj(id).Zone != state.ZBattlefield || e.Derived(id).Power != 2 {
			t.Fatalf("scourge zone %s power %d", e.G.Obj(id).Zone, e.Derived(id).Power)
		}
	})
	t.Run("Goblin Negotiation", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Goblin Negotiation", nil, []*cards.Card{bear})
		victim := moveByName(t, e, 1, bear.Faces[0].Name, state.ZBattlefield)
		addMana(t, e, 0, "RRRR")
		fdnOffer(t, e, id)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("missing X ask: %+v", d)
		}
		found := false
		for _, o := range d.Options {
			if o.Kind == "x" && o.Amount == 2 {
				submitChoices(t, e, o.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("X=2 not offered: %+v", d.Options)
		}
		fdnTarget(t, e, victim)
		fdnResolve(t, e)
		if e.G.Obj(victim).Zone != state.ZGraveyard {
			t.Fatalf("victim %s", e.G.Obj(victim).Zone)
		}
	})
	t.Run("Joust Through", func(t *testing.T) {
		// The spell targets any attacking or blocking creature, so the legal
		// fixture is player 0's OWN attacker (an opponent's creature cannot
		// attack for player 0).
		e, id := fdnSetup(t, reg, "Joust Through", []*cards.Card{bear}, nil)
		attacker := moveByName(t, e, 0, bear.Faces[0].Name, state.ZBattlefield)
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
		if !e.G.Obj(attacker).IsAttacking {
			t.Fatal("creature not attacking")
		}
		if e.G.Obj(attacker).Controller != 0 {
			t.Fatal("attacker not controlled by the caster")
		}
		addMana(t, e, 0, "W")
		life := e.G.Players[0].Life
		fdnOffer(t, e, id)
		fdnTarget(t, e, attacker)
		fdnResolve(t, e)
		if e.G.Obj(attacker).Zone != state.ZGraveyard || e.G.Players[0].Life != life+1 {
			t.Fatalf("attacker %s life %d", e.G.Obj(attacker).Zone, e.G.Players[0].Life)
		}
	})
	t.Run("Refute", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Refute", nil, []*cards.Card{lookup(t, reg, "Opt")})
		spell := moveByName(t, e, 1, "Opt", state.ZHand)
		addMana(t, e, 1, "U")
		addMana(t, e, 0, "UUU")
		// Give the opponent priority and cast its instant onto the stack.
		for n := 0; n < 4 && e.Pending().Player != 1; n++ {
			d := e.Pending()
			for _, o := range d.Options {
				if o.Kind == "pass" {
					submitChoices(t, e, o.Index)
					break
				}
			}
		}
		oppHand := len(e.G.Zone(state.ZHand, 1))
		fdnOffer(t, e, spell)
		d := e.Pending()
		if d == nil || d.Player != 1 || len(e.G.Stack) != 1 {
			t.Fatalf("Opt not on stack: %+v", d)
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
		fdnOffer(t, e, id)
		fdnTarget(t, e, spell)
		fdnResolve(t, e)
		if e.G.Obj(spell).Zone != state.ZGraveyard || e.G.Obj(id).Zone != state.ZGraveyard || len(e.G.Zone(state.ZHand, 1)) != oppHand-1 {
			t.Fatalf("counter did not stop Opt's draw: spell %s refute %s opponent hand %d (before %d)", e.G.Obj(spell).Zone, e.G.Obj(id).Zone, len(e.G.Zone(state.ZHand, 1)), oppHand)
		}
	})
	t.Run("Zombify", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Zombify", []*cards.Card{bear}, nil)
		dead := moveByName(t, e, 0, bear.Faces[0].Name, state.ZGraveyard)
		addMana(t, e, 0, "BBBB")
		fdnOffer(t, e, id)
		fdnTarget(t, e, dead)
		fdnResolve(t, e)
		if e.G.Obj(dead).Zone != state.ZBattlefield {
			t.Fatalf("dead creature %s", e.G.Obj(dead).Zone)
		}
	})
	t.Run("Stab", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Stab", nil, []*cards.Card{bear})
		victim := moveByName(t, e, 1, bear.Faces[0].Name, state.ZBattlefield)
		addMana(t, e, 0, "B")
		fdnOffer(t, e, id)
		fdnTarget(t, e, victim)
		fdnResolve(t, e)
		if e.G.Obj(victim).Zone != state.ZGraveyard {
			t.Fatalf("victim %s", e.G.Obj(victim).Zone)
		}
	})
	t.Run("Bushwhack", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Bushwhack", nil, nil)
		before := len(e.G.Zone(state.ZHand, 0))
		library := len(e.G.Zone(state.ZLibrary, 0))
		addMana(t, e, 0, "G")
		fdnOffer(t, e, id)
		fdnResolve(t, e)
		if e.G.Obj(id).Zone != state.ZGraveyard || len(e.G.Zone(state.ZHand, 0)) != before || len(e.G.Zone(state.ZLibrary, 0)) != library-1 {
			t.Fatalf("spell %s hand %d before %d library %d before %d", e.G.Obj(id).Zone, len(e.G.Zone(state.ZHand, 0)), before, len(e.G.Zone(state.ZLibrary, 0)), library)
		}
	})
	t.Run("Exsanguinate", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Exsanguinate", nil, nil)
		addMana(t, e, 0, "BBBB")
		a, b := e.G.Players[0].Life, e.G.Players[1].Life
		fdnOffer(t, e, id)
		announceXCast(t, e, id, 2)
		fdnResolve(t, e)
		if e.G.Players[0].Life != a+2 || e.G.Players[1].Life != b-2 {
			t.Fatalf("life %d/%d before %d/%d", e.G.Players[0].Life, e.G.Players[1].Life, a, b)
		}
	})
	t.Run("Arbiter of Woe", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Arbiter of Woe", []*cards.Card{bear}, nil)
		fodder := moveByName(t, e, 0, bear.Faces[0].Name, state.ZBattlefield)
		addMana(t, e, 0, "BBBBBB")
		fdnOffer(t, e, id)
		fdnResolve(t, e)
		if e.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(fodder).Zone != state.ZGraveyard {
			t.Fatalf("arbiter %s fodder %s", e.G.Obj(id).Zone, e.G.Obj(fodder).Zone)
		}
	})
	t.Run("Ghalta, Primal Hunger", func(t *testing.T) {
		e, id := fdnSetup(t, reg, "Ghalta, Primal Hunger", []*cards.Card{giant, giant}, nil)
		a := moveByName(t, e, 0, giant.Faces[0].Name, state.ZBattlefield)
		b := moveByName(t, e, 0, giant.Faces[0].Name, state.ZBattlefield)
		if e.Derived(a).Power+e.Derived(b).Power != 12 {
			t.Fatal("fixture power not 12")
		}
		addMana(t, e, 0, "GG")
		fdnOffer(t, e, id)
		fdnResolve(t, e)
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("ghalta %s", e.G.Obj(id).Zone)
		}
	})
}
