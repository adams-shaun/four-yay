package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("Phases", effPhases)
}

// effPhases implements Forge's PhasesEffect (the `SP$`/`DB$ Phases` action),
// CR 702.25's phasing (task phases1). Representatives in the corpus (56
// files): Talon Gates of Madara's "when this enters, up to one target
// creature phases out", Guardian of Faith's "any number of other target
// creatures you control phase out", Teferi's Protection, Riftwing Cloudskate,
// Phase Dolphin, Slip Out the Back, March of Swirling Mist.
//
// The affected set is AllValid$ when present -- every battlefield object the
// filter admits (Out of Time, Unyaro, Disciple of Caelus Nin, Galadriel's
// Dismissal, The City on the Edge of Forever, Teferi's Realm, Teferi,
// Timeless Voyager, Time and Tide: 10 corpus lines) -- otherwise Defined$'s
// ordinary dispatch: with a ValidTgts$ the chosen targets, with none the
// source. AllValid$ is read exactly the way effGainControl reads it (the same
// MatchesObjectCtx filter evaluator, the same deterministic arena walk, the
// resolving controller as You), so a filter predicate means one thing across
// both primitives.
//
// `PhaseInOrOut$` (8 corpus lines) is a TOGGLE, matching Forge's
// PhasesEffect.resolve: an affected permanent that is phased out phases IN,
// one that is phased in phases OUT. Time and Tide is the measured case --
// `AllValid$ Creature.hasKeywordPhasing,Card.phasedOutCreature |
// PhaseInOrOut$ True` is its oracle's "all phased-out creatures phase in and
// all creatures with phasing phase out" in one pass. With the toggle present
// the AllValid$ pool must also include phased-out permanents (Forge's
// getCardsIncludePhasingIn), or the phase-in half can never be reached.
//
// Without the toggle the body is a plain phase-out, and an already-phased-out
// permanent is skipped (Forge's `!gameCard.isPhasedOut()` guard): re-emitting
// PhaseOut on it would be a no-op fold but a dishonest log line, and CR
// 702.25a's "permanents phase out one at a time" means each one gets its own
// event.
//
// `Phaseout$ False` is kept as an explicit phase-in override. It is NOT a
// Forge PhasesEffect parameter and no corpus carrier uses it (measured 0 of
// 74 `$ Phases` lines), so it exists only for an authored/fixture body that
// wants an unconditional direction; the real corpus spells its phase-in
// bodies `PhaseInOrOut$ True`.
//
// Each moveless PhaseOut event is emitted only for a target actually on the
// battlefield: phasing is a battlefield status (CR 702.25b), so a target that
// left in response simply does nothing -- the emit's own Apply gate would
// drop it anyway, but skipping here keeps the log honest.
func effPhases(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	toggle := strings.TrimSpace(sa.ParamStr(cards.PKPhaseInOrOut)) != "" &&
		!strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKPhaseInOrOut)), "False")
	// phaseInOnly is the explicit `Phaseout$ False` override (non-Forge, no
	// corpus carrier): an unconditional phase-in.
	phaseInOnly := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKPhaseout)), "False")
	affected := phasesAffectedObjects(h, c, sa, toggle)
	remember := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberAffected)), "True")
	wontPhaseIn := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKWontPhaseInNormal)), "True")
	// CR 702.25a: each permanent gets its own PhaseOut event, but the whole
	// resolution is ONE "one or more permanents phase out" batch for the
	// batch-level Mode$ PhaseOutAll trigger (The War Doctor), so its latency
	// fires once for the group rather than once per permanent.
	h.BeginZoneBatch()
	for _, id := range affected {
		o := g.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		var amount int32
		switch {
		case phaseInOnly:
			amount = -1
		case toggle:
			// Toggle: phased out phases in, phased in phases out.
			if o.PhasedOut {
				amount = -1
			} else {
				amount = 1
			}
		default:
			if o.PhasedOut {
				// Forge's plain phase-out skips an already-phased-out
				// permanent (PhasesEffect.resolve's `!gameCard.isPhasedOut()`
				// guard): a second PhaseOut would be a no-op fold and a
				// spurious log line.
				continue
			}
			amount = 1
		}
		if remember && amount > 0 {
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
			eventRemember(h, c, id)
		}
		text := ""
		if wontPhaseIn && amount > 0 {
			text = "wont-phase-in-normal"
		}
		h.Emit(events.Event{Kind: events.PhaseOut, Obj: id, Amount: amount, Text: text})
		if amount < 0 && strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKTapped)), "True") {
			h.Emit(events.Event{Kind: events.Tap, Obj: id})
		}
	}
	h.EndZoneBatch()
}

// phasesAffectedObjects is effPhases' affected-set resolver: the AllValid$
// battlefield sweep when the ability carries one, else Defined$'s targets and
// source dispatch. It returns object ids in the deterministic arena order
// each walk already produces, so the PhaseOut events replay identically.
// includePhasedOut mirrors Forge's two AllValid$ pools: the toggle reads
// getCardsIncludePhasingIn (a phased-out permanent must be reachable to phase
// back in), the plain phase-out reads getCardsIn and sees only phased-in
// permanents.
func phasesAffectedObjects(h Host, c *Ctx, sa *cards.SA, includePhasedOut bool) []state.ObjID {
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKAllValid)); spec != "" {
		g := h.Game()
		sc := c.SpecContext(c.Controller)
		var out []state.ObjID
		for i := range g.Objs {
			o := &g.Objs[i]
			if o.Zone != state.ZBattlefield {
				continue
			}
			if o.PhasedOut && !includePhasedOut {
				continue
			}
			if MatchesObjectCtx(g, spec, o, sc) {
				out = append(out, o.ID)
			}
		}
		return out
	}
	var out []state.ObjID
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer || t.Obj == 0 {
			continue
		}
		out = append(out, t.Obj)
	}
	return out
}
