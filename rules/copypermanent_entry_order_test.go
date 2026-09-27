package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCopyPermanentEntryOrderResumesRiders keeps a CopyPermanent resolution
// parked at a real Spike Feeder's entry-counter order ask. The replacement
// order changes the final counter total, and the granted Biomancy ETB must be
// live at entry but must not be pushed twice when the copy resumes.
func TestCopyPermanentEntryOrderResumesRiders(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int32
	}{
		{"scales-first", 6},
		{"evolution-first", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := searchTestRegistry(t)
			biomancy := lookup(t, reg, "Aggressive Biomancy")
			// Exercise CopyPermanent's post-entry memory rider alongside the
			// real card's granted ETB. This test-only parameter keeps the real
			// corpus carrier and all production replacement behavior.
			var copyAbility *cards.SA
			for _, ability := range biomancy.Faces[0].Abilities {
				if ability.API == "CopyPermanent" {
					copyAbility = ability
				}
			}
			if copyAbility == nil {
				t.Fatal("precondition: Aggressive Biomancy has no CopyPermanent ability")
			}
			copyAbility.Params["RememberTokens"] = "True"
			feeder := lookup(t, reg, "Spike Feeder")
			scales := lookup(t, reg, "Hardened Scales")
			evolution := lookup(t, reg, "Branching Evolution")
			wall := lookup(t, reg, "Wall of Stone")
			e, cfg := corpusEngineCfg(t, reg,
				[]*cards.Card{biomancy, feeder, scales, evolution}, []*cards.Card{wall})
			// Enter the carrier before the replacements are active, so this
			// setup move cannot itself park at the order ask under test.
			feederID := moveSeededCard(t, e, 0, feeder, state.ZBattlefield)
			wallID := moveSeededCard(t, e, 1, wall, state.ZBattlefield)
			sid := moveSeededCard(t, e, 0, scales, state.ZBattlefield)
			eid := moveSeededCard(t, e, 0, evolution, state.ZBattlefield)
			biomancyID := moveSeededCard(t, e, 0, biomancy, state.ZHand)
			if e.G.Obj(sid).Zone != state.ZBattlefield || e.G.Obj(eid).Zone != state.ZBattlefield ||
				e.G.Obj(feederID).Zone != state.ZBattlefield || e.G.Obj(wallID).Zone != state.ZBattlefield {
				t.Fatalf("precondition zones: Scales=%s Evolution=%s Feeder=%s Wall=%s", e.G.Obj(sid).Zone, e.G.Obj(eid).Zone, e.G.Obj(feederID).Zone, e.G.Obj(wallID).Zone)
			}
			if got := e.G.Obj(feederID).Counter("P1P1"); got != 2 {
				t.Fatalf("precondition: original Spike Feeder has %d entry counters, want 2", got)
			}
			// Spike Feeder's real ETB adds two counters. Establish its unmodified
			// baseline by moving a seeded copy through entry without modifiers.
			base, _ := tokenReplGame(t, 911, feeder)
			baseID := moveSeededCard(t, base, 0, feeder, state.ZBattlefield)
			if got := base.G.Obj(baseID).Counter("P1P1"); got != 2 {
				t.Fatalf("precondition: unmodified Spike Feeder entered with %d counters, want 2", got)
			}
			e.SetCounterAdder(0)
			addMana(t, e, 0, "GGUUUU")
			opt := castByName(t, e, 0, "Aggressive Biomancy")
			if opt == nil {
				t.Fatalf("Aggressive Biomancy not castable: %+v", e.Pending().Options)
			}
			submitChoices(t, e, opt.Index)
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose {
				t.Fatalf("expected X choose, got %+v", d)
			}
			x := -1
			for _, o := range d.Options {
				if o.Kind == "x" && o.Amount == 1 {
					x = o.Index
				}
			}
			if x < 0 {
				t.Fatalf("no X=1 option: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{x}}); err != nil {
				t.Fatal(err)
			}
			answerKTarget(t, e, feederID)
			for i := 0; i < 8; i++ {
				d = e.Pending()
				if d == nil || d.Kind != decision.KPriority {
					break
				}
				passPriorityOnce(t, e)
			}
			d = e.Pending()
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
				t.Fatalf("expected replacement-order ask before the copy enters, got %+v", d)
			}
			copyID := e.G.NextID - 1
			copyObj := e.G.Obj(copyID)
			if copyObj == nil || copyObj.Zone != state.ZLibrary {
				t.Fatalf("precondition: pending copy %d is not still in library: %+v", copyID, copyObj)
			}
			if e.G.Obj(copyID).Counter("P1P1") != 0 {
				t.Fatalf("pre-entry copy already has counters: %+v", e.G.Obj(copyID))
			}
			rememberedCopy := func() bool {
				for _, target := range e.G.Obj(biomancyID).Remembered {
					if !target.IsPlayer && target.Obj == copyID {
						return true
					}
				}
				return false
			}
			if rememberedCopy() {
				t.Fatalf("post-entry RememberTokens rider ran before entry answer: source remembered %d", copyID)
			}
			for _, ev := range e.L.Events {
				if ev.Kind == events.MoveZone && ev.Obj == copyID && ev.To == state.ZBattlefield {
					t.Fatalf("copy MoveZone folded before answer: %+v", ev)
				}
			}
			// Identify the order semantically from the option's replacement id,
			// not by assuming that presentation order is rules order.
			pick := -1
			for _, o := range d.Options {
				name := ""
				if ce := e.G.Obj(o.Obj); ce != nil && ce.Face() != nil {
					name = ce.Face().Name
				}
				if tc.name == "scales-first" && name == "Hardened Scales" {
					pick = o.Index
				}
				if tc.name == "evolution-first" && name == "Branching Evolution" {
					pick = o.Index
				}
			}
			if pick < 0 {
				t.Fatalf("could not identify desired order from options: %+v", d.Options)
			}
			// Clone while the mutable continuation cursor is parked at the
			// answer; resume the clone to exercise Engine.Clone's copy path.
			e = e.Clone()
			d = e.Pending()
			if d == nil || d.Kind != decision.KReplacement {
				t.Fatalf("cloned engine lost pending replacement order: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
				t.Fatal(err)
			}
			if got := e.G.Obj(copyID).Counter("P1P1"); got != tc.want {
				t.Fatalf("entry counters = %d, want %d for %s", got, tc.want, tc.name)
			}
			if !rememberedCopy() {
				t.Fatalf("post-entry RememberTokens rider did not remember copy %d", copyID)
			}
			if got := countGrantTriggerPush(e); got != 1 {
				t.Fatalf("granted ETB trigger pushed %d times at entry, want exactly once", got)
			}
			if grant := grantedTriggerCE(t, e, copyID, "Aggressive Biomancy"); grant == nil {
				t.Fatal("granted entry trigger is absent after resume")
			}
			// Resolve the granted fight and ensure the continuation did not
			// re-register/re-push that trigger.
			for i := 0; i < 30 && (len(e.G.Stack) > 0 || len(e.pendingTriggers) > 0); i++ {
				d = e.Pending()
				if d == nil {
					continue
				}
				switch d.Kind {
				case decision.KPriority:
					passPriorityOnce(t, e)
				case decision.KTriggerOrder:
					answerTriggerOrders(t, e)
				case decision.KTarget:
					answerKTarget(t, e, wallID)
				default:
					t.Fatalf("unexpected decision during granted Fight: %+v", d)
				}
			}
			if got := countGrantTriggerPush(e); got != 1 {
				t.Fatalf("granted trigger pushed %d times, want exactly once", got)
			}
			replayCheck(t, e, cfg)
		})
	}
}
