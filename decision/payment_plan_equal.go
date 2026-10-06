package decision

// Typed equality for the offered-witness membership check (Validate's
// payment branch), replacing reflect.DeepEqual with exactly its strictness:
// a nil and an empty non-nil Activations list DIFFER (ClonePaymentPlan
// preserves the distinction for this check), and a Consequence compares by
// pointee, both nil or both present and equal.
// TestPaymentPlanEqualMatchesDeepEqual holds it to DeepEqual and to the
// types' field counts.

// Equal reports whether p and q are the same witness, field for field.
func (p *PaymentPlan) Equal(q *PaymentPlan) bool {
	if p.Version != q.Version || p.ID != q.ID || p.Cost != q.Cost ||
		p.PoolSpend != q.PoolSpend || p.PoolAfter != q.PoolAfter ||
		(p.Activations == nil) != (q.Activations == nil) || len(p.Activations) != len(q.Activations) {
		return false
	}
	for i := range p.Activations {
		if !p.Activations[i].Equal(&q.Activations[i]) {
			return false
		}
	}
	return true
}

// Equal reports whether a and b authorize the same activation.
func (a *PaymentActivation) Equal(b *PaymentActivation) bool {
	if a.Source != b.Source || a.SourceZoneSeq != b.SourceZoneSeq || a.Ability != b.Ability || a.Produces != b.Produces {
		return false
	}
	if a.Consequence == nil || b.Consequence == nil {
		return a.Consequence == b.Consequence
	}
	return *a.Consequence == *b.Consequence
}
