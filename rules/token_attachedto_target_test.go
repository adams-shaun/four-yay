package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// token_attachedto_target_test.go pins the target set an AttachedTo$ selector
// on a `DB$ Token` sub-ability reads: the targets of the ability CARRYING the
// param, not the root/chained-union list (effects/context.go's
// definedSpecTargeted / definedSpecThisTargetedCard, via Ctx.PickedTargets).
//
// The WOE/WOC Role cycle is the corpus shape: a spell's root targets one
// permanent (a player, a destroyed permanent, a damaged creature) and its
// DBToken sub carries its OWN ValidTgts$ plus AttachedTo$ ThisTargetedCard or
// AttachedTo$ Targeted. Before the fix the attach read the root's targets, the
// CR 303.4g Aura gate found no legal bearer, and the Role was never created.
// The fixtures are authored inline (never a corpus .txt, per the licensing
// rule); the corpus-wide carrier census is pinned separately by
// token_attachedto_census_test.go.

// roleTokenSrc is the Aura token every fixture below mints. K:Enchant:Creature
// is what rules/aura_entry.go's auraEnchantCandidates needs, so the CR 303.4g
// gate clears exactly when a legal bearer exists.
const roleTokenSrc = "Name:Wicked\nManaCost:no cost\nTypes:Enchantment Aura Role\nK:Enchant:Creature\nOracle:x\n"

const atTokenBearSrc = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// atThisTargetedSpell is the Eriette's Whisper / Cut In shape: the root's
// target is a player-or-creature distinct from the Role's own
// Creature.YouCtrl target.
const atThisTargetedSpell = "Name:Role Maker\nManaCost:3 R\nTypes:Sorcery\n" +
	"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1 | SubAbility$ DBToken\n" +
	"SVar:DBToken:DB$ Token | TokenScript$ role_wicked | TokenOwner$ You | AttachedTo$ ThisTargetedCard | ValidTgts$ Creature.YouCtrl\n" +
	"Oracle:x\n"

// atTargetedSpell is the Witch's Mark / Besotted Knight shape: `AttachedTo$
// Targeted` on a sub that carries its OWN ValidTgts$.
const atTargetedSpell = "Name:Role Maker T\nManaCost:3 R\nTypes:Sorcery\n" +
	"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1 | SubAbility$ DBToken\n" +
	"SVar:DBToken:DB$ Token | TokenScript$ role_wicked | TokenOwner$ You | AttachedTo$ Targeted | ValidTgts$ Creature.YouCtrl\n" +
	"Oracle:x\n"

// atInheritSpell is the Monstrous Rage / Royal Treatment shape: the Token sub
// carries NO ValidTgts$, so `AttachedTo$ Targeted` inherits the root's target
// (the pumped creature).
const atInheritSpell = "Name:Role Maker I\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +2 | SubAbility$ DBToken\n" +
	"SVar:DBToken:DB$ Token | TokenScript$ role_wicked | TokenOwner$ You | AttachedTo$ Targeted\n" +
	"Oracle:x\n"

// atRememberedSpell is the MSH U.S.Agent, John Walker shape: the Token sub
// remembers the token it minted (RememberTokens$ True) and the chained Attach
// fastens it to the source (Defined$ Self).
const atRememberedSpell = "Name:Shield Bearer\nManaCost:3 W\nTypes:Creature Human\nPT:3/2\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigToken\n" +
	"SVar:TrigToken:DB$ Token | TokenScript$ sturdy_shield | TokenOwner$ You | RememberTokens$ True | SubAbility$ DBAttach\n" +
	"SVar:DBAttach:DB$ Attach | Object$ Remembered | Defined$ Self\n" +
	"Oracle:x\n"

const shieldTokenSrc = "Name:Sturdy Shield\nManaCost:no cost\nTypes:Artifact Equipment\nOracle:x\n"

// atTokenWithName returns the ObjID of the permanent named name controlled by
// seat p, failing if it is absent (so a vacuous board cannot pass silently).
func atTokenWithName(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("no battlefield permanent named %q under seat %d", name, p)
	return 0
}

// atCastAndAnswerChainTargets casts spell (already funded and driven to a
// pending priority) and answers the root ask with rootPick, then the sub ask
// with subPick -- both function(option) -> bool selectors over the offered
// options. It fails if the sub ask never arrives, which is itself the
// precondition every caller's assertion depends on.
func atCastAndAnswerChainTargets(t *testing.T, e *Engine, spell state.ObjID,
	rootPick, subPick func(decision.Option) bool) {
	t.Helper()
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == spell {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("spell not offered for cast: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind == "cast_sub" {
		t.Fatalf("root target ask = %+v", d)
	}
	rp := -1
	for _, o := range d.Options {
		if rootPick(o) {
			rp = o.Index
		}
	}
	if rp < 0 {
		t.Fatalf("root target not offered: %+v", d.Options)
	}
	submitChoices(t, e, rp)

	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "cast_sub" {
		t.Fatalf("the chained sub target was not pre-asked at cast time: %+v", d)
	}
	sp := -1
	for _, o := range d.Options {
		if subPick(o) {
			sp = o.Index
		}
	}
	if sp < 0 {
		t.Fatalf("sub target not offered: %+v", d.Options)
	}
	submitChoices(t, e, sp)
	drainNoPostAsk(t, e)
}

// atAssertAttached asserts obj is attached to bearer -- the PRECONDITION the
// whole test turns on.
func atAssertAttached(t *testing.T, e *Engine, obj, bearer state.ObjID) {
	t.Helper()
	if o := e.G.Obj(obj); o == nil || o.AttachedTo != bearer {
		t.Fatalf("token %d AttachedTo = %v, want %d", obj, e.G.Obj(obj).AttachedTo, bearer)
	}
	if got := e.G.Obj(obj).Zone; got != state.ZBattlefield {
		t.Fatalf("token zone = %s, want battlefield", got)
	}
}

// TestTokenAttachedToThisTargetedCardUsesItsOwnTarget: the sub's own
// ValidTgts$ target is the bearer, not the root's (different) damage target.
func TestTokenAttachedToThisTargetedCardUsesItsOwnTarget(t *testing.T) {
	t.Parallel()
	e, cfg, spell := newFixtureDeckWithOpponentCard(t, 301, atThisTargetedSpell, atTokenBearSrc, atTokenBearSrc)
	cfg.Tokens = fixtureTokenMap(cfg.Tokens)
	e.G.Tokens = cfg.Tokens
	e.G.Tokens["role_wicked"] = card(t, roleTokenSrc)
	myBear := moveSeeded(t, e, 0, atTokenBearSrc, state.ZBattlefield)
	oppBear := moveSeeded(t, e, 1, atTokenBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRRR")
	e.Advance()

	atCastAndAnswerChainTargets(t, e, spell,
		func(o decision.Option) bool { return o.Obj == oppBear },
		func(o decision.Option) bool { return o.Obj == myBear })

	role := atTokenWithName(t, e, 0, "Wicked")
	atAssertAttached(t, e, role, myBear)
	replayCheck(t, e, cfg)
}

// TestTokenAttachedToTargetedUsesItsOwnTarget: `AttachedTo$ Targeted` on a
// sub with its own ValidTgts$ reads that sub's target, not the chain union.
func TestTokenAttachedToTargetedUsesItsOwnTarget(t *testing.T) {
	t.Parallel()
	e, cfg, spell := newFixtureDeckWithOpponentCard(t, 302, atTargetedSpell, atTokenBearSrc, atTokenBearSrc)
	cfg.Tokens = fixtureTokenMap(cfg.Tokens)
	e.G.Tokens = cfg.Tokens
	e.G.Tokens["role_wicked"] = card(t, roleTokenSrc)
	myBear := moveSeeded(t, e, 0, atTokenBearSrc, state.ZBattlefield)
	oppBear := moveSeeded(t, e, 1, atTokenBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "RRRR")
	e.Advance()

	atCastAndAnswerChainTargets(t, e, spell,
		func(o decision.Option) bool { return o.Obj == oppBear },
		func(o decision.Option) bool { return o.Obj == myBear })

	role := atTokenWithName(t, e, 0, "Wicked")
	atAssertAttached(t, e, role, myBear)
	replayCheck(t, e, cfg)
}

// TestTokenAttachedToTargetedInheritsParentWhenSubHasNoTargets: a sub with no
// ValidTgts$ of its own keeps the root's target (the regression the per-target
// preference must not break).
func TestTokenAttachedToTargetedInheritsParentWhenSubHasNoTargets(t *testing.T) {
	t.Parallel()
	e, cfg, spell := newFixtureDeck(t, 303, atInheritSpell, atTokenBearSrc)
	cfg.Tokens = fixtureTokenMap(cfg.Tokens)
	e.G.Tokens = cfg.Tokens
	e.G.Tokens["role_wicked"] = card(t, roleTokenSrc)
	myBear := moveSeeded(t, e, 0, atTokenBearSrc, state.ZBattlefield)
	addMana(t, e, 0, "R")
	e.Advance()

	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == spell {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("spell not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("root target ask = %+v", d)
	}
	sub := -1
	for _, o := range d.Options {
		if o.Obj == myBear {
			sub = o.Index
		}
	}
	if sub < 0 {
		t.Fatalf("bear not offered: %+v", d.Options)
	}
	submitChoices(t, e, sub)
	drainNoPostAsk(t, e)

	role := atTokenWithName(t, e, 0, "Wicked")
	atAssertAttached(t, e, role, myBear)
	replayCheck(t, e, cfg)
}

// TestTokenRememberedAttachSelfAttaches: the MSH U.S.Agent shape -- a Token
// sub with RememberTokens$ True feeding a chained `Attach | Object$
// Remembered | Defined$ Self` must fasten the minted token to the source.
func TestTokenRememberedAttachSelfAttaches(t *testing.T) {
	t.Parallel()
	e, cfg, spell := newFixtureDeck(t, 304, atRememberedSpell)
	cfg.Tokens = fixtureTokenMap(cfg.Tokens)
	e.G.Tokens = cfg.Tokens
	e.G.Tokens["sturdy_shield"] = card(t, shieldTokenSrc)
	bearer := moveSeeded(t, e, 0, atRememberedSpell, state.ZBattlefield)
	_ = spell
	// The ETB trigger is queued by the entry; drive priority rounds until it
	// has been put on the stack and resolved (bounded, so a never-queued
	// trigger fails the token assertion below instead of hanging).
	for i := 0; i < 20 && len(e.G.Stack) == 0; i++ {
		e.Advance()
	}
	passUntilStackEmpty(t, e, 20)

	shield := atTokenWithName(t, e, 0, "Sturdy Shield")
	atAssertAttached(t, e, shield, bearer)
	replayCheck(t, e, cfg)
}
