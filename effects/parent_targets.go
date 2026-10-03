package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Forge's ParentTarget / ParentTargeted referents (AbilityUtils
// getDefinedCards' "ParentTarget" arm and the ParentTargeted$ count head)
// read SpellAbility.getParentTargetingCard: the NEAREST ancestor in the
// SubAbility$ chain that targets, not the root. Flourishing Grapple's
// untargeted DBDamage link (`DamageSource$ ParentTarget`, `NumDmg$ X` with
// X = ParentTargeted$CardPower) names DBPump's "target creature you
// control", while the root Animate targets the opponent's creature; reading
// the resolution's Ctx.Targets (the root's list) made the opponent's
// creature the source and its power the amount. Chandra's Revolution's
// "That land doesn't untap", Homesickness's and Stunning Shot's stun
// counters, Glyph of Delusion's Animate, Rhino's trample grant and Cruel
// Entertainment's Controller$ are the same shape (the census in
// rules/parent_target_links_census_test.go).
//
// The walk records each targeting link's answered set as it dispatches
// (Resolve's recordParentLink), so a later link reads the latest record and
// falls back to Ctx.Targets -- the root's own targets, or the depth-1 body
// the placement ask covered -- when no targeting link ran before it in this
// walk. A link's own targets are recorded AFTER its body runs, so a
// targeting link's own ParentTarget (Fight for the Throne's DBFight
// `Defined$ ParentTarget`, Intruder's Inquisition's DBDamage) still names
// its parent.
//
// One deliberate difference from Forge: a targeting link that chose ZERO
// targets (a Min-0 "up to one target" elected none) is recorded as an EMPTY
// parent, where Forge's isTargetingAnyCard walk skips past it to an earlier
// ancestor -- which would put Stunning Shot's stun counter on the creature
// that got the +1/+1 counters. The Oracle text ("put a stun counter on it")
// names the link's own target, so nothing is affected.
//
// Scope: the record lives for one Resolve walk. A resolution that suspends
// at a LATER link and re-enters there with a fresh Ctx loses the in-walk
// record and falls back to Ctx.Targets, the pre-existing behaviour.

// parentLinkTargets is the ParentTarget/ParentTargeted binding for the link
// resolving now.
func parentLinkTargets(c *Ctx) []state.Target {
	if n := len(c.parentLinks); n > 0 {
		return c.parentLinks[n-1]
	}
	return c.Targets
}

// noteLinkAnswer is how a body that consumes its OWN target answer (rather
// than through Resolve's generic pre-ask) hands that answer to the recorder.
func noteLinkAnswer(c *Ctx, ts []state.Target) {
	c.linkAnswer, c.linkAnswered = ts, true
}

// recordParentLink records the targets the just-dispatched link chose.
// fromPreAsk is the generic pre-ask's answer (nil when the pre-ask did not
// handle the link); a body-consumed answer (noteLinkAnswer) is taken
// otherwise. A link that declares no ValidTgts$ never records.
func recordParentLink(c *Ctx, sa *cards.SA, fromPreAsk []state.Target, preAsked bool) {
	answer, answered := fromPreAsk, preAsked
	if c.linkAnswered {
		if !answered {
			answer, answered = c.linkAnswer, true
		}
		c.linkAnswer, c.linkAnswered = nil, false
	}
	if !answered {
		return
	}
	if _, targeted := sa.Param(cards.PKValidTgts); !targeted {
		return
	}
	c.parentLinks = append(c.parentLinks, copyTargets(answer))
}
