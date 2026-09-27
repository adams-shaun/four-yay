package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// answerPosedProbe is one frame-drop probe: while a resolving spell is
// suspended on the OUTER ask, the answer's own work poses an INNER competition
// of another kind; answering the inner ask must resume the suspended
// resolution exactly once (not leave the spell on the stack to re-resolve on
// every priority pass, the round-7 defect the entry-stage fix closed for
// replChoiceEntryOrder only).
//
// Every probe here uses the same reachable shape: a resolving sorcery's
// sacrifice Move (on the stack, e.resolvingObj set) is intercepted by two
// custom "Replaced" move rewriters, so the outer replChoiceMove is posed
// in-resolution. Choosing the rewriter whose ReplaceWith$ body emits the
// INNER event drives engine.go's replacement-body dispatch: a CounterChange
// body is the one kind that still gets its own replacement pass under
// applyingReplacement (engine.go:2558), so the inner AddCounter competition
// is the reliably reachable inner kind; the others are posed by the outer
// answer's own re-drive (token plan, life loop, updated composition).
type answerPosedOuter struct {
	// innerPick is the option index the probe answers the INNER competition
	// with (0 or 1, both tested).
	innerPick int
}

// answerPosedBoard seats Scales + Evolution (the non-commuting AddCounter
// pair) and a spare creature to sacrifice, plus the two custom move
// rewriters: one whose body places a +1/+1 counter (the inner AddCounter the
// Scales/Evolution pair competes over), one whose body is a plain ChangeZone.
func answerPosedAddCounterBoard(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	scales := tokenReplCorpusCard(t, "Hardened Scales")
	evolution := tokenReplCorpusCard(t, "Branching Evolution")
	// The outer rewriter whose answer body places the contested counter.
	ctrRewriter := card(t, "Name:Counter Rewriter\nTypes:Artifact Creature Golem\nPT:0/4\n"+
		"R:Event$ Moved | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card | ReplaceWith$ Ctr | Description$ x\n"+
		"SVar:Ctr:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1\nOracle:x\n")
	// A second non-Updated rewriter: its presence is what makes the OUTER
	// competition a choice (all-Updated matches would compose without asking).
	zoneRewriter := card(t, "Name:Zone Rewriter\nTypes:Enchantment\n"+
		"R:Event$ Moved | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card | ReplaceWith$ Ex | Description$ x\n"+
		"SVar:Ex:DB$ ChangeZone | Origin$ Graveyard | Destination$ Exile | Defined$ ReplacedCard\nOracle:x\n")
	sacTarget := card(t, "Name:Sacrifice Fodder\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	spell := card(t, "Name:Answer Posed Sacrifice\nManaCost:0\nTypes:Sorcery\n"+
		"A:SP$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Graveyard | SubAbility$ Rider | SpellDescription$ x\n"+
		"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
	e, cfg := tokenReplGame(t, seed, scales, evolution, ctrRewriter, zoneRewriter, sacTarget, spell)
	withStagedModifiers(t, e, scales, evolution)
	ctrID := moveSeededCard(t, e, 0, ctrRewriter, state.ZBattlefield)
	moveSeededCard(t, e, 0, zoneRewriter, state.ZBattlefield)
	fodder := moveSeededCard(t, e, 0, sacTarget, state.ZBattlefield)
	moveSeededCard(t, e, 0, spell, state.ZHand)
	for _, id := range []state.ObjID{ctrID, fodder} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %d not on the battlefield", id)
		}
	}
	return e, cfg, ctrID, fodder
}

// TestAnswerPosedAddCounterOrderResumesTheResolution: the outer
// replChoiceMove (the sacrifice, posed while the sorcery resolves) is
// answered with the counter rewriter; its answer body places a +1/+1 counter
// that Hardened Scales and Branching Evolution compete over, posing an inner
// replChoiceAddCounter during the answer. Answering the inner ask must resume
// the suspended sorcery exactly once and replay. Both inner answer orders.
func TestAnswerPosedAddCounterOrderResumesTheResolution(t *testing.T) {
	for _, tc := range []struct {
		name string
		pick int
		want int32
	}{{"scales-first", 0, 4}, {"evolution-first", 1, 3}} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, ctrID, fodder := answerPosedAddCounterBoard(t, 9101)
			addMana(t, e, 0, "")
			castSpellOption(t, e, "Answer Posed Sacrifice")
			innerAsks, outerAsks := 0, 0
			for i := 0; i < 60; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no decision while resolving")
				}
				pick := 0
				switch d.Kind {
				case decision.KPriority:
					if len(e.G.Stack) == 0 {
						i = 60
						continue
					}
					passPriorityOnce(t, e)
					continue
				case decision.KReplacement:
					switch {
					case strings.Contains(d.Prompt, "moves"):
						outerAsks++
						// Choose the counter rewriter's body.
						pick = optionForObj(d, ctrID)
					case strings.Contains(d.Prompt, "counters"):
						innerAsks++
						pick = tc.pick
					default:
						t.Fatalf("unexpected replacement ask %q", d.Prompt)
					}
				case decision.KTarget:
					pick = optionForObj(d, fodder)
					if pick < 0 {
						t.Fatalf("target ask does not offer the fodder: %+v", d.Options)
					}
				default:
					t.Fatalf("unexpected decision %+v", d)
				}
				if pick < 0 || pick >= len(d.Options) {
					t.Fatalf("decision does not offer option %d: %+v", pick, d.Options)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
					t.Fatal(err)
				}
			}
			if outerAsks != 1 || innerAsks != 1 {
				t.Fatalf("asked %d outer and %d inner order choices, want 1 and 1", outerAsks, innerAsks)
			}
			if len(e.G.Stack) != 0 {
				t.Fatalf("the sorcery never left the stack (depth %d)", len(e.G.Stack))
			}
			// Precondition + result: the rewriter is on the battlefield and
			// actually took the contested counters.
			o := e.G.Obj(ctrID)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: counter rewriter %d = %+v, want on the battlefield", ctrID, o)
			}
			if got := o.Counter("P1P1"); got != tc.want {
				t.Fatalf("counter rewriter has %d +1/+1 counters, want %d (1->2->4 vs 1->2->3)", got, tc.want)
			}
			resolves, gains := 0, 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.Resolve {
					resolves++
				}
				if ev.Kind == events.LifeChange && ev.Player == 0 && ev.Amount > 0 {
					gains++
				}
			}
			if resolves != 1 {
				t.Fatalf("the sorcery resolved %d times, want exactly 1", resolves)
			}
			if gains != 1 {
				t.Fatalf("the SubAbility$ rider fired %d times, want exactly 1", gains)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// unused references kept for the probe file's imports; the token/life probes
// below use them.
var _ = cards.Card{}

// tokenRewriterSrc is an authored non-commuting CreateToken rewriter: a
// script rewrite composed against a multiplier changes the result (the
// rewriter after the multiplier rewrites every duplicated mint, before it
// only the original), so a competition with a multiplier must ask.
func tokenRewriterSrc(name, script string) string {
	return "Name:" + name + "\nTypes:Enchantment\n" +
		"R:Event$ CreateToken | ActiveZones$ Battlefield | ValidPlayer$ You | ReplaceWith$ Rewrite | Description$ x\n" +
		"SVar:Rewrite:DB$ ReplaceToken | Type$ ReplaceToken | TokenScript$ " + script + "\n" +
		"Oracle:x\n"
}

// TestAnswerPosedTokenOrderResumesTheResolution: a resolving sorcery creates
// a token while Doubling Season and TWO script rewriters all apply, so the
// first CreateToken order competition is answered, its re-drive sees the
// remaining non-commuting pair and poses a SECOND competition from inside the
// answer. Answering the inner ask must resume the suspended sorcery exactly
// once. Both inner answer orders.
func TestAnswerPosedTokenOrderResumesTheResolution(t *testing.T) {
	for _, tc := range []struct {
		name string
		pick int
	}{{"first-rewriter", 0}, {"second-rewriter", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			ds := tokenReplCorpusCard(t, "Doubling Season")
			rw1 := card(t, tokenRewriterSrc("Token Rewriter One", "c_a_treasure_sac"))
			rw2 := card(t, tokenRewriterSrc("Token Rewriter Two", "c_a_clue_draw"))
			spell := card(t, "Name:Answer Posed Tokens\nManaCost:0\nTypes:Sorcery\n"+
				"A:SP$ Token | TokenAmount$ 1 | TokenScript$ c_a_powerstone | SubAbility$ Rider | SpellDescription$ x\n"+
				"SVar:Rider:DB$ GainLife | LifeAmount$ 1\nOracle:x\n")
			e, cfg := tokenReplGame(t, 9201, ds, rw1, rw2, spell)
			for _, c := range []*cards.Card{ds, rw1, rw2} {
				if o := e.G.Obj(moveSeededCard(t, e, 0, c, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: replacement source %q absent", c.Faces[0].Name)
				}
			}
			moveSeededCard(t, e, 0, spell, state.ZHand)
			addMana(t, e, 0, "")
			castSpellOption(t, e, "Answer Posed Tokens")
			tasks := 0
			for i := 0; i < 60; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no decision while resolving")
				}
				pick := 0
				switch d.Kind {
				case decision.KPriority:
					if len(e.G.Stack) == 0 {
						i = 60
						continue
					}
					passPriorityOnce(t, e)
					continue
				case decision.KReplacement:
					tasks++
					if tc.pick < len(d.Options) {
						pick = tc.pick
					}
				default:
					t.Fatalf("unexpected decision %+v", d)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
					t.Fatal(err)
				}
			}
			if tasks != 2 {
				t.Fatalf("asked %d token order choices, want 2 (outer + inner)", tasks)
			}
			if len(e.G.Stack) != 0 {
				t.Fatalf("the sorcery never left the stack (depth %d)", len(e.G.Stack))
			}
			// Precondition: the mint(s) landed on the battlefield and the chosen
			// rewriter really rewrote the plan (the Powerstone script was
			// replaced by a Treasure/Clue).
			made := 0
			for _, id := range e.G.Zone(state.ZBattlefield, 0) {
				if o := e.G.Obj(id); o != nil && o.IsToken {
					made++
				}
			}
			if made == 0 {
				t.Fatalf("precondition: no token entered; the rewriter did not apply")
			}
			if got := countTokensNamedOnSeat(t, e, 0, "Powerstone Token"); got != 0 {
				t.Fatalf("precondition: %d Powerstone Tokens survived; the rewriter did not rewrite the plan", got)
			}
			resolves, gains := 0, 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.Resolve {
					resolves++
				}
				if ev.Kind == events.LifeChange && ev.Player == 0 && ev.Amount > 0 {
					gains++
				}
			}
			if resolves != 1 || gains != 1 {
				t.Fatalf("the sorcery resolved %d times with %d rider gains, want 1 and 1", resolves, gains)
			}
			replayCheck(t, e, cfg)
		})
	}
}
