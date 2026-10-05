package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The target-bound reader must follow declared Teamwork intent at the real
// cast's 601.2c boundary, before the optional tap payment is answered.
func TestTeamworkTargetAnnouncementEligibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		card string
		mana string
	}{
		{"Atlantis Attacks", "UUUUUUU"},
		{"Cruel Alliance", "BBB"},
		{"Heroic Teamwork", "WWW"},
		{"Too Evil to Stay Dead", "BBB"},
	} {
		for _, paid := range []bool{true, false} {
			t.Run(tc.card+map[bool]string{true: "/paid", false: "/declined"}[paid], func(t *testing.T) {
				e, reg := teamworkTargetEngine(t, tc.card)
				for range 3 {
					seedBattlefield(t, e, reg, "Grizzly Bears")
				}
				spell := searchMoveByName(t, e, tc.card, state.ZHand)
				if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand {
					t.Fatalf("precondition: spell must be in hand: %+v", o)
				}
				low := seedBattlefield(t, e, reg, "Savannah Lions")
				high := seedBattlefield(t, e, reg, "Hill Giant")
				if tc.card == "Too Evil to Stay Dead" {
					low = moveSeededCard(t, e, 0, searchCorpusCard(t, reg, "Savannah Lions"), state.ZGraveyard)
					high = moveSeededCard(t, e, 0, searchCorpusCard(t, reg, "Air Elemental"), state.ZGraveyard)
					if e.G.Obj(low).Zone != state.ZGraveyard || e.G.Obj(high).Zone != state.ZGraveyard {
						t.Fatalf("precondition: candidate cards must be in graveyard: low=%+v high=%+v", e.G.Obj(low), e.G.Obj(high))
					}
				}
				if e.G.Obj(low).Face().ManaValue() == e.G.Obj(high).Face().ManaValue() {
					t.Fatalf("precondition: candidate mana values must differ: %d", e.G.Obj(low).Face().ManaValue())
				}
				addMana(t, e, 0, tc.mana)
				opt := castOptMode(t, e.Pending().Options, spell, "teamworked")
				submitChoices(t, e, opt.Index)
				d := teamworkAskOptions(t, e)
				if e.cast == nil || e.cast.card != spell || castAnswerCodes.Code(e.cast.mode) != castAnswerTeamworkMode {
					t.Fatalf("precondition: expected pending real Teamwork cast: %+v", e.cast)
				}
				face := e.G.Obj(spell).Face()
				root := face.SpellAbility()
				if root == nil {
					t.Fatal("precondition: expected corpus spell ability")
				}
				if tc.card == "Cruel Alliance" || tc.card == "Too Evil to Stay Dead" {
					if root.Sub == nil {
						t.Fatal("precondition: expected linked target ability")
					}
					rootMin, rootMax := e.resolvedTargetBounds(0, spell, root, 0)
					subMin, _ := e.resolvedTargetBounds(0, spell, root.Sub, 0)
					if rootMin != 0 || rootMax != 0 || subMin != 1 {
						t.Fatalf("Teamwork intent root=(%d,%d), linked min=%d; want root=(0,0), linked=1", rootMin, rootMax, subMin)
					}
					if tc.card == "Cruel Alliance" {
						candidates := e.legalTargetCandidates(0, spell, spell, root.Sub)
						if !hasTargetCandidate(candidates, low) || !hasTargetCandidate(candidates, high) {
							t.Fatalf("Teamwork linked Creature target set omitted expected candidates %d/%d: %+v", low, high, candidates)
						}
						if e.G.Obj(low).Face().ManaValue() > 3 || e.G.Obj(high).Face().ManaValue() <= 3 {
							t.Fatalf("precondition: Cruel Alliance characteristic values must straddle 3: %d/%d", e.G.Obj(low).Face().ManaValue(), e.G.Obj(high).Face().ManaValue())
						}
						rootCandidates := e.legalTargetCandidates(0, spell, spell, root)
						if !hasTargetCandidate(rootCandidates, low) || hasTargetCandidate(rootCandidates, high) {
							t.Fatalf("Cruel Alliance root must offer only cmc<=3 branch candidates: %+v", rootCandidates)
						}
					}
					if tc.card == "Too Evil to Stay Dead" {
						candidates := e.legalTargetCandidates(0, spell, spell, root.Sub)
						if !hasTargetCandidate(candidates, low) || !hasTargetCandidate(candidates, high) {
							t.Fatalf("graveyard Teamwork target set omitted candidates %d/%d: %+v", low, high, candidates)
						}
						if e.G.Obj(low).Face().ManaValue() > 4 || e.G.Obj(high).Face().ManaValue() <= 4 {
							t.Fatalf("precondition: Too Evil characteristic values must straddle 4: %d/%d", e.G.Obj(low).Face().ManaValue(), e.G.Obj(high).Face().ManaValue())
						}
						rootCandidates := e.legalTargetCandidates(0, spell, spell, root)
						if !hasTargetCandidate(rootCandidates, low) || hasTargetCandidate(rootCandidates, high) {
							t.Fatalf("Too Evil root must offer only cmc<=4 branch candidates: %+v", rootCandidates)
						}
					}
				}
				if tc.card == "Heroic Teamwork" {
					min, max := e.resolvedTargetBounds(0, spell, root, 0)
					candidates := e.legalTargetCandidates(0, spell, spell, root)
					if min != 1 || max != 2 || len(candidates) < 2 {
						t.Fatalf("announcement target offer bounds/candidates=(%d,%d)/%d, want 1..2 and >=2: %+v", min, max, len(candidates), candidates)
					}
					if e.G.Obj(candidates[0].obj).Zone != state.ZBattlefield || e.G.Obj(candidates[1].obj).Zone != state.ZBattlefield {
						t.Fatal("precondition: Heroic Teamwork target candidates must be on battlefield")
					}
				}
				if tc.card == "Atlantis Attacks" {
					bounce := face.SVars["DBBounce"]
					if root.ParamStr(cards.PKChoices) == "" || !strings.Contains(root.ParamStr(cards.PKCharmNum), "X") ||
						!strings.Contains(bounce, "TargetMin$ 1") || !strings.Contains(bounce, "TargetMax$ 2") {
						t.Fatalf("precondition: expected conditional Charm with 1-2 target bounce mode: choices=%q bounce=%q", root.ParamStr(cards.PKChoices), bounce)
					}
					bounceSA := face.SVars["DBBounce"]
					if !strings.Contains(bounceSA, "Permanent.nonLand") {
						t.Fatalf("precondition: expected bounce mode's nonland target restriction: %q", bounceSA)
					}
					// Charm's Teamwork announcement offers both modes; the bounce
					// mode's own target census must expose the seeded nonland
					// permanents and its 1..2 declaration range.
					if !e.charmTargetsAvailable(0, spell, root, false) {
						t.Fatal("Atlantis Attacks Charm offered no legal mode despite battlefield candidates")
					}
					modes := copyCharmModes(face, root, []string{"DBCreate", "DBBounce"})
					var bounceMode *cards.SA
					for _, mode := range modes {
						if mode != nil && mode.API == "ChangeZone" {
							bounceMode = mode
						}
					}
					if bounceMode == nil {
						t.Fatal("precondition: Atlantis Attacks bounce Charm mode did not compile")
					}
					min, max := e.resolvedTargetBounds(0, spell, bounceMode, 0)
					candidates := e.legalTargetCandidates(0, spell, spell, bounceMode)
					if min != 1 || max != 2 || len(candidates) < 2 {
						t.Fatalf("Atlantis bounce announcement=(%d,%d)/%d; want 1..2 and >=2 candidates: %+v", min, max, len(candidates), candidates)
					}
				}
				if paid {
					var choices []int
					for _, o := range d.Options {
						if o.Kind == "teamwork" && len(choices) < 2 {
							choices = append(choices, o.Index)
						}
					}
					if len(choices) != 2 {
						t.Fatalf("precondition: two eligible Teamwork creatures required: %+v", d.Options)
					}
					submitChoices(t, e, choices...)
				} else {
					submitChoices(t, e)
				}
				finishTeamworkAnnouncement(t, e)
				o := e.G.Obj(spell)
				if o == nil {
					t.Fatal("cast object disappeared")
				}
				if o.Zone != state.ZStack || o.TeamworkPaid != paid {
					t.Fatalf("cast outcome zone/paid=%v/%v, want stack/%v", o.Zone, o.TeamworkPaid, paid)
				}
			})
		}
	}
}

func teamworkTargetEngine(t *testing.T, hero string) (*Engine, *cards.Registry) {
	t.Helper()
	reg := searchTestRegistry(t)
	deck := []*cards.Card{searchCorpusCard(t, reg, hero)}
	for i := 0; i < 8; i++ {
		deck = append(deck, searchCorpusCard(t, reg, "Grizzly Bears"))
	}
	for i := 0; i < 4; i++ {
		deck = append(deck, searchCorpusCard(t, reg, "Goblin Piker"))
	}
	for i := 0; i < 4; i++ {
		deck = append(deck, searchCorpusCard(t, reg, "Savannah Lions"))
	}
	deck = append(deck, searchCorpusCard(t, reg, "Hill Giant"), searchCorpusCard(t, reg, "Air Elemental"))
	for len(deck) < 40 {
		deck = append(deck, searchCorpusCard(t, reg, "Mountain"))
	}
	opponent := make([]*cards.Card, 40)
	for i := range opponent {
		opponent[i] = searchCorpusCard(t, reg, "Mountain")
	}
	cfg := seatZeroStart(Config{Seed: 44207, Names: []string{"teamwork", "opp"},
		Decks: [][]*cards.Card{deck, opponent}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, reg
}

func TestTeamworkTargetAnnouncementEligibilityIntentOnlyOption(t *testing.T) {
	t.Parallel()
	e, reg := teamworkTargetEngine(t, "Cruel Alliance")
	spell := searchMoveByName(t, e, "Cruel Alliance", state.ZHand)
	high := seedBattlefield(t, e, reg, "Hill Giant")
	if e.G.Obj(spell) == nil || e.G.Obj(spell).Zone != state.ZHand || e.G.Obj(high).Zone != state.ZBattlefield {
		t.Fatalf("precondition: spell must be in hand and only target candidate on battlefield: spell=%+v target=%+v", e.G.Obj(spell), e.G.Obj(high))
	}
	if e.G.Obj(high).Face().ManaValue() <= 3 {
		t.Fatalf("precondition: intent-only target must exceed Cruel Alliance base limit 3, got %d", e.G.Obj(high).Face().ManaValue())
	}
	root := e.G.Obj(spell).Face().SpellAbility()
	if root == nil || e.castTargetsAvailable(0, spell, root) {
		t.Fatal("precondition: ordinary cast must be infeasible with only a mana-value-4 root candidate")
	}
	if !teamworkTargetsAvailable(e, 0, spell, root) {
		t.Fatal("precondition: Teamwork-intent declaration must be feasible via its broader linked target")
	}
	addMana(t, e, 0, "BBB")
	opt := castOptMode(t, e.Pending().Options, spell, "teamworked")
	if opt.Mode != "teamworked" {
		t.Fatalf("Teamwork-intent cast option has mode %q", opt.Mode)
	}
	// The ordinary mode remains unavailable, but the intent-selected mode is
	// offered, then can still be declined at its independent payment decision.
	for _, candidate := range e.Pending().Options {
		if candidate.Kind == "cast" && candidate.Obj == spell && candidate.Mode == "" {
			t.Fatal("ordinary cast unexpectedly offered despite no legal base-branch target")
		}
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || !d.AllowNone || len(d.Options) != 1 || d.Options[0].Obj != high {
		t.Fatalf("precondition: expected later optional Teamwork ask for the qualifying creature, got %+v", d)
	}
	submitChoices(t, e) // CR 702.194c's later payment ask may still be declined.
	finishTeamworkAnnouncement(t, e)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || o.TeamworkPaid {
		t.Fatalf("declined intent cast outcome=%+v, want stack spell without paid provenance", o)
	}
}

func hasTargetCandidate(candidates []targetCandidate, id state.ObjID) bool {
	for _, c := range candidates {
		if c.obj == id {
			return true
		}
	}
	return false
}
