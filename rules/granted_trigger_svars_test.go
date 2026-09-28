package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// An AddTrigger grant's Execute$ body and its nested SVar both belong to
// the grantor, even though the stack ability names the affected creature as
// Source. The grant can end while that ability waits on the stack.
const foreignDrawGrant = "Name:Test Foreign Grantor\nManaCost:2\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddTrigger$ CastTrig\n" +
	"SVar:CastTrig:Mode$ SpellCast | ValidCard$ Artifact | ValidActivatingPlayer$ You | Execute$ TrigDraw | TriggerZones$ Battlefield\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ N\n" +
	"SVar:N:2\nOracle:x\n"

const foreignDrawRecipient = "Name:Test Foreign Recipient\nManaCost:2\nTypes:Creature\nPT:1/1\n" +
	"SVar:N:1\nOracle:x\n"

func TestGrantedTriggerUsesGrantorSVarsAfterGrantExpires(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 914, foreignDrawGrant, foreignDrawRecipient, artifactSpellSrc, plainSifterSrc)
	grantor := moveSeeded(t, e, 0, foreignDrawGrant, state.ZBattlefield)
	recipient := moveSeeded(t, e, 0, foreignDrawRecipient, state.ZBattlefield)
	moveSeeded(t, e, 0, artifactSpellSrc, state.ZHand)
	if g, r := e.G.Obj(grantor), e.G.Obj(recipient); g == nil || r == nil || g.Zone != state.ZBattlefield || r.Zone != state.ZBattlefield || g.Face().SVars["N"] == r.Face().SVars["N"] {
		t.Fatal("precondition: grantor and recipient must be in play with different N SVars")
	}
	addMana(t, e, 0, "UUU")
	castCardNow(t, e, "Test Trinket Spell")
	spellOnStack(t, e, "Test Trinket Spell")
	found := false
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && o.Source == recipient && o.Ability != nil && o.Zone == state.ZStack {
			if _, ok := e.triggerLines[id]; !ok {
				t.Fatal("precondition: granted stack ability has no recorded trigger line")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("precondition: no granted ability from recipient on the stack")
	}
	// Once the grantor leaves, neither a live-grant scan nor the recipient's
	// same-named SVar is allowed to supply the resolving body's N.
	e.emit(events.Event{Kind: events.MoveZone, Obj: grantor, From: state.ZBattlefield, To: state.ZExile})
	if e.G.Obj(grantor).Zone != state.ZExile {
		t.Fatal("precondition: grant did not end")
	}
	before := countDraws(e, 0)
	drainCopyGrants(t, e, 40, true, true)
	// One draw from the grant plus the artifact spell's one printed draw.
	if got := countDraws(e, 0) - before; got != 3 {
		t.Fatalf("draws = %d, want grantor N=2 plus spell's 1 (recipient N=1)", got)
	}
	replayCheck(t, e, cfg)
}
