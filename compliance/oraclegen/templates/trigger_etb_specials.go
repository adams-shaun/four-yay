// ETB causes for the entering-object filters a placed cast cannot serve
// (ticket g17, wave 3 "trigger with no recipe"): a token, a face-down
// permanent, a permanent of the source's chosen type, an opponent's
// permanent, and a creature with graveyard/exile provenance. Each shape has
// a turn-1 cause the engine's own matcher accepts or rejects:
//
//   - token: a one-token maker cast by the side the filter names (the
//     opponent's token needs a cast from p1's hand).
//   - faceDown: a Disguise probe is cast face down for {3} and stays down.
//   - ChosenType: the source's own as-enters choice leads the option list
//     with the creature types p0 owns, so the setup fixture supplies a
//     creature of the type the ask's first option names, and the cast of the
//     same card enters it.
//   - OppCtrl: the entering permanent is cast by p1.
//   - graveyard/exile provenance: Reanimate reanimates a creature from
//     p0's graveyard; Flicker returns a battlefield creature from exile.
//
// Where no cause plays, the filter keeps its named skip.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// chosenTypeProbes are the (setup, cast) creature pairs the source's
// as-enters type choice is led to: the owned creature types lead the option
// list, so the setup creature's type is the pick, and the cast creature — a
// DIFFERENT card of the same type, so the scenario ref names one object —
// enters as the permanent of the chosen type. The unrestricted asks pick
// Bear; the ValidTypes$-narrowed ones whose list names Elemental first pick
// Elemental.
var chosenTypeProbes = []struct{ setup, cast string }{
	{bearsProbe, "Bear Cub"},
	{"Air Elemental", "Arc Runner"},
}

// opponentETBProbes are the p1-cast permanents an "an opponent's <filter>
// enters" trigger's filter can accept. p1 casts on p0's turn, so only
// instant-speed permanents qualify: a Flash creature enters and the
// trigger's own matcher decides.
var opponentETBProbes = []string{"King Cheetah", "Ambush Viper"}

// etbSpecialFilterCauses builds the causes of an etb-other trigger whose
// filter is unservable by the cast/played probe walk, or false when the
// filter's qualifier is none of the shapes here.
func etbSpecialFilterCauses(reg *cards.Registry, name string, mode cards.TriggerMode, filter string) ([]triggerCause, bool) {
	switch etbUnservableFilter(filter) {
	case "token":
		return tokenEnterCauses(reg, name, mode, filter)
	case "faceDown":
		return faceDownETBCauses(reg, name)
	case "ChosenType":
		return chosenTypeETBCauses(reg, name)
	case "OppCtrl":
		return opponentETBCauses(reg, name, filter)
	}
	return nil, false
}

// tokenEnterCauses makes the token the filter demands: p0's token maker for
// a YouCtrl (or unqualified) alternative, and p1's for an OppCtrl one.
func tokenEnterCauses(reg *cards.Registry, name string, mode cards.TriggerMode, filter string) ([]triggerCause, bool) {
	if mode != cards.TriggerChangesZone && mode != cards.TriggerChangesZoneAll {
		return nil, false
	}
	you, opp := false, false
	for _, alt := range strings.Split(strings.ToLower(filter), ",") {
		if strings.Contains(alt, "oppctrl") {
			opp = true
		} else {
			you = true
		}
	}
	var out []triggerCause
	if you {
		if causes, why := tokenCauses(reg, name); why == "" {
			out = append(out, causes...)
		}
	}
	if opp {
		if causes, ok := opponentTokenEnterCauses(reg, name); ok {
			out = append(out, causes...)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// opponentTokenEnterCauses casts the one-token maker from p1's hand, so the
// token enters under the opponent's control. The cause's own trailing passes
// are p1-first (the caster holds priority after casting), and end with the
// trigger on the stack; the emitted resolve drains it.
func opponentTokenEnterCauses(reg *cards.Registry, name string) ([]triggerCause, bool) {
	var out []triggerCause
	for _, p := range tokenProbes {
		if p == name {
			continue
		}
		st, ok := castProbe(reg, p)
		if !ok {
			continue
		}
		st.Seat, st.Card = 1, "p1:"+p
		out = append(out, triggerCause{
			opponentHand: []string{p},
			steps:        []oraclegen.Step{{Op: "pass", Seat: 0}, st, {Op: "pass", Seat: 1}, {Op: "pass", Seat: 0}},
		})
	}
	return out, len(out) > 0
}

// faceDownETBCauses casts a Disguise probe face down for {3}; it enters face
// down and stays there, which is the state the trigger's filter names. No
// resolve step: the detection's passes resolve the cast and leave the ETB
// trigger on the stack.
func faceDownETBCauses(reg *cards.Registry, name string) ([]triggerCause, bool) {
	var out []triggerCause
	for _, probe := range turnedFaceUpDisguiseProbes {
		if probe == name || !oraclegen.XMageKnown(probe) {
			continue
		}
		card, ok := reg.Lookup(probe)
		if !ok || len(card.Faces) == 0 {
			continue
		}
		_, _, mode, _, why := morphFamilyCost(card.Faces[0])
		if why != "" {
			continue
		}
		st, ok := castProbe(reg, probe)
		if !ok {
			continue
		}
		st.Mana, st.CastMode = faceDownCastPool, mode
		out = append(out, triggerCause{
			hand:  []string{probe},
			steps: []oraclegen.Step{st},
		})
	}
	return out, len(out) > 0
}

// chosenTypeETBCauses places a creature of the type the source's as-enters
// ask offers first, then casts the same creature so a permanent of the chosen
// type enters. Each candidate is its own cause: the ask's pick follows the
// setup fixture, and the engine's own matcher (not this table) decides
// whether the entering probe is of the chosen type.
func chosenTypeETBCauses(reg *cards.Registry, name string) ([]triggerCause, bool) {
	var out []triggerCause
	for _, pair := range chosenTypeProbes {
		if pair.cast == name || pair.setup == name || !oraclegen.XMageKnown(pair.cast) {
			continue
		}
		c, ok := castCause(reg, name, pair.cast)
		if !ok {
			continue
		}
		c.battlefield = []string{pair.setup}
		out = append(out, c)
	}
	return out, len(out) > 0
}

// opponentETBCauses casts the entering permanent from p1's hand, so it
// enters under the opponent's control. p1 casts on p0's turn, so the probes
// are instant-speed; the trailing passes are p1-first and end with the
// trigger on the stack.
func opponentETBCauses(reg *cards.Registry, name, filter string) ([]triggerCause, bool) {
	var out []triggerCause
	for _, p := range opponentETBProbes {
		if p == name || !oraclegen.XMageKnown(p) {
			continue
		}
		if _, ok := reg.Lookup(p); !ok {
			continue
		}
		if etbAuraProbes[p] {
			continue
		}
		st, ok := castProbe(reg, p)
		if !ok {
			continue
		}
		st.Seat, st.Card = 1, "p1:"+p
		out = append(out, triggerCause{
			opponentHand: []string{p},
			steps:        []oraclegen.Step{{Op: "pass", Seat: 0}, st, {Op: "pass", Seat: 1}, {Op: "pass", Seat: 0}},
		})
	}
	return out, len(out) > 0
}

// etbHistoryProvenanceCauses serves an "enters, having come from the
// graveyard/exile" ChangesZoneAll filter: Reanimate reanimates a creature
// from p0's graveyard onto the battlefield, and Flicker returns a battlefield
// creature from exile, each satisfying the provenance the filter names. ok
// is false when the skip reason is the origin-named shape (which stays a
// skip) or no cause is fundable.
func etbHistoryProvenanceCauses(reg *cards.Registry, name string, t *cards.Trigger, filter string) ([]triggerCause, string, bool) {
	if why := zoneETBHistorySkip(t, filter); !strings.HasPrefix(why, "etb filter zone history") {
		return nil, "", false
	}
	grave, exile := false, false
	for _, alt := range strings.Split(strings.ToLower(filter), ",") {
		switch {
		case strings.Contains(alt, "graveyard"):
			grave = true
		case strings.Contains(alt, "exile"):
			exile = true
		}
	}
	var out []triggerCause
	if grave {
		if c, ok := castCause(reg, name, "Reanimate", "p0:"+bearsProbe); ok {
			c.graveyard = []string{bearsProbe}
			out = append(out, c)
		}
	}
	if exile {
		if c, ok := castCause(reg, name, "Flicker", "p0:"+bearsProbe); ok {
			c.battlefield = []string{bearsProbe}
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, "", false
	}
	return out, "", true
}
