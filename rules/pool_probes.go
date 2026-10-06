package rules

import "github.com/adams-shaun/gorge/cards"

// poolGates is the genesis-time pool census in ONE pass: the answers of
// poolHasSetNameStatic, poolHasLayer4Static and poolHasControlStatic over
// the same decks and token table. Each of those walks the ~840-entry token
// map on its own, and an engine is built per game, per sample attempt and per
// search world, so the three map walks were paid three times per genesis. The
// per-card probes are memoised (cards/card_probes.go); this folds the walks.
// The pass stops as soon as all three are known true.
func poolGates(cfg Config) (setName, layer4, control bool) {
	visit := func(c *cards.Card) bool {
		setName = setName || c.SetsName()
		layer4 = layer4 || c.ChangesTypes()
		control = control || c.MayCarryControlStatic()
		return setName && layer4 && control
	}
	for _, deck := range cfg.Decks {
		for _, c := range deck {
			if visit(c) {
				return
			}
		}
	}
	for _, c := range cfg.Tokens {
		if visit(c) {
			return
		}
	}
	return
}
