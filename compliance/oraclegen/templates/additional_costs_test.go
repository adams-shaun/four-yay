package templates

import (
	"testing"
)

// TestAdditionalCostCardsGenerate: a spell whose additional cost is a card
// shape the fixture did not provide must still get a scenario. Two shapes:
// "discard a card" (a spare card in hand) and AlternateAdditionalCost with a
// {2} buyout (the extra generic mana the generator now tries). Each card was
// skipped as "no fixture gorge can cast" before.
func TestAdditionalCostCardsGenerate(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{
		// Discard an additional card.
		"Seize the Spoils",
		"Laughing Mad",
		"Grab the Prize",
		"Sazacap's Brew",
		"Demand Answers",
		// AlternateAdditionalCost ... :2 -- pay {2} instead.
		"Titania, Rugged Rumbler",
		"Kinsbaile Aspirant",
		"Mudbutton Cursetosser",
		"Silvergill Mentor",
		"Soulbright Seeker",
		"Pumpkin Bombardment",
		"Louisoix's Sacrifice",
	} {
		if _, skip := Generate(reg, name); skip != nil {
			t.Errorf("%s: %s", name, skip.Reason)
		}
	}
}
