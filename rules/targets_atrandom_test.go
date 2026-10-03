package rules

// TargetsAtRandom$ and NumRandomChoices$ on real corpus carriers. Forge's
// TargetSelection draws a random-target ability's targets itself, and
// ChooseGenericEffect offers a NumRandomChoices$ GenericChoice's chooser only
// a random draw of its choices. Both draws come from the engine's seeded rng
// (effects.RandomTargetsAsk, effects' charmRandomOffer), so a log-only replay
// re-derives them; the class census is effects' TestRandomSelectionCensus.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// randomTargetSeeds are the seeds the random-target tests sweep: enough
// that a real draw shows at least two different outcomes.
var randomTargetSeeds = []uint64{1, 7, 20, 33, 40, 61, 80, 99, 120, 140}

// assertDrawnTargetAsk fails unless d is a target ask narrowed to exactly
// one drawn option, and returns that option's label.
func assertDrawnTargetAsk(t *testing.T, seed uint64, d *decision.Decision) string {
	t.Helper()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("seed %d: want the random target ask, got %+v", seed, d)
	}
	if len(d.Options) != 1 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("seed %d: random target ask offers %d options (Min %d Max %d), want exactly the one drawn: %+v",
			seed, len(d.Options), d.Min, d.Max, d.Options)
	}
	if !strings.HasPrefix(d.Prompt, "Targets chosen at random: ") {
		t.Fatalf("seed %d: prompt %q does not say the targets were drawn", seed, d.Prompt)
	}
	return d.Options[0].Label
}

// TestGoblinTestPilotTargetIsDrawnAtRandom activates the real Goblin Test
// Pilot ("deals 2 damage to any target chosen at random") across seeds: the
// activation's target ask offers exactly one drawn target, the seeds draw
// more than one distinct target, and every game replays.
func TestGoblinTestPilotTargetIsDrawnAtRandom(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bears := lookup(t, reg, "Grizzly Bears")
	_, base := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Goblin Test Pilot")},
		[]*cards.Card{bears, bears})
	seen := map[string]bool{}
	for _, seed := range randomTargetSeeds {
		cfg := base
		cfg.Seed = seed
		cfg = seatZeroStart(cfg)
		e := New(cfg)
		e.Advance()
		toMain1(t, e)
		pilot := moveByName(t, e, 0, "Goblin Test Pilot", state.ZBattlefield)
		// Seat 0's next turn: the Pilot has been under its control since
		// the turn began, so its {T} ability is activatable (CR 302.6).
		driveToTurn(t, e, 3, 0)
		if o := e.G.Obj(pilot); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
			t.Fatalf("seed %d: the Pilot is not an untapped permanent at turn 3", seed)
		}
		moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
		moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
		e.priorityRound()
		submitChoices(t, e, abilityOption(t, e, pilot, 0).Index)
		label := assertDrawnTargetAsk(t, seed, e.Pending())
		seen[label] = true
		submitChoices(t, e, 0)
		for i := 0; i < 20 && len(e.G.Stack) > 0; i++ {
			d := e.Pending()
			if d == nil || d.Kind != decision.KPriority {
				t.Fatalf("seed %d: unexpected decision while the ping resolves: %+v", seed, d)
			}
			submitPass(t, e)
		}
		if len(e.G.Stack) != 0 {
			t.Fatalf("seed %d: the ping never resolved", seed)
		}
		replayCheck(t, e, cfg)
	}
	if len(seen) < 2 {
		t.Fatalf("%d seeds all drew %v: the target is not random", len(randomTargetSeeds), seen)
	}
}

// TestScabClanGiantFightsARandomOpposingCreature puts the real Scab-Clan
// Giant onto the battlefield: its enters trigger's placement target ask
// (the trigger-drain path) offers exactly one drawn opposing creature.
func TestScabClanGiantFightsARandomOpposingCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	opp := []*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Hill Giant"), lookup(t, reg, "Craw Wurm")}
	_, base := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Scab-Clan Giant")}, opp)
	seen := map[string]bool{}
	for _, seed := range randomTargetSeeds {
		cfg := base
		cfg.Seed = seed
		cfg = seatZeroStart(cfg)
		e := New(cfg)
		e.Advance()
		toMain1(t, e)
		for _, c := range opp {
			moveByName(t, e, 1, c.Faces[0].Name, state.ZBattlefield)
		}
		ensureInHand(t, e, 0, "Scab-Clan Giant")
		addMana(t, e, 0, "RRRRRG")
		castCardNow(t, e, "Scab-Clan Giant")
		var label string
		for i := 0; i < 40 && label == ""; i++ {
			d := e.Pending()
			if d == nil {
				t.Fatalf("seed %d: no decision before the fight trigger's target ask", seed)
			}
			if d.Kind == decision.KTarget {
				label = assertDrawnTargetAsk(t, seed, d)
				submitChoices(t, e, 0)
				break
			}
			if d.Kind != decision.KPriority {
				t.Fatalf("seed %d: unexpected decision %+v", seed, d)
			}
			submitPass(t, e)
		}
		if label == "" {
			t.Fatalf("seed %d: the enters trigger never posed its target ask", seed)
		}
		switch strings.TrimSuffix(label, " (b)") {
		case "Grizzly Bears", "Hill Giant", "Craw Wurm":
		default:
			t.Fatalf("seed %d: drew %q, not a creature an opponent controls", seed, label)
		}
		seen[label] = true
		replayCheck(t, e, cfg)
	}
	if len(seen) < 2 {
		t.Fatalf("%d seeds all drew %v: the target is not random", len(randomTargetSeeds), seen)
	}
}

// TestDavrielOffersThreeRandomOffers activates the real Davriel, Soul
// Broker's -2: the offer GenericChoice (NumRandomChoices$ 3 of eight) poses
// exactly three choices, the condition GenericChoice after it three of its
// eight, the offered sets vary across seeds, and the answered choice runs
// the offered body (the ask's indexes map against the offered list, not
// the whole Choices$).
func TestDavrielOffersThreeRandomOffers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	_, base := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Davriel, Soul Broker")}, nil)
	offerSets := map[string]bool{}
	provenMapping := false
	for _, seed := range randomTargetSeeds {
		cfg := base
		cfg.Seed = seed
		cfg = seatZeroStart(cfg)
		e := New(cfg)
		e.Advance()
		toMain1(t, e)
		davriel := moveByName(t, e, 0, "Davriel, Soul Broker", state.ZBattlefield)
		if got := e.G.Obj(davriel).Counter("LOYALTY"); got < 2 {
			t.Fatalf("seed %d: Davriel entered with %d loyalty", seed, got)
		}
		for range 4 {
			moveByName(t, e, 0, "Mountain", state.ZBattlefield)
		}
		e.priorityRound()
		submitChoices(t, e, abilityOption(t, e, davriel, 1).Index)
		asks := 0
		lifeBefore := e.G.Players[0].Life
		permsBefore := len(e.G.Zone(state.ZBattlefield, 0))
		condition := ""
		conditionIdx := -1
		for i := 0; i < 60 && len(e.G.Stack) > 0; i++ {
			d := e.Pending()
			if d == nil {
				t.Fatalf("seed %d: no decision while Davriel's -2 resolves", seed)
			}
			switch d.Kind {
			case decision.KPriority:
				submitPass(t, e)
				continue
			case decision.KModes:
				asks++
				if len(d.Options) != 3 {
					t.Fatalf("seed %d: GenericChoice ask %d offers %d choices, want 3: %+v", seed, asks, len(d.Options), d.Options)
				}
				labels := make([]string, len(d.Options))
				for j, o := range d.Options {
					labels[j] = o.Label
				}
				pick := 0
				if asks == 1 {
					offerSets[strings.Join(labels, "|")] = true
					// The offer: draw three when offered (no board change).
					for j, l := range labels {
						if strings.HasPrefix(l, "Draw three") {
							pick = j
						}
					}
				} else {
					// The condition: sacrifice two (a board count) when
					// offered, else lose 6 life.
					for j, l := range labels {
						if strings.HasPrefix(l, "You lose 6") && condition == "" {
							pick, condition = j, "lose6"
						}
						if strings.HasPrefix(l, "Sacrifice two") {
							pick, condition = j, "sac2"
						}
					}
					conditionIdx = pick
				}
				submitChoices(t, e, pick)
			default:
				// A chosen body may ask (a ChangeZone pick, a sacrifice):
				// answer with its first legal shape.
				submitChoices(t, e, firstLegal(d)...)
			}
		}
		if asks != 2 {
			t.Fatalf("seed %d: Davriel's -2 posed %d GenericChoice asks, want 2 (offer, condition)", seed, asks)
		}
		// The answer's index maps against the OFFERED list: the chosen
		// condition's own body runs. Sac2 is Choices$ index 2, so a seed
		// offering it at a lower index proves the offered-list mapping (the
		// whole-list mapping would run a different condition).
		switch condition {
		case "lose6":
			if got := e.G.Players[0].Life; got != lifeBefore-6 {
				t.Fatalf("seed %d: chose the lose-6 condition, life %d -> %d", seed, lifeBefore, got)
			}
		case "sac2":
			if got := len(e.G.Zone(state.ZBattlefield, 0)); got != permsBefore-2 {
				t.Fatalf("seed %d: chose the sacrifice-two condition (offered index %d), permanents %d -> %d",
					seed, conditionIdx, permsBefore, got)
			}
			if conditionIdx < 2 {
				provenMapping = true
			}
		}
		replayCheck(t, e, cfg)
	}
	if len(offerSets) < 2 {
		t.Fatalf("%d seeds all offered %v: the offer is not random", len(randomTargetSeeds), offerSets)
	}
	if !provenMapping {
		t.Fatalf("no seed offered sacrifice-two below its Choices$ index: widen randomTargetSeeds")
	}
}

// firstLegal is the first Min options of d (at least one when d has any).
func firstLegal(d *decision.Decision) []int {
	n := max(d.Min, 0)
	if n == 0 && len(d.Options) > 0 && !d.AllowNone {
		n = 1
	}
	if n > len(d.Options) {
		n = len(d.Options)
	}
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}
