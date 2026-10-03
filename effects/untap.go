package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("Untap", effUntap)
	// Rules intercepts this internal keyword-expansion API while its triggered
	// ability resolves; registration keeps the expanded face's primitive set
	// supported and the no-engine effects fallback harmless.
	Register("CumulativeUpkeep", func(Host, *Ctx, *cards.SA) {})
	// kw:Echo (CR 702.35a): same shape — rules intercepts the keyword
	// expansion's DB$ Echo body while its triggered ability resolves
	// (rules/echo.go); the stub keeps the expanded face's primitive set
	// supported and the no-engine effects fallback harmless.
	Register("Echo", func(Host, *Ctx, *cards.SA) {})
}

// TryUntap is the shared CR 122.1d event proposal for effects and the untap
// step: a stun counter is removed instead of untapping the permanent.
func TryUntap(h Host, id state.ObjID) {
	o := h.Game().Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || !o.Tapped {
		return
	}
	if o.Counter("STUN") > 0 {
		h.Emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "STUN", Amount: -1, Text: events.UntapReplacedByStunNotice})
		return
	}
	h.Emit(events.Event{Kind: events.Untap, Obj: id})
}

func untapBattlefieldCondition(h Host, c *Ctx, sa *cards.SA) bool {
	spec, ok := sa.Params["ConditionPresent"]
	if !ok || sa.ParamStr(cards.PKConditionDefined) != "" {
		return true
	}
	if len(UnknownPredicates(spec)) > 0 {
		return false
	}
	// ConditionZone$ names the zone the ConditionPresent$ spec counts in
	// (Animist's Awakening's "two or more instants/sorceries in your
	// graveyard"). It defaults to the battlefield when absent or spelled
	// "Battlefield"; an unparseable zone name fails closed like every other
	// unresolvable gate input here. The shared conditionMet gate in
	// conditions.go deliberately leaves ConditionZone$ unresolved and defers
	// to this reader.
	zone := state.ZBattlefield
	if z := sa.Params["ConditionZone"]; z != "" {
		parsed, ok := parseZone(z)
		if !ok {
			return false
		}
		zone = parsed
	}
	n := 0
	g := h.Game()
	for si, p := range g.AliveFrom(0) {
		// ConditionZone$ Stack reads the SHARED stack (state/game.go Zone):
		// counting it once per alive seat would make a one-spell stack read
		// as an N-seat count and a ConditionCompare$ EQ1 gate wrongly false.
		// Only the first alive seat scans it; other zones stay per-seat.
		if zone == state.ZStack && si > 0 {
			continue
		}
		for _, id := range g.Zone(zone, p) {
			o := g.Obj(id)
			if o != nil && MatchesObjectCtx(g, spec, o, c.SpecContext(c.Controller)) {
				n++
			}
		}
	}
	op, want, ok := parseConditionCompare(sa.Params["ConditionCompare"])
	if sa.Params["ConditionCompare"] == "" {
		return n > 0
	}
	if !ok {
		return false
	}
	switch op {
	case "EQ":
		return n == want
	case "NE":
		return n != want
	case "LT":
		return n < want
	case "LE":
		return n <= want
	case "GT":
		return n > want
	case "GE":
		return n >= want
	}
	return false
}

// untapTypeCandidates resolves the corpus's UntapType$ family in deterministic
// seat/zone order. A Defined$ player narrows which battlefield is searched;
// otherwise the type/controller predicates themselves determine membership.
func untapTypeCandidates(h Host, c *Ctx, sa *cards.SA) []state.ObjID {
	g := h.Game()
	owners := g.AliveFrom(0)
	if def := Defined(h, c, sa); len(def) > 0 {
		var ps []state.PlayerID
		for _, t := range def {
			if t.IsPlayer {
				seen := false
				for _, p := range ps {
					seen = seen || p == t.Player
				}
				if !seen {
					ps = append(ps, t.Player)
				}
			}
		}
		if len(ps) > 0 {
			owners = ps
		}
	}
	spec := sa.Params["UntapType"]
	var out []state.ObjID
	for _, p := range owners {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
				out = append(out, id)
			}
		}
	}
	return out
}

// effUntap supports both ordinary listed/targeted untaps and Forge's
// UntapType$ + Amount$ chooser family. UntapExactly$ fixes Min=Max; UntapUpTo$
// permits zero through Amount. The resumed answer is scoped and consumed here,
// so a nested Untap poses its own choice.
func effUntap(h Host, c *Ctx, sa *cards.SA) {
	// AIManaPref$ (Basalt Monolith's "{3}: Untap this artifact" carries
	// AIManaPref$ NotSameCard) is Forge's AI mana-generation hint -- which
	// floating mana the AI prefers to leave untapped when it activates the
	// untap. It is deck-building and bot-policy guidance, never a rules tail:
	// the activation's legality and effect are unchanged by its value. The
	// recognition keeps the parameter census honest; the bot-policy half is
	// named in the deck import report's Issues.
	_ = sa.Params["AIManaPref"]
	if !untapBattlefieldCondition(h, c, sa) {
		return
	}
	if sa.Params["UntapType"] == "" {
		// ETB$ True is the "enters untapped" replacement body (Horizon
		// Explorer's lands-enter-untapped, the mirror of effTap's 804
		// enters-tapped bodies). The entry-tap/untap pair's real composition
		// runs through the Updated move-replacement pipeline: the original move
		// is applied first (composeUpdatedReplacements), then each With body in
		// the deterministic scan order, so by the time this body runs the
		// entering object is already on the battlefield and any earlier body's
		// tap is live -- the plain Untap event below is exactly what undoes it
		// (forEachObject's seat-local zone order scans the entering card's own
		// zone -- hand or library -- before the battlefield, so the entered
		// land's own enters-tapped body runs first and Horizon Explorer's untap
		// composes after it). The one semantic difference from TryUntap is
		// deliberate: the STUN-counter substitution (CR 122.1d) is a rule for
		// untapping a permanent already in play; an entering one carries no
		// counters, so the ETB untap is the plain event. Forge's own ETB read
		// (UntapEffect.resolve) clears the tapped state directly and skips the
		// UntapAll trigger, which this build cannot do without an event -- no
		// corpus Mode$ Untaps trigger is reachable through an entry replacement
		// (the mode itself is unregistered here), so the event fires nothing.
		entering := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKETB)), "True")
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				continue
			}
			if entering {
				if o := h.Game().Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield && o.Tapped {
					h.Emit(events.Event{Kind: events.Untap, Obj: t.Obj})
				}
				continue
			}
			TryUntap(h, t.Obj)
		}
		return
	}

	var chosen []state.ObjID
	if c.UntapDone {
		chosen = append(chosen, c.Untap...)
		c.Untap, c.UntapDone = nil, false
	} else {
		candidates := untapTypeCandidates(h, c, sa)
		n := int(Num(h, c, sa, "Amount", 1))
		if n <= 0 || len(candidates) == 0 {
			return
		}
		if n > len(candidates) {
			n = len(candidates)
		}
		upTo := strings.EqualFold(sa.Params["UntapUpTo"], "True")
		needsAsk := upTo || len(candidates) > n
		if needsAsk {
			min := n
			if upTo {
				min = 0
			}
			d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
				Min: min, Max: n, Source: c.Source, ResumeKind: "untap", ResumeSA: sa,
				Prompt: "Choose permanents to untap"}
			for _, id := range candidates {
				o := h.Game().Obj(id)
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "untap", Obj: id, Label: o.Face().Name})
			}
			if h.Ask(d) {
				return
			}
		}
		chosen = candidates[:n]
	}
	for _, id := range chosen {
		TryUntap(h, id)
	}
}
