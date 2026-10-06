package templates

import "strings"

// craftCost reports a Craft keyword's activation cost: its material part
// (ExileCtrlOrGrave) together with the self-exile kwCraft always appends.
func craftCost(cost string) bool {
	return strings.Contains(cost, "ExileCtrlOrGrave") && strings.Contains(cost, "Exile<1/CARDNAME>")
}

// costAnswerKind is the TestPlayer queue a cost part's pick reaches. XMage's
// Craft material is a TargetCardInGraveyardBattlefieldOrStack, answered from
// the TARGET queue by plain card name; every other cost selector this
// template scripts is a makeChoose dialog on the choice queue.
func costAnswerKind(cost, head string) string {
	if head == "ExileCtrlOrGrave" && craftCost(cost) {
		return "target"
	}
	return "choice"
}
