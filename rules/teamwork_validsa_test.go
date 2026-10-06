package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func teamworkValidSACards(t *testing.T) (*cards.Card, *cards.Card) {
	t.Helper()
	quantum := corpusAlternativeCard(t, "Quantum Reduction")
	assistant := corpusAlternativeCard(t, "Virtual Assistant")
	if len(quantum.Faces) == 0 || len(quantum.Faces[0].Statics) == 0 || quantum.Faces[0].Statics[0].Mode != "CastWithFlash" || quantum.Faces[0].Statics[0].ParamStr(cards.PKValidSA) != "Spell.Teamwork" || len(assistant.Faces) == 0 || len(assistant.Faces[0].Triggers) == 0 || assistant.Faces[0].Triggers[0].Mode != "SpellCast" || assistant.Faces[0].Triggers[0].ParamStr(cards.PKValidSA) != "Spell.Teamwork" {
		t.Fatal("precondition: corpus ValidSA script shape changed")
	}
	return quantum, assistant
}

func TestSpellTeamworkValidSAConsumers(t *testing.T) {
	teamworkQuantumFlash(t)
}

func teamworkQuantumFlash(t *testing.T) {
	t.Helper()
	quantum, _ := teamworkValidSACards(t)
	// An Aura in hand on the opponent's turn with enough untapped power and
	// mana has exactly one cast mode: the conditional Teamwork permission.
	for _, pay := range []bool{true, false} {
		e := handEngine(t, quantum)
		e.G.Active = 1
		spell := e.G.Zone(state.ZHand, 0)[0]
		bear := battlePerm(t, e, 0, "Name:Teamwork Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		if e.G.Obj(spell).Zone != state.ZHand || e.G.Active != 1 || e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(spell).Face().IsInstant() {
			t.Fatal("precondition: Quantum in hand, off-turn sorcery, with battlefield creature")
		}
		e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MU] = 1, 1
		var teamwork decision.Option
		teamworkFound, ordinary := false, false
		for _, o := range e.legalActions(0) {
			if o.Obj == spell && o.Kind == "cast" {
				if o.Mode == "teamworked" {
					teamwork, teamworkFound = o, true
				} else {
					ordinary = true
				}
			}
		}
		if !teamworkFound || ordinary {
			t.Fatalf("off-turn offers teamwork=%v ordinary=%v; want Teamwork only", teamworkFound, ordinary)
		}
		e.beginCast(0, teamwork)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || !d.AllowNone || d.MinSum != 2 {
			t.Fatalf("precondition: Teamwork is payable and declinable: %+v", d)
		}
		if pay {
			submitChoices(t, e, teamworkOption(t, d, bear))
		} else {
			submitChoices(t, e)
		}
		for i := 0; i < 6; i++ {
			d = e.Pending()
			if d == nil || d.Kind == decision.KPriority {
				break
			}
			if d.Kind != decision.KTarget {
				t.Fatalf("unexpected announcement decision: %+v", d)
			}
			submitChoices(t, e, istTargetOptionFor(d, bear))
		}
		if pay {
			if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || o.CastFlags&state.FlagTeamworkPaid == 0 {
				t.Fatalf("paid Quantum did not complete off-turn cast: %+v", o)
			}
		} else if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand || o.CastFlags&state.FlagTeamworkPaid != 0 {
			t.Fatalf("declined Quantum acquired off-turn permission: %+v", o)
		}
	}
}

func TestCR702194bSpellTeamworkValidSA(t *testing.T) {
	teamworkQuantumFlash(t)
	_, assistant := teamworkValidSACards(t)
	paidTokens := teamworkAssistantTokens(t, assistant, true)
	declinedTokens := teamworkAssistantTokens(t, assistant, false)
	if paidTokens != 1 || declinedTokens != 0 || paidTokens == declinedTokens {
		t.Fatalf("CR 702.194b: Virtual Assistant Robot tokens paid=%d declined=%d, want 1/0", paidTokens, declinedTokens)
	}
	t.Logf("CR 702.194b: Quantum Reduction off-turn Teamwork only; Virtual Assistant Spell.Teamwork Robot tokens paid=%d declined=%d", paidTokens, declinedTokens)
}

func teamworkAssistantTokens(t *testing.T, assistant *cards.Card, pay bool) int {
	t.Helper()
	e, _, reg := conspireEngine(t, "Go Nuts!")
	va := e.G.AddObject(assistant, 0)
	va.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), va.ID))
	a := seedBattlefield(t, e, reg, "Goblin Piker")
	b := seedBattlefield(t, e, reg, "Grizzly Bears")
	spell := searchMoveByName(t, e, "Go Nuts!", state.ZHand)
	if e.G.Obj(va.ID).Zone != state.ZBattlefield || e.G.Obj(spell).Zone != state.ZHand {
		t.Fatal("precondition: Assistant on battlefield and Go Nuts! in hand")
	}
	addMana(t, e, 0, "G")
	submitChoices(t, e, castOptMode(t, e.Pending().Options, spell, "teamworked").Index)
	d := teamworkAskOptions(t, e)
	if !d.AllowNone || d.MinSum != 3 || len(d.Options) < 2 {
		t.Fatalf("precondition: payable/declinable Teamwork ask: %+v", d)
	}
	if pay {
		submitChoices(t, e, teamworkOption(t, d, a), teamworkOption(t, d, b))
	} else {
		submitChoices(t, e)
	}
	finishTeamworkAnnouncement(t, e)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || (o.CastFlags&state.FlagTeamworkPaid != 0) != pay {
		t.Fatalf("precondition: completed cast/provenance after pay=%v: %+v", pay, o)
	}
	// CR 603.5: the optional choice is made when the triggered ability
	// RESOLVES, not when it is queued. A declined cost queues nothing.
	if pay {
		if len(e.G.Stack) != 2 || e.G.Obj(e.G.Stack[1]).Source != va.ID {
			t.Fatalf("paid cast did not queue Assistant's trigger: stack=%v", e.G.Stack)
		}
		ask := passPriorityUntil(t, e, decision.KTriggerOptional)
		submitChoices(t, e, optionIndexOfKind(t, ask, "yes"))
	} else if len(e.G.Stack) != 1 {
		t.Fatalf("declined cast queued Assistant's trigger: stack=%v", e.G.Stack)
	}
	// Resolve the Assistant trigger only. Go Nuts! itself remains on the
	// stack; its fight rider has a separate resolution-time choice unrelated
	// to the Spell.Teamwork trigger this test measures.
	for i := 0; i < 20 && len(e.G.Stack) > 1; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision while resolving Assistant trigger: %+v", d)
		}
		submitChoicePass(t, e)
	}
	if len(e.G.Stack) != 1 || e.G.Stack[0] != spell {
		t.Fatalf("precondition: Assistant trigger should resolve above Go Nuts!: stack=%v", e.G.Stack)
	}
	tokens := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && strings.Contains(o.Face().Name, "Robot") {
			tokens++
		}
	}
	return tokens
}
