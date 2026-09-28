package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Exercise the real priority -> announcement -> payment -> AbilityPush path,
// not a fabricated call to the spent-mana trigger dispatcher. Sunken Palace's
// SpellAbilityCast rider applies to both targetless and targeted activations.
func TestSunkenPalaceManaSpentOnActivationQueuesTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cmdr := corpusCommander(t, reg, "Wort, Boggart Auntie")
	sunken := corpusCommander(t, reg, "Sunken Palace")
	for _, tc := range []struct {
		name, activated string
		targeted        bool
	}{
		{"targetless", "Wall of Water", false},
		{"targeted", "Indigo Faerie", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := colourIdentityGame(t, 921, FormatCommander, cmdr, nil, sunken, corpusCommander(t, reg, tc.activated))
			sunkenID := moveToBattlefieldByName(t, e, 0, "Sunken Palace")
			abilityID := moveToBattlefieldByName(t, e, 0, tc.activated)
			for _, id := range []state.ObjID{sunkenID, abilityID} {
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("activation fixture object %d is not on battlefield: %+v", id, o)
				}
			}
			pa, ok := e.G.Obj(abilityID).PileAbilityAt(0)
			if !ok || pa.SA == nil || pa.SA.API == "Mana" || pa.SA.Params["Cost"] != "U" {
				t.Fatalf("precondition: expected a non-mana U activation: %+v", pa)
			}
			// Attribute a U batch to the real corpus rider's source, leaving
			// the activation itself to consume it in payCast.
			e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1,
				Text: events.ManaRestrictionText("", sunkenID)})
			if len(e.G.Players[0].RestrictedMana) == 0 {
				t.Fatal("precondition: Sunken Palace mana batch was not recorded")
			}
			e.priorityRound()
			before := len(e.L.Events)
			opt := abilityOption(t, e, abilityID, 0)
			submitChoices(t, e, opt.Index)
			if tc.targeted {
				d := e.Pending()
				if d == nil || d.Kind != decision.KTarget {
					t.Fatalf("precondition: targeted activation did not ask for a target: %+v", d)
				}
				idx := -1
				for _, o := range d.Options {
					if o.Obj == abilityID {
						idx = o.Index
					}
				}
				if idx < 0 {
					t.Fatalf("target permanent not offered: %+v", d.Options)
				}
				submitChoices(t, e, idx)
			}
			if len(e.G.Stack) == 0 {
				t.Fatal("precondition: no activated ability on the stack")
			}
			if len(e.G.Players[0].RestrictedMana) != 0 {
				t.Fatalf("attributed mana was not consumed: %+v", e.G.Players[0].RestrictedMana)
			}
			// Submit drains queued triggers at the priority boundary; the
			// granted push is the replay-visible proof that the rider fired.
			pushes, targets := 0, 0
			for _, ev := range e.L.Events[before:] {
				if ev.Kind == events.TargetsChosen {
					targets++
				}
				if ev.Kind == events.GrantTriggerPush && ev.Obj == sunkenID {
					pushes++
					if tc.targeted && targets == 0 {
						t.Fatal("spent-mana trigger pushed before activation target was recorded")
					}
				}
			}
			if tc.targeted && targets == 0 {
				t.Fatal("targeted activation never recorded its target")
			}
			if pushes != 1 {
				t.Fatalf("spent-mana trigger pushes = %d, want one from Sunken Palace %d; pending=%+v", pushes, sunkenID, e.pendingTriggers)
			}
		})
	}
}
