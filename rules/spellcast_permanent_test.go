package rules

// The "whenever you cast a permanent spell" trigger family (Mode$ SpellCast
// with ValidCard$ Permanent*): trigmatch.spellCastMatches evaluates ValidCard$ through
// trigmatch.SpellCastPermanentSpec, which rewrites the leading `Permanent` base token
// of every comma-alternative to `PermanentCard` -- the base whose
// matchesBase/matchesCompiledBase cases read a permanent SPELL on the stack
// (CR 109.2: artifact, creature, enchantment, planeswalker and battle spells
// are permanent spells). Before that rewrite the zone-blind bare `Permanent`
// base (`o.Zone == ZBattlefield`) kept every carrier in this family dead.
//
// Fixtures are inline per the licensing rule (never a .cards/ .txt); the real
// corpus carriers' T:/SVar: lines are copied verbatim off .cards/cardsfolder.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// defilerOfInstinctSrc is the real corpus carrier
// .cards/cardsfolder/d/defiler_of_instinct.txt's SpellCast trigger line and
// its SVar body, VERBATIM (the card's S:Mode$ OptionalCost static is a
// different primitive -- optional additional cost -- and is not part of this
// pin, so the fixture omits it).
const defilerOfInstinctSrc = "Name:Defiler of Instinct\nManaCost:2 R R\nTypes:Creature Phyrexian Kavu\nPT:4/4\nK:First Strike\n" +
	"T:Mode$ SpellCast | ValidCard$ Permanent.Red | ValidActivatingPlayer$ You | Execute$ TrigDamage | TriggerZones$ Battlefield | TriggerDescription$ Whenever you cast a red permanent spell, CARDNAME deals 1 damage to any target.\n" +
	"SVar:TrigDamage:DB$ DealDamage | ValidTgts$ Any | NumDmg$ 1\n" +
	"Oracle:Whenever you cast a red permanent spell, CARDNAME deals 1 damage to any target.\n"

// archmageOfEchoesSrc is the real corpus carrier
// .cards/cardsfolder/a/archmage_of_echoes.txt's SpellCast trigger line and
// its SVar body, VERBATIM -- the comma-alternative ValidCard$ spelling
// (`Permanent.Faerie,Permanent.Wizard`), the grammar the per-alternative
// rewrite exists for.
const archmageOfEchoesSrc = "Name:Archmage of Echoes\nManaCost:4 U\nTypes:Creature Faerie Wizard\nPT:4/4\nK:Flying\nK:Ward:2\n" +
	"T:Mode$ SpellCast | ValidCard$ Permanent.Faerie,Permanent.Wizard | ValidActivatingPlayer$ You | Execute$ TrigCopySpell | TriggerZones$ Battlefield | TriggerDescription$ Whenever you cast a Faerie or Wizard permanent spell, copy it. (The copy becomes a token.)\n" +
	"SVar:TrigCopySpell:DB$ CopySpellAbility | Defined$ TriggeredSpellAbility\n" +
	"Oracle:Whenever you cast a Faerie or Wizard permanent spell, copy it. (The copy becomes a token.)\n"

// tecutlanSpellCastSrc is the real corpus carrier
// .cards/cardsfolder/b/brasss_tunnel_grinder_tecutlan_the_searing_rift.txt's
// BACK face (Tecutlan, the Searing Rift) SpellCast trigger line and its SVar
// body, VERBATIM, attached to a plain artifact so the front face's other
// triggers stay out of the pin. Its ValidSA$
// Spell.ManaFromCard.StrictlySelf is unread (both predicates unknown, fail
// closed), so the trigger stays INERT after the ValidCard$ fix -- the
// boundary this file pins executable.
const tecutlanSpellCastSrc = "Name:Tecutlan, the Searing Rift\nManaCost:2 R\nTypes:Legendary Artifact\n" +
	"T:Mode$ SpellCast | ValidCard$ Permanent | ValidSA$ Spell.ManaFromCard.StrictlySelf | ValidActivatingPlayer$ You | Execute$ TrigDiscover | TriggerDescription$ Whenever you cast a permanent spell using mana produced by CARDNAME, discover X, where X is that spell's mana value.\n" +
	"SVar:TrigDiscover:DB$ Discover | Num$ TriggeredSpellAbility$CardManaCostLKI\n" +
	"Oracle:x\n"

// myriadPoolsSpellCastSrc is the real corpus carrier
// .cards/cardsfolder/t/the_everflowing_well_the_myriad_pools.txt's BACK face
// (The Myriad Pools) SpellCast trigger line and its SVar body, VERBATIM,
// attached to a plain artifact -- the second still-inert ValidSA$ carrier.
const myriadPoolsSpellCastSrc = "Name:The Myriad Pools\nManaCost:2 U\nTypes:Legendary Artifact\n" +
	"T:Mode$ SpellCast | ValidCard$ Permanent | ValidSA$ Spell.ManaFromCard.StrictlySelf | ValidActivatingPlayer$ You | Execute$ TrigCopy | TriggerDescription$ Whenever you cast a permanent spell using mana produced by CARDNAME, up to one other target permanent you control becomes a copy of that spell until end of turn.\n" +
	"SVar:TrigCopy:DB$ Clone | Defined$ TriggeredCardLKICopy | CloneTarget$ Targeted | ValidTgts$ Permanent.YouCtrl+Other | TargetMin$ 0 | TargetMax$ 1 | TgtPrompt$ Select up to one other target permanent you control | Duration$ UntilEndOfTurn\n" +
	"Oracle:x\n"

// Spell partners for the fixtures.
const redGoblinSrc = "Name:Red Goblin\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\n" +
	"Oracle:x\n"
const blueBirdSrc = "Name:Blue Bird\nManaCost:1 U\nTypes:Creature Bird\nPT:1/1\n" +
	"Oracle:x\n"
const faerieWizardSrc = "Name:Test Faerie\nManaCost:1 U\nTypes:Creature Faerie Wizard\nPT:1/1\n" +
	"Oracle:x\n"

// delayedRegistrarSrc mirrors the real Mistrise Village registration shape
// (.cards/cardsfolder/m/mistrise_village.txt's activated
// `A:AB$ DelayedTrigger | Mode$ SpellCast | ValidCard$ Card | ...` line,
// Cost$ and ThisTurn$/Static$ dropped): an ACTIVATED DelayedTrigger SA whose
// body carries `Mode$ SpellCast | ValidCard$ Permanent`. The corpus has no
// delayed SpellCast carrier whose ValidCard$ names Permanent (measured -- the
// fx41/fx42 synthetic-pin convention), so this synthetic registration pins
// the eventDelayedSpellCastMatches mirror: a permanent spell cast fires it,
// an instant does not.
const delayedRegistrarSrc = "Name:Delayed SpellCast Registrar\nManaCost:3\nTypes:Artifact\n" +
	"A:AB$ DelayedTrigger | Cost$ T | Mode$ SpellCast | ValidCard$ Permanent | ValidActivatingPlayer$ You | Execute$ TrigGain | SpellDescription$ The next permanent spell you cast makes you gain 1 life.\n" +
	"SVar:TrigGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\n" +
	"Oracle:x\n"

// TestDefilerOfInstinctFiresOnRedPermanentSpells is the qualifier leaf: the
// rewritten `PermanentCard.Red` reads the .Red qualifier against the stack
// spell's printed face, so a red creature spell fires the trigger and a blue
// one does not.
func TestDefilerOfInstinctFiresOnRedPermanentSpells(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 53, []string{defilerOfInstinctSrc, redGoblinSrc, blueBirdSrc}, nil)
	defiler := moveSeeded(t, e, 0, defilerOfInstinctSrc, state.ZBattlefield)
	addMana(t, e, 0, "R")
	castFirst(t, e, "cast")
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, defiler); got != 1 {
		t.Fatalf("Defiler of Instinct TriggerPush count after red creature cast = %d, want 1", got)
	}
	// A blue permanent spell: the qualifier must keep the trigger silent.
	addMana(t, e, 0, "UU")
	castFirst(t, e, "cast")
	drainTriggerAsks(t, e, 30)
	if got := pushCount(e, defiler); got != 1 {
		t.Fatalf("Defiler of Instinct TriggerPush count after blue creature cast = %d, want still 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestArchmageOfEchoesCommaAlternativesFire is the comma-alternative leaf:
// `Permanent.Faerie,Permanent.Wizard` fires on a Faerie Wizard spell -- each
// alternative's own leading token is rewritten -- and not on a plain Beast.
func TestArchmageOfEchoesCommaAlternativesFire(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 53, []string{archmageOfEchoesSrc, faerieWizardSrc, nonXBeastSrc}, nil)
	archmage := moveSeeded(t, e, 0, archmageOfEchoesSrc, state.ZBattlefield)
	addMana(t, e, 0, "UU")
	castFirst(t, e, "cast")
	drainTriggerAsks(t, e, 40)
	if got := pushCount(e, archmage); got != 1 {
		t.Fatalf("Archmage of Echoes TriggerPush count after Faerie Wizard cast = %d, want 1", got)
	}
	// A creature that is neither Faerie nor Wizard: silent.
	addMana(t, e, 0, "GG")
	castFirst(t, e, "cast")
	drainTriggerAsks(t, e, 40)
	if got := pushCount(e, archmage); got != 1 {
		t.Fatalf("Archmage of Echoes TriggerPush count after plain Beast cast = %d, want still 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestSpellCastPermanentValidSACarriersStayInert pins the scope boundary:
// Brass's Tunnel-Grinder's and The Everflowing Well's back-face SpellCast
// triggers carry the unread `ValidSA$ Spell.ManaFromCard.StrictlySelf`
// clause (both predicates unknown, fail closed), so they stay dead after the
// ValidCard$ fix -- on an X-cost permanent spell AND a plain one. Firing
// them is a separate ticket, not this one.
func TestSpellCastPermanentValidSACarriersStayInert(t *testing.T) {
	t.Parallel()
	for _, src := range []string{tecutlanSpellCastSrc, myriadPoolsSpellCastSrc} {
		e, cfg, _ := etbConfig(t, 61, []string{src, xCostHydraSrc, nonXBeastSrc}, nil)
		carrier := moveSeeded(t, e, 0, src, state.ZBattlefield)
		addMana(t, e, 0, "GGG")
		castFirst(t, e, "cast")
		if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
			t.Fatalf("X decision %+v", d)
		}
		submitChoices(t, e, 2)
		drainTriggerAsks(t, e, 30)
		addMana(t, e, 0, "GG")
		castFirst(t, e, "cast")
		drainTriggerAsks(t, e, 30)
		if got := pushCount(e, carrier); got != 0 {
			t.Fatalf("%s TriggerPush count after permanent casts = %d, want 0 (ValidSA$ Spell.ManaFromCard.StrictlySelf is unread and fails closed)", src, got)
		}
		replayCheck(t, e, cfg)
	}
}

// TestSpellCastPermanentDelayedMirrorFires is the synthetic delayed pin: an
// activated DelayedTrigger SA whose stored body carries
// `Mode$ SpellCast | ValidCard$ Permanent` (the Mistrise Village registration
// shape, ValidCard$ Permanent substituted -- the corpus has no delayed
// carrier of that shape today). eventDelayedSpellCastMatches mirrors
// trigmatch.spellCastMatches' clause grammar, so a permanent spell cast after the
// registration fires it exactly once and an instant does not.
func TestSpellCastPermanentDelayedMirrorFires(t *testing.T) {
	t.Parallel()
	e, cfg, _ := etbConfig(t, 71, []string{delayedRegistrarSrc, xCostHydraSrc, xBoltSrc}, nil)
	registrar := moveSeeded(t, e, 0, delayedRegistrarSrc, state.ZBattlefield)
	// Register: activate the artifact's DelayedTrigger ability (Cost$ T only).
	addMana(t, e, 0, "")
	castFirst(t, e, "ability")
	drainTriggerAsks(t, e, 20)
	if len(e.G.Delayed) != 1 {
		t.Fatalf("activated registration produced %d delayed triggers, want 1", len(e.G.Delayed))
	}
	if e.G.Delayed[0].EventMode != "SpellCast" {
		t.Fatalf("registration EventMode = %q, want SpellCast", e.G.Delayed[0].EventMode)
	}
	// A permanent spell: the delayed trigger fires (one-shot) -- a delayed
	// fire lands as a DelayedPush (events.Apply's DelayedPush case), not a
	// TriggerPush, so count both kinds.
	delayedPushes := func() int {
		n := 0
		for _, ev := range e.L.Events {
			if (ev.Kind == events.TriggerPush || ev.Kind == events.DelayedPush) && ev.Obj == registrar {
				n++
			}
		}
		return n
	}
	before := delayedPushes()
	addMana(t, e, 0, "GGG")
	castFirst(t, e, "cast")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 2)
	drainTriggerAsks(t, e, 30)
	if got := delayedPushes(); got != before+1 {
		t.Fatalf("delayed SpellCast push count after permanent cast = %d, want %d", got, before+1)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("one-shot registration not consumed: %+v", e.G.Delayed)
	}
	// ... and an instant never was "a permanent spell": a fresh registration
	// must stay silent on one. (The consumed registration cannot be reused,
	// so re-activate first -- untap the registrar the first Cost$ T consumed.)
	e.emit(events.Event{Kind: events.Untap, Obj: registrar})
	e.pending = nil
	e.priorityRound()
	castFirst(t, e, "ability")
	drainTriggerAsks(t, e, 20)
	if len(e.G.Delayed) != 1 {
		t.Fatalf("second registration missing: %+v", e.G.Delayed)
	}
	before = delayedPushes()
	addMana(t, e, 0, "RRR")
	castFirst(t, e, "cast")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("instant X decision %+v", d)
	}
	submitChoices(t, e, 2)
	drainTriggerAsks(t, e, 30)
	if got := delayedPushes(); got != before {
		t.Fatalf("delayed SpellCast push count after instant cast = %d, want still %d", got, before)
	}
	if len(e.G.Delayed) != 1 {
		t.Fatalf("instant cast consumed the registration: %+v", e.G.Delayed)
	}
	replayCheck(t, e, cfg)
}
