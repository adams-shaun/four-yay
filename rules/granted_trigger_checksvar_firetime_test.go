package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// A granted AddTrigger$ line's CheckSVar$/SVarCompare$ fire-time condition
// belongs to the GRANTOR, not to the affected recipient the trigger names as
// source: the condition is part of the trigger LINE, whose SVar table is the
// grantor's -- the same table events.Apply resolves the Execute$ body from.
// level_up and nerd_rage are the corpus shape (an Aura granting its enchanted
// creature an attacking trigger carrying "CheckSVar$ X"). If the fire-time
// read used the recipient's face table, that face's same-named X would answer
// instead and the trigger would fail closed (or fire) wrongly.
const firetimeGrantorSrc = "Name:Test Firetime Grantor\nManaCost:2\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddTrigger$ CastTrig\n" +
	"SVar:CastTrig:Mode$ SpellCast | ValidCard$ Artifact | ValidActivatingPlayer$ You | CheckSVar$ X | SVarCompare$ EQ2 | Execute$ TrigDraw | TriggerZones$ Battlefield\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n" +
	"SVar:X:2\nOracle:x\n"

const firetimeRecipientSrc = "Name:Test Firetime Recipient\nManaCost:2\nTypes:Creature\nPT:1/1\n" +
	"SVar:X:1\nOracle:x\n"

// TestGrantedTriggerFiretimeCheckSVarReadsGrantor proves a cross-object
// AddTrigger$ grant's fire-time CheckSVar$/SVarCompare$ reads the grantor's
// SVar table. The grantor's X is 2 and the recipient's X is 1; the trigger's
// compare is EQ2, so it fires only against the grantor's table. Under the
// pre-fix recipient read the trigger failed closed and never queued, leaving
// the trinket spell's own draw as the only one.
func TestGrantedTriggerFiretimeCheckSVarReadsGrantor(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 917, firetimeGrantorSrc, firetimeRecipientSrc, artifactSpellSrc, plainSifterSrc)
	grantor := moveSeeded(t, e, 0, firetimeGrantorSrc, state.ZBattlefield)
	recipient := moveSeeded(t, e, 0, firetimeRecipientSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, artifactSpellSrc, state.ZHand)
	g, r := e.G.Obj(grantor), e.G.Obj(recipient)
	if g == nil || r == nil || g.Zone != state.ZBattlefield || r.Zone != state.ZBattlefield {
		t.Fatal("precondition: grantor and recipient must both be on the battlefield")
	}
	if gx, rx := g.Face().SVars["X"], r.Face().SVars["X"]; gx != "2" || rx != "1" {
		t.Fatalf("precondition: grantor X=%q recipient X=%q, want 2 and 1", gx, rx)
	}
	addMana(t, e, 0, "UUU")
	castCardNow(t, e, "Test Trinket Spell")
	spellOnStack(t, e, "Test Trinket Spell")
	// The granted-trigger handler ran: the granted trigger queued as a stack
	// ability whose source is the recipient and whose trigger line was
	// recorded (the line is what carries CheckSVar$/SVarCompare$).
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
		t.Fatal("granted trigger did not queue: fire-time CheckSVar$ read the recipient's X=1 instead of the grantor's X=2")
	}
	before := countDraws(e, 0)
	drainCopyGrants(t, e, 40, true, true)
	// The granted trigger's one draw plus the trinket spell's own one.
	if got := countDraws(e, 0) - before; got != 2 {
		t.Fatalf("draws = %d, want granted trigger 1 plus spell 1", got)
	}
	replayCheck(t, e, cfg)
}
