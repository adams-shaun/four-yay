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

func teamworkFlashOffer(w *legalWalk, id state.ObjID, face *cards.Face) {
	if !w.e.stackKeywordPossibleH(id, kwhTeamwork) || face == nil || !w.e.activationPhasesOK(w.p, face.SpellAbility()) ||
		!w.e.castWithFlashTargets(w.p, id, w.e.costPotentialTargets(w.p, id, spellScope("")), true) ||
		!w.e.castTargetsAvailable(w.p, id, face.SpellAbility()) {
		return
	}
	teamworkOffer(w, id, face, w.e.castOfferBase(w.p, id), true)
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
