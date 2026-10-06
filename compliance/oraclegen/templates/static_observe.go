// The characteristics a continuous static can move besides P/T and evergreen
// keywords (ticket levelb-static-cda-types-control): a card's own
// characteristic-defining P/T, a type or colour change, and a control change.
// Every one is an ordinary field of the frozen permanents line, so a served
// item compares it with no opt-in; what this file decides is only whether
// gorge's final snapshot shows the effect, so a static that lands on nothing
// observable stays a skip.
package templates

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// staticChars is the printed type line and colours of a face, in the
// comparison's canonical spelling.
type staticChars struct {
	types  string
	colors string
}

// printedStaticChars reads the characteristics a face prints. Colours come
// from effects.ColorsOf on a face-only object, the same reader the snapshot's
// layer walk starts from, so a printed colour indicator, a mana cost and a
// colour-defining ability all count as printed.
func printedStaticChars(f *cards.Face) staticChars {
	return staticChars{
		types:  canonStaticTypes(f.Types),
		colors: canonStaticColors(effects.ColorsOf(&state.Object{CopyFace: f})),
	}
}

// snapStaticChars is the same pair read from a snapshot permanent.
func snapStaticChars(p rules.OracleSnapPerm) staticChars {
	return staticChars{types: canonStaticTypes(p.Types), colors: canonStaticColors(p.Colors)}
}

func canonStaticTypes(ts []string) string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, strings.ToLower(strings.ReplaceAll(t, " ", "")))
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// canonStaticColors keeps the WUBRG letters in WUBRG order, the comparator's
// spelling of a colour set.
func canonStaticColors(s string) string {
	var b strings.Builder
	for _, c := range "WUBRG" {
		if strings.ContainsRune(strings.ToUpper(s), c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// staticSelfCDA reports whether the static is a characteristic-defining one
// on the card itself (Tarmogoyf's "*/* ... equal to"): its output is the
// card's own P/T, whatever value gorge computes.
func staticSelfCDA(st cards.Static) bool {
	if !strings.EqualFold(strings.TrimSpace(st.ParamStr(cards.PKCharacteristicDefining)), "True") {
		return false
	}
	aff := strings.TrimSpace(st.ParamStr(cards.PKAffected))
	return aff == "" || strings.Contains(aff, "Self")
}

// staticCharsMoved reports whether a permanent's types or colours differ from
// the printed ones.
func staticCharsMoved(p rules.OracleSnapPerm, printed staticChars) bool {
	return snapStaticChars(p) != printed
}

// staticObserveGap names why a static whose probes and card showed no change
// is unobservable when the cause is one of the shapes this file widens the
// observation for but the fixture still cannot reach; "" is the next gap's
// turn. A removal of abilities shows only on a permanent that has some, and
// the fixture's creatures are vanilla; a characteristic-defining P/T on a
// non-creature (an uncrewed Vehicle) is not in the snapshot, which prints P/T
// for creatures only.
func staticObserveGap(f *cards.Face, st cards.Static) string {
	switch {
	case st.HasParam(cards.PKRemoveAllAbilities):
		return "removes the abilities of a permanent the fixture gives none"
	case staticSelfCDA(st) && !f.IsCreature() && (st.HasParam(cards.PKSetPower) || st.HasParam(cards.PKSetToughness)):
		return "characteristic-defining P/T of a non-creature (the snapshot omits its P/T)"
	}
	return ""
}
