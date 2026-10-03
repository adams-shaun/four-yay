package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("ImmediateTrigger", effImmediateTrigger) }

// effImmediateTrigger implements Forge's ImmediateTrigger — the "when you do"
// body an SA chains after a cost (or a token creation with
// RememberOriginalTokens$) resolves: "At the beginning of your upkeep, create
// a token. When you do, return up to one target Aura ... attached to that
// token" (Forum Filibuster), "create two Zombies. When you do, exile up to
// two target cards" (Diregraf Horde), "you may pay {1}. When you do, target
// creature with haste can't be blocked ..." (Speed, Young Avenger's AB shape).
//
// CR 603.12: each instance is a REFLEXIVE TRIGGERED ABILITY. It is handed to
// the host's trigger queue (Host.QueueReflexiveTrigger), which puts it on the
// stack as its own triggered ability the next time a player would receive
// priority: its targets are chosen as it is put there (a real target, so
// hexproof, ward and "becomes the target" apply), players can respond between
// the spawning effect and it, and it resolves or fizzles on its own. Until
// 2026-10 this build resolved the Execute$ body INLINE inside the spawning
// resolution -- an accepted approximation that made the target an untargeted
// resolution-time choice and left no response window (Faebloom Trick's tap).
//
// The inline resolution below is kept for exactly three shapes: Static$ True
// (Forge's static trigger resolves immediately by definition -- Melira, the
// Living Cure's replacement-installed lock), a host that reports it cannot
// mint the ability from the log (a body compiled on a foreign face; an
// effects-package test double), and the re-entry of an inline loop that
// already suspended.
//
// On the inline path, per instance the loop builds a FRESH Ctx copy (the fx42
// scoping rule: an answer must never carry between instances) and resolves Execute$ through
// the ordinary Resolve, so a sub's own mid-resolution ask (Forum Filibuster's
// TargetMin$ 0 / TargetMax$ 1 ChangeZone) suspends the WHOLE resolution and
// re-enters through the same RepeatCursor machinery effRepeatEach uses —
// SuspendRepeat reports the loop's position (SA identity + next instance
// index), the enclosing Resolve chain drops its own continuation report for
// this SA (SuspendContinuation's repeatReported rule), and the resumed pass
// re-enters this function at the cursor, runs the remaining instances, and
// then lets Resolve walk SubAbility$ (the DBCleanup tail) exactly once.
// Anything else would ask again on resume or drop the remaining instances.
//
// The instance Remembered set comes from RememberObjects$ (the parent set is
// this resolution's Ctx.Remembered MINUS its trigger capture — the same
// exclusion iterationBase applies and the documented convention that a
// trigger's captured event object is not part of Forge's host remembered
// list; rememberOriginalTokens' tokens are what the flag names, never the
// firing Phase/ChangesZone trigger's own capture):
//
//   - RememberObjects$ Remembered with RememberEach$ True — one instance per
//     parent remembered object, the i-th instance's Ctx.Remembered is exactly
//     the i-th object (DelayTriggerRememberedLKI then resolves to it for the
//     body's ChangeZone AttachedTo$ / Attach Object$ reads), and the instance
//     count clamps to the remembered count when TriggerAmount$ exceeds it
//     (never index-past-end);
//   - RememberEach$ True with RememberObjects$ ABSENT takes the SAME
//     per-object reading (a documented deviation from the brief's mapping
//     table, which reserved absent for the whole-set arm): RememberEach
//     itself implies per-object instances, and the three corpus carriers
//     (dain_ironfoot, ratonhnhake_ton, forum_filibuster) all name
//     RememberObjects$ Remembered alongside it, so the divergence is
//     corpus-unreachable either way;
//   - RememberObjects$ Remembered / RememberedLKI (or any Remembered...
//     predicate form) without RememberEach$ — every instance sees the whole
//     parent set;
//   - any other RememberObjects$ value — ONE loud Note naming the value, then
//     the whole parent set (never silence);
//   - absent — the whole parent set.
//
// TriggerAmount$ resolves through the ordinary Num grammar against a ctx
// whose Remembered is the capture-excluded parent set (so Remembered$Amount
// and Count$RememberedNumber count exactly the tokens RememberOriginalTokens$
// remembered, not the capture), defaulting to 1. Missing Execute$ is a loud
// Note and a no-op. Conditions gate free (effects.Resolve's shared
// conditionMet pass), and the DB forms' UnlessCost$ gates free through the
// shared unless gate; the AB forms' Cost$ is paid by rules' triggered-cost
// window before this function ever runs (rules/stack.go's resolveTop gate).
func effImmediateTrigger(h Host, c *Ctx, sa *cards.SA) {
	execName := strings.TrimSpace(sa.ParamStr(cards.PKExecute))
	sub := cards.ResolveSVar(c.SVars, execName)
	if sub == nil {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "ImmediateTrigger with no resolvable Execute$ " + execName})
		return
	}
	// The parent remembered set minus the trigger's own event capture, via the
	// shared helper refTargets' TriggerRemembered case also uses (so the two
	// cannot disagree about what Forge's host list holds).
	parent := rememberedExcludingCapture(h, c)
	// TriggerAmount$ against the capture-excluded ctx, so the Remembered$-
	// anchored count heads see exactly what the script remembered.
	amountCtx := *c
	amountCtx.Remembered = append([]state.Target(nil), parent...)
	amount := Num(h, &amountCtx, sa, "TriggerAmount", 1)
	if amount < 0 {
		amount = 0
	}
	remember := strings.TrimSpace(sa.ParamStr(cards.PKRememberObjects))
	each := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberEach)), "True")
	// wholeSet is the set every non-RememberEach instance's Ctx.Remembered sees
	// (the body reads it through DelayTriggerRememberedLKI / Remembered). It
	// defaults to the capture-excluded parent set; an EXPLICIT RememberObjects$
	// spec outside the Remembered* family REPLACES it with the objects that
	// spec names -- Behemoth of Vault 0's `RememberObjects$ Targeted` hands its
	// Destroy body the nonland permanent its own pre-asked target picked
	// (PickedTargets), and Back for More / Curse of the Werefox / Novel Nunchaku
	// feed a Fight the same way. effectRemembered's Targeted arm already prefers
	// PickedTargets over Targets, and immediateRememberObjects mirrors that
	// through the one shared fail-closed knownDefinedTargets resolver so the two
	// readers cannot disagree about what the spelling means.
	wholeSet := parent
	// subjects is the per-instance index space; for a RememberEach loop it
	// holds one entry per instance's remembered object, otherwise amount
	// identical slots that all share wholeSet.
	var subjects []state.Target
	eachMode := false
	switch {
	case each && (remember == "" || strings.HasPrefix(remember, "Remembered")):
		n := amount
		if n > int32(len(parent)) {
			n = int32(len(parent))
		}
		subjects = append(subjects, parent[:n]...)
		eachMode = true
	case remember == "" || remember == "RememberedLKI" || strings.HasPrefix(remember, "Remembered"):
		for i := int32(0); i < amount; i++ {
			subjects = append(subjects, state.Target{})
		}
	default:
		if ts, ok := immediateRememberObjects(h, c, remember); ok {
			wholeSet = ts
		} else {
			// A genuinely unknown spec: keep the loud Note and the whole
			// parent set (never silence, never a guessed target).
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "ImmediateTrigger RememberObjects$ " + remember + " is not implemented; every instance sees the whole remembered set"})
		}
		for i := int32(0); i < amount; i++ {
			subjects = append(subjects, state.Target{})
		}
	}
	start := 0
	if cur := (*RepeatCursor)(nil); cur != nil && cur.SA == sa {

		subjects, start = cur.Subjects, cur.Next
		if eachMode && start > len(subjects) {
			start = len(subjects)
		}
	}
	// CR 603.12: each instance is a reflexive triggered ability. It goes on
	// the stack -- targets chosen as it is put there, respondable, ward and
	// "becomes the target" triggers firing -- the next time a player would
	// receive priority, through the host's trigger queue. Three shapes keep
	// the inline resolution: Static$ True (Forge's static trigger, which
	// resolves immediately by definition -- Melira, the Living Cure's lock
	// must be installed before its replacement returns), a re-entry into an
	// inline loop that already suspended, and a body the host cannot mint
	// from the log (QueueReflexiveTrigger reports false; decided on the first
	// instance, so one ImmediateTrigger never mixes the two).
	queue := start == 0 && !strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKStatic)), "True")
	for i := start; i < len(subjects); i++ {
		cc := *c

		if eachMode {
			// The i-th instance remembers exactly its own subject (the token
			// DelayTriggerRememberedLKI names).
			cc.Remembered = []state.Target{subjects[i]}
		} else {
			cc.Remembered = copyTargets(wholeSet)
		}
		if queue {
			if h.QueueReflexiveTrigger(c, execName, sub, cc.Remembered) {
				continue
			}
			queue = false
		}
		Resolve(h, &cc, sub)
		if h.Suspended() {
			h.SuspendRepeat(RepeatSuspension{
				RepeatCursor: RepeatCursor{SA: sa, Subjects: copyTargets(subjects), Next: i + 1},
				Body:         copyTargets(cc.Remembered),
				Subject:      subjects[i],
				Outer:        copyTargets(c.Remembered),
			})
			return
		}
	}
}

// immediateRememberObjects resolves an explicit ImmediateTrigger
// RememberObjects$ spec to the object set the body's DelayTriggerRememberedLKI
// / Remembered reads should see. It is the ONE home for that spec, so the
// Targeted preference for the body's own pre-asked target and the shared
// knownDefinedTargets spelling coverage cannot drift apart.
//
// The bare Targeted / ThisTargetedCard spellings prefer Ctx.PickedTargets over
// Ctx.Targets exactly as effectRemembered's Targeted arm does: an AB-form
// ImmediateTrigger carrying its own ValidTgts$ has the answer delivered to
// PickedTargets, and the resolution-level Targets list is either the outer
// SA's or empty. Every other spelling -- TriggeredTarget, TargetedController,
// TriggeredCard, the dotted and " & "-joined forms, ... -- goes through the
// shared fail-closed knownDefinedTargets resolver, so an unknown spelling is
// ok=false here and the caller keeps the loud Note.
func immediateRememberObjects(h Host, c *Ctx, spec string) ([]state.Target, bool) {
	switch spec {
	case "Targeted", "ThisTargetedCard":
		targets := c.Targets
		if c.PickedTargets != nil {
			targets = c.PickedTargets
		}
		return copyTargets(targets), true
	}
	return knownDefinedTargets(h, c, spec)
}
