package v1agent

// Hooks for a caller that drives Tactical from a board it built itself
// rather than from a kernel decision's scan groups: the v2 shadow
// (internal/spellbench/v2shadow, policy shadow-generic) synthesizes a
// kernel view from a SpellBench v2 observation and asks for whole combat
// declarations. They read and update exactly the state Choose's own combat
// paths do.

// PlanAttackFor is the attack declaration Tactical would plan on d's board
// over the creatures named by arena id: the attacking ones map to true.
func (t *Tactical) PlanAttackFor(d *Decision, attackers []uint32) map[uint32]bool {
	b := NewBoard(d)
	t.observeBlocks(b)
	t.observeAttacks(b)
	if t.gen && t.deckList == nil {
		t.observeOwnCards(b)
	}
	var cands []*KCard
	for _, a := range attackers {
		if c := b.Card(a); c != nil {
			cands = append(cands, c)
		}
	}
	return t.planAttack(b, cands)
}

// PlanBlocksFor is the block declaration Tactical would plan on d's board
// (its combat names the attackers): blocker arena id -> attacker arena id.
func (t *Tactical) PlanBlocksFor(d *Decision) map[uint32]uint32 {
	b := NewBoard(d)
	t.observeBlocks(b)
	t.observeAttacks(b)
	if t.gen && t.deckList == nil {
		t.observeOwnCards(b)
	}
	return t.planBlocks(b)
}

// KernelCardID is the kernel card-database id of a card name.
func KernelCardID(name string) (uint16, bool) {
	k := KernelCardByName(name)
	if k == nil {
		return 0, false
	}
	for i := range kernelCards {
		if &kernelCards[i] == k {
			return uint16(i), true
		}
	}
	return 0, false
}
