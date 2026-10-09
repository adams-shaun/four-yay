package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// attackerCountPrelude builds the setup a "PlayerCountPlayers$AttackersDeclared"
// cost-reduction amount needs: p0 attacks with at least the compared number of
// creatures, then the probe is cast in the second main. The head counts the
// creatures that attacked this turn, so the probe cast happens after combat
// while the count still holds.
func attackerCountPrelude(reg *cards.Registry, compare string) ([]conditionPrelude, string) {
	n := staticCountFrom(compare)
	if n < 1 {
		n = 1
	}
	fillers := firstN(reg, attackFillers, "", n)
	if fillers == nil {
		return nil, "cost static condition: attacker fixture unavailable"
	}
	attackers := make([]string, len(fillers))
	for i, f := range fillers {
		attackers[i] = "p0:" + f
	}
	return []conditionPrelude{{battlefield: fillers, steps: []oraclegen.Step{
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers},
		{Op: "pass_to", Step: "main2"},
	}}}, ""
}

// attackerCountBody reports whether an SVar body is the PlayerCountPlayers$
// attacker head, so a caller can name it without re-spelling the string.
func attackerCountBody(body string) bool {
	return strings.Contains(strings.ToLower(body), "playercountplayers$attackersdeclared")
}
