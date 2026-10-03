package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// A CR 616.1 damage-replacement order choice posed while a spell resolves
// parks that resolution on e.resume. The answer (handleReplacement) applies
// the chosen DB$ ReplaceEffect body -- and the recomputed lone survivor --
// OUTSIDE any resolution pass, with the parked frame still on e.resume. Each
// body's effects.Resolve walk then read Suspended() as its OWN suspension:
// it stopped after the head, skipping the body's SubAbility$ chain, and
// reported a continuation frame into e.contChain that no pass owns. The live
// engine carried those frames to the next priority window while a clone
// (which never copies the per-pass scratch) read an empty chain: paymirror's
// control route, commander4-fresh seed 4130 seq 7213 (Solphim, Mayhem
// Dominus + Ojer Axonil answering Razorkin Needlehead's trigger damage),
// "control|mismatch|state_differs|contChain.len".

const parkedOrderDoubler = "Name:Doubler\nManaCost:2 R\nTypes:Enchantment\n" +
	"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Opponent | ReplaceWith$ DmgTwice | Description$ Double it.\n" +
	"SVar:DmgTwice:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n" +
	"SVar:X:ReplaceCount$DamageAmount/Twice\nOracle:x\n"

// parkedOrderAmplifier's body carries a SubAbility$ rider, so a truncated
// walk is visible as a missing life gain.
const parkedOrderAmplifier = "Name:Amplifier\nManaCost:2 R\nTypes:Enchantment\n" +
	"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Opponent | ReplaceWith$ DmgPlus1 | Description$ Plus one, then gain 1 life.\n" +
	"SVar:DmgPlus1:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X | SubAbility$ DBGain\n" +
	"SVar:DBGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\n" +
	"SVar:X:ReplaceCount$DamageAmount/Plus.1\nOracle:x\n"

const parkedOrderZap = "Name:Zap\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ DealDamage | Defined$ Player.Opponent | NumDmg$ 1 | SpellDescription$ Zap each opponent.\nOracle:x\n"

func TestReplacementOrderAnswerRunsBodyChainOutsideParkedResolution(t *testing.T) {
	tapeLegacyOnly(t)
	for _, first := range []string{"Doubler", "Amplifier"} {
		t.Run(first+"_first", func(t *testing.T) {
			e, cfg, _ := newFixtureDeck(t, 91, parkedOrderZap, parkedOrderDoubler, parkedOrderAmplifier)
			doubler := putCreature(t, e, 0, parkedOrderDoubler)
			amplifier := putCreature(t, e, 0, parkedOrderAmplifier)
			addMana(t, e, 0, "R")
			life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
			castCardNow(t, e, "Zap")
			for i := 0; i < 16; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no pending decision while resolving Zap")
				}
				if d.Kind == decision.KReplacement {
					break
				}
				if d.Kind != decision.KPriority {
					t.Fatalf("unexpected %s decision %q before the order choice", d.Kind, d.Prompt)
				}
				submitChoices(t, e, passIndex(t, d))
			}
			if e.resume == nil {
				t.Fatal("the order choice did not park Zap's resolution (e.resume nil): the fixture no longer reaches the shape")
			}
			pick := doubler
			if first == "Amplifier" {
				pick = amplifier
			}
			pickOption(t, e, pick)
			if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
				t.Fatalf("after the order answer pending = %+v, want priority", d)
			}
			if n := len(e.contChain); n != 0 {
				t.Fatalf("contChain holds %d frame(s) at the priority window after the answer; a body applied outside a resolution pass leaked its continuation report", n)
			}
			want := int32(3) // (1*2)+1
			if first == "Amplifier" {
				want = 4 // (1+1)*2
			}
			if got := life1 - e.G.Players[1].Life; got != want {
				t.Fatalf("opponent lost %d life, want %d", got, want)
			}
			if got := e.G.Players[0].Life - life0; got != 1 {
				t.Fatalf("seat 0 gained %d life, want 1: the Amplifier body's SubAbility$ rider was skipped", got)
			}
			if o := e.G.Obj(spellID(e, "Zap")); o == nil || o.Zone == state.ZStack {
				t.Fatal("Zap is still on the stack after its damage settled")
			}
			if c := e.Clone(); len(c.contChain) != len(e.contChain) {
				t.Fatalf("clone contChain %d vs live %d", len(c.contChain), len(e.contChain))
			}
			replayCheck(t, e, cfg)
		})
	}
}

// spellID is the object id of the card named name, wherever it now is.
func spellID(e *Engine, name string) state.ObjID {
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Face() != nil && o.Face().Name == name {
			return o.ID
		}
	}
	return 0
}
