package effects

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// param:api:GenericChoice.AtRandom. Forge's ChooseGenericEffect picks an
// AtRandom$ GenericChoice ITSELF (Aggregates.random over the choice list);
// the chooser is never asked. The corpus carriers (9 lines `AtRandom$ True`,
// 3 lines `AtRandom$ Urza`, measured at the FORGE_REF pin):
//
//   - Face to Face / Buzzing Whack-a-Doodle: one-choice "Notify"
//     GenericChoices, Forge's way of revealing the caster's secret pick
//     (ShowChoice$ ExceptSelf). A one-option ask to the caster is a
//     needless decision, so a single eligible choice never asks and never
//     draws from the rng.
//   - Whimsy, Faerie Dragon, Item Crate, Master of the Wild Hunt Avatar:
//     several choices, the random pick decides the effect.
//   - Urza, Academy Headmaster: `AtRandom$ Urza` is the same random pick
//     over the choices whose mandatory ValidTgts$ (if any) has a legal
//     target right now, so a targeted outcome with nothing to target is not
//     drawn.
//
// The pick is drawn at RESOLUTION from the engine's seeded rng (Host.Rand),
// exactly as Charm's Random$ pick and ChooseCard/NameCard's AtRandom$ picks
// are, so a log-only replay re-derives it; a Note records the outcome in the
// log for the players (the ShowChoice$ reveal). Every ask site honours this
// one predicate: effCharm (the controller path), charmGenericPlayersRun (the
// per-Defined$-player path) and rules' trigger placement ask, which declines
// a random GenericChoice so the draw happens once, at resolution.

// GenericChoiceAtRandom reports whether sa is an api:GenericChoice whose
// choice the engine picks at random (AtRandom$ True or AtRandom$ Urza). Any
// other AtRandom$ value is unread: the ordinary ask applies.
func GenericChoiceAtRandom(sa *cards.SA) bool {
	if sa == nil || sa.API != "GenericChoice" {
		return false
	}
	v := CharmOf(sa).AtRandom
	return strings.EqualFold(v, "True") || v == "Urza"
}

// genericChoiceRandomPick draws one of names (already filtered to the
// eligible/available choices, in Choices$ order) for chooser and records it
// with a Note. It returns "" when nothing can be picked (an empty list, or
// an Urza list whose every choice lacks a mandatory legal target), which the
// caller treats as "no choice is made". A single candidate is taken without
// consuming the rng.
func genericChoiceRandomPick(h Host, c *Ctx, sa *cards.SA, chooser state.PlayerID, names []string) string {
	pool := names
	if CharmOf(sa).AtRandom == "Urza" {
		pool = make([]string, 0, len(names))
		for _, name := range names {
			if genericChoiceTargetsFeasible(h, c, chooser, cards.ResolveSVar(c.SVars, name)) {
				pool = append(pool, name)
			}
		}
	}
	if len(pool) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "GenericChoice AtRandom$ had no eligible choice"})
		return ""
	}
	idx := 0
	if len(pool) > 1 {
		idx = h.Rand(len(pool))
	}
	name := pool[idx]
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
		Text: "chose a mode at random: " + CharmModeLabel(cards.ResolveSVar(c.SVars, name), name)})
	return name
}

// genericChoiceTargetsFeasible reports whether a choice body can be resolved
// with a legal target: a body with no ValidTgts$, or an optional one
// (TargetMin$ 0), always can; a mandatory one needs at least one legal
// target in the rules census.
func genericChoiceTargetsFeasible(h Host, c *Ctx, chooser state.PlayerID, sub *cards.SA) bool {
	if sub == nil {
		return false
	}
	if !TargetsOf(sub).Targeted() {
		return true
	}
	if strings.TrimSpace(TargetsOf(sub).Min.Text) == "0" {
		return true
	}
	return len(h.LegalTargets(chooser, c.Source, sub)) > 0
}

// genericChoiceRandomRun is effCharm's controller-path random GenericChoice:
// pick one eligible choice with the engine rng and resolve its body.
func genericChoiceRandomRun(h Host, c *Ctx, sa *cards.SA, choices []string) {
	name := genericChoiceRandomPick(h, c, sa, c.Controller, choices)
	if name == "" {
		return
	}
	if sub := cards.ResolveSVar(c.SVars, name); sub != nil {
		Resolve(h, c, sub)
	}
}

// randomObjIDs draws n distinct ids from pool (in pool order, without
// replacement) through the engine's seeded rng -- the ObjID twin of
// randomChoices, shared by the hidden-origin ChangeZone AtRandom$ picks. A
// pool of exactly n (or fewer) is taken whole without consuming the rng: no
// draw can change which cards move.
func randomObjIDs(h Host, pool []state.ObjID, n int) []state.ObjID {
	if n <= 0 || len(pool) == 0 {
		return nil
	}
	if n >= len(pool) {
		return append([]state.ObjID(nil), pool...)
	}
	rest := append([]state.ObjID(nil), pool...)
	picked := make([]state.ObjID, 0, n)
	for len(picked) < n {
		i := h.Rand(len(rest))
		picked = append(picked, rest[i])
		rest = append(rest[:i], rest[i+1:]...)
	}
	return picked
}

// ChooseTypeAtRandom reports whether an api:ChooseType draws its type at
// random (AtRandom$ True). effChooseType picks from the engine rng at
// resolution, and rules' as-enters choice builder declines to pose the
// entry ask for it, so the draw happens once, in the replacement body.
func ChooseTypeAtRandom(sa *cards.SA) bool {
	return sa != nil && sa.API == "ChooseType" && strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKAtRandom)), "True")
}

// aiLogicRandom reports AILogic$ Random: the script's note that an AI makes
// this choice at random. It does not change the rules (a player still
// chooses); it rides the ask as decision.Decision.AIRandom so unattended
// bots answer from their own seeded rng.
func aiLogicRandom(sa *cards.SA) bool {
	return sa != nil && CharmOf(sa).AILogicRandom
}

// aiRandomNoAskPick is the R-9 no-ask answer for a single-pick ask of n
// options: the first option, except for an AILogic$ Random ask (Face to
// Face's throw), which draws from the engine's seeded rng instead. A
// Repeat that re-poses a random choice until the answers differ (Face to
// Face replays a tied throw) must see fresh draws on every pass: the fixed
// first option would throw Rock against Rock to the Repeat cap. A
// one-option (or non-random) ask consumes no rng, so every other no-ask
// answer -- and every replay over one -- is unchanged.
func aiRandomNoAskPick(h Host, sa *cards.SA, n int) int {
	if n < 2 || !aiLogicRandom(sa) {
		return 0
	}
	return h.Rand(n)
}

// RandomTargetsAsk is the one honouring site of TargetsAtRandom$ (and its
// RandomNumTargets$ rider), whatever the carrying API: Forge's
// TargetSelection draws a random-target ability's targets itself
// (Aggregates.random over the legal candidates), and the chooser is never
// consulted. Every target ask -- the cast/activation ask, the trigger
// placement ask, the chained-sub asks and the mid-resolution ValidTgts$ ask
// -- builds its ordinary decision over every legal candidate and then hands
// it here just before posing it. For a random-target sa, the draw comes
// from the engine's seeded rng (Host.Rand), so a log-only replay re-derives
// it, and the decision is narrowed to exactly the drawn options (Min ==
// Max == the drawn count): the seat confirms the draw and can answer
// nothing else, and the logged intent names exactly what was drawn.
//
// The count is the decision's Max clamped to the candidates (Goblin Polka
// Band's announced TgtNum, Goblin Test Pilot's one), or with
// RandomNumTargets$ True a draw between the bounds (Orcish Catapult's "a
// random number of random target creatures"; at least one, since a zero
// draw would pose the empty-only ask and that no seat can answer).
// Candidates are drawn one at a time without replacement and a draw that
// would break a cross-candidate constraint the decision carries (a
// per-controller Group, a set property, a total budget) is skipped, so
// the narrowed decision always validates. A pool the count covers whole is
// taken without consuming the rng. It reports whether d was narrowed; a
// draw that cannot reach Min leaves d unchanged behind a Note -- the loud,
// permissive direction.
func RandomTargetsAsk(h Host, d *decision.Decision, sa *cards.SA) bool {
	tp := TargetsOf(sa)
	if d == nil || !tp.Has(TgtAtRandom) || len(d.Options) == 0 {
		return false
	}
	n := len(d.Options)
	lo, hi := d.Min, d.Max
	if hi < 0 || hi > n {
		hi = n
	}
	if lo < 0 {
		lo = 0
	}
	if lo > hi {
		lo = hi
	}
	count := hi
	if tp.Has(TgtRandomNum) {
		base := max(lo, 1)
		if hi > base {
			count = base + h.Rand(hi-base+1)
		}
	}
	if count <= 0 {
		return false
	}
	probe := *d
	probe.Min, probe.AllowNone = 0, true
	fits := func(picked []int) bool {
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: slices.Sorted(slices.Values(picked))}
		return probe.Validate(in) == nil
	}
	var picked []int
	if count >= n {
		all := make([]int, n)
		for i := range all {
			all[i] = i
		}
		if fits(all) {
			picked = all
		}
	}
	if picked == nil {
		rest := make([]int, n)
		for i := range rest {
			rest[i] = i
		}
		picked = make([]int, 0, count)
		for len(picked) < count && len(rest) > 0 {
			i := 0
			if len(rest) > 1 {
				i = h.Rand(len(rest))
			}
			idx := rest[i]
			rest = append(rest[:i], rest[i+1:]...)
			if fits(append(picked, idx)) {
				picked = append(picked, idx)
			}
		}
	}
	if len(picked) == 0 || len(picked) < lo {
		h.Emit(events.Event{Kind: events.Note, Obj: d.Source, Player: d.Player,
			Text: "TargetsAtRandom$ found no valid random draw; the chooser picks"})
		return false
	}
	slices.Sort(picked)
	opts := make([]decision.Option, len(picked))
	for i, idx := range picked {
		opts[i] = d.Options[idx]
		opts[i].Index = i
	}
	d.Options = opts
	d.Min, d.Max, d.AllowNone = len(opts), len(opts), false
	if d.AffordableTargets > len(opts) {
		d.AffordableTargets = len(opts)
	}
	d.Prompt = "Targets chosen at random: " + d.Prompt
	return true
}

// charmRandomOffer is NumRandomChoices$ (GenericChoice; Davriel, Soul
// Broker's "accept one of Davriel's offers" over three of eight): of the
// available choices (Choices$ order), only that many -- drawn without
// replacement from the engine's seeded rng -- are offered to chooser, kept
// in Choices$ order. A Note records the offer for the log. An absent,
// non-positive or covering count returns choices unchanged and consumes no
// rng. The draw is made at resolution, the same moment the ask is posed,
// so a log-only replay re-derives it.
func charmRandomOffer(h Host, c *Ctx, sa *cards.SA, chooser state.PlayerID, choices []string) []string {
	p := CharmOf(sa)
	if !p.NumRandomChoices.Present {
		return choices
	}
	n := int(numText(h, c, p.NumRandomChoices, 0))
	if n <= 0 || n >= len(choices) {
		return choices
	}
	rest := make([]int, len(choices))
	for i := range rest {
		rest[i] = i
	}
	picked := make([]int, 0, n)
	for len(picked) < n {
		i := h.Rand(len(rest))
		picked = append(picked, rest[i])
		rest = append(rest[:i], rest[i+1:]...)
	}
	slices.Sort(picked)
	out := make([]string, n)
	labels := make([]string, n)
	for i, idx := range picked {
		out[i] = choices[idx]
		labels[i] = CharmModeLabel(cards.ResolveSVar(c.SVars, out[i]), out[i])
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
		Text: "offered at random: " + strings.Join(labels, "; ")})
	return out
}
