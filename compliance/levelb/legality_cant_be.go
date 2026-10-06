package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// The CantBeCast / CantBeActivated shapes beyond the combat-window pair in
// supportedLegalityStatic. Each is a "this option is not offered" claim the
// level-B static template observes with an Offered expectation and a
// source-removed control. A shape is recognized only when every parameter the
// script carries is one the template models, so an unmodelled condition stays
// a visible gap.

// cantBeShape names the sub-family serving st, or ok=false.
func cantBeShape(f *cards.Face, st *cards.Static) (sub string, ok bool) {
	switch strings.ToLower(st.Mode) {
	case "cantbecast":
		switch {
		case paramsAre(st, map[string]string{"ValidCard": "Card", "Condition": "PlayerTurn", "Caster": "Opponent"}):
			return "static.cant-be-cast-opponent-turn", true
		case paramsAre(st, map[string]string{"ValidCard": "Card.Self", "EffectZone": "All", "Caster": "Player.Active", "SVarCompare": "LE3"}, "CheckSVar") && yourTurnsSVar(f, st.Params["CheckSVar"]):
			return "static.cant-be-cast-first-turns", true
		}
	case "cantbeactivated":
		switch {
		case paramsAre(st, map[string]string{"Condition": "PlayerTurn", "Activator": "Opponent"}, "ValidCard", "AffectedZone") && coversCreatures(st.Params["ValidCard"]):
			return "static.cant-be-activated-opponent-turn", true
		case paramsAre(st, map[string]string{"ValidSA": "Activated"}, "ValidCard", "AffectedZone") && coversCreatures(st.Params["ValidCard"]):
			return "static.cant-be-activated-all", true
		case paramsAre(st, nil, "ValidCard") && enchantedBearer(st.Params["ValidCard"]):
			return "static.cant-be-activated-enchanted", true
		}
	}
	return "", false
}

// cantBeNamedGap is the named reason a CantBeCast / CantBeActivated shape the
// template cannot observe stays a gap, or "".
func cantBeNamedGap(st *cards.Static) string {
	switch strings.ToLower(st.Mode) {
	case "cantbeactivated":
		if strings.Contains(strings.ToLower(st.Params["ValidCard"]), "namedcard") {
			return "static CantBeActivated chosen-name needs an as-enters name choice"
		}
	case "cantbecast":
		if strings.Contains(strings.ToLower(st.Params["Caster"]), "attackedyou") {
			return "static CantBeCast attacked-you condition"
		}
		if _, ok := st.Params["NumLimitEachTurn"]; ok {
			return "static CantBeCast NumLimitEachTurn is not modelled by gorge (its first spell is refused too)"
		}
	}
	return ""
}

// paramsAre reports whether st carries exactly the want parameters (values
// compared case-insensitively), any of the optional names, and nothing but
// the Mode and display parameters besides.
func paramsAre(st *cards.Static, want map[string]string, optional ...string) bool {
	for k, v := range want {
		got, ok := st.Params[k]
		if !ok || !strings.EqualFold(got, v) {
			return false
		}
	}
	for k := range st.Params {
		if _, ok := want[k]; ok {
			continue
		}
		switch k {
		case "Mode", "Description", "Secondary":
			continue
		}
		if !containsString(optional, k) {
			return false
		}
	}
	return true
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// coversCreatures reports whether a ValidCard$ filter names creatures with no
// further restriction (the observation's probe is a creature). An empty
// filter is every object.
func coversCreatures(valid string) bool {
	if valid == "" {
		return true
	}
	for _, part := range strings.Split(valid, ",") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "creature", "card", "permanent":
			return true
		}
	}
	return false
}

// enchantedBearer reports whether a ValidCard$ filter is "the enchanted
// permanent" with no further restriction.
func enchantedBearer(valid string) bool {
	return strings.EqualFold(valid, "Permanent.EnchantedBy") || strings.EqualFold(valid, "Card.EnchantedBy")
}

// yourTurnsSVar reports whether a CheckSVar$ value counts the controller's own
// turns, written inline or through an SVar ("Z" -> Count$YourTurns).
func yourTurnsSVar(f *cards.Face, check string) bool {
	if strings.EqualFold(check, "Count$YourTurns") {
		return true
	}
	return check != "" && !strings.Contains(check, "$") && strings.EqualFold(f.SVars[check], "Count$YourTurns")
}
