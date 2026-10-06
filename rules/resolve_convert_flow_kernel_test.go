package rules

// Kernel-era restorations of the W3 step 2 dual-run tests of
// resolve_convert_flow_test.go: one kernel run per scenario (kr7Run), the
// asks served from the tape, the named asks posed, the replay identical.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kr7RunFlowCases runs every case over seeds base, base+1, ... (n seeds:
// the deterministic driver answers by decision sequence, so different seeds
// reach both arms of a yes/no) and requires each case's kinds posed at least
// once and its asks served from the tape.
func kr7RunFlowCases(t *testing.T, base uint64, seeds int, cases []tapeFlowCase) {
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
			if tc.pick != nil {
				tapeFlowPick = tc.pick
				t.Cleanup(func() { tapeFlowPick = nil })
			}
			seen := map[string]int{}
			for s := 0; s < seeds; s++ {
				kinds := map[string]int{}
				_, st := kr7Dual(t, seats, base+uint64(100*ci+s), func(t *testing.T, e *Engine) {
					if tc.setup != nil {
						tc.setup(t, e)
					}
					tapeCastAndResolveKinds(t, e, tc.name, "U", kinds)
				}, srcs...)
				if st.LegacySwitch != 0 || st.Aborts != 0 || st.Served < tc.served {
					t.Fatalf("seed %d: the asks were not served from the tape: %+v (kinds %v)", s, st, kinds)
				}
				for k, n := range kinds {
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

func TestKernelAskFlowReveal(t *testing.T) {
	kr7RunFlowCases(t, 31000, 3, []tapeFlowCase{
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

func TestKernelAskFlowDiscard(t *testing.T) {
	kr7RunFlowCases(t, 32000, 3, []tapeFlowCase{
		{name: "Tape Duress All", src: "A:SP$ Discard | Defined$ Player | Mode$ RevealYouChoose | NumCards$ 1 | RememberDiscarded$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"discard"}, served: 2},
		{name: "Tape Thirst", src: "A:SP$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 2 | UnlessType$ Land",
			kinds: []string{"discard_unless", "discard"}, served: 2},
		{name: "Tape Maybe Discard", src: "A:SP$ Discard | Defined$ Player | Mode$ TgtChoose | NumCards$ 1 | Optional$ True | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"discard_may", "discard"}, served: 2, pick: tapePickYes},
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
func TestKernelAskFlowDiscardUnlessEachTarget(t *testing.T) {
	kr7RunFlowCases(t, 32500, 3, []tapeFlowCase{
		{name: "Tape Each Thirst", src: "A:SP$ Discard | Defined$ Player | Mode$ TgtChoose | NumCards$ 2 | UnlessType$ Land",
			kinds: []string{"discard_unless", "discard"}, served: 2},
	})
}

func TestKernelAskFlowDig(t *testing.T) {
	kr7RunFlowCases(t, 33000, 3, []tapeFlowCase{
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

func TestKernelAskFlowSurveilLook(t *testing.T) {
	onField := func(t *testing.T, e *Engine) { moveByName(t, e, 0, "Tape Enhanced", state.ZBattlefield) }
	kr7RunFlowCases(t, 34000, 3, []tapeFlowCase{
		{name: "Tape Surveil Look", src: "A:SP$ Surveil | Amount$ 2 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"surveil_look_optional", "arrange"}, served: 2, extra: []string{tapeEnhancedSrc}, setup: onField},
	})
}

func TestKernelAskFlowHideaway(t *testing.T) {
	kr7RunFlowCases(t, 35000, 2, []tapeFlowCase{
		{name: "Tape Hideaway", src: "A:SP$ Hideaway | Amount$ 4 | SubAbility$ DBGain\nSVar:DBGain:DB$ GainLife | LifeAmount$ 2",
			kinds: []string{"hideaway_pick", "hideaway_arrange"}, served: 2},
	})
}

func TestKernelAskFlowNameCard(t *testing.T) {
	kr7RunFlowCases(t, 36000, 2, []tapeFlowCase{
		{name: "Tape Name", src: "A:SP$ NameCard | Defined$ You | SubAbility$ DBDig\nSVar:DBDig:DB$ Dig | DigNum$ 3 | ChangeNum$ 1 | ChangeValid$ Card.NamedCard",
			kinds: []string{"name"}, served: 1},
	})
}

func TestKernelAskFlowDraw(t *testing.T) {
	inYard := func(t *testing.T, e *Engine) { moveByName(t, e, 0, "Tape Dredger", state.ZGraveyard) }
	dredger := []string{tapeDredgerSrc}
	kr7RunFlowCases(t, 37000, 3, []tapeFlowCase{
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
func TestKernelAskFlowDredgeOptionalDraw(t *testing.T) {
	inYard := func(t *testing.T, e *Engine) { moveByName(t, e, 0, "Tape Dredger", state.ZGraveyard) }
	dredger := []string{tapeDredgerSrc}
	kr7RunFlowCases(t, 37500, 3, []tapeFlowCase{
		{name: "Tape Dredge Remora", src: "A:SP$ Draw | NumCards$ 2 | OptionalDecider$ You",
			kinds: []string{"draw_optional", "dredge"}, served: 2, extra: dredger, setup: inYard,
			// The case needs the election answered yes (a no draws nothing
			// and never reaches the dredge ask).
			pick: tapePickYes},
	})
}

func TestKernelAskFlowDigUntil(t *testing.T) {
	auraBoard := func(t *testing.T, e *Engine) {
		moveByName(t, e, 0, "Tape Aura", state.ZLibrary)
		moveByName(t, e, 0, "Tape Bear A", state.ZBattlefield)
		moveByName(t, e, 0, "Tape Bear B", state.ZBattlefield)
	}
	kr7RunFlowCases(t, 38000, 3, []tapeFlowCase{
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
func TestKernelAskFlowExcessSVarAcrossAsk(t *testing.T) {
	onField := func(t *testing.T, e *Engine) { moveByName(t, e, 0, "Tape Bear A", state.ZBattlefield) }
	src := "A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 5 | ExcessSVar$ X | SubAbility$ DBDig\n" +
		"SVar:DBDig:DB$ Dig | DigNum$ X | ChangeNum$ 1 | Optional$ True | DestinationZone$ Exile | RestRandomOrder$ True"
	kr7RunFlowCases(t, 39000, 3, []tapeFlowCase{
		{name: "Tape Warcrafting", src: src, kinds: []string{"dig"}, served: 1,
			extra: []string{tapeBearASrc}, setup: onField},
	})
	e, _ := kr7Fixture(t, 2, 39000, "Name:Tape Warcrafting\nManaCost:U\nTypes:Sorcery\n"+src+"\nOracle:x\n", tapeBearASrc)
	onField(t, e)
	tapeCastAndResolveKinds(t, e, "Tape Warcrafting", "U", map[string]int{})
	exiled := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.From == state.ZLibrary && ev.To == state.ZExile {
			exiled++
		}
	}
	if exiled != 1 {
		t.Fatalf("the answered Dig exiled %d cards, want 1 (the ExcessSVar$ binding was lost across the ask)", exiled)
	}
}
