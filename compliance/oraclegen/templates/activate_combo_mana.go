package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// comboManaColourPrefix selects the XMage per-colour mana ability matching
// gorge's resolved choice for a plain tap-for-mana Combo ability.
func comboManaColourPrefix(sa *cards.SA, cost string, step int, res rules.OracleResult) (string, bool) {
	if sa.API != "Mana" || strings.TrimSpace(cost) != "T" {
		return "", false
	}
	produced := strings.Fields(sa.ParamStr(cards.PKProduced))
	if len(produced) < 3 || produced[0] != "Combo" || len(produced) > 4 {
		return "", false
	}
	allowed := make(map[string]bool, len(produced)-1)
	for _, colour := range produced[1:] {
		if len(colour) != 1 || !strings.Contains("WUBRG", colour) || allowed[colour] {
			return "", false
		}
		allowed[colour] = true
	}
	if step < 0 || len(res.Snapshots) <= step+1 || len(res.Snapshots[step+1].Players) == 0 {
		return "", false
	}
	pool := res.Snapshots[step+1].Players[0].Pool
	if len(pool) == 0 || !allowed[string(pool[0])] {
		return "", false
	}
	for i := 1; i < len(pool); i++ {
		if pool[i] != pool[0] {
			return "", false
		}
	}
	return "{T}: Add {" + string(pool[0]) + "}", true
}

// dropColourChoices removes queued colour answers once XMage selects the
// specific colour ability itself rather than asking a colour question.
func dropColourChoices(answers [][]oraclegen.XAnswer, step int) {
	if step < 0 || step >= len(answers) {
		return
	}
	kept := answers[step][:0]
	for _, answer := range answers[step] {
		if answer.Kind == "choice" && isColourName(answer.Value) {
			continue
		}
		kept = append(kept, answer)
	}
	answers[step] = kept
}
