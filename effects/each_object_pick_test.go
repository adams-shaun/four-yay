package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The non-library object paths of Forge's EACH multi-type search grammar
// ("EACH Creature & Land") used to keep a FLAT union pick: the union matcher
// made every listed type's candidates reachable, but the pick followed the
// ordinary ChangeNum count, so "a creature and/or a land" could move one card
// from the union where the script means one of EACH. The hidden-library
// search's structured branch carried its own remainders: a per-type
// ChangeNum$ above 1 degraded to a flat count behind a loud Note, a
// quantity-only EACH kept the mandatory-find reading over the union, and
// overlapping sub-specs offered one card in both Groups so picking it in one
// blocked the other. These leaves pin the closed behaviour.

// eachSyntheticChangeZone builds a resolved ChangeZone SA of the shape the
// row's carriers use, with the given params.
func eachSyntheticChangeZone(params map[string]string) *cards.SA {
	p := map[string]string{"Origin": "Library", "Destination": "Hand"}
	for k, v := range params {
		p[k] = v
	}
	return &cards.SA{API: "ChangeZone", Params: p}
}

// eachSetZone replaces p's zone list with ids and stamps each object's own
// Zone field the way an engine move would (the SetZone list alone does not).
func eachSetZone(g *state.Game, p state.PlayerID, z state.Zone, ids ...state.ObjID) {
	g.SetZone(z, p, append([]state.ObjID(nil), ids...))
	for _, id := range ids {
		g.Obj(id).Zone = z
	}
}

// TestEachHandMoveAsksOneOfEachListedType is the object-path fix, driven by
// the Michelangelo Improvisers shape (Origin$ Hand | Destination$ Battlefield
// | ChangeType$ EACH Creature & Land | Optional$ True): the ask is one card
// of EACH listed type (Max 2 -- one Group per type), not a flat one-of-union
// pick (the pre-fix Max 1), and the answered pair moves both.
func TestEachHandMoveAsksOneOfEachListedType(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Src\nTypes:Sorcery\nOracle:x\n"), 0)
	crea := h.g.AddObject(mkCard(t, "Name:Grizzly\nTypes:Creature Bear\nOracle:x\n"), 0)
	land := h.g.AddObject(mkCard(t, "Name:Hill\nTypes:Land Mountain\nOracle:x\n"), 0)
	inst := h.g.AddObject(mkCard(t, "Name:Bolt\nTypes:Instant\nOracle:x\n"), 0)
	h.g.SetZone(state.ZHand, 0, []state.ObjID{src.ID, crea.ID, land.ID, inst.ID})
	eachSetZone(h.g, 0, state.ZHand, src.ID, crea.ID, land.ID, inst.ID)
	// Precondition the assertion depends on: both listed types ARE in the
	// hand the rule reads, and the instant is not a candidate of either.
	for _, id := range []state.ObjID{crea.ID, land.ID, inst.ID} {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZHand {
			t.Fatalf("precondition failed: object %d is not in hand: %+v", id, o)
		}
	}
	sa := eachSyntheticChangeZone(map[string]string{
		"Origin": "Hand", "Destination": "Battlefield",
		"ChangeType": "EACH Creature & Land", "Optional": "True",
	})
	sh := &suspendHost{fakeHost: *h}
	Resolve(sh, &Ctx{Source: src.ID, Controller: 0}, sa)
	d := sh.asked
	// The explicit Optional$ True marker poses Forge's confirm-before-pick
	// gate first: accept it, then the structured pick follows.
	if d == nil || d.ResumeKind != "hand_move_confirm" {
		t.Fatalf("decision = %+v, want the hand_move_confirm gate (Optional$ True asks before the pick)", d)
	}
	sh.suspended = false // the double's suspension flag: the resume is a fresh pass
	Resolve(sh, &Ctx{Source: src.ID, Controller: 0,
		HandMoveConfirmDone: true, HandMoveConfirm: "yes", HandMoveConfirmTarget: 0}, sa)
	d = sh.asked
	if d == nil {
		t.Fatal("no hand-move decision was posed (the feature never ran)")
	}
	if d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("decision = %+v, want a KChoose hand_move", d)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("range = %d..%d, want 0..2 (one of EACH listed type; the pre-fix flat pick was 0..1)", d.Min, d.Max)
	}
	if len(d.Options) != 2 {
		t.Fatalf("%d options, want one per nonempty group: %+v", len(d.Options), d.Options)
	}
	var creaIdx, landIdx = -1, -1
	for i, o := range d.Options {
		switch o.Obj {
		case crea.ID:
			creaIdx = i
			if o.Group != "0" {
				t.Fatalf("creature option group = %q, want the Creature sub-spec's ordinal \"0\"", o.Group)
			}
		case land.ID:
			landIdx = i
			if o.Group != "1" {
				t.Fatalf("land option group = %q, want the Land sub-spec's ordinal \"1\"", o.Group)
			}
		case inst.ID:
			t.Fatal("the instant was offered: it matches neither listed type")
		}
	}
	if creaIdx < 0 || landIdx < 0 {
		t.Fatalf("precondition failed: the offered options %+v do not cover both listed types", d.Options)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{creaIdx, landIdx}}); err != nil {
		t.Fatalf("one-of-each answer rejected: %v", err)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err == nil && d.Options[0].Group == d.Options[1].Group {
		t.Fatal("two same-group choices accepted: the per-Group cap must refuse them")
	}
	// The answer (one creature AND one land) re-enters and moves BOTH.
	sh.suspended = false // the double's suspension flag: the resume is a fresh pass
	fresh := &Ctx{Source: src.ID, Controller: 0,
		HandMove: []state.ObjID{crea.ID, land.ID}, HandMoveDone: true, HandMoveTarget: 0}
	h.log = nil
	Resolve(sh, fresh, sa)
	moved := 0
	for _, ev := range sh.log {
		if ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
			moved++
		}
	}
	if moved != 2 {
		t.Fatalf("%d creature/land MoveZone events, want both answered cards moved: %v", moved, sh.log)
	}
	for _, id := range []state.ObjID{crea.ID, land.ID} {
		o := h.g.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("object %d did not move to the battlefield (on battlefield: %v)", id, o != nil && o.Zone == state.ZBattlefield)
		}
	}
}

// TestEachHiddenPickAsksOneOfEachListedType is the public-origin object-path
// fix, driven by the Druidic Ritual shape (Hidden$ True | Origin$ Graveyard |
// Destination$ Hand | ChangeType$ EACH Creature.YouOwn & Land.YouOwn): the
// ask is one of EACH listed type, and the answered pair returns both.
func TestEachHiddenPickAsksOneOfEachListedType(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Ritual\nTypes:Sorcery\nOracle:x\n"), 0)
	crea := h.g.AddObject(mkCard(t, "Name:Zed\nTypes:Creature Zombie\nOracle:x\n"), 0)
	land := h.g.AddObject(mkCard(t, "Name:Bog\nTypes:Land Swamp\nOracle:x\n"), 0)
	other := h.g.AddObject(mkCard(t, "Name:Page\nTypes:Instant\nOracle:x\n"), 0)
	h.g.SetZone(state.ZGraveyard, 0, []state.ObjID{crea.ID, land.ID, other.ID})
	eachSetZone(h.g, 0, state.ZGraveyard, crea.ID, land.ID, other.ID)
	for _, id := range []state.ObjID{crea.ID, land.ID} {
		if o := h.g.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition failed: object %d is not in the graveyard", id)
		}
	}
	sa := eachSyntheticChangeZone(map[string]string{
		"Origin": "Graveyard", "Destination": "Hand", "Hidden": "True",
		"ChangeType": "EACH Creature.YouOwn & Land.YouOwn", "Optional": "True",
	})
	sh := &suspendHost{fakeHost: *h}
	Resolve(sh, &Ctx{Source: src.ID, Controller: 0}, sa)
	d := sh.asked
	// The explicit Optional$ True marker poses Forge's confirm-before-pick
	// gate first: accept it, then the structured pick follows.
	if d == nil || d.ResumeKind != "hidden_pick_confirm" {
		t.Fatalf("decision = %+v, want the hidden_pick_confirm gate (Optional$ True asks before the pick)", d)
	}
	sh.suspended = false // the double's suspension flag: the resume is a fresh pass
	Resolve(sh, &Ctx{Source: src.ID, Controller: 0,
		HiddenPickConfirmDone: true, HiddenPickConfirm: "yes", HiddenPickConfirmTarget: 0}, sa)
	d = sh.asked
	if d == nil {
		t.Fatal("no hidden-pick decision was posed (the feature never ran)")
	}
	if d.ResumeKind != "hidden_pick" {
		t.Fatalf("decision = %+v, want a hidden_pick", d)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("range = %d..%d, want 0..2 (one of EACH listed type; the pre-fix flat pick was 0..1)", d.Min, d.Max)
	}
	if len(d.Options) != 2 {
		t.Fatalf("%d options, want one per nonempty group: %+v", len(d.Options), d.Options)
	}
	var creaIdx, landIdx = -1, -1
	for i, o := range d.Options {
		switch o.Obj {
		case crea.ID:
			creaIdx = i
			if o.Group != "0" {
				t.Fatalf("creature option group = %q, want \"0\"", o.Group)
			}
		case land.ID:
			landIdx = i
			if o.Group != "1" {
				t.Fatalf("land option group = %q, want \"1\"", o.Group)
			}
		case other.ID:
			t.Fatal("the instant was offered: it matches neither listed type")
		}
	}
	if creaIdx < 0 || landIdx < 0 {
		t.Fatalf("precondition failed: the offered options %+v do not cover both listed types", d.Options)
	}
	// The answer (one creature AND one land) re-enters and moves BOTH to hand.
	sh.suspended = false // the double's suspension flag: the resume is a fresh pass
	fresh := &Ctx{Source: src.ID, Controller: 0,
		HiddenPick: []state.ObjID{crea.ID, land.ID}, HiddenPickDone: true, HiddenPickTarget: 0}
	Resolve(sh, fresh, sa)
	moved := 0
	for _, ev := range sh.log {
		if ev.Kind == events.MoveZone && ev.To == state.ZHand {
			moved++
		}
	}
	if moved != 2 {
		t.Fatalf("%d MoveZone events to hand, want both answered cards moved: %v", moved, sh.log)
	}
	for _, id := range []state.ObjID{crea.ID, land.ID} {
		o := h.g.Obj(id)
		if o == nil || o.Zone != state.ZHand {
			t.Fatalf("object %d did not return to hand (in zone %v)", id, o != nil && o.Zone == state.ZHand)
		}
	}
}

// TestEachSearchPartitionResolvesTheOverlap is the overlap fix: a card
// matching TWO listed qualities (a white-blue card under
// "EACH Card.White & Card.Blue") is offered ONCE, in the FIRST matching
// group, and picking it cannot block the other group's pick. Pre-fix, the
// per-sub scan offered the multicoloured card in both Groups (three options,
// and a pick in one group removed it from the other).
func TestEachSearchPartitionResolvesTheOverlap(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Probe\nTypes:Instant\nOracle:x\n"), 0)
	wu := h.g.AddObject(mkCard(t, "Name:Azor\nManaCost:1 W U\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	blue := h.g.AddObject(mkCard(t, "Name:Cerulean\nManaCost:1 U\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{wu.ID, blue.ID})
	eachSetZone(h.g, 0, state.ZLibrary, wu.ID, blue.ID)
	if len(h.g.Zone(state.ZLibrary, 0)) != 2 {
		t.Fatal("precondition failed: the search pool is not the two-card library")
	}
	// Precondition the overlap on: the Azorius card must match BOTH listed
	// sub-specs, the Cerulean card only Card.Blue.
	sc := SpecContext{You: 0}
	if !MatchesSpecCtx(h.g, "Card.White", wu.ID, sc) || !MatchesSpecCtx(h.g, "Card.Blue", wu.ID, sc) {
		t.Fatal("precondition failed: the multicoloured card does not match both listed qualities")
	}
	if MatchesSpecCtx(h.g, "Card.White", blue.ID, sc) || !MatchesSpecCtx(h.g, "Card.Blue", blue.ID, sc) {
		t.Fatal("precondition failed: the control card does not match Card.Blue alone")
	}
	sa := eachSyntheticChangeZone(map[string]string{
		"Origin": "Library", "Destination": "Hand", "ChangeType": "EACH Card.White & Card.Blue",
	})
	sh := &suspendHost{fakeHost: *h}
	Resolve(sh, &Ctx{Source: src.ID, Controller: 0}, sa)
	d := sh.asked
	if d == nil {
		t.Fatal("no search decision was posed (the feature never ran)")
	}
	wuCount := 0
	for _, o := range d.Options {
		if o.Obj == wu.ID {
			wuCount++
		}
	}
	if wuCount != 1 {
		t.Fatalf("the multicoloured card was offered %d times, want exactly once in its FIRST matching group: %+v", wuCount, d.Options)
	}
	if d.Max != 2 || len(d.Options) != 2 {
		t.Fatalf("range = %d..%d over %d options, want 0..2 over 2 (one per group, no cross-group duplicate): %+v", d.Min, d.Max, len(d.Options), d.Options)
	}
}

// TestEachSearchPerTypeChangeNumCapsEachGroup closes the per-type ChangeNum
// clause: "EACH Forest & Plains" with ChangeNum$ 2 poses a per-type pick with
// Decision.GroupLimit 2 and Max 4 over a library of 3 Forests and 2 Plains --
// not the pre-fix flat count (Max 2 behind a loud Note), and an answer taking
// three of one group is refused while two of EACH is accepted.
func TestEachSearchPerTypeChangeNumCapsEachGroup(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Verge\nTypes:Land\nOracle:x\n"), 0)
	f1 := h.g.AddObject(mkCard(t, "Name:F1\nTypes:Land Forest\nOracle:x\n"), 0)
	f2 := h.g.AddObject(mkCard(t, "Name:F2\nTypes:Land Forest\nOracle:x\n"), 0)
	f3 := h.g.AddObject(mkCard(t, "Name:F3\nTypes:Land Forest\nOracle:x\n"), 0)
	p1 := h.g.AddObject(mkCard(t, "Name:P1\nTypes:Land Plains\nOracle:x\n"), 0)
	p2 := h.g.AddObject(mkCard(t, "Name:P2\nTypes:Land Plains\nOracle:x\n"), 0)
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{f1.ID, p1.ID, f2.ID, p2.ID, f3.ID})
	eachSetZone(h.g, 0, state.ZLibrary, f1.ID, p1.ID, f2.ID, p2.ID, f3.ID)
	if len(h.g.Zone(state.ZLibrary, 0)) != 5 {
		t.Fatal("precondition failed: the library does not hold the 3 Forests and 2 Plains")
	}
	sa := eachSyntheticChangeZone(map[string]string{
		"Origin": "Library", "Destination": "Hand",
		"ChangeType": "EACH Forest & Plains", "ChangeNum": "2",
	})
	sh := &suspendHost{fakeHost: *h}
	Resolve(sh, &Ctx{Source: src.ID, Controller: 0}, sa)
	d := sh.asked
	if d == nil {
		t.Fatal("no search decision was posed (the feature never ran)")
	}
	if d.GroupLimit != 2 {
		t.Fatalf("GroupLimit = %d, want 2 (the per-type ChangeNum the flat-count path never carried)", d.GroupLimit)
	}
	if d.Max != 4 {
		t.Fatalf("Max = %d, want 4 (two of EACH listed type; the pre-fix flat count was 2)", d.Max)
	}
	for _, tc := range []struct {
		choices []int
		wantOK  bool
	}{
		{[]int{0, 2, 3, 4}, true},  // two Forests + two Plains
		{[]int{0, 1, 3, 4}, true},  // the only other two-of-each take
		{[]int{0, 1, 2, 3}, false}, // three Forests: past group "0"'s cap of 2
		{[]int{0, 3, 4}, true},     // one Forest + two Plains (Min 0: fail-to-find)
	} {
		err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tc.choices})
		if tc.wantOK && err != nil {
			t.Fatalf("choices %v rejected: %v", tc.choices, err)
		}
		if !tc.wantOK && err == nil {
			t.Fatalf("choices %v accepted: the per-group cap must refuse them", tc.choices)
		}
	}
	// The pick structure ran, not the flat path: no "resolves as a flat
	// count" Note is in the log, and the options carry the per-type Groups.
	for _, ev := range sh.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "resolves as a flat count") {
			t.Fatalf("the flat-count Note fired for a structured per-type ask: %v", ev)
		}
	}
	if d.Options[0].Group != "0" || d.Options[3].Group != "1" {
		t.Fatalf("options %+v do not partition the two listed types", d.Options)
	}
}

// TestQuantityOnlyEachKeepsMandatoryPerType closes the quantity-only clause:
// an EACH spec whose sub-specs state no quality keeps CR 701.23d's
// mandatory-find reading, now per type and structured (Group options, forced
// to each group's achievable count), instead of the flat union reading.
func TestQuantityOnlyEachKeepsMandatoryPerType(t *testing.T) {
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Q\nTypes:Sorcery\nOracle:x\n"), 0)
	c1 := h.g.AddObject(mkCard(t, "Name:C1\nTypes:Instant\nOracle:x\n"), 0)
	c2 := h.g.AddObject(mkCard(t, "Name:C2\nTypes:Instant\nOracle:x\n"), 0)
	c3 := h.g.AddObject(mkCard(t, "Name:C3\nTypes:Instant\nOracle:x\n"), 0)
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{c1.ID, c2.ID, c3.ID})
	eachSetZone(h.g, 0, state.ZLibrary, c1.ID, c2.ID, c3.ID)
	if got := SearchStatesQuality("EACH Card.YouOwn & Card.YouCtrl"); got {
		t.Fatal("precondition failed: the spec must classify as quantity-only for CR 701.23d to bind")
	}
	sa := eachSyntheticChangeZone(map[string]string{
		"Origin": "Library", "Destination": "Hand",
		"ChangeType": "EACH Card.YouOwn & Card.YouCtrl", "ChangeNum": "2",
	})
	sh := &suspendHost{fakeHost: *h}
	Resolve(sh, &Ctx{Source: src.ID, Controller: 0}, sa)
	d := sh.asked
	if d == nil {
		t.Fatal("no search decision was posed (the feature never ran)")
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("range = %d..%d, want 2..2 (the mandatory-find reading, forced to each group's achievable count)", d.Min, d.Max)
	}
	// The structure ran: the options carry the sub-spec's ordinal (both
	// quantity-only sub-specs match every card, so the FIRST-match
	// partition puts them all in group "0" and the second group is empty).
	if d.Options[0].Group != "0" || d.Options[1].Group != "0" {
		t.Fatalf("options %+v lost their per-type Groups", d.Options)
	}
	if d.GroupLimit != 2 {
		t.Fatalf("GroupLimit = %d, want 2", d.GroupLimit)
	}
}
