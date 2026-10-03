package rules

// W3 step 2 (group "flow"): dual-run tests for the card-flow ask sites
// converted onto the resolution kernel -- the look ack, the reveal pick and
// may-reveal, the discard family, Dig, DigUntil, Surveil's look election,
// Hideaway, NameCard, Draw's optional/up-to asks, Dredge and Recruit. Each
// case is cast and resolved on legacy and on the tape kernel by the same
// deterministic driver (tapeDual) and must stay identical, its asks served
// from the tape with no legacy switch, and its named resume kind posed.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// tapeFlowCase is one converted-site scenario: the sorcery's ability lines,
// the resume kinds the run must pose (on both paths) and the minimum
// number of tape-served answers.
type tapeFlowCase struct {
	name, src string
	kinds     []string
	served    int64
	seats     int
	extra     []string // further fixture cards, moved to seat 0's hand
	setup     func(t *testing.T, e *Engine)
	// checkpointAll: the site's ask is board-dependent (a Dredge ask over
	// an otherwise ask-free Draw), which the ask-free predicate cannot see,
	// so the case checkpoints every resolution (tapeCheckpointAll).
	checkpointAll bool
}

// tapeCastAndResolveKinds is tapeCastAndResolve recording every posed
// non-priority decision's resume kind.
func tapeCastAndResolveKinds(t *testing.T, e *Engine, name, mana string, kinds map[string]int) {
	t.Helper()
	addMana(t, e, 0, mana)
	id := fixtureInHand(t, e, name)
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		kinds[d.ResumeKind]++
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
			t.Fatalf("submit %s/%s %v: %v", d.Kind, d.ResumeKind, tapePick(d), err)
		}
	}
	t.Fatal("stack never drained")
}

// runTapeFlowCases runs every case over seeds base, base+1, ... (n seeds:
// the deterministic driver answers by decision sequence, so different seeds
// reach both arms of a yes/no) and requires each case's kinds posed at least
// once and its asks served from the tape.
func runTapeFlowCases(t *testing.T, base uint64, seeds int, cases []tapeFlowCase) {
	t.Helper()
	for ci, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "Name:" + tc.name + "\nManaCost:U\nTypes:Sorcery\n" + tc.src + "\nOracle:x\n"
			srcs := append([]string{src}, tc.extra...)
			seats := tc.seats
			if seats == 0 {
				seats = 2
			}
			if tc.checkpointAll {
				tapeCheckpointAll = true
				t.Cleanup(func() { tapeCheckpointAll = false })
			}
			seen := map[string]int{}
			for s := 0; s < seeds; s++ {
				legacyKinds := map[string]int{}
				tapeKinds := map[string]int{}
				first := true
				_, st := tapeDual(t, seats, base+uint64(100*ci+s), func(t *testing.T, e *Engine) {
					if tc.setup != nil {
						tc.setup(t, e)
					}
					k := tapeKinds
					if first {
						k = legacyKinds
						first = false
					}
					tapeCastAndResolveKinds(t, e, tc.name, "U", k)
				}, srcs...)
				if st.LegacySwitch != 0 || st.Aborts != 0 || st.Served < tc.served {
					t.Fatalf("seed %d: the converted asks were not served from the tape: %+v (kinds %v)", s, st, legacyKinds)
				}
				for k, n := range legacyKinds {
					seen[k] += n
				}
			}
			for _, k := range tc.kinds {
				if seen[k] == 0 {
					t.Fatalf("resume kind %q never posed (posed: %v)", k, seen)
				}
			}
			t.Logf("posed kinds: %v", seen)
		})
	}
}

func TestTapeConvertFlowReveal(t *testing.T) {
	runTapeFlowCases(t, 31000, 3, []tapeFlowCase{
		{name: "Tape Bauble", src: "A:SP$ PeekAndReveal | Defined$ You | NoReveal$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"look_ack"}, served: 1},
		{name: "Tape Each Bauble", src: "A:SP$ PeekAndReveal | Defined$ Player | NoReveal$ True",
			kinds: []string{"look_ack"}, served: 2},
		{name: "Tape Probe", src: "A:SP$ RevealHand | Defined$ Player | Look$ True | SubAbility$ DBDraw\nSVar:DBDraw:DB$ Draw | NumCards$ 1",
			kinds: []string{"look_ack"}, served: 2},
		{name: "Tape Tutor Reveal", src: "A:SP$ Reveal | Defined$ You | NumCards$ 1 | RememberRevealed$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"reveal_pick"}, served: 1},
		{name: "Tape Each Reveal", src: "A:SP$ Reveal | Defined$ Player | AnyNumber$ True",
			kinds: []string{"reveal_pick"}, served: 2},
		{name: "Tape Maybe Reveal", src: "A:SP$ Reveal | Defined$ Player | NumCards$ 1 | Optional$ True | RememberRevealed$ True",
			kinds: []string{"reveal_optional", "reveal_pick"}, served: 2},
		{name: "Tape Delver Peek", src: "A:SP$ PeekAndReveal | Defined$ Player | RevealOptional$ True | RememberRevealed$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2 | ConditionDefined$ Remembered | ConditionPresent$ Card | ConditionCompare$ GE1",
			kinds: []string{"reveal_optional"}, served: 2},
	})
}

func TestTapeConvertFlowDiscard(t *testing.T) {
	runTapeFlowCases(t, 32000, 3, []tapeFlowCase{
		{name: "Tape Duress All", src: "A:SP$ Discard | Defined$ Player | Mode$ RevealYouChoose | NumCards$ 1 | RememberDiscarded$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"discard"}, served: 2},
		{name: "Tape Thirst", src: "A:SP$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 2 | UnlessType$ Land",
			kinds: []string{"discard_unless", "discard"}, served: 2},
		{name: "Tape Maybe Discard", src: "A:SP$ Discard | Defined$ Player | Mode$ TgtChoose | NumCards$ 1 | Optional$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"discard_may", "discard"}, served: 2},
		{name: "Tape Wheel Maybe", src: "A:SP$ Discard | Defined$ Player | Mode$ Hand | Optional$ True | SubAbility$ DBDraw\nSVar:DBDraw:DB$ Draw | NumCards$ 1 | Defined$ Player",
			kinds: []string{"discard_hand"}, served: 2},
		{name: "Tape Recruit", src: "A:SP$ Recruit",
			kinds: []string{"discard"}, served: 1},
	})
}

// A multi-target UnlessType$ discard (Bandit's Talent's Defined$ Opponent,
// Compulsive Research's Defined$ Targeted): each target's election applies
// to that target alone. Before the discard_unless cursor the legacy
// re-entry handed target 0 a later target's election and re-ran its
// discard; the tape path never could, so the two diverged.
func TestTapeConvertFlowDiscardUnlessEachTarget(t *testing.T) {
	runTapeFlowCases(t, 32500, 3, []tapeFlowCase{
		{name: "Tape Each Thirst", src: "A:SP$ Discard | Defined$ Player | Mode$ TgtChoose | NumCards$ 2 | UnlessType$ Land",
			kinds: []string{"discard_unless", "discard"}, served: 2},
	})
}

func TestTapeConvertFlowDig(t *testing.T) {
	runTapeFlowCases(t, 33000, 3, []tapeFlowCase{
		{name: "Tape Impulse", src: "A:SP$ Dig | DigNum$ 4 | ChangeNum$ 1 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"dig", "dig_arrange"}, served: 2},
		{name: "Tape Each Dig", src: "A:SP$ Dig | Defined$ Player | DigNum$ 3 | ChangeNum$ 1",
			kinds: []string{"dig", "dig_arrange"}, served: 2},
		{name: "Tape Maybe Dig", src: "A:SP$ Dig | Defined$ Player | DigNum$ 3 | ChangeNum$ 1 | Optional$ True | DestinationZone$ Graveyard | DestinationZone2$ Library | LibraryPosition2$ 0",
			kinds: []string{"dig"}, served: 2},
		{name: "Tape Skip Dig", src: "A:SP$ Dig | DigNum$ 3 | ChangeNum$ 1 | PromptToSkipOptionalAbility$ True",
			kinds: []string{"dig"}, served: 1},
		{name: "Tape Library Dig", src: "A:SP$ Dig | DigNum$ 4 | ChangeNum$ 1 | DestinationZone$ Library | LibraryPosition$ 0",
			kinds: []string{"dig", "dig_arrange"}, served: 2},
		{name: "Tape Any Dig", src: "A:SP$ Dig | DigNum$ 3 | ChangeNum$ Any | DestinationZone$ Graveyard",
			kinds: []string{"dig"}, served: 1},
		{name: "Tape Bottom Dig", src: "A:SP$ Dig | Defined$ Player | DigNum$ 3 | ChangeNum$ 0",
			kinds: []string{"dig_arrange"}, served: 1},
	})
}

const tapeEnhancedSrc = "Name:Tape Enhanced\nManaCost:1 U\nTypes:Enchantment\n" +
	"S:Mode$ SurveilNum | Num$ 2 | ValidPlayer$ You | Optional$ True | Description$ x\nOracle:x\n"

func TestTapeConvertFlowSurveilLook(t *testing.T) {
	onField := func(t *testing.T, e *Engine) { moveByName(t, e, 0, "Tape Enhanced", state.ZBattlefield) }
	runTapeFlowCases(t, 34000, 3, []tapeFlowCase{
		{name: "Tape Surveil Look", src: "A:SP$ Surveil | Amount$ 2 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"surveil_look_optional", "arrange"}, served: 2, extra: []string{tapeEnhancedSrc}, setup: onField},
	})
}

func TestTapeConvertFlowHideaway(t *testing.T) {
	runTapeFlowCases(t, 35000, 2, []tapeFlowCase{
		{name: "Tape Hideaway", src: "A:SP$ Hideaway | Amount$ 4 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"hideaway_pick", "hideaway_arrange"}, served: 2},
	})
}

func TestTapeConvertFlowNameCard(t *testing.T) {
	runTapeFlowCases(t, 36000, 2, []tapeFlowCase{
		{name: "Tape Name", src: "A:SP$ NameCard | Defined$ You | SubAbility$ DBDig\nSVar:DBDig:DB$ Dig | DigNum$ 3 | ChangeNum$ 1 | ChangeValid$ Card.NamedCard",
			kinds: []string{"name"}, served: 1},
	})
}

const tapeDredgerSrc = "Name:Tape Dredger\nManaCost:G\nTypes:Sorcery\nK:Dredge:2\nA:SP$ GainLife | LifeAmount$ 1\nOracle:x\n"

func TestTapeConvertFlowDraw(t *testing.T) {
	inYard := func(t *testing.T, e *Engine) { moveByName(t, e, 0, "Tape Dredger", state.ZGraveyard) }
	dredger := []string{tapeDredgerSrc}
	runTapeFlowCases(t, 37000, 3, []tapeFlowCase{
		{name: "Tape Remora", src: "A:SP$ Draw | NumCards$ 2 | OptionalDecider$ You | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"draw_optional"}, served: 1},
		{name: "Tape Truce", src: "A:SP$ Draw | NumCards$ 2 | Upto$ True | Defined$ Player | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"draw_upto"}, served: 2},
		{name: "Tape Dredge Draw", src: "A:SP$ Draw | NumCards$ 3 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"dredge"}, served: 1, extra: dredger, setup: inYard, checkpointAll: true},
		{name: "Tape Dredge Remember", src: "A:SP$ Draw | NumCards$ 2 | RememberDrawn$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2 | ConditionDefined$ Remembered | ConditionPresent$ Card | ConditionCompare$ GE1",
			kinds: []string{"dredge"}, served: 1, extra: dredger, setup: inYard, checkpointAll: true},
		{name: "Tape Dredge Upto", src: "A:SP$ Draw | NumCards$ 2 | Upto$ True | Defined$ Player",
			kinds: []string{"draw_upto", "dredge"}, served: 2, extra: dredger, setup: inYard},
		{name: "Tape Dredge Recruit", src: "A:SP$ Recruit",
			kinds: []string{"dredge", "discard"}, served: 2, extra: dredger, setup: inYard, checkpointAll: true},
	})
}

// OptionalDecider$ over a Dredge-replaced draw (Mystic Remora's "may draw"
// with a dredger in the graveyard): the decider is asked once. Before the
// fix the legacy dredge re-entry re-posed the draw_optional election and a
// second yes restarted the draws from zero; the tape path, continuing past
// the dredged draw, never re-asked, so the two diverged.
func TestTapeConvertFlowDredgeOptionalDraw(t *testing.T) {
	inYard := func(t *testing.T, e *Engine) { moveByName(t, e, 0, "Tape Dredger", state.ZGraveyard) }
	dredger := []string{tapeDredgerSrc}
	runTapeFlowCases(t, 37500, 3, []tapeFlowCase{
		{name: "Tape Dredge Remora", src: "A:SP$ Draw | NumCards$ 2 | OptionalDecider$ You",
			kinds: []string{"draw_optional", "dredge"}, served: 2, extra: dredger, setup: inYard},
	})
}

const (
	tapeAuraSrc  = "Name:Tape Aura\nManaCost:W\nTypes:Enchantment Aura\nK:Enchant creature\nA:SP$ Attach | Cost$ W | ValidTgts$ Creature | AILogic$ Pump\nOracle:x\n"
	tapeBearASrc = "Name:Tape Bear A\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	tapeBearBSrc = "Name:Tape Bear B\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
)

func TestTapeConvertFlowDigUntil(t *testing.T) {
	auraBoard := func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Tape Aura", state.ZLibrary)
		moveByName(t, e, 0, "Tape Bear A", state.ZBattlefield)
		moveByName(t, e, 0, "Tape Bear B", state.ZBattlefield)
	}
	runTapeFlowCases(t, 38000, 3, []tapeFlowCase{
		{name: "Tape Songbirds", src: "A:SP$ DigUntil | Valid$ Card.Land | FoundDestination$ Battlefield | OptionalFoundMove$ True | RevealedDestination$ Graveyard | RememberFound$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"diguntil_move"}, served: 1},
		{name: "Tape Each Songbirds", src: "A:SP$ DigUntil | Defined$ Player | Valid$ Card.Land | FoundDestination$ Hand | OptionalFoundMove$ True | RevealedDestination$ Library | RevealedLibraryPosition$ -1",
			kinds: []string{"diguntil_move"}, served: 1},
		{name: "Tape Aura Dig", src: "A:SP$ DigUntil | Valid$ Card.Aura | FoundDestination$ Battlefield | RevealedDestination$ Library | RevealedLibraryPosition$ -1 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"diguntil_aura"}, served: 1, extra: []string{tapeAuraSrc, tapeBearASrc, tapeBearBSrc}, setup: auraBoard},
		{name: "Tape Aura Maybe", src: "A:SP$ DigUntil | Valid$ Card.Aura | FoundDestination$ Battlefield | OptionalFoundMove$ True | RevealedDestination$ Graveyard",
			kinds: []string{"diguntil_move"}, served: 1, extra: []string{tapeAuraSrc, tapeBearASrc, tapeBearBSrc}, setup: auraBoard},
	})
}

// A published resolution-scoped SVar (DealDamage's ExcessSVar$) read by a
// later sub-ability that ASKS (Nahiri's Warcrafting's DigNum$ X): the
// legacy resume rebuilt the Ctx's SVar table from the face and lost the
// binding, so the re-entered Dig saw an empty window and dropped the
// answered exile. The resume point now carries the published bindings;
// the tape path, continuing in place, always had them.
func TestTapeConvertFlowExcessSVarAcrossAsk(t *testing.T) {
	onField := func(t *testing.T, e *Engine) { moveByName(t, e, 0, "Tape Bear A", state.ZBattlefield) }
	src := "A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 5 | ExcessSVar$ X | SubAbility$ DBDig\n" +
		"SVar:DBDig:DB$ Dig | DigNum$ X | ChangeNum$ 1 | Optional$ True | DestinationZone$ Exile | RestRandomOrder$ True"
	runTapeFlowCases(t, 39000, 3, []tapeFlowCase{
		{name: "Tape Warcrafting", src: src, kinds: []string{"dig"}, served: 1,
			extra: []string{tapeBearASrc}, setup: onField},
	})
	e, _ := tapeFixture(t, 2, 39000, false, "Name:Tape Warcrafting\nManaCost:U\nTypes:Sorcery\n"+src+"\nOracle:x\n", tapeBearASrc)
	onField(t, e)
	tapeCastAndResolveKinds(t, e, "Tape Warcrafting", "U", map[string]int{})
	exiled := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZExile {
			exiled++
		}
	}
	if exiled != 1 {
		t.Fatalf("legacy: the answered Dig exiled %d cards, want 1 (the ExcessSVar$ binding was lost across the ask)", exiled)
	}
}
