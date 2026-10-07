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
	// Decks only. A token cannot be on the board before a TokenCreate (or a
	// copy-token mint) puts it there, so its probe is raised by
	// notePoolCard from the mint path instead of folded in here. Folding the
	// whole token table in (as this did) made layer4InPool true in every
	// match, so refreshDerivedTypes ran on every event whatever the decks
	// (emit-action.md E1).
	for _, deck := range cfg.Decks {
		for _, c := range deck {
			if visit(c) {
				return
			}
		}
	}
	return
}

// notePoolCard raises this match's pool gates for a card that entered play
// outside the deck census -- a minted token or a copy of an out-of-pool
// card. The gates only ever move false -> true within a game, so a replay
// reaches the same value at every log position (emit-action.md E1).
func (e *Engine) notePoolCard(c *cards.Card) {
	if c == nil {
		return
	}
	if c.SetsName() {
		e.setNameInPool = true
	}
	if c.ChangesTypes() {
		e.layer4InPool = true
	}
	if c.MayCarryControlStatic() {
		e.controlStaticInPool = true
	}
}
