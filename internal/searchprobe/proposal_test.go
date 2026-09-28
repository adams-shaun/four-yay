package searchprobe

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"runtime"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// A single playable land among nineteen uncastable spells is in an ordinary
// opening hand only 7/20 of the time. The public play must guide that deadline.
func TestProposalGuidesOpponentPublicLand(t *testing.T) {
	land := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	spell := syntheticCard(t, "Name:Uncastable\nManaCost:99\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(land, 20), repeatCard(spell, 20)}
	decks[1][0] = land
	setup, h := proposalHistory(t, decks, 1, func(e *rules.Engine) bool { return len(e.G.Zone(state.ZBattlefield, 1)) == 1 })
	result, err := Sample(setup, h, SampleOptions{Seed: 37, Attempts: 16, Worlds: 4, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 16 || len(result.Worlds) != 4 || result.ESS < 15.999 {
		t.Fatalf("opponent public land was not fully guided: %+v", result)
	}
	for _, w := range result.Worlds {
		if err := VerifyWorld(w); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProposalReplaysNamelessActivatedAbilityHistory(t *testing.T) {
	card := syntheticCard(t, "Name:Fixture Fountain\nTypes:Land Creature Construct\nPT:1/1\nK:Haste\nA:AB$ GainLife | Cost$ T | Defined$ You | LifeAmount$ 1\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(card, 20), repeatCard(card, 20)}
	setup, h := proposalHistory(t, decks, 0, func(e *rules.Engine) bool { return len(e.G.Stack) > 0 })
	assertNamelessAbilityHistorySamples(t, setup, h, events.AbilityPush)
}

func TestProposalReplaysNamelessTriggeredAbilityHistory(t *testing.T) {
	card := syntheticCard(t, "Name:Fixture Spring\nTypes:Land\nT:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ Gain\nSVar:Gain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(card, 20), repeatCard(card, 20)}
	setup, h := proposalHistory(t, decks, 0, func(e *rules.Engine) bool { return len(e.G.Stack) > 0 })
	assertNamelessAbilityHistorySamples(t, setup, h, events.TriggerPush)
}

func assertNamelessAbilityHistorySamples(t *testing.T, setup PublicGame, h History, kind events.Kind) {
	t.Helper()
	nameless, pushed := false, false
	for _, f := range h.Frames {
		for _, id := range f.Identities {
			nameless = nameless || id.ID != 0 && id.Name == ""
		}
		for _, ev := range f.Events {
			pushed = pushed || ev.Kind == kind
		}
	}
	if !nameless || !pushed {
		t.Fatalf("fixture missing actual nameless stack ability: nameless=%v push=%v", nameless, pushed)
	}
	if _, err := compileEpochs(h); err != nil {
		t.Fatalf("valid stack ability rejected by compiler: %v", err)
	}
	result, err := Sample(setup, h, SampleOptions{Seed: 799, Attempts: 8, Worlds: 4, MaxSubmits: 500})
	if err != nil {
		t.Fatalf("valid stack ability rejected by sampling: %v", err)
	}
	if result.Accepted != 8 || len(result.Worlds) != 4 {
		t.Fatalf("ability reconstruction accepted=%d worlds=%d first=%s", result.Accepted, len(result.Worlds), result.FirstRejection)
	}
	for _, world := range result.Worlds {
		if err := VerifyWorld(world); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProposalOpponentWeightMatchesExhaustivePhysicalPermutations(t *testing.T) {
	land := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	b := syntheticCard(t, "Name:Uncastable\nManaCost:99\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:Fixture.\n")
	a := syntheticCard(t, "Name:Public Spell\nManaCost:0\nTypes:Sorcery\nA:SP$ GainLife | LifeAmount$ 1\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(land, 8), repeatCard(b, 8)}
	decks[1][0] = a
	setup, h := proposalHistory(t, decks, 1, func(e *rules.Engine) bool { return len(e.G.Zone(state.ZGraveyard, 1)) == 1 })
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	tape, tossWeight, err := publicToss(setup, h)
	if err != nil {
		t.Fatal(err)
	}
	p := &proposalState{epochs: epochs, observer: NewCollector(0), result: &SampleResult{}, logWeight: tossWeight}
	e, err := rules.NewHypotheticalPlanned(rules.Config{Seed: 91, Names: setup.Names, Decks: decks}, tape, p.plan)
	if err != nil {
		t.Fatal(err)
	}
	// Card zero is the single spell. Enumerate all eight distinct physical
	// objects, independently of the constrained sampler and its DP counts.
	order := []int{0, 1, 2, 3, 4, 5, 6, 7}
	total, compatible := 0, 0
	var walk func(int)
	walk = func(pos int) {
		if pos == len(order) {
			total++
			for _, card := range order[:7] {
				if card == 0 {
					compatible++
					break
				}
			}
			return
		}
		for j := pos; j < len(order); j++ {
			order[pos], order[j] = order[j], order[pos]
			walk(pos + 1)
			order[pos], order[j] = order[j], order[pos]
		}
	}
	walk(0)
	if total != 40320 || compatible != 35280 {
		t.Fatalf("bad exhaustive fixture: %d/%d", compatible, total)
	}
	want := float64(compatible) / float64(total) / 2
	if math.Abs(math.Exp(p.logWeight)-want) > 1e-12 {
		t.Fatalf("actual engine proposal weight=%g want %g", math.Exp(p.logWeight), want)
	}
	found := false
	for _, id := range e.G.Zone(state.ZHand, 1) {
		if e.G.Obj(id).Card == a {
			found = true
		}
	}
	if !found {
		t.Fatal("real genesis failed the public spell deadline")
	}
}

// With the required land already in hand, requiring it AGAIN from the later
// library would falsely reject that otherwise compatible hypothetical past.
func TestProposalLaterShuffleCreditsOpponentHand(t *testing.T) {
	land := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	spell := syntheticCard(t, "Name:Uncastable\nManaCost:99\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:Fixture.\n")
	shuffle := syntheticCard(t, "Name:Shuffle Opponent\nManaCost:0\nTypes:Sorcery\nA:SP$ Shuffle | Defined$ Opponent\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(land, 20), repeatCard(spell, 20)}
	decks[0][0], decks[1][0] = shuffle, land
	setup, h := proposalHistory(t, decks, 0, func(e *rules.Engine) bool { return len(e.G.Zone(state.ZBattlefield, 1)) == 1 })
	result, err := Sample(setup, h, SampleOptions{Seed: 91, Attempts: 16, Worlds: 1, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 16 || result.IncompatibleProposals != 0 || result.GuidedLater != 16 {
		t.Fatalf("pre-shuffle hand was not credited: accepted=%d incompatible=%d later=%d first=%s", result.Accepted, result.IncompatibleProposals, result.GuidedLater, result.FirstRejection)
	}
	for _, w := range result.Worlds {
		if err := VerifyWorld(w); err != nil {
			t.Fatal(err)
		}
	}
	result, err = Sample(setup, h, SampleOptions{Seed: 91, Attempts: 16, Worlds: 16, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 16 || result.ESS >= 16 || len(result.Worlds) != 0 {
		t.Fatalf("ESS fallback bypassed: accepted=%d ESS=%g worlds=%d", result.Accepted, result.ESS, len(result.Worlds))
	}
}

func TestProposalLaterDrawsPreserveKnownDuplicateObjects(t *testing.T) {
	land := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	// LibraryPosition$ -1 (bottom) is pinned deliberately, and this pin is
	// NOT incidental: it was re-examined by agent-20260928T191540Z and found
	// necessary for a reason other than the one the follow-up ticket assumed.
	// Without the param the ChangeZoneAll takes Forge's absent-position TOP
	// default (golgari_thug2) and emits a LibraryOrder before the shuffle.
	//
	// The ticket attributed the fixture's failure to compileEpochs marking the
	// PRE-shuffle epoch unguided on that LibraryOrder. Measured: that epoch is
	// unguided anyway by the five hand-to-library MoveZone events
	// ("library_mutation"), and forcing it guided changes the sample result
	// not at all (3/16 accepted either way). The real limitation is that the
	// observed top five draws are 1 previously-known object + 4 NEVER-SEEN
	// objects, and the epoch/constraint model counts cards by NAME only
	// (internal/searchprobe/constraints.go), so it cannot require a drawn
	// position to be a previously-unseen copy rather than one of the known
	// duplicates that were moved into the same library. The TOP-placement
	// history simply draws fewer known duplicates into the top five than the
	// pre-golgari_thug2 bottom-placement history did, so the limitation
	// surfaces. Fixing it needs a new per-card "unseen" constraint in the
	// permutation solver; tracked separately. The paired-shuffle epochs fix
	// this round DID land (a placement erased by a shuffle no longer unguides
	// its epoch) and is pinned by placement_shuffle_test.go.
	spell := syntheticCard(t, "Name:Return Shuffle Draw\nManaCost:0\nTypes:Sorcery\nA:SP$ ChangeZoneAll | Origin$ Hand | Destination$ Library | ChangeType$ Card.YouOwn | LibraryPosition$ -1 | SubAbility$ Mix\nSVar:Mix:DB$ Shuffle | Defined$ You | SubAbility$ Pull\nSVar:Pull:DB$ Draw | Defined$ You | NumCards$ 5\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(land, 12), repeatCard(land, 12)}
	decks[0][0] = spell
	setup, h := proposalHistory(t, decks, 0, func(e *rules.Engine) bool { return len(e.G.Zone(state.ZGraveyard, 0)) == 1 })
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	known := 0
	for _, p := range epochs[epochKey{Player: 0, Ordinal: 1}].Positions {
		if p.Ref != 0 {
			known++
		}
	}
	if known == 0 {
		t.Fatal("fixture did not redraw a known object")
	}
	result, err := Sample(setup, h, SampleOptions{Seed: 219, Attempts: 16, Worlds: 4, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	// Previously-known objects are exact constraints; unseen-name constraints
	// may still choose a different known duplicate and fail the full prefix.
	if len(result.Worlds) != 4 || result.GuidedLater != 16 {
		t.Fatalf("known physical redraw not guided: accepted=%d later=%d rejected=%d first=%s", result.Accepted, result.GuidedLater, result.PrefixRejected, result.FirstRejection)
	}
	for _, w := range result.Worlds {
		if err := VerifyWorld(w); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProposalIncompatibleLaterLibraryRejectsOnlyAttempt(t *testing.T) {
	epoch := epochConstraints{Positions: []epochPosition{{Index: 3, Name: "A"}}}
	_, result, _, err := runLaterProposal(t, epoch)
	if !errors.Is(err, errIncompatibleProposal) || result.IncompatibleProposals != 1 {
		t.Fatalf("too-short hypothetical library: err=%v incompatible=%d", err, result.IncompatibleProposals)
	}
}

func TestProposalDuplicateHandDeficitsHaveExactWeights(t *testing.T) {
	for _, tc := range []struct {
		name         string
		deadlines    []deadlineConstraint
		weight       float64
		incompatible bool
	}{
		{"already in hand", []deadlineConstraint{{Through: 0, Name: "A", Count: 1}}, 1, false},
		{"one of two in hand", []deadlineConstraint{{Through: 0, Name: "A", Count: 1}, {Through: 1, Name: "A", Count: 2}}, 1.0 / 3, false},
		{"no completion", []deadlineConstraint{{Through: 1, Name: "A", Count: 3}}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, result, logWeight, err := runLaterProposal(t, epochConstraints{Deadlines: tc.deadlines})
			if tc.incompatible {
				if !errors.Is(err, errIncompatibleProposal) || result.IncompatibleProposals != 1 {
					t.Fatalf("err=%v diagnostics=%+v", err, result)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// Enumerate all 3! physical permutations independently: ABB has six
			// orders, of which exactly two put the remaining A in the first slot.
			if math.Abs(math.Exp(logWeight)-tc.weight) > 1e-12 {
				t.Fatalf("weight=%g want %g", math.Exp(logWeight), tc.weight)
			}
			if tc.weight < 1 && e.G.Obj(e.G.Zone(state.ZLibrary, 1)[0]).Card.Faces[0].Name != "A" {
				t.Fatal("deadline not installed by Shuffle event")
			}
		})
	}
}

func TestProposalExecutesLaterShuffleAndArrangeDrawHistory(t *testing.T) {
	for _, tc := range []struct {
		name, body     string
		later, arrange int
	}{
		{"later shuffle draw", "Shuffle | Defined$ You | SubAbility$ Pull", 16, 0},
		{"arrange reordered draw", "RearrangeTopOfLibrary | Defined$ You | NumCards$ 3 | SubAbility$ Pull", 0, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			land := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
			spell := syntheticCard(t, "Name:Look Shuffle Draw\nManaCost:0\nTypes:Sorcery\nA:SP$ "+tc.body+"\nSVar:Pull:DB$ Draw | Defined$ You | NumCards$ 1\nOracle:Fixture.\n")
			decks := [][]*cards.Card{repeatCard(land, 12), repeatCard(land, 12)}
			decks[0][0] = spell
			for i := 7; i < 12; i++ {
				decks[0][i] = syntheticCard(t, "Name:"+string(rune('A'+i-7))+"\nTypes:Land\nOracle:Fixture.\n")
			}
			setup, h := proposalHistory(t, decks, 0, func(e *rules.Engine) bool { return len(e.G.Zone(state.ZGraveyard, 0)) == 1 })
			if tc.arrange > 0 {
				found := false
				for i, f := range h.Frames {
					if f.Decision != nil && f.Decision.Kind == decision.KArrange {
						found = true
						if h.Answers[i][0].Obj != f.Decision.Options[2].Action.Obj {
							t.Fatal("fixture failed to reverse arrangement")
						}
					}
				}
				if !found {
					t.Fatal("missing arrange window")
				}
			}
			result, err := Sample(setup, h, SampleOptions{Seed: 619, Attempts: 16, Worlds: 4, MaxSubmits: 500})
			if err != nil {
				t.Fatal(err)
			}
			if result.Accepted != 16 || result.GuidedLater != tc.later || result.ArrangeWindows != tc.arrange {
				t.Fatalf("accepted=%d later=%d arrange=%d first=%s", result.Accepted, result.GuidedLater, result.ArrangeWindows, result.FirstRejection)
			}
			for _, w := range result.Worlds {
				if err := VerifyWorld(w); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestProposalGuidesUnseenDrawPastBottomedArrangeWindow(t *testing.T) {
	land := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	spell := syntheticCard(t, "Name:Look Then Draw\nManaCost:0\nTypes:Sorcery\nA:SP$ Scry | Defined$ You | ScryNum$ 3 | SubAbility$ Pull\nSVar:Pull:DB$ Draw | Defined$ You | NumCards$ 2\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(land, 16), repeatCard(land, 16)}
	decks[0][0] = spell
	for i := 7; i < 16; i++ {
		decks[0][i] = syntheticCard(t, "Name:"+string(rune('A'+i-7))+"\nTypes:Land\nOracle:Fixture.\n")
	}
	setup, h := proposalHistory(t, decks, 0, func(e *rules.Engine) bool { return len(e.G.Zone(state.ZGraveyard, 0)) == 1 }, func(d *decision.Decision, in decision.Intent) decision.Intent {
		if d.Kind == decision.KArrange {
			in.Choices = []int{0}
		}
		return in
	})
	epochs, err := compileEpochs(h)
	if err != nil {
		t.Fatal(err)
	}
	ep := epochs[epochKey{Player: 0}]
	// Opening seven, then look at A/B/C; keep A and bottom B/C. The second
	// subsequent draw is previously unseen D at ORIGINAL index 10, not 8.
	found := false
	for _, p := range ep.Positions {
		if p.Name == "D" {
			found = p.Index == 10
		}
	}
	if !found || len(ep.Unguided) != 0 {
		t.Fatalf("post-arrange original position not guided: %+v", ep)
	}
	result, err := Sample(setup, h, SampleOptions{Seed: 883, Attempts: 8, Worlds: 4, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 8 || len(result.Worlds) != 4 {
		t.Fatalf("post-arrange guidance accepted=%d worlds=%d first=%s", result.Accepted, len(result.Worlds), result.FirstRejection)
	}
	for _, world := range result.Worlds {
		if err := VerifyWorld(world); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProposalSamplerContinuesAfterIncompatiblePast(t *testing.T) {
	land := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	spell := syntheticCard(t, "Name:Uncastable\nManaCost:99\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:Fixture.\n")
	a := syntheticCard(t, "Name:Public Spell\nManaCost:0\nTypes:Sorcery\nA:SP$ GainLife | LifeAmount$ 1\nOracle:Fixture.\n")
	shuffle := syntheticCard(t, "Name:Shuffle Opponent\nManaCost:0\nTypes:Sorcery\nA:SP$ Shuffle | Defined$ Opponent\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(land, 20), repeatCard(spell, 20)}
	decks[0][0], decks[1][0], decks[1][1] = shuffle, a, a
	setup, h := proposalHistory(t, decks, 0, func(e *rules.Engine) bool { return len(e.G.Zone(state.ZGraveyard, 1)) == 2 })
	result, err := Sample(setup, h, SampleOptions{Seed: 43, Attempts: 24, Worlds: 1, MaxSubmits: 500})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted == 0 || result.IncompatibleProposals == 0 || result.Attempts != 24 || len(result.Worlds) != 1 {
		t.Fatalf("failed to continue: accepted=%d incompatible=%d attempts=%d worlds=%d", result.Accepted, result.IncompatibleProposals, result.Attempts, len(result.Worlds))
	}
	if result.Accepted+result.IncompatibleProposals+result.PrefixRejected+result.BudgetExhausted != result.Attempts {
		t.Fatal("attempt accounting drift")
	}
	for _, w := range result.Worlds {
		if err := VerifyWorld(w); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProposalWorkerSchedulingPreservesWorlds(t *testing.T) {
	setup, h := samplingHistory(t)
	opts := SampleOptions{Seed: 83, Attempts: 8, Worlds: 4, MaxSubmits: 500}
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	want, err := Sample(setup, h, opts)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GOMAXPROCS(4)
	type outcome struct {
		result SampleResult
		err    error
	}
	results := make(chan outcome, 3)
	for range 3 {
		go func() { got, err := Sample(setup, h, opts); results <- outcome{got, err} }()
	}
	for range 3 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		assertSameProposalWorlds(t, want, got.result)
	}
}

func TestProposalIgnoresSourceSecretsAndPrivateIntentIndices(t *testing.T) {
	a, b := observationEngine(t, 17), observationEngine(t, 83)
	b.Rand(37)
	a.L.Intents = append(a.L.Intents, decision.Intent{Player: 1, Choices: []int{0}})
	b.L.Intents = append(b.L.Intents, decision.Intent{Player: 1, Choices: []int{19}})
	if a.L.Head() == b.L.Head() || a.RNGDraws() == b.RNGDraws() {
		t.Fatal("fixture needs distinct source secrets")
	}
	fa, err := NewCollector(0).Capture(a, a.L.Events)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := NewCollector(0).Capture(b, b.L.Events)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fa, fb) {
		t.Fatal("source secrets reached owned observation")
	}
	setup, _ := samplingHistory(t)
	opts := SampleOptions{Seed: 89, Attempts: 8, Worlds: 4, MaxSubmits: 500}
	x, err := Sample(setup, History{Actor: 0, Frames: []Frame{fa}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	y, err := Sample(setup, History{Actor: 0, Frames: []Frame{fb}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	assertSameProposalWorlds(t, x, y)
}

func assertSameProposalWorlds(t *testing.T, a, b SampleResult) {
	t.Helper()
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) || len(a.Worlds) != 4 || len(b.Worlds) != 4 {
		t.Fatal("proposal diagnostics differ or worlds missing")
	}
	for i := range a.Worlds {
		x, y := a.Worlds[i], b.Worlds[i]
		if x.Engine.L.Head() != y.Engine.L.Head() || !reflect.DeepEqual(x.Engine.ChanceTranscript(), y.Engine.ChanceTranscript()) || !reflect.DeepEqual(x.Engine.L.Intents, y.Engine.L.Intents) {
			t.Fatal("proposal depends on source secret or worker schedule")
		}
	}
}

func TestProposalExplicitlyUnguidedEpochUsesOrdinaryPrior(t *testing.T) {
	card := syntheticCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:Fixture.\n")
	cfg := rules.Config{Seed: 87, Names: []string{"a", "b"}, Decks: [][]*cards.Card{repeatCard(card, 12), repeatCard(card, 12)}}
	p := &proposalState{epochs: map[epochKey]epochConstraints{{Player: 0}: {Unguided: []string{"library_mutation"}}}, observer: NewCollector(0), result: &SampleResult{}}
	a, err := rules.NewHypotheticalPlanned(cfg, nil, p.plan)
	if err != nil {
		t.Fatal(err)
	}
	b, err := rules.NewHypothetical(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.L.Head() != b.L.Head() || a.RNGDraws() != b.RNGDraws() || !reflect.DeepEqual(a.ChanceTranscript(), b.ChanceTranscript()) {
		t.Fatal("unguided epoch changed prior chance")
	}
	if p.logWeight != 0 || p.result.UnguidedConstraints != 1 || p.result.GuidedGenesis != 0 {
		t.Fatalf("unguided diagnostics=%+v weight=%g", p.result, p.logWeight)
	}
}

func TestProposalDuplicateResamplesOwnMutableState(t *testing.T) {
	setup, h := samplingHistory(t)
	for seed := uint64(0); seed < 20; seed++ {
		result, err := Sample(setup, h, SampleOptions{Seed: seed, Attempts: 4, Worlds: 4, MaxSubmits: 500})
		if err != nil {
			t.Fatal(err)
		}
		for i, a := range result.Worlds {
			for j, b := range result.Worlds {
				if i >= j || a.Config.Seed != b.Config.Seed {
					continue
				}
				beforeHead, beforeLife := b.Engine.L.Head(), b.Engine.G.Players[0].Life
				a.Engine.Emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -3})
				if a.Engine.G.Players[0].Life != beforeLife-3 || b.Engine.G.Players[0].Life != beforeLife || b.Engine.L.Head() != beforeHead {
					t.Fatal("duplicate engines share mutable state")
				}
				id := a.Engine.G.Zone(state.ZLibrary, 0)[0]
				if a.Observer.ref(id) != 0 || b.Observer.ref(id) != 0 {
					t.Fatal("fixture card already known")
				}
				a.Observer.introduce(a.Engine, id)
				if a.Observer.ref(id) == 0 || b.Observer.ref(id) != 0 {
					t.Fatal("duplicate observer dictionaries alias")
				}
				return
			}
		}
	}
	t.Fatal("fixture never resampled a duplicate")
}

// Drive a real spell through the checked engine boundary. The opponent's
// actual hypothetical hand is ABBBBBB and the shuffled library is ABB.
func runLaterProposal(t *testing.T, epoch epochConstraints) (*rules.Engine, SampleResult, float64, error) {
	t.Helper()
	a := syntheticCard(t, "Name:A\nManaCost:99\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:Fixture.\n")
	b := syntheticCard(t, "Name:B\nManaCost:99\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:Fixture.\n")
	shuffle := syntheticCard(t, "Name:Shuffle Opponent\nManaCost:0\nTypes:Sorcery\nA:SP$ Shuffle | Defined$ Opponent\nOracle:Fixture.\n")
	decks := [][]*cards.Card{repeatCard(b, 10), repeatCard(b, 10)}
	decks[0][0], decks[1][0], decks[1][7] = shuffle, a, a
	tape := []rules.ChanceDraw{{Bound: 2, Value: 0}}
	for range decks {
		for n := 10; n > 1; n-- {
			tape = append(tape, rules.ChanceDraw{Bound: n, Value: n - 1})
		}
	}
	var result SampleResult
	c := NewCollector(0)
	p := &proposalState{epochs: map[epochKey]epochConstraints{{Player: 1, Ordinal: 1}: epoch}, observer: c, result: &result}
	e, err := rules.NewHypotheticalPlanned(rules.Config{Seed: 8, Names: []string{"a", "b"}, Decks: decks}, tape, p.plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(17, 29))
	pos := 0
	for i := 0; i < 30; i++ {
		if _, err := c.Capture(e, e.L.Events[pos:]); err != nil {
			t.Fatal(err)
		}
		if len(e.G.Zone(state.ZGraveyard, 0)) > 0 {
			return e, result, p.logWeight, nil
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("fixture ended before shuffle")
		}
		in := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, r)
		pos = len(e.L.Events)
		if err := e.SubmitHypothetical(in); err != nil {
			return e, result, p.logWeight, err
		}
	}
	t.Fatal("fixture did not shuffle")
	return nil, result, p.logWeight, nil
}

func repeatCard(card *cards.Card, n int) []*cards.Card {
	deck := make([]*cards.Card, n)
	for i := range deck {
		deck[i] = card
	}
	return deck
}

func proposalHistory(t *testing.T, decks [][]*cards.Card, toss int, stop func(*rules.Engine) bool, answers ...func(*decision.Decision, decision.Intent) decision.Intent) (PublicGame, History) {
	t.Helper()
	setup := PublicGame{Names: []string{"a", "b"}, Decks: decks}
	tape := []rules.ChanceDraw{{Bound: 2, Value: toss}}
	for _, deck := range decks {
		for n := len(deck); n > 1; n-- {
			tape = append(tape, rules.ChanceDraw{Bound: n, Value: n - 1})
		}
	}
	e, err := rules.NewHypothetical(rules.Config{Seed: 1, Names: setup.Names, Decks: decks}, tape)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	c := NewCollector(0)
	h := History{Actor: 0, Answers: make(map[int][]Action)}
	r := rand.New(rand.NewPCG(4, 8))
	pos := 0
	for i := 0; i < 500; i++ {
		frame, err := c.Capture(e, e.L.Events[pos:])
		if err != nil {
			t.Fatal(err)
		}
		h.Frames = append(h.Frames, frame)
		if stop(e) {
			return setup, h
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("fixture ended before root")
		}
		in := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, r)
		if d.Player == 0 {
			if d.Kind == decision.KArrange {
				in.Choices = nil
				for j := len(d.Options) - 1; j >= 0; j-- {
					in.Choices = append(in.Choices, j)
				}
			}
			for _, answer := range answers {
				in = answer(d, in)
			}
			h.Answers[i], err = c.Actions(d, in)
			if err != nil {
				t.Fatal(err)
			}
		}
		pos = len(e.L.Events)
		if err := e.SubmitHypothetical(in); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("fixture never reached root")
	return setup, h
}
