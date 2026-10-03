package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// engineTriggerMaps groups the Engine's per-stack trigger provenance and
// LKI maps. It is embedded by value in Engine (rules/engine_struct.go), so
// every field keeps its documented contract comment and every existing
// e.<field> access keeps compiling unchanged through Go's field promotion.
// Clone's per-field copy classes (rules/clone.go) are unchanged by the
// move.
type engineTriggerMaps struct {
	// Per-stack-instance trigger provenance, derived while queuing/placing
	// triggers, cloned at intent boundaries and removed when the stack object
	// leaves. Never encoded in events or inferred from a resolving source.
	triggerContexts map[state.ObjID]effects.TriggerContext
	// triggerEffectFrames carries the source-scoped Effect frame an
	// Effect-created delayed trigger body resolves under, keyed by the stack
	// instance the trigger was placed into (the same key triggerContexts
	// uses). A non-static Effect trigger's body is minted by events.Apply's
	// DelayedPush from game state alone, so the frame the trigger queued with
	// must ride this scratch map to the resolution Ctx; the static fire arm
	// needs no map because it resolves the body inline. Resolution-scratch
	// like triggerContexts: never event-encoded, cloned at intent boundaries
	// and removed when the stack object leaves.
	triggerEffectFrames map[state.ObjID]effects.EffectFrame
	// triggerLines maps a stack object id to the granted/delayed trigger line
	// whose Execute$ body it resolves to. A granted (AddTrigger$) or delayed
	// (Effect Triggers$) body is an SVar-named *cards.SA, and cards.ResolveSVar
	// parses a FRESH pointer on every call -- so the pointer identity
	// findTriggerForAbilityFace uses for compiled Face.Triggers bodies can never
	// match one. This map carries the line from the push (which already records
	// triggerContexts) to resolution, so OptionalDecider$, the Cost$ window,
	// ResolvedLimit$, the intervening-if recheck and the label all see it.
	// Replay-derived exactly like triggerContexts: pushTrigger folds the same
	// lines in the same order. Appended to (not a redefinition of) the existing
	// map fields so a zero Engine stays valid.
	triggerLines map[state.ObjID]cards.Trigger
	// triggerLineSVars snapshots the owning script table of each recorded line.
	// The recipient's face is not necessarily the grantor's, and a grant can
	// disappear before the stack object resolves.
	triggerLineSVars map[state.ObjID]map[string]string
	// currentEffectFrame is the Effect-created continuous-effect registration
	// the effects.Resolve walk currently running belongs to. effects.Resolve
	// publishes it (through the optional effectFrameHost interface) for the
	// whole of a body walk and restores the enclosing value on exit, and
	// Ask captures it onto the resume point so a body that suspends on a
	// mid-resolution ask resumes still bound to its registration. It is
	// resolution-scratch like the trigger contexts -- never event-encoded, and
	// a replay re-derives it by re-running the same walk -- and it is zero
	// outside an Effect-created body, so every ordinary resolution is
	// unchanged.
	currentEffectFrame effects.EffectFrame
	// triggerLKI preserves the causing event's object snapshot from trigger
	// match through placement and resolution. TriggerPush can log Remembered
	// ids but not the pre-move object value (whose counters Move clears), so
	// this replay-derived map is the LKI analogue of triggerContexts.
	triggerLKI map[state.ObjID]triggerObjectLKI
	// sacrificedLKI maps a stack object id to the last-known-information
	// snapshot of every permanent that object sacrificed (as a cost), captured
	// at the instant of the sacrifice (Task sac1). It is engine-only, never
	// written to a state.Object, because a log-only state.Game reconstruction
	// (replayFromLog) rebuilds the stack object from the AbilityPush event
	// alone and would not reproduce an object field we set in cast.go -- the
	// same reason triggerContexts is engine-only. Resolution reads it and
	// builds effects.Ctx.Sacrificed; the entry is removed when the stack
	// object leaves, mirroring triggerContexts.
	sacrificedLKI map[state.ObjID][]state.SacrificedInfo
	// castExiled / castRevealed map a stack object id to the cards its own
	// cast/activation COST removed: the `ExileFromHand`/`ExileFromGrave`/
	// `Exile` parts (Forge's CostExile, paid-list key "Exiled") and the
	// `Reveal` parts (CostReveal, key "Revealed"), in stable cost order.
	// Engine-only scratch in the sacrificedLKI discipline: a log-only
	// reconstruction rebuilds it because payCast re-executes, cloned with the
	// engine at intent boundaries, read by the spell's own resolution Ctx
	// (effects.Ctx.Exiled/Revealed) so the `Exiled$<Property>` /
	// `Revealed$<Property>` count refs and `Defined$ Exiled`/`Revealed` read
	// the exact paid cards, and removed with the stack object. A stack COPY
	// inherits neither map -- referenced here is the deliberate reason the
	// StackCopy branch does not carry them, unlike fuseTargets: a copy was
	// never cast and paid no cost (CR 707.10).
	castExiled   map[state.ObjID][]state.ObjID
	castRevealed map[state.ObjID][]state.ObjID
	// fuseTargets maps a fused (FlagFused) stack object id to its two target
	// stages' own chosen targets (index 0 the front half's, index 1 the
	// alternate half's). Recorded by payCast at payment, read by resolveFused
	// (split.go) so each half resolves exactly the targets chosen FOR it:
	// re-deriving the split from the object's flat target list through each
	// half's ValidTgts spec mis-assigns any target a half's spec merely
	// overlaps (Turn // Burn's Creature vs Any). Engine-only scratch like
	// sacrificedLKI: rebuilt by replay because payCast re-executes, cloned
	// with the engine at intent boundaries, removed with the stack object.
	// StackCopy inherits this split alongside its flat targets when available;
	// resolveFused uses its spec fallback only for copies without provenance.
	fuseTargets map[state.ObjID][][]state.Target
	// copyTargetStage tracks the in-progress per-declaration copy-target
	// election (CR 707.10c), keyed on the copying stack object: the value is
	// the index of the NEXT declaration AskCopyTargets must ask. The
	// TargetsChosen fold clears Object.CopyMayChooseTarget after the FIRST
	// declaration's answer, so this scratch is what carries the election
	// across the stage that follows -- a fused copy's second half, exactly as
	// the cast's pendingCast.targetStage carries it for a cast. Engine-only
	// scratch like fuseTargets: rebuilt by replay (the ask re-executes on the
	// re-entered resolveTop), cloned with the engine, and removed with the
	// stack object so a later object reusing the id never reads a stale stage.
	copyTargetStage map[state.ObjID]int
	// copyAnswerTargets accumulates a multi-declaration copy-target election's
	// PER-DECLARATION answers until every declaration has been asked, at which
	// point the flattened list is recorded in one replace. Recording each
	// stage as it arrives would replace (or duplicate) the flat list mid-
	// election and lose a later declaration's inherited keep-current slots.
	// Engine-only scratch, rebuilt by replay, cloned with the engine, removed
	// with the stack object.
	copyAnswerTargets map[state.ObjID][][]decision.Option
	// castSubTargets carries a cast or activation's CAST-TIME pre-asked
	// SubAbility$ target answers (task alltargeted1), keyed by the stack
	// object that will resolve the chain and then by the sub SA's Line.
	// Forge asks every targeting SA in the chain BEFORE cost payment
	// (CR 601.2c); the engine pre-asks them in the cast flow (cast.go's
	// subTargetAsk) and installs the answers here at payment, so the
	// resolution consumes them (effects' chosenTargetsFor, through
	// Ctx.SubPreAsk) instead of re-posing the asks mid-resolution. An EMPTY
	// recorded set is a real answer (a Min-0 sub elected zero or had no
	// candidate at cast time) and still consumes its line. Engine-only
	// scratch in the fuseTargets discipline: rebuilt by replay because the
	// cast flow re-executes, cloned with the engine at intent boundaries,
	// removed when the stack object leaves the stack. A stack COPY of the
	// spell has no entry and falls back to the mid-resolution asking path.
	castSubTargets map[state.ObjID]map[string][]state.Target
	// trigSub is the in-flight CR 603.3d announcement of a triggered
	// ability's SubAbility$ chain targets (rules/trigger_subtargets.go): set
	// by pushTrigger, advanced by each "trig_sub" target answer, and
	// installed into castSubTargets when the last link is answered. At most
	// one exists (the drain places one trigger at a time). Engine scratch,
	// rebuilt by replay, deep-copied by Clone.
	trigSub *trigSubAsk
	// charmTargets maps a modal stack object to the selected distinct modes'
	// target groups, in target-bearing mode order. It is engine scratch like
	// fuseTargets: the cast/placement target answer rebuilds it during replay.
	charmTargets map[state.ObjID][][]state.Target
}
