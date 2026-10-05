package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// The scripts are loaded from the pinned corpus; no Forge text is embedded in
// the repository. A pending Teamwork cast is the only pre-payment reader that
// may select the Teamwork target-bound branch.
func TestTeamworkTargetAnnouncementEligibility(t *testing.T) {
	t.Parallel()
	for _, card := range []string{"Cruel Alliance", "Too Evil to Stay Dead"} {
		t.Run(card, func(t *testing.T) {
			e, _, _ := conspireEngine(t, card)
			spell := searchMoveByName(t, e, card, state.ZStack)
			if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || o.Face() == nil {
				t.Fatalf("precondition: corpus spell must be on stack, got %+v", o)
			}
			// The two branches in both scripts differ: the root target minimum
			// is Count$Teamwork.0.1 and the linked target minimum is .1.0.
			// Bind the same pending-cast state targetBoundCtx receives between
			// the real cast option and target announcement.
			e.cast = &pendingCast{card: spell, player: 0, mode: "teamworked"}
			root := e.G.Obj(spell).Face().SpellAbility()
			if root == nil || root.Sub == nil {
				t.Fatal("precondition: expected linked target abilities")
			}
			rootMin, _ := e.resolvedTargetBounds(0, spell, root, 0)
			linkedMin, _ := e.resolvedTargetBounds(0, spell, root.Sub, 0)
			if rootMin != 0 || linkedMin != 1 {
				t.Fatalf("Teamwork intent target minima root=%d linked=%d, want 0 and 1", rootMin, linkedMin)
			}
			// Declining the optional payment leaves the intent branch in force
			// for this announcement; the later paid flag is not the criterion.
			e.cast.teamworkDone = true
			e.cast.teamworkPaid = false
			rootMin, _ = e.resolvedTargetBounds(0, spell, root, 0)
			linkedMin, _ = e.resolvedTargetBounds(0, spell, root.Sub, 0)
			if rootMin != 0 || linkedMin != 1 {
				t.Fatalf("declined payment changed announced target branch: root=%d linked=%d", rootMin, linkedMin)
			}
		})
	}
}
