package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("Mana", effMana)
	Register("ReplaceMana", effReplaceMana)
	Register("Effect", effEffect)
	Register("Cleanup", effCleanup)
	Register("SetState", effSetState)
	Register("Counter", effCounter)
	Register("DelayedTrigger", effDelayedTrigger)
	Register("Repeat", effRepeat)
	Register("Charm", effCharm)
	Register("GenericChoice", effCharm)
	Register("VillainousChoice", effVillainousChoice)
	Register("Vote", effVote)
	Register("BecomeMonarch", effBecomeMonarch)
	Register("RingTemptsYou", effRingTemptsYou)
	Register("RestartGame", effRestartGame)
	Register("Goad", effGoad)
	Register("AlterAttribute", effAlterAttribute)
	Register("Ward", effWard)
}

// effGoad records each independently-lived goad relationship. Duration and
// source are event payload so replay can expire conditional goads identically.
//
// RememberGoaded$ True (2 corpus files: Havoc Eater, Kaima the Fractured
// Calm) makes the resolution remember each goaded creature, so the chained
// SubAbility$ (both carriers' DB$ PutCounter reading SVar:Y:Remembered$
// CardPower — "X +1/+1 counters, where X is the total power of creatures
// goaded this way") reads exactly what was goaded. Ctx is threaded by
// pointer through Resolve, so appending here is visible to the sub-ability
// without any state write — the same per-resolution ctx Remembered the
// RememberDamaged$ arm of DealDamage takes (damage.go), replay re-derived by
// re-running the resolution. Only the goad-granting arm remembers; a
// NoLonger$ release remembers nothing (its corpus shape never pairs the
// rider with a release).
func effGoad(h Host, c *Ctx, sa *cards.SA) {
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberGoaded"]), "True")
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		if o := h.Game().Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
			if strings.EqualFold(sa.Params["NoLonger"], "True") {
				h.Emit(events.Event{Kind: events.Goad, Obj: o.ID, Amount: -1})
				continue
			}
			duration := sa.ParamStr(cards.PKDuration)
			if duration == "" {
				duration = "UntilYourNextTurn"
			}
			h.Emit(events.Event{Kind: events.Goad, Obj: o.ID, Player: c.Controller,
				Text: duration, IDs: []state.ObjID{c.Source}, Amount: int32(o.Controller) + 1})
			if remember {
				c.Remembered = append(c.Remembered, state.Target{Obj: o.ID})
			}
		}
	}
}

// effAlterAttribute applies Forge's AlterAttribute effect: it flips a
// designation attribute on each resolved target (task alterattr1). Targets
// come through the ordinary Defined path, so a body with no Defined$ asks
// its ValidTgts$ targets the way every other targeting primitive does --
// Nelly Borca's "whenever it attacks, suspect target creature" gets its ask
// from the trigger's placement ask, Hot Pursuit's ETB the same way, and a
// deeper sub (the DBDebuff family) through Resolve's generic pre-ask.
//
// The engine models TWO attributes: Suspected (CR 702.157, the Blame Game
// precon family) and Prepared (CR 722.3a, the Secrets of Strixhaven
// preparation cards). Both designations live on state.Object behind the
// events.AlterAttribute fold; Suspected's two end conditions (leaves the
// battlefield, another player gains control) and Prepared's (leaves the
// battlefield, or an unprepare effect) are events.Apply's folds -- Prepared
// deliberately keeps its designation across a control change, because CR
// 722.3c ties the copy to the permanent, not to a controller.
// A body naming any other attribute (Solved, Plotted, Saddled, Commander,
// Harnessed -- the corpus's remaining populations) emits the loud
// unsupported-attribute Note and moves nothing, exactly like the
// Manifest/Cloak out-of-scope shapes: registration claims the API, the Note
// claims the gap.
//
// Activate$ False is Forge's removal spelling ("becomes unprepared"); for
// Suspected it removes the designation (the DBDebuff family's
// "un-suspect an opponent's suspected creature" shape).
func effAlterAttribute(h Host, c *Ctx, sa *cards.SA) {
	attr := strings.TrimSpace(sa.Params["Attributes"])
	if attr == "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "AlterAttribute names no Attributes$"})
		return
	}
	activate := !strings.EqualFold(strings.TrimSpace(sa.Params["Activate"]), "False")
	for _, name := range strings.FieldsFunc(attr, func(r rune) bool { return r == ',' || r == ' ' }) {
		prepared := strings.EqualFold(name, "Prepared")
		saddled := strings.EqualFold(name, "Saddled")
		if !strings.EqualFold(name, "Suspected") && !prepared && !saddled {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "AlterAttribute: attribute " + name + " not modelled"})
			continue
		}
		amount := int32(1)
		if !activate {
			amount = -1
		}
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				continue
			}
			o := h.Game().Obj(t.Obj)
			if o == nil || o.Zone != state.ZBattlefield {
				continue
			}
			// CR 722.3a: only a permanent that HAS a prepare spell can gain
			// the prepared designation; an unprepare (Activate$ False) always
			// applies, exactly like Suspected's removal.
			if prepared && activate && !o.HasPrepareSpell() {
				continue
			}
			text := "Suspected"
			if prepared {
				text = "Prepared"
			} else if saddled {
				text = "Saddled"
			}
			h.Emit(events.Event{Kind: events.AlterAttribute, Obj: o.ID,
				Text: text, Amount: amount})
		}
	}
}

// effWard is the resolution half of the Ward keyword trigger. The triggering
// spell/ability is held in TriggerSource; after a declined payment it is
// countered and an ability is parked in exile (CR 608.2m).
func effWard(h Host, c *Ctx, sa *cards.SA) {
	cause := c.TriggerStack
	o := h.Game().Obj(cause)
	if o == nil || o.Zone != state.ZStack {
		return
	}
	if c.UnlessPay == "" {
		// The prompt is player-facing text: render the raw UnlessCost$
		// (PayLife<2>, Sac<1/Creature>) through unlessPayPhrase, never
		// verbatim. Display only; the charge is rules' unless-payment path.
		pay := unlessPayPhrase(sa.ParamStr(cards.PKUnlessCost))
		d := &decision.Decision{Player: o.Controller, Kind: decision.KModes, Min: 1, Max: 1,
			Prompt: pay + " for ward?", ResumeKind: "unless_pay", ResumeSA: sa,
			Options: []decision.Option{{Index: 0, Kind: "mode", Label: pay, Player: o.Controller, Mode: decision.ModeUnlessPay}, {Index: 1, Kind: "mode", Label: "Don't pay", Player: o.Controller, Mode: decision.ModeUnlessDecline}}}
		h.Ask(d)
		return
	}
	paid := c.UnlessPay == "pay"
	c.UnlessPay = ""
	if paid {
		return
	}
	to := state.ZGraveyard
	if o.Face() == nil {
		to = state.ZExile
	}
	h.Emit(events.Event{Kind: events.MoveZone, Obj: cause, From: state.ZStack, To: to, Text: "countered by ward"})
}

// CopySpellAbility is NOT registered. It needs to create a brand new game
// object mid-match (a copy of a spell or ability already on the stack), and
// every state mutation in this engine goes through events.Apply -- there is
// no Apply case yet that mints an ID and decides how a copy's Card/FaceIdx/
// Ability/Targets carry over. Token had the identical shape (see the Task
// 18 report for the original analysis of both) and closed it in Task 13 via
// events.TokenCreate, whose Apply case mints the new object from
// Game.Tokens; see token.go. CopySpellAbility's own object comes from
// wherever it is on the stack already, not a registry, so it needs its own
// event kind (StackCopy exists as of Task 12) wired up rather than reusing
// TokenCreate's shape verbatim. Registering it as a Note-only stub would
// make Supported()/Coverage claim a card is playable when it cannot
// actually do what its text says. Left unregistered, Resolve's existing
// "unimplemented API" Note fallback applies, and Coverage correctly
// excludes any card that needs it.

// effectContinuous registers one continuous effect created by the api:Effect
// primitive, marking it Effect-created (state.ContinuousEffect.FromEffect).
// The marker is what the source-scoped form of the one-shot self-exile ender
// (Host.EndEffectSource) keys on, so a printed static of the SAME source is
// never ended by an Effect's self-exile.
func effectContinuous(h Host, ce state.ContinuousEffect) {
	ce.FromEffect = true
	h.AddContinuous(ce)
}

// effectChosenSnapshot captures the resolution's chosen-card set for an
// Effect-delivered may-play grant whose Affected$ reads it (Card.ChosenCard):
// Forge's EffectEffect copies the host's chosen cards onto the effect card it
// creates, so the grant keeps naming the card chosen at creation after the
// chain's own `DB$ Cleanup | ClearChosenCard$ True` wipes the source's list
// (Strongbox Raider, Chandra, Flameshaper, Feldon, Ronom Excavator, Party
// Thrasher, End-Blaze Epiphany, Case of the Burning Masks, Jaya, Fiery
// Negotiator). Without the snapshot rules' grant walk had no chosen binding
// at all and the ChosenCard predicate failed closed: the chosen card was
// never playable. A grant whose spec does not read the chosen set takes no
// snapshot.
func effectChosenSnapshot(h Host, c *Ctx, affects string) ([]state.ObjID, bool) {
	if !strings.Contains(affects, "ChosenCard") {
		return nil, false
	}
	var out []state.ObjID
	for _, t := range resolutionChosenCards(h.Game(), c) {
		if !t.IsPlayer && t.Obj != 0 {
			out = append(out, t.Obj)
		}
	}
	return out, true
}

// mayPlayGrantFromLine builds the may-play ContinuousEffect from one parsed
// SVar static line an Effect SA registers. ok=false is the fail-closed grant:
// nothing is registered rather than a half-read grant going live. Unlike the
// printed S: route's MayPlayStaticParams, this path may carry a
// ValidAfterStack$ spell-characteristic qualifier (Nahiri, Forged in Fury's
// STPlay2), returned verbatim on MayPlayValidAfterStack for rules to evaluate.
//
// cascadeKeywordGrantFromLine is the AddKeyword$ Cascade twin (task
// cascade1): ok only when the line's AddKeyword$ value is entirely Cascade
// tokens (the whitelist — a mixed Cascade & Haste grant or any other keyword
// fails closed to the unimplemented Note) and carries no condition gate this
// registration path cannot evaluate. Returns the granted keyword list (all
// "Cascade", one entry per instance), the Affected$ spec (Forge's omitted
// default is the controller's own cards, the same Card.Self default the
// layer walk's static scan applies) and the AffectedZone$ value verbatim.
