package effects

import (
	"strings"

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
// rules/parent_target_nearest_test.go).
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

// ParentLinkRide returns a deep copy of the walk's parent-link record and the
// pending in-walk link answer (parentLinks, linkAnswer, linkAnswered), so the
// resolution machinery -- which cannot see these unexported fields -- can carry
// them across a suspension. The record is otherwise scoped to one Resolve walk
// (Resolve's defer), so a resolution that suspends at a later link and re-enters
// with a fresh Ctx would lose it and fall back to the root's targets. The
// answered flag is returned separately so an empty (Min-0) parent survives the
// ride as a RECORDED empty, not as "unset".
func (c *Ctx) ParentLinkRide() ([][]state.Target, []state.Target, bool) {
	links := make([][]state.Target, len(c.parentLinks))
	for i, ts := range c.parentLinks {
		links[i] = copyTargets(ts)
	}
	if !c.linkAnswered {
		return links, nil, false
	}
	return links, copyTargets(c.linkAnswer), true
}

// ResumeParentLinks re-binds a ride captured by ParentLinkRide onto a rebuilt
// Ctx, so a later untargeted link's ParentTarget/ParentTargeted still names the
// NEAREST targeting ancestor (parentLinkTargets) rather than falling back to
// Ctx.Targets, the root's list. Each recorded entry is copied so the Ctx owns
// its own storage; a recorded empty (Min-0) entry stays a recorded empty.
func (c *Ctx) ResumeParentLinks(links [][]state.Target, answer []state.Target, answered bool) {
	if len(links) > 0 {
		c.parentLinks = make([][]state.Target, len(links))
		for i, ts := range links {
			c.parentLinks[i] = copyTargets(ts)
		}
	}
	if answered {
		c.linkAnswer, c.linkAnswered = copyTargets(answer), true
	}
}

// rememberChosenTargets applies Forge's handleRemembering after an SA body.
// When there was no pre-ask, the root SA's choices are the targeting context
// captured on Ctx and resolved through Defined.
func rememberChosenTargets(h Host, c *Ctx, sa *cards.SA, targets []state.Target, preAsked bool) {
	if !strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberTargets)), "True") ||
		!TargetsOf(sa).Has(TgtValidPresent) {
		return
	}
	// These two bodies already record only the targets actually destroyed or
	// moved, not the complete chosen set. Dispatch identity is enough to
	// exclude them here: no per-walk Ctx cursor is needed, even for a nested
	// Resolve or a body that returns early.
	code := sa.CompiledAPI()
	if code == cards.APIUnknown {
		code = cards.APICodeForName(sa.API)
	}
	if code == cards.APIDestroy || code == cards.APIChangeZone {
		return
	}
	// Animate's RememberAnimated$ records the affected set, its historical
	// behavior when both flags are present; do not also add chosen targets.
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberAnimated)), "True") {
		return
	}
	if !preAsked {
		targets = Defined(h, c, sa)
	}
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKForgetOtherTargets)), "True") {
		c.Remembered = nil
		clearEventRemembered(h, c)
	}
	for _, target := range targets {
		rememberTarget(h, c, target)
	}
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
	if !TargetsOf(sa).Has(TgtValidPresent) {
		return
	}
	c.parentLinks = append(c.parentLinks, copyTargets(answer))
}
