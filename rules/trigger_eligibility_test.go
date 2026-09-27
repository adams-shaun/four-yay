package rules

import (
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

var benchmarkObjectTriggerEligibility bool

// The mapping is an over-approximation of the CURRENT matcher, not an
// expansion of Forge support. SpellAbilityCast is the spell-or-activate
// union (targetsvalid1): AbilityPush on the activation arm, PutOnStack on
// the spell arm.
func TestTriggerEligibilityEventMatrix(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		kinds []events.Kind // nil means conservatively retain every event
	}{
		{"ChangesZone", []events.Kind{events.MoveZone, events.Draw, events.PutOnStack}},
		{"SpellCast", []events.Kind{events.PutOnStack}},
		{"AbilityCast", []events.Kind{events.AbilityPush}},
		{"SpellAbilityCast", []events.Kind{events.AbilityPush, events.PutOnStack}},
		{"Attacks", []events.Kind{events.DeclareAttackers}},
		{"AttackersDeclaredOneTarget", []events.Kind{events.DeclareAttackers}},
		{"Blocks", []events.Kind{events.DeclareBlockers}},
		{"Sacrificed", []events.Kind{events.MoveZone}},
		{"Discarded", []events.Kind{events.MoveZone}},
		{"DiscardedAll", []events.Kind{events.MoveZone}},
		{"CommitCrime", []events.Kind{events.TargetsChosen}},
		{"Taps", []events.Kind{events.Tap}},
		{"TapsForMana", []events.Kind{events.Tap}},
		{"DamageDone", []events.Kind{events.Damage}},
		{"DamageDealtOnce", []events.Kind{events.Damage}},
		{"DamageDoneOnce", []events.Kind{events.Damage}},
		{"Drawn", []events.Kind{events.Draw}},
		{"LifeLost", []events.Kind{events.Damage, events.LifeChange}},
		{"LifeGained", []events.Kind{events.LifeChange}},
		{"LifeLostAll", nil},
		{"BecomesTarget", []events.Kind{events.TargetsChosen}},
		{"Attached", []events.Kind{events.Attach}},
		{"Explores", []events.Kind{events.Explore}},
		{"Investigated", []events.Kind{events.Investigate}},
		{"SearchedLibrary", []events.Kind{events.SearchedLibrary}},
		{"Discover", []events.Kind{events.Discover}},
		{"SeekAll", []events.Kind{events.Seek}},
		{"Exploited", []events.Kind{events.Exploit}},
		{"BecomeMonarch", []events.Kind{events.MonarchChange}},
		{"Exerted", []events.Kind{events.Exert}},
		{"LandPlayed", []events.Kind{events.MoveZone}},
		{"Phase", []events.Kind{events.StepChange}},
		{"Always", nil},
		{"FutureMode", nil},
		{"", nil},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			mask := triggerModeEvents(tc.mode)
			for k := 0; k < 256; k++ {
				kind := events.Kind(k)
				// Kinds beyond this representation must fail OPEN to the old
				// matcher, never silently truncate a new event's eligibility.
				want := tc.kinds == nil || k >= triggerMaskKindBits || slices.Contains(tc.kinds, kind)
				if got := mask.allows(kind); got != want {
					t.Fatalf("%s kind %d: eligible=%v, want %v", tc.mode, k, got, want)
				}
			}
		})
	}
}

func TestTriggerEventInterestMapping(t *testing.T) {
	for kind := events.Kind(0); int(kind) < events.NumKinds; kind++ {
		var want cards.TriggerInterest
		switch kind {
		case events.MoveZone:
			want = cards.TriggerInterestZoneChange
		case events.Draw:
			want = cards.TriggerInterestZoneChange | cards.TriggerInterestDraw
		case events.LifeChange:
			want = cards.TriggerInterestLifeChange
		case events.Damage:
			want = cards.TriggerInterestDamage
		case events.Tap:
			want = cards.TriggerInterestTap
		case events.StepChange:
			want = cards.TriggerInterestStepChange
		case events.PutOnStack:
			want = cards.TriggerInterestZoneChange | cards.TriggerInterestStackPut
		case events.DeclareAttackers, events.DeclareBlockers:
			want = cards.TriggerInterestAttackDeclaration
		case events.TargetsChosen:
			want = cards.TriggerInterestTargetsChosen
		case events.AbilityPush, events.KeywordAbilityPush:
			want = cards.TriggerInterestAbilityPush
		case events.Attach:
			want = cards.TriggerInterestAttach
		case events.Explore:
			want = cards.TriggerInterestExplore
		case events.CastInfo:
			// manaexpend1: the pay-time CastInfo has its own interest bit
			// (the FlagManaExpendCast crossing emission), not the zero
			// mapping it carried before the ManaExpend mode existed.
			want = cards.TriggerInterestCastInfo
		case events.Investigate:
			// A trigger-relevant Kind past the 64-bit mask's reach: the
			// conservative catch-all, and compiledTriggerInterestAllows fails
			// open for it before this mapping is even consulted.
			want = cards.TriggerInterestAny
		case events.Discover, events.Seek, events.Connive:
			// The Investigate shape: trigger-relevant kinds past the 64-bit
			// mask's reach, the conservative catch-all -- compiledTrigger-
			// InterestAllows fails open for them before this mapping is even
			// consulted.
			want = cards.TriggerInterestAny
		case events.Scry:
			// Scry is not currently a trigger-interest class, so it retains
			// the conservative catch-all mapping.
			want = cards.TriggerInterestAny
		case events.Surveil:
			// The same Investigate shape (trig-surveil): the surveil marker is
			// trigger-relevant (trig:Surveil matches it through the full
			// matcher) and past the mask's reach, so the conservative
			// catch-all is the honest mapping.
			want = cards.TriggerInterestAny
		case events.Exploit:
			// The same Investigate shape: the exploit marker is
			// trigger-relevant (trig:Exploited) and past the mask's reach, so
			// the conservative catch-all is the honest mapping.
			want = cards.TriggerInterestAny
		case events.MonarchChange:
			// trig-become-monarch: the monarch designation transition has its
			// own interest bit (the BecomeMonarch mode), not the zero mapping
			// it carried while no mode matched it.
			want = cards.TriggerInterestMonarch
		case events.TurnFaceUp:
			// agent-20260919T183249Z-0fb8ed97: the turn-up marker is
			// trigger-relevant (trig:TurnFaceUp matches it through the full
			// matcher) and past the mask's reach, so the conservative
			// catch-all is the honest mapping (the Investigate shape).
			want = cards.TriggerInterestAny
		case events.Specialize:
			// agent-20260925T053724Z-5196df96: the specialize marker is
			// trigger-relevant (Mode$ Specializes matches it through the full
			// matcher) and past the mask's reach, so the conservative
			// catch-all is the honest mapping (the TurnFaceUp shape).
			want = cards.TriggerInterestAny
		case events.SearchedLibrary:
			// trig-searched-library: the completed-search marker is
			// trigger-relevant (trig:SearchedLibrary matches it through the
			// full matcher) and past the mask's reach, so the conservative
			// catch-all is the honest mapping (the TurnFaceUp shape).
			want = cards.TriggerInterestAny
		case events.Proliferate:
			// trig-proliferate: the completed-action marker is
			// trigger-relevant (trig:Proliferate matches it through the full
			// matcher) and past the mask's reach, so the conservative
			// catch-all is the honest mapping (the Surveil/TurnFaceUp shape).
			want = cards.TriggerInterestAny
		case events.Evolved:
			// trig:Evolved: the completed-evolve marker is trigger-relevant
			// (trig:Evolved matches it through the full matcher) and past the
			// mask's reach, so the conservative catch-all is the honest
			// mapping (the SearchedLibrary shape).
			want = cards.TriggerInterestAny
		}
		if got := eventTriggerInterest(kind); got != want {
			t.Fatalf("kind %s interest = %x, want %x", kind, got, want)
		}
	}
	if got := eventTriggerInterest(events.Kind(events.NumKinds)); got != cards.TriggerInterestAny {
		t.Fatalf("future event interest = %x, want catch-all", got)
	}
}

func TestCompiledTriggerInterestParity(t *testing.T) {
	modes := []string{
		"ChangesZone", "SpellCast", "AbilityCast", "SpellAbilityCast", "Attacks",
		"AttackersDeclaredOneTarget", "AttackersDeclared", "AttackerBlocked", "AttackerBlockedByCreature", "AttackerUnblocked", "AttackerUnblockedOnce", "Blocks", "Sacrificed",
		"Discarded", "DiscardedAll", "LandPlayed", "Cycled", "CommitCrime", "BecomesTarget", "Taps",
		"TapsForMana", "DamageDone", "DamageDealtOnce", "DamageDoneOnce", "CounterAdded",
		"CounterAddedOnce", "CounterRemoved", "CounterRemovedOnce", "DamagePreventedOnce", "TokenCreated", "TokenCreatedOnce",
		"ChangesZoneAll", "SpellCastOrCopy", "SpellCopy", "Mutates",
		"CounterPlayerAddedAll", "Drawn", "LifeLost", "Phase", "Attached", "Explores", "Investigated", "Discover", "SeekAll", "Exploited", "BecomeMonarch", "Always", "LifeLostAll", "FutureMode", "",
	}
	card := &cards.Card{}
	for _, mode := range modes {
		card.Faces = append(card.Faces, &cards.Face{Triggers: []cards.Trigger{{Mode: mode}}})
	}
	phaseDiagnostic := &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast", Params: map[string]string{"Phase": "Bad"}}}}
	card.Faces = append(card.Faces, phaseDiagnostic)
	r := cards.NewRegistry()
	r.Add(card)
	if err := r.CompileMetadata(); err != nil {
		t.Fatal(err)
	}

	e := &Engine{}
	for i, mode := range modes {
		face := card.Faces[i]
		for kind := events.Kind(0); int(kind) < events.NumKinds; kind++ {
			want := triggerModeEvents(mode).allows(kind)
			if got := e.faceMayTrigger(face, kind); want && !got {
				t.Fatalf("mode %q kind %s: compiled prefilter rejected a textual candidate", mode, kind)
			}
		}
	}
	for kind := events.Kind(0); int(kind) < events.NumKinds; kind++ {
		if !e.faceMayTrigger(phaseDiagnostic, kind) {
			t.Fatalf("phase diagnostic rejected %s", kind)
		}
	}
	if len(e.triggerEventMasks) != 0 {
		t.Fatalf("bound faces populated textual pointer cache with %d entries", len(e.triggerEventMasks))
	}
}

func TestTriggerEligibilityFaceUnionAndConservativeFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		face *cards.Face
		kind events.Kind
		want bool
	}{
		{"nil", nil, events.Note, false},
		{"empty", &cards.Face{}, events.Note, false},
		{"irrelevant", &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast"}}}, events.Note, false},
		{"union", &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast"}, {Mode: "Drawn"}}}, events.Draw, true},
		{"phase diagnostic", &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast", Params: map[string]string{"Phase": "Bad"}}}}, events.Note, true},
		{"valid phase", &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast", Params: map[string]string{"Phase": "Upkeep"}}}}, events.Note, true},
		{"blank phase", &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast", Params: map[string]string{"Phase": " \t"}}}}, events.Note, false},
		{"state trigger", &cards.Face{Triggers: []cards.Trigger{{Mode: "Always"}}}, events.Note, true},
		{"life batch", &cards.Face{Triggers: []cards.Trigger{{Mode: "LifeLostAll"}}}, events.Note, true},
		{"unknown", &cards.Face{Triggers: []cards.Trigger{{Mode: "FutureMode"}}}, events.Note, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &Engine{}
			if got := e.faceMayTrigger(tc.face, tc.kind); got != tc.want {
				t.Fatalf("eligible=%v, want %v", got, tc.want)
			}
			if a := testing.AllocsPerRun(100, func() { e.faceMayTrigger(tc.face, tc.kind) }); a != 0 {
				t.Fatalf("warm face lookup allocated %v objects", a)
			}
		})
	}
}

func TestTriggerEligibilityCacheIsCloneIndependent(t *testing.T) {
	e := layerEngine(t)
	f := &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast"}}}
	e.faceMayTrigger(f, events.Note)
	c := e.Clone()
	other := &cards.Face{Triggers: []cards.Trigger{{Mode: "Drawn"}}}
	if !c.faceMayTrigger(other, events.Draw) {
		t.Fatal("clone rejected newly encountered face")
	}
	if _, shared := e.triggerEventMasks[other]; shared {
		t.Fatal("clone writes to parent's eligibility cache")
	}
	// Concurrent misses on independently writable caches must not race.
	for _, branch := range []*Engine{e, c} {
		t.Run("branch", func(t *testing.T) {
			t.Parallel()
			for range 100 {
				face := &cards.Face{Triggers: []cards.Trigger{{Mode: "Attacks"}}}
				if !branch.faceMayTrigger(face, events.DeclareAttackers) {
					t.Fatal("branch rejected attack trigger")
				}
			}
		})
	}
}

// TestObjectTriggerEligibilityTracksBothFaces catches a dense object cache
// reusing the active face's mask after a transform or evicting one Room half
// when both halves are checked on every event.
func TestObjectTriggerEligibilityTracksBothFaces(t *testing.T) {
	e := layerEngine(t)
	spell := &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast"}}}
	draw := &cards.Face{Triggers: []cards.Trigger{{Mode: "Drawn"}}}
	card := &cards.Card{Faces: []*cards.Face{spell, draw}}
	r := cards.NewRegistry()
	r.Add(card)
	if err := r.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	o := e.G.AddObject(card, 0)

	if !e.objectFaceMayTrigger(o.ID, 0, spell, events.PutOnStack) {
		t.Fatal("front face rejected its spell-cast event")
	}
	if e.objectFaceMayTrigger(o.ID, 0, spell, events.Draw) {
		t.Fatal("front face admitted an unrelated draw")
	}
	if !e.objectFaceMayTrigger(o.ID, 1, draw, events.Draw) {
		t.Fatal("back face rejected its draw event")
	}
	if e.objectFaceMayTrigger(o.ID, 1, draw, events.PutOnStack) {
		t.Fatal("back face admitted an unrelated cast")
	}
	if !e.objectFaceMayTrigger(o.ID, 0, spell, events.PutOnStack) {
		t.Fatal("checking the back face evicted the front-face eligibility")
	}

	c := e.Clone()
	replacement := &cards.Face{Triggers: []cards.Trigger{{Mode: "Attacks"}}}
	if !c.objectFaceMayTrigger(o.ID, 0, replacement, events.DeclareAttackers) {
		t.Fatal("clone did not independently accept replacement face metadata")
	}
	if !e.objectFaceMayTrigger(o.ID, 0, spell, events.PutOnStack) {
		t.Fatal("clone cache mutation changed parent eligibility")
	}
}

// Bound catalog faces own immutable trigger-interest metadata. Looking one up
// must not allocate or populate a mutable per-engine object cache merely to
// repeat that same immutable lookup.
func TestCompiledObjectTriggerEligibilitySkipsRuntimeCache(t *testing.T) {
	e := &Engine{G: state.NewGame([]string{"a"})}
	face := &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast"}}}
	card := &cards.Card{Faces: []*cards.Face{face}}
	r := cards.NewRegistry()
	r.Add(card)
	if err := r.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	o := e.G.AddObject(card, 0)
	if !e.objectFaceMayTrigger(o.ID, o.FaceIdx, face, events.PutOnStack) {
		t.Fatal("compiled face rejected its spell-cast event")
	}
	if len(e.triggerObjectMasks) != 0 {
		t.Fatalf("compiled face populated %d runtime object-mask entries", len(e.triggerObjectMasks))
	}
}

func BenchmarkCompiledObjectTriggerEligibility(b *testing.B) {
	e := &Engine{G: state.NewGame([]string{"a"})}
	face := &cards.Face{Triggers: []cards.Trigger{{Mode: "SpellCast"}}}
	card := &cards.Card{Faces: []*cards.Face{face}}
	r := cards.NewRegistry()
	r.Add(card)
	if err := r.CompileMetadata(); err != nil {
		b.Fatal(err)
	}
	o := e.G.AddObject(card, 0)
	b.ReportAllocs()
	for range b.N {
		benchmarkObjectTriggerEligibility = e.objectFaceMayTrigger(o.ID, o.FaceIdx, face, events.PutOnStack)
	}
}

func TestGrantedKeywordTriggerEventFilter(t *testing.T) {
	for _, tc := range []struct {
		kind events.Kind
		want bool
	}{
		{events.TargetsChosen, true},
		{events.DeclareAttackers, true},
		// MoveZone is the granted-exploit gate: a layer-6 AddKeyword$ Exploit
		// grant synthesizes its ETB election in checkGrantedExploitTriggers,
		// which must run on the entering creature's MoveZone (the Afflict/
		// Dethrone/Conspire precedent, each of which needs its own carrying
		// event admitted here).
		{events.MoveZone, true},
		// StepChange is the granted-cumulative-upkeep gate: a layer-6
		// AddKeyword$ Cumulative upkeep:<cost> or a KW$ Cumulative upkeep:<cost>
		// pump grant synthesizes its beginning-of-upkeep Phase trigger in
		// checkGrantedCumulativeUpkeepTriggers, which must run on the upkeep
		// step's StepChange (the Afflict/Dethrone/Exploit precedent, each of
		// which needs its own carrying event admitted here).
		{events.StepChange, true},
		{events.Priority, false},
	} {
		if got := grantedKeywordTriggerEvent(tc.kind); got != tc.want {
			t.Fatalf("kind %s: eligible=%v, want %v", tc.kind, got, tc.want)
		}
	}
}

// An impossible event must be rejected before parsing dynamic gates, even on
// a cold look-back observer. Moving the kind guard after phaseGate allocates.
func TestTriggerEligibilityRejectsBeforeDynamicGates(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:Gate probe\nTypes:Enchantment\nOracle:x\n")
	tr := cards.Trigger{Mode: "ChangesZone", Params: map[string]string{"Phase": "Upkeep"}}
	observer := &Engine{G: e.G, L: e.L}
	allocs := testing.AllocsPerRun(100, func() {
		observer.phaseSpecs = nil // cold syntax; observer construction is setup
		if observer.triggerMatches(tr, id, events.Event{Kind: events.Note}, nil) {
			t.Fatal("a Note matched ChangesZone")
		}
	})
	if allocs != 0 {
		t.Fatalf("irrelevant event allocated %.0f objects evaluating gates; want zero", allocs)
	}
}

// These synthetic fixtures pin the scanner's existing semantics before
// pruning. Diagnostics ignore event kind AND source zone; duplicate specs
// name the first source in seat/zone/face/trigger order, not insertion order.
func TestTriggerEligibilityKeepsHiddenDiagnosticOrder(t *testing.T) {
	e := layerEngine(t)
	later := onBoard(t, e, 0, "Name:Later\nTypes:Enchantment\n"+
		"T:Mode$ ChangesZone | Phase$ BadA | Execute$ Gain\n"+
		"T:Mode$ SpellCast | Phase$ BadC | Execute$ Gain\n"+
		"SVar:Gain:DB$ GainLife | LifeAmount$ 1 | Defined$ You\nOracle:x\n")
	first := onBoard(t, e, 0, "Name:Hidden first\nTypes:Enchantment\n"+
		"T:Mode$ SpellCast | Phase$ BadA | Execute$ Gain\n"+
		"T:Mode$ FutureMode | Phase$ BadB | Execute$ Gain\n"+
		"SVar:Gain:DB$ GainLife | LifeAmount$ 1 | Defined$ You\nOracle:x\n")
	events.Apply(e.G, events.Event{Kind: events.MoveZone, Obj: first, From: state.ZBattlefield, To: state.ZLibrary})
	start := len(e.L.Events)
	for range 2 {
		e.checkFaceTriggers(e, events.Event{Kind: events.Priority}, nil, 0, 0, false, false, false)
	}
	var got []string
	var sources []state.ObjID
	for _, ev := range e.L.Events[start:] {
		got = append(got, ev.Text)
		sources = append(sources, ev.Obj)
	}
	want := []string{
		"Phase$ BadA names no engine step; the trigger never fires",
		"Phase$ BadB names no engine step; the trigger never fires",
		"Phase$ BadC names no engine step; the trigger never fires",
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(sources, []state.ObjID{first, first, later}) {
		t.Fatalf("diagnostics = %v from %v; want %v in zone order", got, sources, want)
	}
}

func TestTriggerEligibilityKeepsAlwaysOnBookkeepingEvents(t *testing.T) {
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:State watcher\nTypes:Enchantment\n"+
		"T:Mode$ Always | LifeTotal$ You | LifeAmount$ GE20 | Execute$ Gain\n"+
		"SVar:Gain:DB$ GainLife | LifeAmount$ 1 | Defined$ You\nOracle:x\n")
	for _, kind := range []events.Kind{events.Priority, events.Note, events.DecisionMade} {
		e.checkFaceTriggers(e, events.Event{Kind: kind}, nil, 0, 0, false, false, false)
	}
	if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != id {
		t.Fatalf("Always queue = %+v, want exactly one outstanding instance", e.pendingTriggers)
	}
}

// The active face has no printed triggers. Its fast rejection must still
// inspect an unlocked Room's other face, including when face 1 was cast.
func TestTriggerEligibilityKeepsRoomAlternateFace(t *testing.T) {
	for _, face := range []int32{0, 1} {
		e := layerEngine(t)
		quiet := "Name:Quiet\nTypes:Enchantment Room\nOracle:x\n"
		watcher := "Name:Watcher\nTypes:Enchantment Room\n" +
			"T:Mode$ SpellCast | Execute$ Gain\nSVar:Gain:DB$ GainLife | LifeAmount$ 1 | Defined$ You\nOracle:x\n"
		src := quiet + "ALTERNATE\n" + watcher
		if face == 1 {
			src = watcher + "ALTERNATE\n" + quiet
		}
		id := onBoard(t, e, 0, src)
		events.Apply(e.G, events.Event{Kind: events.FlipFace, Obj: id, Amount: face})
		spell := onBoard(t, e, 0, "Name:Spell\nTypes:Sorcery\nOracle:x\n")
		ev := events.Event{Kind: events.PutOnStack, Obj: spell, Player: 0}
		e.checkFaceTriggers(e, ev, nil, 0, 0, false, false, false)
		if len(e.pendingTriggers) != 0 {
			t.Fatal("locked alternate face fired")
		}
		events.Apply(e.G, events.Event{Kind: events.DoorUnlock, Obj: id})
		e.checkFaceTriggers(e, ev, nil, 0, 0, false, false, false)
		if len(e.pendingTriggers) != 1 || e.pendingTriggers[0].Source != id || !e.pendingTriggers[0].Delayed {
			t.Fatalf("cast face %d: queue = %+v, want alternate-face trigger", face, e.pendingTriggers)
		}
	}
}

// TestTriggerEventMaskAndPrefilterAgreePastTheMaskWidth pins the ONE bound
// both trigger-eligibility classifiers share. triggerEventMask is a uint64, so
// triggerMaskKindBits (64) is the last ordinal it can encode; a kind at or past
// it must fail OPEN -- to the full matcher, never silently truncated by a
// shift -- in the TEXTUAL mask (allows) and in the COMPILED interest prefilter
// (compiledTriggerInterestAllows) alike. One path rejecting what the other
// admits is the divergence CombatRetarget (the first kind past the bound)
// exposed, and it is what this test exists to keep closed.
//
// Measured ordinals at this merge: Explore 63 is the last kind INSIDE the
// mask; CombatRetarget 64 is the first past it, and everything after --
// RingTemptsYou 65, RingEmblemPush 66, GrantAbilityPush 67, Investigate 68,
// BlessingChange 69, ClonePermanent 70, Mutate 71, MergedTriggerPush 72,
// Discover 73, Seek 74 (NumKinds 75) -- is past it too. So mutate's two kinds are NOT a special
// case: they fail open like every other kind past the bound, and trig:Mutates
// is gated by the full matcher (mutatesMatches), not by the mask. An earlier
// version of this test asserted the opposite contract (an enumerated mask
// classifying a known kind past the width EXACTLY, with the compiled side left
// wider); main has since resolved the same question the other way, symmetric
// across both paths, and this test follows main rather than re-litigating it.
func TestTriggerEventMaskAndPrefilterAgreePastTheMaskWidth(t *testing.T) {
	if int(events.NumKinds) <= triggerMaskKindBits {
		t.Skipf("no kind past the mask bound yet (NumKinds %d, bound %d): the "+
			"fail-open branch below is unreachable and the exactness question returns",
			int(events.NumKinds), triggerMaskKindBits)
	}
	// Inside the bound an enumerated mask is still exact.
	drawn := triggerModeEvents("Drawn")
	if !drawn.allows(events.Draw) || drawn.allows(events.Tap) {
		t.Fatal("inside the mask width an enumerated mode mask must name exactly its own events")
	}
	// At and past it, both classifiers fail open -- for an enumerated mask and
	// for an empty compiled interest set alike.
	mutates := triggerModeEvents("Mutates")
	for k := triggerMaskKindBits; k < int(events.NumKinds); k++ {
		kind := events.Kind(k)
		if !mutates.allows(kind) {
			t.Fatalf("kind %s (ordinal %d) is past the mask bound and must fail open textually", kind, k)
		}
		if !compiledTriggerInterestAllows(0, kind) {
			t.Fatalf("kind %s (ordinal %d) is past the mask bound and must fail open in the compiled prefilter", kind, k)
		}
	}
	if !allTriggerEvents.allows(events.MergedTriggerPush) {
		t.Fatal("the conservative catch-all mask dropped a kind past the mask width")
	}
	// The compiled prefilter is never the narrower side, for any kind.
	e := &Engine{}
	face := &cards.Face{Triggers: []cards.Trigger{{Mode: "Mutates"}}}
	for kind := events.Kind(0); int(kind) < events.NumKinds; kind++ {
		if mutates.allows(kind) && !e.faceMayTrigger(face, kind) {
			t.Fatalf("kind %s: compiled prefilter rejected a textual candidate", kind)
		}
	}
}

// The hoisted per-event half checkFaceTriggers uses must agree with
// compiledTriggerInterestAllows for every interest value and every kind,
// including the fail-open kinds past the mask's reach.
func TestCompiledTriggerInterestEventHoistParity(t *testing.T) {
	for kind := events.Kind(0); int(kind) <= events.NumKinds+1; kind++ {
		all, mask := compiledTriggerInterestEvent(kind)
		for i := 0; i <= 0xffff; i++ {
			in := cards.TriggerInterest(i)
			if got, want := all || in&mask != 0, compiledTriggerInterestAllows(in, kind); got != want {
				t.Fatalf("kind %d interests %#x: hoisted=%v, compiledTriggerInterestAllows=%v", kind, i, got, want)
			}
		}
	}
}

// grantedTriggerStaticsFor keeps exactly the statics with a granted trigger
// whose Mode$ passes triggerMatches' leading kind gate, in active() order;
// a gained face's trigger with no linked Effect never counts (the walk skips
// it before matching).
func TestGrantedTriggerStaticsForKeepsObservingGrantsInOrder(t *testing.T) {
	gained := &cards.Face{Name: "Gained", Triggers: []cards.Trigger{
		{Mode: "Attacks", Effect: &cards.SA{}},
		{Mode: "SpellCast"}, // unlinked: ignored
	}}
	statics := []ContinuousEffect{
		{Timestamp: 1, AddTrigger: &cards.Trigger{Mode: "ChangesZone"}},
		{Timestamp: 2},
		{Timestamp: 3, GainedTriggerFaces: []state.GainedFace{{Face: gained}}},
		{Timestamp: 4, AddTrigger: &cards.Trigger{Mode: "Always"}},
		{Timestamp: 5, AddTrigger: &cards.Trigger{Mode: "Phase"}},
	}
	stamps := func(kind events.Kind) []int64 {
		var out []int64
		for _, ce := range grantedTriggerStaticsFor(statics, kind, nil) {
			out = append(out, int64(ce.Timestamp))
		}
		return out
	}
	for _, tc := range []struct {
		kind events.Kind
		want []int64
	}{
		{events.MoveZone, []int64{1, 4}},
		{events.DeclareAttackers, []int64{3, 4}},
		{events.PutOnStack, []int64{1, 4}},
		{events.StepChange, []int64{4, 5}},
		{events.Note, []int64{4}},
	} {
		if got := stamps(tc.kind); !slices.Equal(got, tc.want) {
			t.Errorf("kind %d: kept timestamps %v, want %v", tc.kind, got, tc.want)
		}
	}
	// The kept entries alias the input slice (no copies).
	if got := grantedTriggerStaticsFor(statics, events.MoveZone, nil); got[0] != &statics[0] {
		t.Fatalf("kept entry does not alias the active() slice")
	}
}
