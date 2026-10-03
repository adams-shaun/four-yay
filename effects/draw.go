package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effDraw is api:Draw's resolution. Every parameter of its own it reads comes
// from the compiled DrawParams (draw_params.go), the one reader of a Draw
// ability's parameters; internal/codeshape's drawParamLeaks ratchet holds
// this file to no parameter read at all. Whose draws (actingPlayers) is the
// shared Defined$/ValidTgts$ selector tier.
func effDraw(h Host, c *Ctx, sa *cards.SA) {
	dp := DrawOf(sa)
	noteUnreadParams(h, c, "Draw", dp.Unread)
	n := numText(h, c, dp.NumCards, 1)
	if n <= 0 {
		return
	}
	// RememberDrawn$ records every card actually drawn into the resolution's
	// Remembered (Breathstealer's Crypt's reveal-and-maybe-discard chain acts
	// on exactly the drawn card; a library that ran out mid-draw records only
	// what moved). Unread before this — the whole sub-chain saw nothing. A
	// draw that parked on a dredge ask has not happened yet, so the record
	// waits until the draw is real (the suspend check below).
	// The corpus uses both True and AllReplaced; both record cards this
	// ability's draws actually moved into a hand (replaced draws are not here).
	remember := dp.RememberDrawn
	// tapeReentry is what a legacy re-entry re-runs from this primitive's
	// first line before it reaches its answer: the unread-parameter Note
	// and the capture exclusion. A resolution-kernel tape answer continues
	// in place instead, so it emits the same at the same point.
	tapeReentry := func() {
		noteUnreadParams(h, c, "Draw", dp.Unread)
		if remember {
			c.Remembered = rememberedExcludingCapture(h, c)
		}
	}
	if remember {
		// A triggered ability's resolution starts with its fire-time event
		// capture already in Ctx.Remembered (rules/resolution.go seeds both
		// Remembered and Captured from the ability object's own Remembered).
		// That object -- for Communal Brewing's self-ETB trigger, the
		// entering Brewing itself -- is not a card drawn this way, so it must
		// not inflate Remembered$Amount ("one ingredient counter ... for each
		// card drawn this way") nor defeat the did-I-draw-anything gate
		// (Mr. Foxglove's `ConditionDefined$ Remembered | ConditionCompare$
		// EQ0`). Drop it before recording what the draws actually moved; the
		// helper is the same capture-exclusion every TriggerRemembered$Amount
		// read uses, and it is a no-op for an activated ability (no capture).
		c.Remembered = rememberedExcludingCapture(h, c)
	}
	targets := actingPlayers(h, c, sa)
	total := int32(len(targets)) * n
	// OptionalDecider$ (Mystic Remora, Rhystic Study — Forge's DrawEffect
	// resolve: optional = hasParam("OptionalDecider")... the decider confirms
	// "do you want to draw N cards?" and a decline skips): the DRAW itself is
	// optional, decided by the NAMED decider, resolved through the same
	// player selector grammar every other resolver owns ("You" — the
	// corpus's dominant value, and its bare "True" spelling — is the
	// resolving controller, the enchantment's controller asking themselves
	// whether to draw off their own trigger; TargetedController is the
	// targeted spell's controller (Vex's "that spell's controller may draw
	// a card"), TriggeredCardController the entering creature's (Selvala),
	// Opponent the controller's opponents). The ask is the same mid-
	// resolution KChoose yes/no every other asking primitive poses, answered
	// through rules' "draw_optional" resume arm into Ctx.DrawOpt; a host that
	// cannot ask keeps the pre-ask mandatory draw (the R-9 degradation). A
	// spec the grammar cannot resolve fails closed below this read's own
	// convention: the pre-ask mandatory draw stays and one loud Note names
	// the unmodelled value. A target with an empty library makes the draw a
	// non-choice — Forge's canDrawAmount guard skips those silently, so the
	// ask only fires when SOME target could actually draw; with none, no
	// question is posed and nothing is drawn (an empty-library draw event is
	// a no-op either way).
	if decider := dp.OptionalDecider; decider != "" && total > 0 {
		answered := c.DrawOpt
		c.DrawOpt = "" // fx42 scoping: consumed once; a nested optional draw poses its own ask
		if answered == "" && c.DrawDone > 0 {
			// A Dredge re-entry mid-draw (the "dredge" arm restored the draw
			// cursor): draws happen only after the decider said yes, so the
			// election is already made -- re-posing it would ask again and,
			// on a second yes, restart the draws from zero.
			answered = "yes"
		}
		if answered == "" {
			canDraw := false
			for _, t := range targets {
				if len(zoneOf(h.Game(), state.ZLibrary, t)) > 0 {
					canDraw = true
					break
				}
			}
			if canDraw {
				seat := c.Controller
				spec := decider
				if strings.EqualFold(spec, "True") {
					spec = "You"
				}
				askable := true
				if !strings.EqualFold(spec, "You") {
					resolved := DefinedSpec(h, c, spec)
					if len(resolved) == 0 || !resolved[0].IsPlayer {
						// The decider's identity is unresolvable — a spec the
						// selector grammar does not carry resolves to no player
						// (the self-default fallback returns an OBJECT, which
						// the IsPlayer gate rejects). Fail closed: the pre-ask
						// mandatory draw stays, one loud Note names the value,
						// and NO ask is posed (a yes/no ask to the controller
						// would be exactly the wrong-seat decision this read
						// exists to avoid).
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
							Text: "unmodelled Draw OptionalDecider$ " + decider})
						askable = false
					} else {
						seat = resolved[0].Player
					}
				}
				if askable {
					d := &decision.Decision{Player: seat, Kind: decision.KChoose, Min: 1, Max: 1,
						ResumeKind: "draw_optional", ResumeSA: sa, Source: c.Source,
						Prompt: "Draw " + strconv.Itoa(int(n)) + " card(s)?"}
					d.Options = []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes — draw", Player: seat},
						{Index: 1, Kind: "no", Label: "No", Player: seat},
					}
					if ans, ok := AskTape(h, d); ok {
						// The resolution kernel's answer in hand (the
						// "draw_optional" arm's DrawOpt).
						answered = "no"
						if answerYes(ans) {
							answered = "yes"
						}
						tapeReentry()
					} else if Ask(h, d) == AskAsked {
						return
					}
				}
			} else {
				answered = "no"
			}
		}
		if answered == "no" {
			// Declined (or no drawable pool): no draw; the walk continues to
			// any SubAbility$ chain.
			return
		}
	}
	// Upto$ True (task mordorparams1: Arcane Denial's "Its controller may
	// draw up to two cards at the beginning of the next turn's upkeep",
	// Truce's "Each player may draw up to two cards"): the draw is a real
	// per-target COUNT choice, one KChoose per Defined$ target before that
	// target's draws, Min 0, Max min(NumCards, the target's library size),
	// options the top cards of the TARGET's own library (the library-search
	// ask's private card options — a decision is visible only to
	// Decision.Player, so no leak). Answered through rules' "draw_upto"
	// resume arm into Ctx.DrawUptoIdx/Count/Answered (the DrawOpt pattern,
	// fx42-scoped per target); a batch that parks on a Dredge choice
	// re-enters through the dredge arm, which restores the in-flight
	// target's cursor from the ask's ResumeUpto rider. A host that cannot
	// ask keeps the pre-ask mandatory draw (the R-9 degradation every
	// effDraw arm takes). A library with fewer than n cards caps the ask at
	// what is there; an empty library is a no-op (never a decision whose
	// only answer is empty — OnlyEmptyAnswer refuses it — and never a
	// mandatory draw event that would mill a player the card only offered
	// to draw).
	if dp.Upto {
		for idx := int(c.DrawUptoIdx); idx < len(targets); idx++ {
			p := targets[idx]
			if !c.DrawUptoAnswered {
				lib := zoneOf(h.Game(), state.ZLibrary, p)
				m := n
				if int32(len(lib)) < m {
					m = int32(len(lib))
				}
				if m <= 0 {
					c.DrawUptoIdx = int32(idx + 1)
					continue
				}
				d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 0, Max: int(m),
					ResumeKind: "draw_upto", ResumeSA: sa, ResumeTarget: idx, Source: c.Source,
					// The walk's Remembered rides the ask (the hidden-library
					// search's ResumeRemembered precedent): the re-entered
					// effDraw recomputes `targets` from Defined$, and for the
					// Remembered-valued selectors -- Arcane Denial's
					// `Defined$ DelayTriggerRemembered` is the corpus shape --
					// a resume that rebuilt an empty set would resolve a
					// DIFFERENT target list than the one the cursor indexes,
					// so the answered count would be drawn for the wrong
					// player or for nobody. The ability-object resume path
					// restores the same set from o.Remembered; this covers
					// every other frame.
					ResumeRemembered: append([]state.Target(nil), c.Remembered...),
					Prompt:           "Draw up to " + strconv.Itoa(int(n)) + " card(s)?"}
				for i := int32(0); i < m; i++ {
					id := lib[i]
					label := "a card"
					if o := h.Game().Obj(id); o != nil && o.Face() != nil {
						label = o.Face().Name
					}
					d.Options = append(d.Options, decision.Option{Index: len(d.Options),
						Kind: "card", Label: label, Obj: id, Player: p})
				}
				if ans, ok := AskTape(h, d); ok {
					// The resolution kernel's answer in hand: the count the
					// "draw_upto" arm binds, drawn for this target now.
					c.DrawUptoCount, c.DrawUptoAnswered = int32(len(ans)), true
					tapeReentry()
				} else if Ask(h, d) == AskAsked {
					return
				} else {
					// No-host (R-9): the pre-ask mandatory draw of what was offered.
					c.DrawUptoCount, c.DrawUptoAnswered = m, true
				}
			}
			for c.DrawDone < c.DrawUptoCount {
				var lib []state.ObjID
				if remember {
					lib = zoneOf(h.Game(), state.ZLibrary, p)
				}
				if drawFor(h, p, int(c.DrawDone), sa, drawUptoRider{idx: idx, count: c.DrawUptoCount}) {
					// A tape-served Dredge answer, already applied: on past
					// this draw, as the "dredge" re-entry continues.
					tapeReentry()
					c.DrawDone++
					continue
				}
				if h.Suspended() {
					// A Dredge choice is between individual draws. Its resume
					// point restores this target's cursor (idx, answered count
					// and DrawDone); do not run later targets yet.
					return
				}
				if remember && len(lib) > 0 {
					c.Remembered = append(c.Remembered, state.Target{Obj: lib[0]})
				}
				c.DrawDone++
			}
			c.DrawDone = 0
			c.DrawUptoIdx = int32(idx + 1)
			c.DrawUptoCount, c.DrawUptoAnswered = 0, false
		}
		return
	}
	for c.DrawDone < total {
		p := targets[c.DrawDone/n]
		var lib []state.ObjID
		if remember {
			lib = zoneOf(h.Game(), state.ZLibrary, p)
		}
		if drawFor(h, p, int(c.DrawDone), sa, drawUptoRider{}) {
			// A tape-served Dredge answer, already applied: on past this
			// draw, as the "dredge" re-entry continues.
			tapeReentry()
			c.DrawDone++
			continue
		}
		if h.Suspended() {
			// A Dredge choice is between individual draws. Its resume point
			// carries this cursor; do not run later draws, Remembered or
			// SubAbility$ yet.
			return
		}
		if remember && len(lib) > 0 {
			c.Remembered = append(c.Remembered, state.Target{Obj: lib[0]})
		}
		c.DrawDone++
	}
	// DrawDone is scoped to this primitive like the other Ctx answer fields.
	c.DrawDone = 0
}
