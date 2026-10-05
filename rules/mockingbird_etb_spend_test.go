package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMockingbirdETBSpendSelector uses the corpus script, not a synthetic
// selector: the cast's pay-time CastInfo must bound both the offered templates
// and the legality check when the recorded election is consumed.
func TestMockingbirdETBSpendSelector(t *testing.T) {
	t.Parallel()
	for _, lowerSpendAtEntry := range []bool{false, true} {
		name := "copy_eligible"
		if lowerSpendAtEntry {
			name = "revalidate_spend"
		}
		t.Run(name, func(t *testing.T) {
			e := handEngine(t, corpusAlternativeCard(t, "Mockingbird"))
			bear := e.G.AddObject(corpusAlternativeCard(t, "Grizzly Bears"), 1)
			dread := e.G.AddObject(corpusAlternativeCard(t, "Colossal Dreadmaw"), 1)
			bear.Zone, dread.Zone = state.ZBattlefield, state.ZBattlefield
			e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{bear.ID, dread.ID})
			id := e.G.Zone(state.ZHand, 0)[0]
			// Pay {2}{U}: the two creature mana values must straddle 3.
			e.G.Players[0].Pool[state.MC], e.G.Players[0].Pool[state.MU] = 2, 1
			castMode(t, e, id, "")
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || len(d.Options) < 3 || d.Options[0].Kind != "x" {
				t.Fatalf("expected X payment choice, got %+v", d)
			}
			submitChoices(t, e, 2)
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack || o.CastFlags&state.FlagManaSpent == 0 || o.ManaSpent != 3 {
				t.Fatalf("precondition: cast-spend capture on stack = %+v, want 3 with FlagManaSpent", o)
			}
			if bear.Zone != state.ZBattlefield || dread.Zone != state.ZBattlefield || bear.Face().ManaValue() != 2 || dread.Face().ManaValue() <= 3 {
				t.Fatalf("precondition: templates not on battlefield or mana values do not straddle 3: bear=%+v dread=%+v", bear, dread)
			}
			kr6ResolveTop(e)
			d = e.Pending()
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
				t.Fatalf("expected ETB election, got %+v", d)
			}
			choice := -1
			for _, opt := range d.Options {
				if opt.Obj == dread.ID {
					t.Fatalf("over-bound creature offered: %+v", d.Options)
				}
				if opt.Obj == bear.ID {
					choice = opt.Index
				}
			}
			if choice < 0 {
				t.Fatalf("eligible creature missing from offered set: %+v", d.Options)
			}
			if lowerSpendAtEntry {
				// A later spend-provenance update simulates a tighter bound while
				// the entry ask is outstanding. Revalidation must reject the
				// formerly legal template, not rely on the stale offered set.
				e.emit(events.Event{Kind: events.CastInfo, Obj: id, Counter: events.FlagsString(state.FlagManaSpent), Amount: 1})
				if got := e.G.Obj(id).ManaSpent; got != 1 {
					t.Fatalf("precondition: entry spend = %d, want 1", got)
				}
			}
			submitChoices(t, e, choice)
			kr6Settle(e)
			if !lowerSpendAtEntry {
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Face().Name != "Grizzly Bears" {
					t.Fatalf("copied template face or zone wrong: zone=%s face=%s", o.Zone, o.Face().Name)
				}
				der := e.Derived(id)
				if der.Power != 2 || der.Toughness != 2 || !slices.Contains(der.Types, "Bird") || !slices.Contains(der.Keywords, "Flying") {
					t.Fatalf("Grizzly Bears copy with Bird/flying = %+v", der)
				}
				if !hasEvent(e, events.ClonePermanent, id) {
					t.Fatal("eligible election did not copy")
				}
			} else if hasEvent(e, events.ClonePermanent, id) {
				t.Fatal("now-over-bound template was copied at replacement")
			}
			if hasNote(e, "unimplemented API Clone") {
				t.Fatal("supported carrier used the unimplemented fallback")
			}
		})
	}
}
