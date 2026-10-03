package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestReconfigureFaceDownKeepsCreature keeps the filter grammar and the
// layer walk on ONE answer for a face-down attached reconfigure card (the
// r2 review MINOR, same attach-time-internal-inconsistency class as the
// disclosed attachableTo divergence). rules/layers.go's
// chars.reconfigureTypeSwitch guards its CR 702.150c strip with
// `o.FaceDown && o.Zone == state.ZBattlefield`: a face-down battlefield
// permanent keeps its CR 708.5 set, because its printed face -- and with it
// the Reconfigure keyword the switch keys on -- does not exist while face
// down. effects/filter.go's hasType (the every-filter-read path: target
// offer, Count$Valid, cost candidates, statics' Affected$) carried the same
// strip WITHOUT the guard, so a face-down attached reconfigure card read
// non-Creature to the filter grammar while the layer walk still said
// Creature. The guard is now shared; both reads agree that the face-down
// battlefield card (whose printed face is Razorfield Ripper's own creature
// face) stays a creature, and the face-up attached form stays non-Creature.
//
// Reachability: not through the printed-ability path (abilities are hidden
// face down) -- an external Choices$-pool attach onto a manifested
// reconfigure card is the shape this leaf protects, so the next sibling
// that attaches an arbitrary object cannot reintroduce the split.
func TestReconfigureFaceDownKeepsCreature(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	g := state.NewGame([]string{"you", "them"})

	bearer := corpusObject(t, reg, g, "Grizzly Bears")

	// Precondition: the corpus carrier really is printed with Reconfigure
	// and really is a creature on its own face (both halves of the guard's
	// tension must be live, or the leaf proves nothing).
	c, ok := reg.Lookup("Razorfield Ripper")
	if !ok {
		t.Fatal("precondition: the corpus has no Razorfield Ripper")
	}
	if !c.Faces[0].HasKeyword("Reconfigure") {
		t.Fatal("precondition: Razorfield Ripper is not printed with Reconfigure")
	}

	// Face-up + attached: CR 702.150c strips Creature in every filter read.
	attachedUp := g.Obj(corpusObject(t, reg, g, "Razorfield Ripper").ID)
	attachedUp.AttachedTo = bearer.ID
	if MatchesObjectCtx(g, "Creature", attachedUp, SpecContext{You: 0}) {
		t.Fatal("the face-up attached reconfigure card must not read Creature (CR 702.150c)")
	}

	// Face-down + attached, ON the battlefield: the CR 708.5 set stands -- a
	// manifested permanent keeps being a creature, and the Reconfigure strip
	// must not fire because the keyword the strip keys on does not exist
	// while face down.
	attachedDown := g.Obj(corpusObject(t, reg, g, "Razorfield Ripper").ID)
	attachedDown.AttachedTo = bearer.ID
	attachedDown.FaceDown = true
	attachedDown.Zone = state.ZBattlefield
	if !MatchesObjectCtx(g, "Creature", attachedDown, SpecContext{You: 0}) {
		t.Fatal("the face-down battlefield attached reconfigure card must still read Creature (CR 708.5); the strip must not fire face-down")
	}

	// Unattached (either way): the creature face is live and nothing strips.
	unattached := g.Obj(corpusObject(t, reg, g, "Razorfield Ripper").ID)
	unattached.FaceDown = true
	unattached.Zone = state.ZBattlefield
	if !MatchesObjectCtx(g, "Creature", unattached, SpecContext{You: 0}) {
		t.Fatal("the face-down unattached reconfigure card must read Creature")
	}
}
