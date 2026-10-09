// Level-B static shapes for the ticket's three servable static modes
// (TapPowerValue, CastWithFlash, CantDraw). Each reports one exact parameter
// shape the matching template in compliance/oraclegen/templates serves; any
// other parameter keeps the requirement a named gap, so a shape a template
// cannot observe never changes class silently.
package levelb

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// tapPowerValueShape reports whether st is the Pilot-family TapPowerValue
// static the crew-offer template observes: "This creature saddles Mounts and
// crews Vehicles as though its power were N greater" (Cloudspire Captain) --
// ValidSA$ names only Crew/Saddle actions, ValidCard$ is Card.Self, Value$ is
// a plain integer, and no other parameter scopes it. Tapestry Warden's
// Station shape (Value$ Toughness, ValidCard$ powerLTtoughness) is a real
// shape the engine reads, but its observation needs the Station tap-pick
// decision, which no template scripts yet: it stays a named gap.
func TapPowerValueShape(st *cards.Static) bool {
	if !strings.EqualFold(st.Mode, "TapPowerValue") {
		return false
	}
	if !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.Self") {
		return false
	}
	v := strings.TrimSpace(st.ParamStr(cards.PKValue))
	if _, err := strconv.Atoi(v); err != nil && !strings.EqualFold(v, "Toughness") {
		return false
	}
	for alt := range strings.SplitSeq(st.ParamStr(cards.PKValidSA), ",") {
		kind, rest, ok := strings.Cut(strings.TrimSpace(alt), ".")
		if !ok || !strings.EqualFold(kind, "Activated") {
			return false
		}
		token, _, _ := strings.Cut(rest, "+")
		switch strings.TrimSpace(token) {
		case "Crew", "Saddle":
		default:
			return false
		}
	}
	for k := range st.Params {
		switch k {
		case "Mode", "Description", "Secondary", "ValidSA", "ValidCard", "Value":
		default:
			return false
		}
	}
	return true
}

// castWithFlashParams are the parameters the cast-with-flash template reads;
// the condition parameters ride the engine's shared static timing gate.
var castWithFlashParams = map[string]bool{
	"Mode": true, "Description": true, "Secondary": true,
	"ValidSA": true, "ValidCard": true, "Caster": true, "EffectZone": true,
	"IsPresent": true, "PresentZone": true, "PresentCompare": true,
	"CheckSVar": true, "SVarCompare": true,
}

// castWithFlashShape reports whether st is a CastWithFlash grant the
// template can observe by casting a spell at instant speed on the other
// player's main phase: ValidSA$ Spell, Caster$ you (or unset), ValidCard$ a
// spell filter the template's probe set can satisfy, and only parameters the
// engine's shared static timing gate (or the template's setup) models. The
// template proves the grant gorge-side per candidate, so a filter no probe
// satisfies still yields an item-less named skip rather than a silent gap.
func CastWithFlashShape(st *cards.Static) bool {
	if !strings.EqualFold(st.Mode, "CastWithFlash") {
		return false
	}
	if !strings.EqualFold(st.ParamStr(cards.PKValidSA), "Spell") {
		return false
	}
	if c := st.ParamStr(cards.PKCaster); c != "" && !strings.EqualFold(c, "You") {
		return false
	}
	for k := range st.Params {
		if !castWithFlashParams[k] {
			return false
		}
	}
	return castWithFlashFilterServable(st.ParamStr(cards.PKValidCard))
}

// castWithFlashFilterServable reports whether every comma alternative of the
// ValidCard$ filter is one word the template's probe set covers (a creature
// word for the Grizzly Bears probe, a noncreature spell word for Divination)
// or names the card itself.
func castWithFlashFilterServable(filter string) bool {
	for alt := range strings.SplitSeq(filter, ",") {
		alt = strings.TrimSpace(alt)
		if strings.EqualFold(alt, "Card.Self") {
			continue
		}
		words := strings.FieldsFunc(alt, func(r rune) bool { return r == '.' || r == '+' })
		if len(words) == 0 {
			return false
		}
		for _, w := range words {
			switch strings.ToLower(w) {
			case "card", "creature", "noncreature", "sorcery", "dragon":
			default:
				return false
			}
		}
	}
	return true
}

// cantDrawShape reports whether st is an unconditional CantDraw ("Players
// can't draw cards", Mornsong Aria): only display parameters and an optional
// ValidPlayer$ naming every player or you, which the draw-block observation
// covers (p0's hand cannot grow while the card is on the battlefield).
func CantDrawShape(st *cards.Static) bool {
	if !strings.EqualFold(st.Mode, "CantDraw") {
		return false
	}
	switch strings.ToLower(st.ParamStr(cards.PKValidPlayer)) {
	case "", "player", "you":
	default:
		return false
	}
	for k := range st.Params {
		switch k {
		case "Mode", "Description", "Secondary", "ValidPlayer":
		default:
			return false
		}
	}
	return true
}

// UntapOtherPlayerShape reports whether st is the "untap during each other
// player's untap step" shape the untap-step observation serves: the card
// itself (Thousand Moons Infantry, Bender's Waterskin) or each creature you
// control (Prop Room), and no other parameter beyond the display ones.
func UntapOtherPlayerShape(st *cards.Static) bool {
	if !strings.EqualFold(st.Mode, "UntapOtherPlayer") {
		return false
	}
	switch v := strings.ToLower(st.ParamStr(cards.PKValidCard)); v {
	case "", "card.self", "creature.youctrl":
	default:
		return false
	}
	for k := range st.Params {
		switch k {
		case "Mode", "Description", "ValidCard":
		default:
			return false
		}
	}
	return true
}
