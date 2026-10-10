package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// manaColourAllocation answers a Produced$ "Combo <colours>" mana ask. The
// decision carries the ask's offered colour set (OracleDecision.ManaColours,
// read off the ask's own options), and XMage's AddManaInAnyCombinationEffect
// poses one multi-amount message per colour of that set, in the set's own
// order, whatever the unit count -- a count-1 restricted set included
// (Muerra, Trash Tactician's Add R / Add G pair; agent 20261009T041408Z,
// cluster C2). Each message consumes one "X=<n>" amount answer in order, so
// the answers are one per offered colour carrying the number of units the
// engine allocated to it. The dialog's message order is the set's option
// order, which is what ManaColours records; a wrong order shows up as a
// stable harness or divergent row on the host replay, not a flake.
//
// A produced-Any ask (XMage's DynamicManaEffect: a colour dialog at one
// unit, a WUBRG multi-amount above it) never reaches here -- it carries no
// ManaColours and keeps the routing it already agrees with.
func manaColourAllocation(d rules.OracleDecision) []XAnswer {
	counts := make(map[string]int, len(d.ManaColours))
	for k := range d.Picks {
		if c, ok := manaAskPickColour(d, k); ok {
			counts[c]++
		}
	}
	as := make([]XAnswer, 0, len(d.ManaColours))
	for _, c := range d.ManaColours {
		as = append(as, XAnswer{d.Seat, "amount", strconv.Itoa(counts[c])})
	}
	return as
}

// manaAskPickColour names the mana symbol one picked option allocates: the
// pick's label ("Add R", with or without a cost prefix) is the authority,
// falling back to the option's index into the recorded set (the options are
// laid out unit-major over the set).
func manaAskPickColour(d rules.OracleDecision, k int) (string, bool) {
	if k < len(d.Picks) {
		label := d.Picks[k]
		if i := strings.LastIndex(label, ": "); i >= 0 {
			label = label[i+2:]
		}
		if strings.HasPrefix(label, "Add ") && len(label) == 5 {
			return label[4:5], true
		}
	}
	if k < len(d.PickIdx) && len(d.ManaColours) > 0 {
		return d.ManaColours[d.PickIdx[k]%len(d.ManaColours)], true
	}
	return "", false
}
