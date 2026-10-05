package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func init() { effects.RegisterNonAPI("kw:Teamwork", "count:Teamwork") }

func teamworkTargetsAvailable(e *Engine, p state.PlayerID, id state.ObjID, sa *cards.SA) bool {
	// The cast option declares the Teamwork branch before target announcement
	// (CR 601.2b-c). Its target census must therefore resolve Count$Teamwork
	// as intent-paid, even though the player may still decline at the later
	// optional-cost ask. Reuse the normal target census and its target-bound
	// context rather than maintaining a second Teamwork-specific evaluator.
	previous := e.cast
	e.cast = &pendingCast{player: p, card: id, mode: "teamworked"}
	defer func() { e.cast = previous }()
	return e.castTargetsAvailable(p, id, sa)
}

func teamworkOffer(w *legalWalk, id state.ObjID, face *cards.Face, base Cost, targets bool) {
	e := w.e
	if !e.stackKeywordPossibleH(id, kwhTeamwork) || !targets {
		return
	}
	var threshold int32
	for _, keyword := range e.derivedWith(id, state.ZStack).Keywords {
		head, param, found := strings.Cut(keyword, ":")
		if found && kwHeadOf(head).ID == kwhTeamwork.ID {
			if n, err := strconv.ParseInt(strings.TrimSpace(param), 10, 32); err == nil && n > 0 {
				threshold = int32(n)
				break
			}
		}
	}
	if threshold == 0 || !w.offerCastable(w.p, id, pay.WithSpellAbilityExtras(face, base), spellScope(""), false) {
		return
	}
	var power int32
	for _, creature := range e.G.Zone(state.ZBattlefield, w.p) {
		o := e.G.Obj(creature)
		if o != nil && !o.Tapped && e.matchesSpecFrom("Creature.YouCtrl", creature, w.p, id) {
			power += e.Power(creature)
		}
	}
	if power >= threshold {
		w.out = append(w.out, decision.Option{Index: len(w.out), Kind: "cast",
			Label: "Cast " + face.Name + " (teamwork)", Obj: id, Mode: "teamworked"})
	}
}

func paidTapCastInfo(pc *pendingCast, flags string) []events.Event {
	if pc == nil {
		return nil
	}
	var out []events.Event
	if pc.conspirePaid {
		out = append(out, events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: 1,
			Counter: events.FlagsString(events.FlagsFrom(flags) | state.FlagConspired)})
	}
	if pc.teamworkPaid {
		out = append(out, events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: 1,
			Counter: events.FlagsString(events.FlagsFrom(flags) | state.FlagTeamworkPaid)})
	}
	return out
}
