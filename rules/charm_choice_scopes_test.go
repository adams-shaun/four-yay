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

func charmScopeFixture(t *testing.T, cardName string) (*Engine, Config, state.ObjID, *cards.SA) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card := mustCorpusCard(t, reg, cardName)
	cfg := seatZeroStart(Config{Seed: 78341, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{append([]*cards.Card{card}, mountainDeck(t, 39)...), mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	source := placeFromDeck(t, e, 0, cardName)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	o := e.G.Obj(source)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("fixture precondition: %q source=%+v, want controlled battlefield permanent", cardName, o)
	}
	sa := cards.ResolveSVar(o.Face().SVars, "TrigCharm")
	if sa == nil {
		t.Fatalf("%s TrigCharm did not compile", cardName)
	}
	return e, cfg, source, sa
}

func assertCharmModes(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("eligible modes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("eligible modes = %v, want %v", got, want)
		}
	}
}

func recordCharmMode(t *testing.T, e *Engine, source state.ObjID, sa *cards.SA, name string) {
	t.Helper()
	effects.RecordCharmChoices(e, source, sa, []string{name})
	o := e.G.Obj(source)
	if len(o.ModeChoices) == 0 || o.ModeChoices[len(o.ModeChoices)-1].Mode != name {
		t.Fatalf("pick %q not recorded on source: %+v", name, o.ModeChoices)
	}
}

func beginCharmCombat(e *Engine) {
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
}

func TestCharmChoiceRestrictionYourLastCombatByElspethsCommand(t *testing.T) {
	t.Parallel()
	e, cfg, source, sa := charmScopeFixture(t, "By Elspeth's Command")
	if got := sa.Params["ChoiceRestriction"]; got != state.ModeScopeYourLastCombat {
		t.Fatalf("compiled ChoiceRestriction$ = %q", got)
	}
	all := []string{"PumpField", "PumpHand", "Token"}
	askAndChoose := func(want string, expected []string) {
		t.Helper()
		beginCharmCombat(e)
		e.putTriggersOnStack()
		if len(e.G.Stack) != 1 {
			t.Fatalf("combat trigger stack = %v, want exactly By Elspeth's Command", e.G.Stack)
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KModes || d.ResumeKind != "modes" {
			t.Fatalf("pending = %+v, want trigger-placement modes decision", d)
		}
		assertCharmModes(t, d.ResumeModes, expected)
		idx := modeNameIndex(d, want)
		if idx < 0 {
			t.Fatalf("trigger ask does not offer %q: %v", want, d.ResumeModes)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("choose %s: %v", want, err)
		}
		if target := e.Pending(); target != nil && target.Kind == decision.KTarget {
			if len(target.Options) == 0 {
				t.Fatalf("target ask has no legal options: %+v", target)
			}
			if err := e.Submit(decision.Intent{Seq: target.Seq, Player: target.Player, Choices: []int{target.Options[0].Index}}); err != nil {
				t.Fatalf("answer target for %s: %v", want, err)
			}
		}
		if got := e.G.Obj(source).ModeChoices; len(got) == 0 || got[len(got)-1].Mode != want {
			t.Fatalf("trigger choice %q not recorded on source: %+v", want, got)
		}
		passUntilStackEmpty(t, e, 60)
	}

	askAndChoose("Token", all)
	first := e.G.Obj(source).ModeChoices[len(e.G.Obj(source).ModeChoices)-1]
	if first.Turn != e.G.Turn || first.Combat != e.G.CombatsThisTurn {
		t.Fatalf("pick combat stamp = (%d,%d), clock=(%d,%d)", first.Turn, first.Combat, e.G.Turn, e.G.CombatsThisTurn)
	}
	// TurnChange preserves the pick; the next actual begin-combat trigger ask
	// must withhold it across the turn boundary.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	askAndChoose("PumpField", []string{"PumpField", "PumpHand"})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 4})
	// The first pick has aged out after combat two, while combat two's pick is
	// now the sole forbidden mode.
	askAndChoose("Token", []string{"PumpHand", "Token"})
	replayCheck(t, e, cfg)
}

func TestCharmChoiceRestrictionYourLastCombatPicklessCombatReleasesPick(t *testing.T) {
	t.Parallel()
	e, _, source, sa := charmScopeFixture(t, "By Elspeth's Command")
	all := []string{"PumpField", "PumpHand", "Token"}
	beginCharmCombat(e)
	recordCharmMode(t, e, source, sa, "PumpField")
	beginCharmCombat(e) // No pick: the trigger was countered or suppressed.
	assertCharmModes(t, effects.CharmEligibleModes(e, source, sa, all), []string{"PumpHand", "Token"})
	beginCharmCombat(e)
	got := effects.CharmEligibleModes(e, source, sa, all)
	assertCharmModes(t, got, all)
	if len(e.G.Obj(source).ModeChoices) != 0 {
		t.Fatalf("old pick survived a pickless intervening combat: %+v", e.G.Obj(source).ModeChoices)
	}
}

func TestCharmChoiceRestrictionYourLastCombatYotianCourier(t *testing.T) {
	t.Parallel()
	e, _, source, sa := charmScopeFixture(t, "Yotian Courier")
	if sa.Params["ChoiceRestriction"] != state.ModeScopeYourLastCombat {
		t.Fatalf("compiled ChoiceRestriction$ = %q", sa.Params["ChoiceRestriction"])
	}
	all := []string{"Powerstone", "Seek"}
	beginCharmCombat(e)
	recordCharmMode(t, e, source, sa, "Powerstone")
	// A second attack trigger in this combat is not constrained by this
	// combat's pick; only the immediately preceding combat is forbidden.
	assertCharmModes(t, effects.CharmEligibleModes(e, source, sa, all), all)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	beginCharmCombat(e)
	got := effects.CharmEligibleModes(e, source, sa, all)
	assertCharmModes(t, got, []string{"Seek"})
}

func TestCharmChoiceRestrictionThisGamePersists(t *testing.T) {
	t.Parallel()
	e, _, source, sa := charmScopeFixture(t, "Silent Hallcreeper")
	if sa.Params["ChoiceRestriction"] != state.ModeScopeThisGame {
		t.Fatalf("compiled ChoiceRestriction$ = %q", sa.Params["ChoiceRestriction"])
	}
	all := []string{"DBPutCounter", "DBDraw", "DBClone"}
	face := e.G.Obj(source).Face()
	if face == nil || len(face.Triggers) == 0 || face.Triggers[0].Mode != "DamageDone" {
		t.Fatalf("precondition: Silent Hallcreeper has no DamageDone trigger: %+v", face)
	}
	fireDamageTrigger := func(expected []string, choose string) {
		t.Helper()
		// Drive the compiled combat-damage trigger, then inspect and answer its
		// real placement ask (not the helper in isolation).
		e.damaging, e.combatDamaging = source, true
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
		e.damaging, e.combatDamaging = 0, false
		e.putTriggersOnStack()
		if len(e.G.Stack) == 0 {
			t.Fatal("combat damage did not put Silent Hallcreeper's trigger on the stack")
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KModes || d.ResumeKind != "modes" {
			t.Fatalf("pending = %+v, want Hallcreeper trigger modes ask", d)
		}
		assertCharmModes(t, d.ResumeModes, expected)
		idx := modeNameIndex(d, choose)
		if idx < 0 {
			t.Fatalf("Hallcreeper trigger ask omits %s: %v", choose, d.ResumeModes)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("choose %s: %v", choose, err)
		}
		if got := e.G.Obj(source).ModeChoices; len(got) == 0 || got[len(got)-1].Mode != choose {
			t.Fatalf("choice %s not recorded on Hallcreeper: %+v", choose, got)
		}
		passUntilStackEmpty(t, e, 60)
	}
	fireDamageTrigger(all, "DBDraw")
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 3})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 4})
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 5})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 6})
	fireDamageTrigger([]string{"DBPutCounter", "DBClone"}, "DBClone")
}

func TestCharmChoiceRestrictionUnknownScopeNotesAndFailsOpen(t *testing.T) {
	t.Parallel()
	e, _, source, _ := charmScopeFixture(t, "By Elspeth's Command")
	sa := &cards.SA{Params: map[string]string{"ChoiceRestriction": "FutureScope"}}
	modes := []string{"A", "B"}
	before := len(e.L.Events)
	got := effects.CharmEligibleModes(e, source, sa, modes)
	assertCharmModes(t, got, modes)
	if len(e.L.Events) != before+1 || e.L.Events[len(e.L.Events)-1].Kind != events.Note || e.L.Events[len(e.L.Events)-1].Text != "unmodelled ChoiceRestriction$ scope: FutureScope" {
		t.Fatalf("unknown scope did not emit its diagnostic Note: %+v", e.L.Events[before:])
	}
}

func TestCharmChoiceRestrictionYourLastCombatObjectEnteredAfterBeginCombat(t *testing.T) {
	t.Parallel()
	// A permanent that enters the battlefield AFTER this combat's BeginCombat
	// rotation (a blink, a flash creature, a reanimation) was not in the
	// rotation loop, so its CurCombat* stamp is zero/stale. A pick it makes in
	// THIS combat is still a current-combat pick, so a second ask in the same
	// combat must not withhold that mode -- only the preceding combat's picks
	// are forbidden. Before the Choose-fold resync this test failed: the
	// second ask returned [PumpField PumpHand] instead of all three modes.
	e, _, source, sa := charmScopeFixture(t, "By Elspeth's Command")
	all := []string{"PumpField", "PumpHand", "Token"}
	beginCharmCombat(e)
	// Blink the source: leave the battlefield and return. The departure block
	// clears ModeChoices and the combat stamp (CR 400.7), so the returning
	// permanent is a new object that was never rotated this combat.
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZBattlefield, To: state.ZExile})
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZExile, To: state.ZBattlefield})
	o := e.G.Obj(source)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: blinked source not a controlled battlefield permanent: %+v", o)
	}
	if o.CurCombatTurn != 0 || o.CurCombatCombat != 0 {
		t.Fatalf("precondition: blinked source kept a combat stamp (%d,%d), want zero", o.CurCombatTurn, o.CurCombatCombat)
	}
	if len(o.ModeChoices) != 0 {
		t.Fatalf("precondition: blinked source kept mode picks: %+v", o.ModeChoices)
	}
	recordCharmMode(t, e, source, sa, "Token")
	pick := e.G.Obj(source).ModeChoices[len(e.G.Obj(source).ModeChoices)-1]
	if pick.Scope != state.ModeScopeYourLastCombat || pick.Mode != "Token" {
		t.Fatalf("precondition: pick not recorded as a YourLastCombat Token: %+v", pick)
	}
	// Same combat, second ask: the current combat's own pick must not exclude.
	assertCharmModes(t, effects.CharmEligibleModes(e, source, sa, all), all)
	// The next combat ages it, so it is then withheld.
	beginCharmCombat(e)
	assertCharmModes(t, effects.CharmEligibleModes(e, source, sa, all), []string{"PumpField", "PumpHand"})
}

func containsMode(modes []string, name string) bool {
	for _, mode := range modes {
		if mode == name {
			return true
		}
	}
	return false
}
