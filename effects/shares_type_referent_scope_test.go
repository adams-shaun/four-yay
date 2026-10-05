package effects

import "testing"

func TestSharesTypePredicatesKeepNameOnlyReferentsUnknown(t *testing.T) {
	// These referents were admitted only for sharesNameWith. Accepting them in
	// the separate type/colour grammar silently widens unrelated card filters.
	for _, predicate := range []string{
		"Card.sharesCardTypeWith ",
		"Creature.sharesCreatureTypeWith ",
		"Card.sharesCardTypeWithOther ",
		"Card.sharesAllCardTypesWithOther ",
		"Card.SharesColorWithOther ",
	} {
		for _, referent := range []string{"Targeted", "Remembered", "TriggeredCard"} {
			spec := predicate + referent
			if got := UnknownPredicates(spec); len(got) == 0 {
				t.Errorf("UnknownPredicates(%q) = empty; name-only referent must remain unsupported here", spec)
			}
		}
	}
}
