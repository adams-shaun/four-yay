// target_legal.go holds target legality and candidate construction: the target
// bounds (fixed/dynamic/X and the gift promise), the zones a target lives in,
// the stack-object kind tests, and the candidate walks plus their validity and
// controller filters.
// Split out of stack.go by a pure move (no rename, no behaviour change).
package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// targetBounds is the literal-only TargetMin$/TargetMax$ pair, compiled once
// (effects.TargetParams.BoundMin/BoundMax): a literal bound is honoured, an
// absent or dynamic one defaults to 1, then min < 0 -> 1, max < 1 -> 1,
// max < min -> min.
func targetBounds(sa *cards.SA) (int, int) {
	tp := effects.TargetsOf(sa)
	return tp.BoundMin, tp.BoundMax
}

// isLiteralBound reports whether a raw TargetMin$/TargetMax$ token is a
// plain signed integer.
func isLiteralBound(v string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(v))
	return err == nil
}

// targetBoundsDynamic reports whether either bound token is present and not
// a literal -- the only shape resolvedTargetBounds does extra work for, so a
// literal-only script never leaves the byte-identical fast path.
func targetBoundsDynamic(sa *cards.SA) bool {
	return effects.TargetsOf(sa).Has(effects.TgtBoundsDynamic)
}

// targetBoundCtx binds the effects numeric grammar to the asking player and
// the target declaration's source. The anchor follows what source IS: for a
// spell the stack object IS the card, so its face carries the SVar table
// (Kiora's Dismissal's SVar:X); for an ability or trigger wrapper
// (o.Card == nil, so Face() returns nil) the anchor is o.Source -- the
// source permanent every TriggerPush/AbilityPush stamps -- and ITS face's
// SVar table. No anchor (the object gone, or a sourceless wrapper) fails
// closed to the literal reader.
func (e *Engine) targetBoundCtx(p state.PlayerID, source state.ObjID) (*effects.Ctx, bool) {
	o := e.G.Obj(source)
	if o == nil {
		return nil, false
	}
	ctx := effects.NewCtxPtr(0, p, effects.CtxInit{})
	// A trigger's dynamic target bound reading the causing event (Vitality
	// Hunter's `TargetMax$ MaxTgts` with `SVar:MaxTgts:TriggerCount$Amount`,
	// task agent-20260919T190014Z): the trigger context recorded for this
	// stack wrapper carries TriggerAmount, so the bound reads the mark/damage
	// magnitude instead of degrading to the clamp's 1. Measured corpus: the
	// ONLY two TargetMax$ TriggerCount$Amount shapes (one inline, one behind
	// the MaxTgts SVar name) are Vitality Hunter's; every other dynamic bound
	// names a Count$ body targetBoundCtx's SVar table already resolves.
	if tc, ok := e.triggerContexts[source]; ok {
		ctx.TriggerContext = tc
		if tc.Reflexive {
			// A reflexive ability's bound may count what its spawning
			// resolution remembered ("return up to THAT MANY target cards":
			// TargetMax$ X over TriggerRemembered$Amount), which rides the
			// minted object's own Remembered (rules/reflexive.go).
			ctx.Remembered = append([]state.Target(nil), o.Remembered...)
			reflexiveCaptured(ctx)
		}
	}
	// The pending cast's own multikicker count (rules/cast.go's multikickAsk):
	// at the CR 601.2c announcement ask the pay-time CastInfo has not run
	// yet, so a TimesKicked bound (Comet Storm's TargetMin/Max$ TargetsNum)
	// would read 0 off the stack object. When the asking source IS the card
	// the pending cast is casting, seed the count the ask just settled --
	// exactly the `x` resolvedTargetBounds threads for a Count$xPaid bound.
	if pc := e.cast; pc != nil && pc.card == source {
		if pc.multikickSet {
			ctx.Kicker.TimesKicked = pc.multikickTimes
		}
		// The CHOSEN cast mode's kicked bit (Tear Asunder's kicked main SA is
		// TargetMin$ X | TargetMax$ X over SVar:X:Count$Kicked.0.1): the same
		// pre-payment gap TimesKicked closes, for the FlagKicked half. The
		// mode was settled when the cast OPTION was picked, before this ask.
		ctx.Kicker.PendingKicked = modeIsKicked(pc.mode)
	}
	if f := o.Face(); f != nil {
		ctx.Source = source
		effects.SetSVars(ctx, f.SVars)
		return ctx, true
	}
	src := e.G.Obj(o.Source)
	if src == nil {
		return nil, false
	}
	if f := src.Face(); f == nil {
		return nil, false
	}
	ctx.Source = o.Source
	// A mutated pile's under-card triggered ability (CR 702.140d) reads its
	// OWN face's SVar table, not the pile's top card's: Archipelagoe and
	// Nethroi, Apex of Death both bound their targeting with TargetMax$ X,
	// and the pile's top card can be any creature (with no X at all). The
	// owning face comes from the compiled trigger pointer; an ordinary
	// trigger's owning face is the top face, so nothing else moves. A
	// HAS-ALL-ABILITIES-OF wrapper (r3) is covered inside the recovery
	// functions themselves, so every caller shares the one read.
	if owned, ok := e.triggerLineSVars[source]; ok {
		effects.SetSVars(ctx, owned)
	} else if _, mf, ok := e.findTriggerForAbilityFace(o.Source, o.Ability); ok && mf != nil {
		effects.SetSVars(ctx, mf.SVars)
	} else if mf, ok := e.pileFaceForSA(o.Source, o.Ability); ok && mf != nil {
		// An activated ability of a MUTATED pile (CR 702.140d): the ask's SVar
		// bounds (TargetMin$/TargetMax$ X) resolve against the under-card's own
		// table, the same owning-face rule resolveTop's ability branch applies.
		effects.SetSVars(ctx, mf.SVars)
	} else {
		effects.SetSVars(ctx, src.Face().SVars)
	}
	return ctx, true
}

// resolvedTargetBounds is targetBounds extended to the dynamic bounds the
// corpus writes as TargetMax$ X / TargetMin$ X with an SVar body (212 raw
// TargetMax$ X lines / 209 files, 102 TargetMin$ X lines / 100 files -- the
// dominant shape is TargetMin$ 0 + TargetMax$ X, "return any number up to
// X"). A bound token that is a plain literal keeps targetBounds' reading
// byte-for-byte; a token that is PRESENT and not a literal resolves through
// the effects numeric grammar (NumResolved: an SVar name, an inline
// Count$/... expression, or the bare X bound to ctx.X), bound to the asking
// player and the source anchor targetBoundCtx builds. A present token the
// grammar cannot resolve (TargetMax$ Y, the MaxTgts family, a named SVar the
// face does not define) keeps today's semantics -- the parameter is dropped
// to the default 1 -- because Num's degrade-to-zero contract is correct for
// an effect amount ("the card did nothing") but wrong for a mandatory
// target MINIMUM (a TargetMin$ X degrading to 0 would let a mandatory spell
// resolve untargeted). x is the cast's settled {X} (pc.x at the CR 601.2c
// announcement ask, where the announce has already run) so a
// SVar:X:Count$xPaid bound reads the paid value; at a placement ask no X
// applies (a trigger was never paid an X) and 0 is correct there -- an
// xPaid body still finds the cast's value on the source permanent via
// count.go's provenance fallback. The clamp contract is targetBounds',
// applied AFTER resolution: min >= 0, max >= 1, max >= min.
func (e *Engine) resolvedTargetBounds(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) (int, int) {
	return e.resolvedTargetBoundsWithGift(p, source, sa, x, nil)
}

// resolvedTargetBoundsWithGift is the offer-gate variant: a non-nil promise
// override evaluates Count$PromisedGift as though that Gift election had been
// made, without changing the source object or the normal post-election reader.
func (e *Engine) resolvedTargetBoundsWithGift(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32, promised *bool) (int, int) {
	min, max := targetBounds(sa)
	if !targetBoundsDynamic(sa) {
		return min, max
	}
	ctx, ok := e.targetBoundCtx(p, source)
	if !ok {
		return min, max
	}
	ctx.X = x
	ctx.Kicker.PromisedGiftOverride = promised
	tp := effects.TargetsOf(sa)
	if tp.Min.Present && !isLiteralBound(tp.Min.Text) {
		if n, resolved := effects.NumTextResolvedStrict(e, ctx, tp.Min, 1); resolved {
			min = int(n)
		}
	}
	resolvedMax := false
	if tp.Max.Present && !isLiteralBound(tp.Max.Text) {
		if n, resolved := effects.NumTextResolvedStrict(e, ctx, tp.Max, 1); resolved {
			max = int(n)
			resolvedMax = true
		}
	}
	if min < 0 {
		min = 1
	}
	if max < 0 {
		max = 0
	}
	// A dynamic bound the grammar RESOLVED is honoured as written, zero
	// included. The "instead" idiom writes exactly that: Tear Asunder's
	// kicked main SA is TargetMin$ X | TargetMax$ X over
	// SVar:X:Count$Kicked.0.1, meaning "target nothing here, the chained sub
	// (Condition$ Kicked, SVar:Y:Count$Kicked.1.0) does the work". Clamping
	// that resolved 0 up to 1 asks for an artifact/enchantment the kicked
	// spell must not exile. Only an UNRESOLVED token -- and a literal, already
	// clamped by targetBounds -- keep the documented max >= 1 clamp; the
	// max < min clamp below still lifts a resolved 0 when a genuine minimum
	// is present (a bare TargetMax$ X announced 0, min defaulting to 1).
	if !resolvedMax && max < 1 {
		max = 1
	}
	if max < min {
		max = min
	}
	return min, max
}

// resolvedTargetMin is the Min half of resolvedTargetBounds, for resolveTop's
// N2 gate -- an ability or spell that MAY target zero things (a resolved
// TargetMin$ 0) and has none recorded resolves untargeted rather than
// fizzling. An unresolvable dynamic Min keeps the literal reader's default 1,
// so the N2 exemption never opens for a bound this build cannot price.
func (e *Engine) resolvedTargetMin(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) int {
	min, _ := e.resolvedTargetBounds(p, source, sa, x)
	return min
}

// targetZones resolves TgtZone$ (comma-separated) and TargetType$ into the
// zones to search for target options. TgtZone$ is the explicit zone
// declaration; TargetType$ (Forge) names the KIND of thing targeted, and
// when it names a stack object -- Spell, Instant, Sorcery, Activated,
// Triggered, SpellAbility -- the target lives on the stack. Mana Leak is
// scripted `TargetType$ Spell | ValidTgts$ Card` with NO TgtZone$ at all, so
// without reading TargetType$ the search defaulted to the battlefield and
// offered every permanent of every seat for a counterspell -- the bug this
// fixes (every counterspell in the corpus was inert, targeting a permanent
// so effCounter found o.Zone != state.ZStack and did nothing). The default
// remains the battlefield. An unknown TgtZone$ token is dropped (reviewer
// minor 4), but the battlefield default applies only when NEITHER source
// named a zone -- a typo'd TgtZone$ on a TargetType$ Spell card must not
// silently widen a stack target back to the battlefield.
//
// fb-20260916T024739Z-b89aea46: the remaining default is wrong for one more
// shape -- the Wrenn and Six ability (`AB$ ChangeZone | Origin$ Graveyard |
// Destination$ Hand | TargetMin$ 0 | TargetMax$ 1 | ValidTgts$ Land.YouOwn`,
// no TgtZone$). Its census searched the battlefield, offered a land already
// in play, and omitted the eligible graveyard card the prompt names -- and
// the offered battlefield land could never pass effChangeZone's own Origin$
// Graveyard resolution guard, so the ability could not do what it promises.
// For that one unambiguous shape the Origin$ implies the target zone; see
// originImpliedTargetZone for the four gates that admit it.
//
// The returned slice is shared and read-only: the TgtZone$/TargetType$ zones
// are compiled once (effects.TargetParams.Zones), and every fallback is a
// static single-zone slice, so the census and the recheck allocate nothing.
func targetZones(sa *cards.SA) []state.Zone {
	tp := effects.TargetsOf(sa)
	// TgtZone$'s known tokens in order, then the stack when TargetType$ names
	// a stack object: a stack-targeting TargetType$ adds the stack even when
	// no TgtZone$ is present (the counterspell shape) and even alongside a
	// TgtZone$ Battlefield for a spell-or-permanent effect (TgtZone$
	// Stack,Battlefield).
	zones := tp.Zones
	if len(zones) == 0 {
		// The graveyard-enchant Aura family (Animate Dead, Dance of the Dead;
		// Spellweaver Volute for instants) casts its kw:Enchant attach spell
		// (`SP$ Attach | ValidTgts$ Creature.inZoneGraveyard`, no TgtZone$, API
		// Attach with no Origin$, so the Origin$ route below cannot fire) and
		// the default battlefield census offered no candidate and withheld the
		// cast entirely. The inZone<X> words in the comma-split ValidTgts$
		// alternatives name the census zones directly. Deliberately narrow, the
		// same shape as the fb-20260916 Origin$ precedent below: Attach-only,
		// inZone-words-only -- every other API keeps its existing zone
		// resolution, so the ~37 corpus specs carrying non-battlefield inZone<X>
		// outside this API are untouched. An explicit Origin$ outranks this
		// inference even when the two declarations disagree.
		if z, ok := originImpliedTargetZone(sa); ok {
			zones = singleZone(z)
		} else if sa.API == "Attach" {
			// Attach's ValidTgts$ inZone<X> zones, compiled once
			// (effects.AttachParams.ValidTgtsZones).
			if zs := effects.AttachOf(sa).ValidTgtsZones; len(zs) > 0 {
				zones = zs
			}
		}
		if len(zones) == 0 {
			// Without an explicit zone, a ValidTgts$ stack-object kind
			// targets the stack; otherwise the battlefield remains the
			// default. An explicit TgtZone$ whose tokens were unknown must
			// not silently widen a target back to the stack.
			if tp.ZoneText == "" && tp.Has(effects.TgtValidStack) {
				zones = singleZone(state.ZStack)
			} else {
				zones = singleZone(state.ZBattlefield)
			}
		}
	}
	return zones
}

// originImpliedTargetZone reports the implicit target zone for a ChangeZone
// or Attach with an explicit Origin$. For Attach it only arbitrates against
// an inZone<X> ValidTgts$ inference: a single concrete Origin$ wins over the
// conflicting inferred zone. ChangeZone remains limited to the established
// unambiguous public-graveyard object-targeted shape: extending it to
// Hand/Library/Exile needs hidden-information and mixed-zone semantics that
// this does not establish (Origin$ Hand's mixed multi-zone handling lives in
// effects/zone.go). It admits an SA when ALL of these hold:
//
//  1. it is API$ ChangeZone, or Attach with inZone<X> ValidTgts$;
//  2. it has no explicit TgtZone$ (explicit TgtZone$ stays authoritative;
//     this helper only runs from targetZones' empty fallback, but a TgtZone$
//     whose tokens were all unknown must not silently fall through to Origin$
//     either) and no stack-targeting TargetType$;
//  3. effects.ParseZones parses its Origin$ as exactly one concrete zone
//     (Graveyard only for ChangeZone) -- not Any/All, not an unknown token,
//     not a multi-zone origin (ParseZones' ok=false fails closed);
//  4. its ValidTgts$ is object-only under the existing targetsPlayers
//     classifier, so a player-targeted ChangeZone keeps its existing
//     player-target route untouched.
//
// The zone feeds both legalTargetCandidates (offer time) and legalTargets
// (the CR 608.2b resolution recheck) through their shared targetZones calls,
// and effChangeZone's own Origin$ guard -- unchanged -- then accepts the
// chosen graveyard object at resolution.
func originImpliedTargetZone(sa *cards.SA) (state.Zone, bool) {
	changeZone := sa.API == "ChangeZone"
	if !changeZone {
		if sa.API != "Attach" {
			return 0, false
		}
		if len(effects.AttachOf(sa).ValidTgtsZones) == 0 {
			return 0, false
		}
	}
	tp := effects.TargetsOf(sa)
	if tp.ZoneText != "" || tp.Has(effects.TgtTypeStack) {
		return 0, false
	}
	if tp.Has(effects.TgtValidPlayers) {
		return 0, false
	}
	if changeZone {
		// ChangeZone's Origin$ is read through its compiled parameters, the
		// same parse effChangeZone's Origin$ precondition applies.
		if !effects.ChangeZoneOf(sa).OriginExactly(state.ZGraveyard) {
			return 0, false
		}
		return state.ZGraveyard, true
	}
	// Attach's Origin$ through its compiled parameters.
	if a := effects.AttachOf(sa); a.OriginSingle {
		return a.OriginZone, true
	}
	return 0, false
}

// singleZones backs singleZone: one shared one-element slice per zone.
var singleZones = func() (out [16][1]state.Zone) {
	for z := range out {
		out[z][0] = state.Zone(z)
	}
	return out
}()

// singleZone is the shared, read-only one-zone slice for z (targetZones'
// fallbacks), allocated only for a zone ordinal past the table.
func singleZone(z state.Zone) []state.Zone {
	if int(z) < len(singleZones) {
		return singleZones[z][:]
	}
	return []state.Zone{z}
}

// stackObjKind classifies one stack object for TargetType$ legality. The
// classifier moved to state.StackKindOf -- effects' Defined$ ValidStack arm
// must admit exactly what this census admits and cannot import rules, so one
// shared classifier serves both (state.StackKindOf's doc). The type alias
// and the three kind constants keep every existing rules-side name valid.
type stackObjKind = state.StackObjKind

const (
	stackSpell     = state.StackKindSpell     // a card object (a Face) on the stack
	stackActivated = state.StackKindActivated // an ability object minted by AbilityPush
	stackTriggered = state.StackKindTriggered // an ability object minted by TriggerPush/DelayedPush
)

func (e *Engine) stackObjKind(o *state.Object) stackObjKind { return state.StackKindOf(e.G, o) }

// stackTargetOptionKind maps the engine's stack-object classifier to the
// public target-option kind. Keep this aligned with view.StackView.Kind so a
// stack target is not mislabeled as a battlefield permanent on the wire.
func stackTargetOptionKind(k stackObjKind) string {
	switch k {
	case stackSpell:
		return "spell"
	case stackTriggered:
		return "trigger"
	case stackActivated:
		return "ability"
	default:
		return "spell"
	}
}

// targetTypeToken is state.StackKindToken: one comma-separated TargetType$
// token -- which stack object kinds its base admits, the controller qualifier
// read off the qualifiers after the base ("YouCtrl" -- controller must be the
// chooser; "OppCtrl" -- controller must not be; e.g. Weaver of Harmony's
// `Activated.YouCtrl,Triggered.YouCtrl`, Kang Dynasty's `Spell.OppCtrl`), and
// the card-type restriction the base or a qualifier can impose -- the bases
// "Instant" and "Sorcery" (Spider Sense's `Instant,Sorcery,Triggered`) and the
// Spell qualifiers "Instant"/"Sorcery" (Sister of Silence's
// `Spell.Instant,Spell.Sorcery,Activated,Triggered`) restrict the Spell kind
// to instant/sorcery CARD objects -- a creature spell is never admitted.
// Target-count, controller and spell-characteristic qualifiers are parsed
// here as part of the same token. The rules-side matcher supplies the live
// characteristics and chosen-target count that the lower state package cannot
// derive.
type targetTypeToken = state.StackKindToken

// A TargetType$ value's kind tokens are compiled once
// (effects.TargetParams.TypeTokens, state.StackKindTokens): a parameter that
// is absent -- or whose tokens name no stack kind at all -- defaults to
// Spell-only (deliberately narrow: a spec that never said it wants abilities
// does not get them).

// stackKindAdmits reports whether any TargetType$ token admits the stack
// object o (of kind k) controlled by controller, from chooser you's
// perspective. Token semantics are OR, matching ValidTgts$ alternatives:
// the object is offered when SOME token whose kind set contains k admits it
// under that token's own controller and card-type restriction. An
// instantOnly/sorceryOnly token checks the card object's Face, so a creature
// spell is never admitted by Spider Sense's `Instant,Sorcery,Triggered` or
// Sister of Silence's `Spell.Instant,Spell.Sorcery,...`; a Face-less object
// (never reachable for stackSpell, since StackObjKind only classifies a
// Face-bearing object as a spell) fails closed.
func (e *Engine) stackKindAdmits(toks []targetTypeToken, k stackObjKind, o *state.Object, controller, you state.PlayerID) bool {
	for _, tok := range toks {
		if !state.StackKindAdmits([]state.StackKindToken{tok}, k, o, controller, you) {
			continue
		}
		if tok.SingleTarget && len(o.Targets) != 1 {
			continue
		}
		if tok.NumTargetsOp != "" && !targetCountMatches(len(o.Targets), tok.NumTargetsOp, tok.NumTargets) {
			continue
		}
		if k == stackSpell {
			if tok.NonCreature && e.IsCreature(o.ID) {
				continue
			}
			if tok.Colorless && e.Colors(o.ID) != "" {
				continue
			}
			if tok.Legendary && !stackHasType(e.derivedTypesOf(o.ID), "Legendary") {
				continue
			}
		}
		return true
	}
	return false
}

func stackHasType(types []string, want string) bool {
	for _, typ := range types {
		if strings.EqualFold(typ, want) {
			return true
		}
	}
	return false
}

func targetCountMatches(count int, op string, want int) bool {
	switch targetCountMatchesdc21Codes.Code(string(op)) {
	case targetCountMatchesdc21EQ:
		return count == want
	case targetCountMatchesdc21NE:
		return count != want
	case targetCountMatchesdc21GE:
		return count >= want
	case targetCountMatchesdc21GT:
		return count > want
	case targetCountMatchesdc21LE:
		return count <= want
	case targetCountMatchesdc21LT:
		return count < want
	default:
		return false
	}
}

// targetName is the object's name for a target prompt, tolerating the ability
// stack object (no Face) a triggered ability's own target ask produces by
// falling back to its source permanent's name.
func (e *Engine) targetName(source state.ObjID) string {
	if o := e.G.Obj(source); o != nil {
		if f := o.Face(); f != nil && f.Name != "" {
			return f.Name
		}
		if s := e.G.Obj(o.Source); s != nil {
			if sf := s.Face(); sf != nil && sf.Name != "" {
				return sf.Name
			}
		}
	}
	// Falling back to the literal word "target" (reviewer minor 7) is
	// deliberate: every caller has already given the decision a usable
	// prompt, so this is only reached for an object with no name at all --
	// an unreadable name there is better than a fabricated one.
	return "target"
}

// targetOptionLabel renders one target option's label: the target's name
// followed by its controller's. The Face-less ability object a
// TargetType$ Activated/Triggered census now offers has no name of its own,
// so targetName's fallback (the source permanent's name) serves -- the two
// census consumers (cast.go's targetAsk and stack.go's askTarget) share this
// one helper so an ability object can never reach a nil-Face dereference in
// either.
func (e *Engine) targetOptionLabel(candidate targetCandidate) string {
	// The controller's name is seat-facing (the seat that answers sees it),
	// so it prefers the table's display name over the deck-identity slug.
	label := seatFacingName(e.G, candidate.player)
	if candidate.obj != 0 {
		label = e.targetName(candidate.obj) + " (" + label + ")"
	}
	return label
}

// protectionSource resolves the object whose characteristics decide whether
// "protection from X" filters out a candidate target or a Damage recipient
// (Task 15 fix round 1, Critical C2): for an ability stack object -- the
// top-of-stack wrapper events.Apply mints via AbilityPush/TriggerPush with no
// Face -- that is the Source permanent that carries the ability, because
// effects.ColorsOf and the Face-type reads in sourceHasQuality both return
// nothing for a Face-less object (CR 606.3: targeting checks the spell or
// ability's source, and for an activated or triggered ability that source is
// the permanent that granted it). For everything else -- a spell's own stack
// object, which IS the card and so carries a Face, or a plain permanent -- it
// is the object itself. This is the single definition both askTarget (which
// in the same round discovered it was passing the Face-less ability object)
// and legalTargets consult, so the two sites always agree on what "the
// source" is.
func (e *Engine) protectionSource(source state.ObjID) state.ObjID {
	if o := e.G.Obj(source); o != nil && o.Ability != nil && o.Source != 0 {
		return o.Source
	}
	return source
}

// describeTargetEffect is the context-aware target payload builder. The
// target ask is CR 601.2c's choice among legal targets, so a dynamic amount
// must be evaluated in the same announced/cast context the eventual effect
// will use -- never guessed from a zero-value Num read.
func (e *Engine) describeTargetEffect(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) *decision.TargetEffect {
	if sa == nil {
		return nil
	}
	out := &decision.TargetEffect{API: sa.API}
	if x == 0 {
		if o := e.G.Obj(source); o != nil {
			x = o.X
		}
	}
	if removal := targetRemoval(sa); removal != nil {
		out.Removal = removal
	}
	if sa.API == "Effect" {
		out.Statics = e.grantedStaticModes(p, source, sa)
	}
	switch describeTargetEffectdc22Codes.Code(string(sa.API)) {
	case describeTargetEffectdc22DealDamage:
		out.Damage = &decision.DamageEffect{}
		// A missing amount remains null even though the effect implementation
		// has a defensive runtime default. A literal or a resolvable X/SVar is
		// the amount the client and bot can actually reason about at this ask.
		if amount, ok := e.targetDamageAmount(p, source, sa, x); ok && amount >= 0 {
			n := int(amount)
			out.Damage.Amount = &n
		}
	}
	return out
}

// grantedStaticModes resolves an Effect SA's StaticAbilities$ SVar names
// against the source face's SVar table and returns each body's Mode$ value,
// in the order the names were written. It is the target-ask half of the
// Effect registration path (effects' effEffect walks the same names and
// resolves the same bodies via parseStaticLine), so the decision payload and
// the registered effect cannot disagree about what static an activation
// grants. A name that does not resolve, or whose body carries no Mode$, is
// silently skipped -- the payload publishes only what it can prove, exactly
// the way an unknown API stays uninterpreted.
func (e *Engine) grantedStaticModes(p state.PlayerID, source state.ObjID, sa *cards.SA) []string {
	// The SVar table is whichever face targetBoundCtx binds -- the source
	// permanent for an ability object (Whirler Rogue's activation), the
	// spell itself for a spell. It is the same anchor the dynamic numeric
	// grammar uses, so one read serves both.
	ctx, ok := e.targetBoundCtx(p, source)
	if !ok || ctx.SVars == nil {
		return nil
	}
	return staticModesFromSVars(sa, ctx.SVars)
}

// staticModesFromSVars is the one resolver an Effect SA's StaticAbilities$
// names go through: it splits the name list, resolves each against the
// supplied SVar table (the target ask binds targetBoundCtx's; the ability
// OFFER binds the ability's own face's table, which for an ability object
// is the same face) and returns each body's Mode$ value in the order the
// names were written. It carries its own API guard so the offer site in
// legal.go cannot publish statics for a non-Effect activation by forgetting
// the check, and it skips a name that does not resolve or a body with no
// readable Mode$ -- the payload publishes only what it can prove, exactly
// the way an unknown API stays uninterpreted.
func staticModesFromSVars(sa *cards.SA, svars map[string]string) []string {
	if sa == nil || sa.API != "Effect" || svars == nil {
		return nil
	}
	names := strings.FieldsFunc(effects.EffectOf(sa).StaticAbilities, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		body := strings.TrimSpace(svars[name])
		if body == "" {
			continue
		}
		statics, ok := cards.ParseStaticLines(body)
		if !ok {
			continue
		}
		for _, st := range statics {
			if st.Mode != "" {
				out = append(out, st.Mode)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// targetDamageAmount evaluates NumDmg with a verdict. effects.NumResolved is
// intentionally a broad numeric reader for effect sites that degrade an
// unmodelled SVar to zero; a decision payload must not turn that degradation
// into a claimed damage amount, so SVar bodies use EvalCountOK here.
func (e *Engine) targetDamageAmount(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) (int32, bool) {
	ctx, ok := e.targetBoundCtx(p, source)
	if !ok {
		ctx = effects.NewCtxPtr(source, p, effects.CtxInit{})
		if o := e.G.Obj(source); o != nil && o.Face() != nil {
			effects.SetSVars(ctx, o.Face().SVars)
		}
	}
	ctx.X = x
	amt := effects.DamageAmount(sa)
	if !amt.Present {
		return 0, false
	}
	raw := strings.TrimSpace(amt.Text)
	if n, err := strconv.ParseInt(raw, 10, 32); err == nil {
		return int32(n), true
	}
	sign := int32(1)
	if len(raw) > 1 && (raw[0] == '+' || raw[0] == '-') {
		if raw[0] == '-' {
			sign = -1
		}
		raw = raw[1:]
	}
	if ctx.SVars != nil {
		if body, found := ctx.SVars[raw]; found {
			// A body reading the target reference family has no value at this
			// ask: the unbound evaluation is the empty target set's sum, and
			// publishing it would claim a false zero (Kiku's Shadow's
			// SVar:X:Targeted$CardPower against a legal 5/5 deals 5, not 0).
			if e.amountDependsOnPendingTarget(ctx, p, source, sa, body) {
				return 0, false
			}
			n, resolved := effects.EvalCountOK(e, ctx, body)
			return sign * n, resolved
		}
	}
	// A direct helper/test ask without a source object has no announced or
	// resolving context in which a dynamic value could be known. Keep it null
	// rather than turning the evaluator's zero fallback into a claim.
	if source == 0 {
		return 0, false
	}
	if raw == "X" {
		return sign * ctx.X, true
	}
	// Inline Count$/ref-property bodies are valid direct numeric parameters.
	// The evaluator supplies the unknown verdict instead of collapsing them to
	// zero. This also covers published trigger/result values when their body is
	// supported by the effects count grammar. The same pending-target probe
	// guards this branch: an inline Targeted$ body is exactly as unvalued at
	// the ask as an SVar one.
	if e.amountDependsOnPendingTarget(ctx, p, source, sa, raw) {
		return 0, false
	}
	n, resolved := effects.EvalCountOK(e, ctx, raw)
	return sign * n, resolved
}

// amountDependsOnPendingTarget reports whether a NumDmg body's value moves
// with WHICH target the answering player is about to choose. The target ask
// is CR 601.2c's choice among legal candidates, so a body reading the target
// reference family (Targeted$CardPower, TargetedPlayer$Valid..., their
// Parent/This/All spellings) has NO value yet: its unbound evaluation is the
// empty target set's sum, and EvalCountOK rightly treats an empty set as a
// legitimate count -- which is precisely why the payload cannot take that 0
// as a nominal amount. The verdict is derived from evaluation, not from a
// hand-built token list, so a future target-reading head is covered without
// this site learning about it: bind each legal candidate as the body's ONLY
// target and compare against the unbound read. Any disagreement means the
// pending choice moves the amount, and no scalar may be published (null --
// "unknown" -- is the honest payload). A body that agrees with its unbound
// read under every candidate (Count$YourLifeTotal, Count$xPaid) is genuinely
// target-independent and stays publishable; a target-dependent sum over a
// multi-target ask also disagrees (any single binding differs from the empty
// sum whenever the value is nonzero), so a plural selection cannot smuggle a
// single-binding value through either. The census is the same
// legalTargetCandidates walk askTarget poses its options from, so the probe
// never sees a candidate the ask cannot offer. Cost: one extra census plus
// len(candidates) evaluations per posed damage ask -- decision posing, not a
// hot path.
func (e *Engine) amountDependsOnPendingTarget(ctx *effects.Ctx, p state.PlayerID, source state.ObjID, sa *cards.SA, body string) bool {
	base, _ := effects.EvalCountOK(e, ctx, body)
	for _, cand := range e.legalTargetCandidates(p, source, source, sa) {
		// Ctx is threaded by pointer through the evaluator; the probe binds
		// targets on a value copy and never touches the caller's context.
		probe := *ctx
		if cand.kind == "player" {
			probe.Targets = []state.Target{{Player: cand.player, IsPlayer: true}}
		} else {
			probe.Targets = []state.Target{{Obj: cand.obj}}
		}
		if v, _ := effects.EvalCountOK(e, &probe, body); v != base {
			return true
		}
	}
	return false
}

// targetRemoval classifies only APIs and destinations whose direct meaning is
// known. In particular, an unfamiliar API is never inferred to be removal
// from a label or parameter spelling.
func targetRemoval(sa *cards.SA) *decision.RemovalEffect {
	if sa == nil {
		return nil
	}
	switch targetRemovaldc23Codes.Code(string(sa.API)) {
	case targetRemovaldc23Destroy:
		return &decision.RemovalEffect{Kind: "destroy"}
	case targetRemovaldc23Sacrifice:
		return &decision.RemovalEffect{Kind: "sacrifice"}
	case targetRemovaldc23ChangeZone:
		// ChangeZone's Destination$ through its compiled parameters: the
		// zone the resolver moves to, named by its lower-case word.
		cz := effects.ChangeZoneOf(sa)
		if !cz.DestinationKnown {
			return nil
		}
		var kind string
		switch cz.Destination {
		case state.ZExile:
			kind = "exile"
		case state.ZHand:
			kind = "bounce"
		case state.ZGraveyard:
			kind = "graveyard"
		case state.ZLibrary:
			kind = "library"
		case state.ZCommand:
			kind = "command"
		default:
			return nil
		}
		return &decision.RemovalEffect{Kind: kind, Destination: cz.Destination.String()}
	case targetRemovaldc23ChangeZoneAll:
		// ChangeZoneAll's Destination$ through its compiled parameters.
		destination := effects.ChangeZoneAllOf(sa).DestinationLower
		kind := destination
		switch targetRemovaldc24Codes.Code(string(destination)) {
		case targetRemovaldc24Exile:
			kind = "exile"
		case targetRemovaldc24Hand:
			kind = "bounce"
		case targetRemovaldc24Graveyard:
			kind = "graveyard"
		case targetRemovaldc24Library:
			kind = "library"
		case targetRemovaldc24Command:
			kind = "command"
		default:
			return nil
		}
		return &decision.RemovalEffect{Kind: kind, Destination: destination}
	}
	return nil
}

type targetCandidate struct {
	kind   string
	obj    state.ObjID
	player state.PlayerID
}

// legalTargetCandidates is the pure target census shared by cast-option
// enumeration and the post-announcement target ask. It reads state in the
// same deterministic order as the old askTarget loops and emits no events.
//
// source is the object the spec's Self/Other predicates and the protection
// test are resolved against (for an ability, the Source permanent).
// excludeSelf is the object a prospective target may not equal -- the CR
// playerTargetSpecMatches judges one candidate seat q against a ValidTgts$
// player spec from the asker's (you) perspective. It is the ONE judge both
// target sites go through -- the offer (candidatesFor) and the resolution
// recheck (legalTargets) -- so the two cannot disagree (the one-definition
// rule). Every ordinary alternative is judged by the shared
// effects.MatchesPlayerSpecFrom grammar; an alternative whose qualifier names
// a trigger role the ask's own TriggerContext carries (pg2 event roles) is
// judged by triggerRolePlayerAlt below, because MatchesPlayerSpecFrom takes
// no trigger context and fails closed on the role names -- which turned The
// Lord of Pain's mandatory "choose another target player" trigger
// (ValidTgts$ Player.!TriggeredActivator) into an ask with no legal target
// and a silent fizzle. Forge's comma is OR: the spec matches when any one
// alternative matches.
func (e *Engine) playerTargetSpecMatches(sc effects.SpecContext, spec string, q, you state.PlayerID, source state.ObjID) bool {
	for alt := range strings.SplitSeq(spec, ",") {
		if matched, known := e.triggerRolePlayerAlt(sc, alt, q, you); known {
			if matched {
				return true
			}
			continue
		}
		if effects.MatchesPlayerSpecFrom(e.G, alt, q, you, source) {
			return true
		}
	}
	return false
}

// triggerRolePlayerAlt evaluates ONE comma-alternative of a ValidTgts$
// player spec whose qualifier names a trigger role the ask's own
// TriggerContext carries. The corpus writes exactly two such qualifiers on a
// player alternative, both negated: Player.!TriggeredActivator (The Lord of
// Pain) and Player.!TriggeredCardController (Lucy MacLean, Positively
// Armed); a positive form resolves the same way. The binding rides the ask's
// SpecContext (pg2); an absent binding matches NOBODY, including under !
// (the documented pg2 absent-binding contract), so the fail-closed direction
// is kept for every qualifier the context cannot answer. known=false when
// the alternative does not name a trigger role at all, leaving it to the
// shared grammar.
func (e *Engine) triggerRolePlayerAlt(sc effects.SpecContext, alt string, q, you state.PlayerID) (bool, bool) {
	base, qualifier, qualified := strings.Cut(strings.TrimSpace(alt), ".")
	if !qualified {
		return false, false
	}
	neg := strings.HasPrefix(qualifier, "!")
	var role state.PlayerID
	bound := false
	switch triggerRolePlayerAltdc25Codes.Code(string(strings.TrimPrefix(qualifier, "!"))) {
	case triggerRolePlayerAltdc25TriggeredActivator:
		role, bound = sc.TriggerContext.TriggerActivator.Player, sc.TriggerContext.TriggerActivator.IsPlayer
	case triggerRolePlayerAltdc25TriggeredCardController:
		// effects.TriggeredCardController is the one resolver -- Defined$,
		// OptionalDecider$ and the targeting restriction all read it.
		if p, ok := effects.TriggeredCardController(e.G, sc.TriggerContext, sc.Remembered); ok {
			role, bound = p, true
		}
	default:
		return false, false
	}
	// The base still applies to the role alternative, exactly as
	// MatchesPlayerSpecFrom applies it to every other qualifier.
	switch triggerRolePlayerAltdc26Codes.Code(string(base)) {
	case triggerRolePlayerAltdc26Player:
	case triggerRolePlayerAltdc26You:
		if q != you {
			return false, true
		}
	case triggerRolePlayerAltdc26Opponent:
		if q == you {
			return false, true
		}
	default:
		// An object alternative (Creature.!TriggeredTarget, ...): not this
		// helper's business -- the object arm judges it.
		return false, false
	}
	return bound && ((q == role) != neg), true
}

// 115.5 self-targeting rule. It is separate from source because during a
// cast/activation proposal the two diverge: a spell on the stack may not
// target itself (excludeSelf == the card), while an activated ability CAN
// target its own Source permanent (excludeSelf == 0, since the Face-less
// ability object is not on the stack yet). Callers set excludeSelf == 0 to
// disable the rule.
func (e *Engine) legalTargetCandidates(p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA) []targetCandidate {
	return e.candidatesFor(p, source, excludeSelf, sa, true)
}

// affectedCandidates is the Overload counterpart of legalTargetCandidates.
// It applies the script's object/player filter and zone/type restrictions but
// deliberately omits every rule that exists only because something is a
// target: protection, hexproof/CantTarget, and becomes-target bookkeeping.
func (e *Engine) affectedCandidates(p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA) []targetCandidate {
	return e.candidatesFor(p, source, excludeSelf, sa, false)
}

func (e *Engine) candidatesFor(p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA, targeting bool) []targetCandidate {
	return e.candidatesForLimit(p, source, excludeSelf, sa, targeting, 0)
}

// candidatesForLimit is candidatesFor that may stop enumerating once limit
// (> 0) candidates are collected. The early stop is taken only when neither
// post-filter (TargetsWithDefinedController$, TargetValidTargeting$) is
// present -- both can only DROP candidates, so without them the census is
// append-only and its first limit entries are exactly the full list's. The
// feasibility gate (targetSAAvailable) needs a count, never the list.
func (e *Engine) candidatesForLimit(p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA, targeting bool, limit int) []targetCandidate {
	return e.candidatesForLimitInto(nil, p, source, excludeSelf, sa, targeting, limit)
}

// candidatesCountForLimit is len(candidatesForLimit(...)), built in the
// engine's census scratch list (taken for the call, so a nested census
// allocates its own) instead of a fresh one.
func (e *Engine) candidatesCountForLimit(p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA, targeting bool, limit int) int {
	buf := e.targetCensusBuf
	e.targetCensusBuf = nil
	out := e.candidatesForLimitInto(buf[:0], p, source, excludeSelf, sa, targeting, limit)
	n := len(out)
	clear(out)
	e.targetCensusBuf = out[:0]
	return n
}

// candidatesForLimitInto is candidatesForLimit appending into dst[:0].
func (e *Engine) candidatesForLimitInto(dst []targetCandidate, p state.PlayerID, source, excludeSelf state.ObjID, sa *cards.SA, targeting bool, limit int) []targetCandidate {
	tp := effects.TargetsOf(sa)
	if limit > 0 && (tp.DefinedController != "" ||
		tp.ValidTargeting != "" ||
		tp.ControllerProperty != "" ||
		// tpc1: TargetingPlayerControls$ True is also a DROPPING post-filter,
		// so the census must not stop at limit before the whole search space
		// (battlefield included) has been walked and filtered.
		tp.Has(effects.TgtPlayerControlsSet) ||
		tp.SharedCardType != "") {
		limit = 0
	}
	spec := tp.ValidTgts
	// The spec-relative source (Self/Other/CARDNAME/sameName predicates read
	// it) is the SOURCE PERMANENT when the ask belongs to a minted ability
	// object -- the same object resolution-time recheck (legalTargets) already
	// judges its specs against (rules/stack.go passes o.Source there), so the
	// offer and the recheck cannot disagree (Critical C2's one-definition
	// rule). The Face-less wrapper itself is never a creature/permanent, so
	// a spec like Flamerush Rider's `Creature.attacking+Other` judged the
	// wrapper id meant "every creature but nobody in particular" and offered
	// the ability's own source as its own copy target. excludeSelf stays the
	// object the CR 115.5 self-targeting rule keys on (the stack object, for
	// an ability -- an ability CAN legally target its own Source permanent),
	// and the trigger-context lookup stays keyed on the stack id.
	specSrc := source
	if o := e.G.Obj(source); o != nil && o.Face() == nil && o.Ability != nil && o.Source != 0 {
		specSrc = o.Source
	}
	sc := e.targetSpecContext(specSrc, excludeSelf, p)
	defer e.releaseSpecEnv()
	if e.subOfferBound {
		sc.ParentTargets, sc.ParentBound = e.subOfferParent, true
	}
	zones := targetZones(sa)
	out := dst[:0]
	// Resolve the source ONCE for the whole census -- for an ability this is
	// the Source permanent, not the Face-less stack object. Every protection
	// test below is guarded on the candidate's zone, because a permanent's
	// static ability functions only on the battlefield (CR 604.3), so a
	// printed protection does not withhold a target sitting in the
	// Graveyard/Hand/Exile that a TgtZone$ spec is asking about. The PLAYER
	// candidates below read it too (hexproof from a quality resolves the
	// same source); the player-side shroud and hexproof grants carry no zone
	// gate -- a player is always in play.
	protSrc := e.protectionSource(source)
	// Players are offered only alongside the default battlefield search and
	// only when the spec actually names a seat. A spec that routes elsewhere
	// (TgtZone$ Graveyard/Hand/Exile) targets objects only -- never a player.
	// Each candidate seat is judged by the SAME shared player filter the
	// resolution recheck (legalTargets) applies, from the asker's perspective,
	// so offer and recheck cannot disagree (the one-definition rule): a
	// ValidTgts$ Opponent ask no longer offers the controller, ValidTgts$ You
	// no longer offers opponents, and a spec whose qualifier the filter cannot
	// evaluate fails closed to no seat (the AGENTS.md MatchesPlayerSpec
	// convention). Note MatchesPlayerSpecFrom splits on ',' and skips
	// alternatives whose base is not a player base, so a mixed
	// `Creature,Opponent` spec keeps the object half and matches only the
	// player half's seats.
	// CR 702.18 (player shroud), CR 702.11 (player hexproof) and CR 702.16c
	// (player protection): a seat a live `Affected$ You | AddKeyword$`
	// static grants those keywords is withheld here exactly as a permanent
	// carrying them is withheld in the object arm below -- the grant is read
	// off the same layer walk, through playerKeywords (rules/playerkeywords.go).
	// Protection is judged against the same census-wide protSrc the object
	// arm's protectedFrom uses, so the two arms resolve "the source"
	// identically (CR 702.16c). Only the targeting arm consults them; the
	// affected census (targeting=false) does not, the same split the
	// permanent shroud gate applies.
	if len(zones) == 1 && zones[0] == state.ZBattlefield {
		for _, q := range e.G.AliveFrom(0) {
			if e.playerTargetSpecMatches(sc, spec, q, p, specSrc) &&
				(!targeting || !e.playerShroudBlocksTarget(q)) &&
				(!targeting || !e.playerHexproofBlocksTarget(q, p, protSrc)) &&
				(!targeting || !e.playerProtectedFrom(q, protSrc)) {
				out = append(out, targetCandidate{kind: "player", player: q})
			}
		}
	}
	if limit > 0 && len(out) >= limit {
		return out
	}
zoneLoop:
	for _, z := range zones {
		if z == state.ZStack {
			// The stack is a single, shared sequence, not a per-seat zone, so
			// its objects are enumerated ONCE each -- labelled with the
			// object's own controller -- rather than once per alive seat,
			// which would offer the same spell N times in an N-seat game and
			// drift the option list. Which stack objects are targetable is
			// TargetType$'s job (CR 115.5 aside): Spell admits card objects,
			// Instant/Sorcery admit instant/sorcery card objects only (Spider
			// Sense, Sister of Silence), Activated/Triggered admit the ability
			// objects AbilityPush/TriggerPush mint, SpellAbility admits all
			// three, and a TargetType$ naming no stack kind falls back to
			// Spell -- the pre-fix behaviour, kept for every spec that never
			// said otherwise (the default stays the narrow one, never
			// widened).
			toks := tp.TypeTokens
			for _, oid := range e.G.Zone(state.ZStack, 0) {
				o := e.G.Obj(oid)
				// CR 115.5: a spell or ability on the stack is an illegal
				// target for itself. This census runs after PutOnStack or
				// AbilityPush, so source is already that object atop the
				// stack -- its own id must never be offered, or a
				// counterspell would counter itself. Only the source OBJECT
				// is excluded, never a different copy of the same card.
				if o == nil || (excludeSelf != 0 && oid == excludeSelf) {
					continue
				}
				if !e.stackKindAdmits(toks, e.stackObjKind(o), o, o.Controller, p) {
					continue
				}
				// The cast-provenance split (castprov1/castprov3/wascastfrom):
				// a stack-target spec carrying a wasCast* token — Wash Away's
				// `Card.!wasCastFromTheirHand` — evaluates the token against the
				// candidate's cast log here, before the ordinary filter; the
				// effects-side filter never strips the token, so without this the
				// spec fails closed to no candidate.
				tspec, ok := e.castProvenanceAdmits(targetSpecForZone(spec, z), oid, p)
				if !ok {
					continue
				}
				if e.matchesSpec(tspec, oid, sc) {
					out = append(out, targetCandidate{kind: stackTargetOptionKind(e.stackObjKind(o)), obj: oid, player: o.Controller})
					if limit > 0 && len(out) >= limit {
						break zoneLoop
					}
				}
			}
			continue
		}
		// Hand is the chooser's own hand only (CR 701.15a); the other
		// non-battlefield zones are public, so every seat's slice is offered.
		players := e.G.AliveFrom(0)
		if z == state.ZHand {
			players = []state.PlayerID{p}
		}
		for _, q := range players {
			for _, oid := range e.G.Zone(z, q) {
				o := e.G.Obj(oid)
				// CR 702.16c withholds a permanent protected from the
				// targeting source's qualities; a CantTarget restriction
				// (Vines of Vastwood) withholds one from the spoke player;
				// CR 702.14 shroud withholds one from EVERY targeting spell
				// or ability, its controller's included.
				// All function only on the battlefield (CR 604.3), the same
				// gate as protection above. CR 115.5 excludes the source.
				if o != nil && o.Face() != nil && !o.PhasedOut && (excludeSelf == 0 || oid != excludeSelf) {
					// The cast-provenance split at the non-battlefield target
					// zones too (wascastfrom): the token evaluates against the
					// candidate's cast log before the ordinary filter.
					tspec, ok := e.castProvenanceAdmits(targetSpecForZone(spec, z), oid, p)
					if !ok {
						continue
					}
					if e.matchesSpec(tspec, oid, sc) &&
						e.mentorAdmits(sa, specSrc, oid) &&
						(!targeting || !(o.Zone == state.ZBattlefield && e.protectedFrom(oid, protSrc))) &&
						(!targeting || !(o.Zone == state.ZBattlefield && e.shroudBlocksTarget(oid))) &&
						(!targeting || !(o.Zone == state.ZBattlefield && e.hexproofBlocksTarget(oid, p, protSrc))) &&
						(!targeting || !(o.Zone == state.ZBattlefield && e.restrictionBlocksTarget(oid, p))) {
						out = append(out, targetCandidate{kind: "permanent", obj: oid, player: q})
						if limit > 0 && len(out) >= limit {
							break zoneLoop
						}
					}
				}
			}
		}
	}
	out = e.filterTargetsWithDefinedController(out, sa, sc)
	out = e.filterTargetControllerProperty(out, sa)
	out = e.filterTargetsWithSharedCardType(out, sa, source, sc)
	out = e.filterTargetValidTargeting(out, sa, sc)
	if targeting {
		// tpc1: TargetingPlayerControls$ True -- the answering seat's
		// battlefield permanents only. Applied to the targeting census only;
		// the Overload affected sweep keeps its non-target semantics.
		out = e.filterTargetingPlayerControls(out, sa, p, source)
	}
	return out
}

// filterTargetValidTargeting implements TargetValidTargeting$ (Not of This
// World: "Counter target spell or ability that targets a permanent you
// control", TargetValidTargeting$ Permanent.YouCtrl+inRealZoneBattlefield):
// the candidate stack object QUALIFIES only when its own chosen targets
// include an object matching the spec, evaluated from the targeting
// ability's controller (the counter's "you" is its controller, not the
// countered spell's). Every OBJECT candidate carries its targets the same
// way -- the census labels a stack spell kind "spell", not "permanent", but
// its chosen targets are recorded on the same Object.Targets (notofthisworld1:
// before this admission the filter dropped every stack candidate, so a
// TargetValidTargeting$ counter offered no target at all and, through
// costPotentialTargets, no potential-target cost reduction either; 31 corpus
// files carry the parameter, every one a counter or retarget of a spell).
// A player candidate has no targets to check, and an object candidate with
// no recorded targets matches nothing -- both fail the filter, the narrower
// direction (an ability whose per-stack target bindings live in the
// trigger/activation roles this filter cannot see is never offered, never
// wrongly offered).
func (e *Engine) filterTargetValidTargeting(in []targetCandidate, sa *cards.SA, sc effects.SpecContext) []targetCandidate {
	spec := effects.TargetsOf(sa).ValidTargeting
	if spec == "" {
		return in
	}
	out := make([]targetCandidate, 0, len(in))
	for _, cand := range in {
		if cand.kind == "player" {
			continue
		}
		o := e.G.Obj(cand.obj)
		if o == nil {
			continue
		}
		for _, t := range o.Targets {
			if t.IsPlayer || t.Obj == 0 {
				continue
			}
			if e.matchesSpec(spec, t.Obj, sc) {
				out = append(out, cand)
				break
			}
		}
	}
	return out
}

// filterTargetsWithDefinedController implements the common target restriction
// that says the chosen object must be controlled by an event-role player. It
// is deliberately applied once after every zone's candidates are collected,
// so battlefield, graveyard and stack target offers cannot drift apart.
//
// Only a selector this build binds narrows the offer. An unsupported selector
// (ParentTarget, ParentTargetedController, TriggeredCauser, ...) and a
// supported role the current trigger did not bind leave the candidates
// unchanged -- the offer every such ability had before this restriction was
// read -- so no existing ability silently loses its targets. The two roles
// only an attack-declaration trigger binds (TriggeredAttackingPlayer,
// TriggeredAttackedTarget: Karazikar, Firkraag, Seifer, Gornog, Whirlwind
// Killer) and NonTriggeredCardController (Confusion in the Ranks: "target
// permanent another player controls") fail closed when unbound instead, since
// offering every creature would widen "target creature that player controls"
// to any player's, or "another player's permanent" to your own.
func (e *Engine) filterTargetsWithDefinedController(in []targetCandidate, sa *cards.SA, sc effects.SpecContext) []targetCandidate {
	ref := effects.TargetsOf(sa).DefinedController
	if ref == "" {
		return in
	}
	var player state.PlayerID
	var ok bool
	failClosed := false
	nonTriggeredController := false
	switch filterTargetsWithDefinedControllerdc27Codes.Code(string(ref)) {
	case filterTargetsWithDefinedControllerdc27NonTriggeredCardController:
		nonTriggeredController = true
		player, ok = effects.TriggeredCardController(e.G, sc.TriggerContext, sc.Remembered)
	case filterTargetsWithDefinedControllerdc27TriggeredTarget:
		if sc.TriggerTarget.IsPlayer {
			player, ok = sc.TriggerTarget.Player, true
		} else if o := e.G.Obj(sc.TriggerTarget.Obj); o != nil {
			player, ok = o.Controller, true
		}
	case filterTargetsWithDefinedControllerdc27TriggeredDefendingPlayer:
		if sc.DefendingPlayer.IsPlayer {
			player, ok = sc.DefendingPlayer.Player, true
		}
	case filterTargetsWithDefinedControllerdc27TriggeredPlayer:
		if sc.TriggerPlayer.IsPlayer {
			player, ok = sc.TriggerPlayer.Player, true
		}
	case filterTargetsWithDefinedControllerdc27TriggeredAttackingPlayer:
		failClosed = true
		if sc.AttackingPlayer.IsPlayer {
			player, ok = sc.AttackingPlayer.Player, true
		}
	case filterTargetsWithDefinedControllerdc27TriggeredAttackedTarget:
		failClosed = true
		if sc.AttackedTarget.IsPlayer {
			player, ok = sc.AttackedTarget.Player, true
		}
	case filterTargetsWithDefinedControllerdc27TriggeredCardController:
		player, ok = effects.TriggeredCardController(e.G, sc.TriggerContext, nil)
	}
	if !ok {
		if failClosed || nonTriggeredController {
			return nil
		}
		return in
	}
	out := in[:0]
	for _, candidate := range in {
		if candidate.kind != "permanent" {
			continue
		}
		o := e.G.Obj(candidate.obj)
		if o == nil {
			continue
		}
		if nonTriggeredController {
			if nonTriggeredControllerAdmits(o, player, ok) {
				out = append(out, candidate)
			}
			continue
		}
		if o.Controller == player {
			out = append(out, candidate)
		}
	}
	return out
}

// nonTriggeredControllerAdmits is the ONE definition of the
// TargetsWithDefinedController$ NonTriggeredCardController predicate ("target
// permanent another player controls", CR 109.5's complement of the triggering
// card's controller). It is shared by the offer post-filter
// (filterTargetsWithDefinedController) and the CR 608.2b resolution recheck
// (legalTargets) so the two cannot drift: both sites admit a candidate only
// when the triggering card's controller resolved (ok) and the candidate is a
// battlefield permanent controlled by a different player. The same-controller
// case is rejected, and an unresolvable controller fails closed, at both
// sites.
func nonTriggeredControllerAdmits(o *state.Object, controller state.PlayerID, ok bool) bool {
	return ok && o != nil && o.Zone == state.ZBattlefield && o.Controller != controller
}

// charmTargetSlots returns the selected DISTINCT target-bearing mode bodies.
// Repeated mode instances deliberately return nil: their later occurrences
// retain the established per-instance ask path.

const (
	targetCountMatchesdc21EQ uint16 = 1 // "EQ"
	targetCountMatchesdc21NE uint16 = 2 // "NE"
	targetCountMatchesdc21GE uint16 = 3 // "GE"
	targetCountMatchesdc21GT uint16 = 4 // "GT"
	targetCountMatchesdc21LE uint16 = 5 // "LE"
	targetCountMatchesdc21LT uint16 = 6 // "LT"
)

var targetCountMatchesdc21Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "EQ", Val: targetCountMatchesdc21EQ},
	state.StrEntry[uint16]{Key: "NE", Val: targetCountMatchesdc21NE},
	state.StrEntry[uint16]{Key: "GE", Val: targetCountMatchesdc21GE},
	state.StrEntry[uint16]{Key: "GT", Val: targetCountMatchesdc21GT},
	state.StrEntry[uint16]{Key: "LE", Val: targetCountMatchesdc21LE},
	state.StrEntry[uint16]{Key: "LT", Val: targetCountMatchesdc21LT},
)

const (
	describeTargetEffectdc22DealDamage uint16 = 1 // "DealDamage", "DamageAll"
)

var describeTargetEffectdc22Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "DealDamage", Val: describeTargetEffectdc22DealDamage},
	state.StrEntry[uint16]{Key: "DamageAll", Val: describeTargetEffectdc22DealDamage},
)

const (
	targetRemovaldc23Destroy       uint16 = 1 // "Destroy", "DestroyAll"
	targetRemovaldc23Sacrifice     uint16 = 2 // "Sacrifice", "SacrificeAll"
	targetRemovaldc23ChangeZone    uint16 = 3 // "ChangeZone"
	targetRemovaldc23ChangeZoneAll uint16 = 4 // "ChangeZoneAll"
)

var targetRemovaldc23Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Destroy", Val: targetRemovaldc23Destroy},
	state.StrEntry[uint16]{Key: "DestroyAll", Val: targetRemovaldc23Destroy},
	state.StrEntry[uint16]{Key: "Sacrifice", Val: targetRemovaldc23Sacrifice},
	state.StrEntry[uint16]{Key: "SacrificeAll", Val: targetRemovaldc23Sacrifice},
	state.StrEntry[uint16]{Key: "ChangeZone", Val: targetRemovaldc23ChangeZone},
	state.StrEntry[uint16]{Key: "ChangeZoneAll", Val: targetRemovaldc23ChangeZoneAll},
)

const (
	targetRemovaldc24Exile     uint16 = 1 // "exile"
	targetRemovaldc24Hand      uint16 = 2 // "hand"
	targetRemovaldc24Graveyard uint16 = 3 // "graveyard"
	targetRemovaldc24Library   uint16 = 4 // "library"
	targetRemovaldc24Command   uint16 = 5 // "command"
)

var targetRemovaldc24Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "exile", Val: targetRemovaldc24Exile},
	state.StrEntry[uint16]{Key: "hand", Val: targetRemovaldc24Hand},
	state.StrEntry[uint16]{Key: "graveyard", Val: targetRemovaldc24Graveyard},
	state.StrEntry[uint16]{Key: "library", Val: targetRemovaldc24Library},
	state.StrEntry[uint16]{Key: "command", Val: targetRemovaldc24Command},
)

const (
	triggerRolePlayerAltdc25TriggeredActivator      uint16 = 1 // "TriggeredActivator"
	triggerRolePlayerAltdc25TriggeredCardController uint16 = 2 // "TriggeredCardController"
)

var triggerRolePlayerAltdc25Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "TriggeredActivator", Val: triggerRolePlayerAltdc25TriggeredActivator},
	state.StrEntry[uint16]{Key: "TriggeredCardController", Val: triggerRolePlayerAltdc25TriggeredCardController},
)

const (
	triggerRolePlayerAltdc26Player   uint16 = 1 // "Player", "Any"
	triggerRolePlayerAltdc26You      uint16 = 2 // "You"
	triggerRolePlayerAltdc26Opponent uint16 = 3 // "Opponent", "Other"
)

var triggerRolePlayerAltdc26Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "Player", Val: triggerRolePlayerAltdc26Player},
	state.StrEntry[uint16]{Key: "Any", Val: triggerRolePlayerAltdc26Player},
	state.StrEntry[uint16]{Key: "You", Val: triggerRolePlayerAltdc26You},
	state.StrEntry[uint16]{Key: "Opponent", Val: triggerRolePlayerAltdc26Opponent},
	state.StrEntry[uint16]{Key: "Other", Val: triggerRolePlayerAltdc26Opponent},
)

const (
	filterTargetsWithDefinedControllerdc27NonTriggeredCardController uint16 = 1 // "NonTriggeredCardController"
	filterTargetsWithDefinedControllerdc27TriggeredTarget            uint16 = 2 // "TriggeredTarget"
	filterTargetsWithDefinedControllerdc27TriggeredDefendingPlayer   uint16 = 3 // "TriggeredDefendingPlayer"
	filterTargetsWithDefinedControllerdc27TriggeredPlayer            uint16 = 4 // "TriggeredPlayer"
	filterTargetsWithDefinedControllerdc27TriggeredAttackingPlayer   uint16 = 5 // "TriggeredAttackingPlayer"
	filterTargetsWithDefinedControllerdc27TriggeredAttackedTarget    uint16 = 6 // "TriggeredAttackedTarget"
	filterTargetsWithDefinedControllerdc27TriggeredCardController    uint16 = 7 // "TriggeredCardController"
)

var filterTargetsWithDefinedControllerdc27Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "NonTriggeredCardController", Val: filterTargetsWithDefinedControllerdc27NonTriggeredCardController},
	state.StrEntry[uint16]{Key: "TriggeredTarget", Val: filterTargetsWithDefinedControllerdc27TriggeredTarget},
	state.StrEntry[uint16]{Key: "TriggeredDefendingPlayer", Val: filterTargetsWithDefinedControllerdc27TriggeredDefendingPlayer},
	state.StrEntry[uint16]{Key: "TriggeredPlayer", Val: filterTargetsWithDefinedControllerdc27TriggeredPlayer},
	state.StrEntry[uint16]{Key: "TriggeredAttackingPlayer", Val: filterTargetsWithDefinedControllerdc27TriggeredAttackingPlayer},
	state.StrEntry[uint16]{Key: "TriggeredAttackedTarget", Val: filterTargetsWithDefinedControllerdc27TriggeredAttackedTarget},
	state.StrEntry[uint16]{Key: "TriggeredCardController", Val: filterTargetsWithDefinedControllerdc27TriggeredCardController},
)
