package rules

import (
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

// spellID is the object id of the card named name, wherever it now is.
func spellID(e *Engine, name string) state.ObjID {
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Face() != nil && o.Face().Name == name {
			return o.ID
		}
	}
	return 0
}
