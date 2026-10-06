package templates

import "strings"

// This file is the single home of the Discard<N/Filter> placement the activate
// template serves beyond the plain-card shapes: a card named like the source
// and a discard of the whole hand. activate.go's fixture adder and XMage answer
// scripter both read discardCostPlacement, so the card gorge discards and the
// card XMage is scripted to discard cannot disagree.

// legendaryHandCard is the legendary card the Discard<1/Card.Legendary>
// shapes are paid with.
const legendaryHandCard = "Ajani, Caller of the Pride"

// discardFilter reads a Discard token's count and filter text.
func discardFilter(tok string) (count, filter string, ok bool) {
	payload, ok := bracketPayload(tok)
	if !ok {
		return "", "", false
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

// discardNamedCard returns the card name of a Card.named<Name> filter.
func discardNamedCard(filter string) (string, bool) {
	i := strings.Index(filter, "Card.named")
	if i < 0 {
		return "", false
	}
	name := strings.TrimSpace(filter[i+len("Card.named"):])
	return name, name != ""
}

// discardFixtureSupported covers the single-card discard selectors: any card,
// a legendary card and a card with a literal name. A legendary card that must
// share a name with a legendary permanent (Key to the Side-Door's
// `sharesNameWith Valid Permanent.Legendary+YouCtrl`) stays a named gap: the
// engine's sharesNameWith reads only a Targeted/Remembered/TriggeredCard
// referent (effects/filter_attachment.go sharesNameWithArg), so it never
// offers the ability however the board is set. One deterministic hand fixture is
// sufficient and the engine/XMage answer translation selects it.
func discardFixtureSupported(tok string) bool {
	count, filter, ok := discardFilter(tok)
	if !ok || count != "1" {
		return false
	}
	if _, named := discardNamedCard(filter); named {
		return true
	}
	return strings.EqualFold(filter, "Card") || strings.EqualFold(filter, "Card.Legendary")
}

// discardCostPlacement is what a non-self Discard cost needs on p0's side:
// the hand cards it discards (and XMage is scripted to pick). A discard of the
// whole hand (Discard<0/Hand>) has nothing to place or pick.
func discardCostPlacement(tok string) []string {
	count, filter, ok := discardFilter(tok)
	if !ok {
		return nil
	}
	if count == "0" && strings.EqualFold(filter, "Hand") {
		return nil
	}
	if name, named := discardNamedCard(filter); named {
		return []string{name}
	}
	if strings.Contains(strings.ToLower(filter), "legendary") {
		return []string{legendaryHandCard}
	}
	if hand := discardCostFixtures(tok); len(hand) > 0 {
		return hand
	}
	return []string{"Wastes"}
}
