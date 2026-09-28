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

// This file pins `api:Token`'s TokenRemembered$ parameter end to end on the
// real compiled Timothar, Baron of Bats script. Timothar's dies trigger is
//
//	SVar:TrigToken:AB$ Token | Cost$ 1 ExileAnyGrave<1/Card.TriggeredNewCard> |
//	  TokenRemembered$ ExiledCards | TokenScript$ b_1_1_bat_flying |
//	  ImprintTokens$ True | SubAbility$ DBAnimate
//	SVar:DBAnimate:DB$ Animate | Defined$ Imprinted | Duration$ Permanent | Triggers$ CDTrigger
//	SVar:CDTrigger:Mode$ DamageDone | ValidSource$ Card.Self | ValidTarget$ Player |
//	  CombatDamage$ True | Execute$ TrigSac | TriggerZones$ Battlefield
//	SVar:TrigSac:DB$ Sacrifice | SubAbility$ DBReturn
//	SVar:DBReturn:DB$ ChangeZone | Defined$ Remembered | Origin$ Exile |
//	  Destination$ Battlefield | Tapped$ True | GainControl$ True
//
// so the whole card depends on TokenRemembered$ binding the exiled Vampire to
// the CREATED Bat: without that read the Bat's granted return trigger resolves
// `Defined$ Remembered` against an empty list and the exiled card is stranded.

// tokenRememberedCard remedies the one rider that is measured into the target
// object: the Bat's Remembered list (set by the TokenRemembered$ Choose
// "remembered" event) is where the granted return trigger's `Defined$
// Remembered` resolves.
func tokenRememberedCards(e *Engine, id state.ObjID, ids ...state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil {
		return false
	}
	want := map[state.ObjID]bool{}
	for _, w := range ids {
		want[w] = true
	}
	seen := map[state.ObjID]bool{}
	for _, r := range o.Remembered {
		if !r.IsPlayer {
			seen[r.Obj] = true
		}
	}
	for w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

// tokenRememberedBoard is exileAnyGraveBoard's shape with the token scripts
// registered on the Config, so a Timothar token mint resolves and the whole
// board round-trips through replayFromLog (which rebuilds from cfg).
func tokenRememberedBoard(t *testing.T, reg *cards.Registry, top, bearer string) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	topCard := mustCorpusCard(t, reg, top)
	bearerCard := mustCorpusCard(t, reg, bearer)
	cfg := Config{Seed: 12, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{topCard, bearerCard}, mountainDeck(t, 38)...),
			mountainDeck(t, 40),
		}}
	e := New(cfg)
	out := map[string]state.ObjID{}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || o.Card == nil {
			continue
		}
		switch o.Card {
		case topCard:
			out[top] = o.ID
		case bearerCard:
			out[bearer] = o.ID
		}
	}
	if out[top] == 0 || out[bearer] == 0 {
		t.Fatalf("board cards not found: %+v", out)
	}
	for _, id := range []state.ObjID{out[top], out[bearer]} {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg, out
}

// driveToAttackersAt drives the game (passing priority, declaring no attackers
// at every declare-attackers step) until it reaches turn `turn` seat `active`
// at a declare-attackers decision -- the pre-attack stopping point a fixture
// attacking on a LATER turn than the current one needs, so no direct
// SummonSick mutation (which replayCheck would flag) is used.
func driveToAttackersAt(t *testing.T, e *Engine, turn int32, active state.PlayerID) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Over {
			t.Fatalf("game ended before turn %d seat %d", turn, active)
		}
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == state.StepDeclareAttackers {
			if d := e.Pending(); d != nil && d.Kind == decision.KAttackers {
				return
			}
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			passOnce(t, e)
		case decision.KAttackers:
			submitNoAttackers(t, e)
		default:
			if len(d.Options) == 0 {
				t.Fatalf("empty non-priority decision %+v while driving to turn %d", d, turn)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit: %v", err)
			}
		}
	}
	t.Fatalf("did not reach turn %d seat %d declare-attackers within the pass budget", turn, active)
}

// TestTokenRememberedTimotharEndToEnd is the brief's headline pin: another
// nontoken Vampire dies, Timothar's trigger pays {1} and exiles it, the Bat
// token is created REMEMBERING the exiled Vampire, the Bat deals combat damage
// to the opponent, and the granted trigger sacrifices the Bat and returns the
// exiled Vampire to the battlefield tapped under seat 0's control.
func TestTokenRememberedTimotharEndToEnd(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := tokenRememberedBoard(t, reg, "Timothar, Baron of Bats", "Vampire Nighthawk")
	timothar := ids["Timothar, Baron of Bats"]
	vampire := ids["Vampire Nighthawk"]
	if timothar == 0 || vampire == 0 {
		t.Fatalf("board missing cards: timothar=%d vampire=%d", timothar, vampire)
	}
	if e.G.Obj(timothar).Zone != state.ZBattlefield || e.G.Obj(vampire).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Timothar/Vampire must both start on the battlefield (%v/%v)",
			e.G.Obj(timothar).Zone, e.G.Obj(vampire).Zone)
	}
	if !strings.Contains(strings.ToLower(e.G.Obj(vampire).Face().Name), "vampire") {
		t.Fatalf("precondition: bearer %q is not a Vampire", e.G.Obj(vampire).Face().Name)
	}

	// {1} in the pool so the trigger's `Cost$ 1` is payable when the window
	// opens. The mana is added BEFORE the kill so the window's pay gate sees
	// it.
	addMana(t, e, 0, "C")

	// The other nontoken Vampire dies. The changes-zone trigger
	// (Origin$ Battlefield | Destination$ Graveyard | ValidCard$
	// Vampire.Other+!token+YouCtrl) fires Timothar's TrigToken body.
	e.emit(events.Event{Kind: events.MoveZone, Obj: vampire, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "destroyed"})
	e.putTriggersOnStack()

	// Resolve into the trigger situation; the cost window (the `Cost$ 1
	// ExileAnyGrave<1/Card.TriggeredNewCard>` half) opens as a pay/decline ask.
	d := waitForWindow(t, e)
	if !strings.Contains(d.Options[0].Label, "1") {
		t.Fatalf("Timothar's window pay label lost the {{1}}: %q", d.Options[0].Label)
	}
	submitChoices(t, e, d.Options[0].Index)

	// Precondition for the whole feature: paying exiled the triggering
	// Vampire (never left in the graveyard).
	if z := e.G.Obj(vampire).Zone; z != state.ZExile {
		t.Fatalf("paying the cost did not exile the dead Vampire: zone %v, log %+v", z, e.L.Events)
	}

	// Let the Token + Animate chain resolve.
	passUntilStackEmpty(t, e, 30)

	// The Bat exists and REMEMBERS the exiled Vampire (TokenRemembered$
	// ExiledCards). This is the assertion the feature exists for.
	var bat state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil &&
			strings.Contains(o.Face().Name, "Bat") {
			bat = id
		}
	}
	if bat == 0 {
		t.Fatalf("Timothar's token was not created; battlefield %v log %+v",
			e.G.Zone(state.ZBattlefield, 0), e.L.Events)
	}
	if !tokenRememberedCards(e, bat, vampire) {
		t.Fatalf("Bat token %d Remembered = %+v, want the exiled Vampire %d",
			bat, e.G.Obj(bat).Remembered, vampire)
	}
	// The Animate's Triggers$ CDTrigger must have granted the return trigger
	// to the Bat, or the damage assertions below are vacuous.
	if grants := triggerGrantsOn(e, bat); len(grants) != 1 {
		t.Fatalf("the Bat carries %d trigger grants, want 1 (the Animate's Triggers$ CDTrigger); log %+v",
			len(grants), e.L.Events)
	}

	// Drive to seat 0's NEXT turn so the Bat is free of summoning sickness and
	// can attack. The grant is Duration$ Permanent, so the granted trigger
	// survives the turn boundary. Every intervening declare-attackers step is
	// answered with no attackers.
	driveToAttackersAt(t, e, 4, 0)
	submitAttackers(t, e, bat)
	drainCombatDamagePriority(t, e)
	passUntilStackEmpty(t, e, 60)

	// The Bat dealt 1 combat damage to seat 1 (the granted trigger's gate).
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("seat 1 life = %d, want 19 (the Bat's 1 combat damage)", got)
	}
	// The trigger sacrificed the Bat and returned the exiled Vampire to the
	// battlefield TAPPED under seat 0's control.
	if o := e.G.Obj(bat); o == nil || o.Zone == state.ZBattlefield {
		t.Fatalf("the Bat was not sacrificed off the battlefield: %+v", o)
	}
	back := e.G.Obj(vampire)
	if back.Zone != state.ZBattlefield {
		t.Fatalf("the exiled Vampire was not returned: zone %v log %+v", back.Zone, e.L.Events)
	}
	if back.Controller != 0 {
		t.Fatalf("returned Vampire controller = %d, want seat 0", back.Controller)
	}
	if !back.Tapped {
		t.Fatal("returned Vampire did not enter tapped")
	}
	replayCheck(t, e, cfg)
}
