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
