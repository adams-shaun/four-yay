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
				if tc.card == "Cruel Alliance" {
					ask := e.Pending()
					if ask == nil || ask.Kind != decision.KTarget || ask.ResumeKind != "cast_sub" || ask.Min != 1 || ask.Max != 1 ||
						!decisionHasObj(ask, low) || !decisionHasObj(ask, high) {
						t.Fatalf("Teamwork linked battlefield target announcement (payment accepted=%v) = %+v; want both candidates %d/%d", paid, ask, low, high)
					}
					submitChoices(t, e, decisionObjIndex(t, ask, high))
					if o := e.G.Obj(spell); o == nil || len(o.SubTargets) == 0 || o.SubTargets[0].Obj != high {
						t.Fatalf("linked target declaration not recorded on spell: %+v", o)
					}
				}
				if tc.card == "Heroic Teamwork" || tc.card == "Too Evil to Stay Dead" {
					ask := e.Pending()
					wantMax := 1
					if tc.card == "Heroic Teamwork" {
						wantMax = 2
					}
					if ask == nil || ask.Kind != decision.KTarget || ask.Min != 1 || ask.Max != wantMax ||
						!decisionHasObj(ask, low) || !decisionHasObj(ask, high) {
						t.Fatalf("Teamwork-intent target announcement for %s (payment accepted=%v) = %+v; want %d..%d with both candidates %d/%d", tc.card, paid, ask, 1, wantMax, low, high)
					}
				}
				if tc.card == "Atlantis Attacks" {
					modes := e.Pending()
					if modes == nil || modes.Kind != decision.KModes || modes.ResumeKind != "cast_modes" || modes.Min != 2 || modes.Max != 2 || len(modes.Options) != 2 {
						t.Fatalf("Teamwork-intent Charm announcement (payment accepted=%v) = %+v, want both modes required", paid, modes)
					}
					// DBCreate targets a player and DBBounce targets 1..2
					// nonland permanents; both declarations belong to this cast.
					submitChoices(t, e, modes.Options[0].Index, modes.Options[1].Index)
					seenBounce := false
					for i := 0; i < 3; i++ {
						ask := e.Pending()
						if ask == nil || ask.Kind != decision.KTarget {
							break
						}
						if ask.Min == 1 && ask.Max == 2 {
							seenBounce = true
							if !decisionHasObj(ask, low) || !decisionHasObj(ask, high) || e.G.Obj(low).Zone != state.ZBattlefield || e.G.Obj(high).Zone != state.ZBattlefield {
								t.Fatalf("bounce target ask missing battlefield candidates %d/%d: %+v", low, high, ask)
							}
						}
						submitChoices(t, e, ask.Options[0].Index)
					}
					if !seenBounce {
						t.Fatal("selected DBBounce never announced its 1..2 targets")
					}
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
	// Control cast: without Teamwork intent, the same corpus Charm offers
	// one mode, not the two required above (even on declined payment).
	t.Run("Atlantis Attacks/ordinary", ordinaryTeamworkCharm)
	t.Run("Atlantis Attacks/no bounce targets", teamworkCharmNoBounce)
}

// An ordinary Atlantis Attacks cast chooses one mode, unlike either
// Teamwork-intent cast (even one whose later tap payment is declined).
func ordinaryTeamworkCharm(t *testing.T) {
	e, reg := teamworkTargetEngine(t, "Atlantis Attacks")
	spell := searchMoveByName(t, e, "Atlantis Attacks", state.ZHand)
	low := seedBattlefield(t, e, reg, "Savannah Lions")
	high := seedBattlefield(t, e, reg, "Hill Giant")
	if e.G.Obj(spell).Zone != state.ZHand || e.G.Obj(low).Zone != state.ZBattlefield || e.G.Obj(high).Zone != state.ZBattlefield ||
		e.G.Obj(low).Face().ManaValue() == e.G.Obj(high).Face().ManaValue() {
		t.Fatal("precondition: spell in hand and distinct nonland candidates on battlefield")
	}
	addMana(t, e, 0, "UUUUUUU")
	opt := castOptMode(t, e.Pending().Options, spell, "")
	submitChoices(t, e, opt.Index)
	modes := e.Pending()
	if modes == nil || modes.Kind != decision.KModes || modes.ResumeKind != "cast_modes" || modes.Min != 1 || modes.Max != 1 || len(modes.Options) != 2 {
		t.Fatalf("ordinary Atlantis mode announcement = %+v, want exactly one of two modes", modes)
	}
	// The second corpus mode is DBBounce; no DBCreate player-target
	// declaration may appear when only DBBounce was selected.
	if modes.Options[1].Label == modes.Options[0].Label {
		t.Fatalf("precondition: expected distinct corpus modes: %+v", modes.Options)
	}
	submitChoices(t, e, modes.Options[1].Index)
	ask := e.Pending()
	if ask == nil || ask.Kind != decision.KTarget || ask.Min != 1 || ask.Max != 2 || !decisionHasObj(ask, low) || !decisionHasObj(ask, high) {
		t.Fatalf("ordinary bounce target ask = %+v, want 1..2 battlefield candidates", ask)
	}
	submitChoices(t, e, ask.Options[0].Index)
	finishTeamworkAnnouncement(t, e)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || o.TeamworkPaid {
		t.Fatalf("ordinary Atlantis stack result = %+v", o)
	}
}

// Both shrouded creatures can pay Teamwork 4, but neither can be a
// DBBounce target. Choosing both modes is therefore not a legal proposal.
func teamworkCharmNoBounce(t *testing.T) {
	e, reg := teamworkTargetEngine(t, "Atlantis Attacks")
	spell := searchMoveByName(t, e, "Atlantis Attacks", state.ZHand)
	first := seedBattlefield(t, e, reg, "Kalonian Behemoth")
	second := seedBattlefield(t, e, reg, "Kalonian Behemoth")
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand || e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield ||
		e.Power(first)+e.Power(second) < 4 || !e.G.Obj(first).Face().HasKeyword("Shroud") {
		t.Fatalf("precondition: hand spell and two shrouded Teamwork-eligible creatures: spell=%+v first=%+v second=%+v", o, e.G.Obj(first), e.G.Obj(second))
	}
	root := e.G.Obj(spell).Face().SpellAbility()
	bounce := copyCharmModes(e.G.Obj(spell).Face(), root, []string{"DBBounce"})
	if len(bounce) != 1 || bounce[0] == nil || len(e.legalTargetCandidates(0, spell, spell, bounce[0])) != 0 {
		t.Fatal("precondition: shrouded battlefield has no legal bounce targets")
	}
	addMana(t, e, 0, "UUUUUUU")
	ordinary := false
	for _, opt := range e.Pending().Options {
		if opt.Kind != "cast" || opt.Obj != spell {
			continue
		}
		if opt.Mode == "teamworked" {
			t.Fatal("Teamwork-intent cast offered with only one of two required Charm modes targetable")
		}
		if opt.Mode == "" {
			ordinary = true
		}
	}
	if !ordinary {
		t.Fatal("ordinary choose-one cast should still offer the player-target mode")
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
	deck = append(deck, searchCorpusCard(t, reg, "Hill Giant"), searchCorpusCard(t, reg, "Air Elemental"),
		searchCorpusCard(t, reg, "Kalonian Behemoth"), searchCorpusCard(t, reg, "Kalonian Behemoth"))
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
	ask := e.Pending()
	if ask == nil || ask.Kind != decision.KTarget || ask.ResumeKind != "cast_sub" || ask.Min != 1 || ask.Max != 1 || !decisionHasObj(ask, high) {
		t.Fatalf("intent-only linked target ask = %+v, want high-value battlefield creature %d", ask, high)
	}
	submitChoices(t, e, decisionObjIndex(t, ask, high))
	if o := e.G.Obj(spell); o == nil || len(o.SubTargets) != 1 || o.SubTargets[0].Obj != high {
		t.Fatalf("intent-only target not declared on stack spell: %+v", o)
	}
	finishTeamworkAnnouncement(t, e)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack || o.TeamworkPaid {
		t.Fatalf("declined intent cast outcome=%+v, want stack spell without paid provenance", o)
	}
}

func decisionObjIndex(t *testing.T, d *decision.Decision, id state.ObjID) int {
	t.Helper()
	for _, opt := range d.Options {
		if opt.Obj == id {
			return opt.Index
		}
	}
	t.Fatalf("target %d absent from %+v", id, d)
	return -1
}

func decisionHasObj(d *decision.Decision, id state.ObjID) bool {
	for _, opt := range d.Options {
		if opt.Obj == id {
			return true
		}
	}
	return false
}

func hasTargetCandidate(candidates []targetCandidate, id state.ObjID) bool {
	for _, c := range candidates {
		if c.obj == id {
			return true
		}
	}
	return false
}
