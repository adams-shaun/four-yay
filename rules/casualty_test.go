package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func casualtyEngine(t *testing.T, hero string) (*Engine, Config, *cards.Registry) {
	t.Helper()
	reg := searchTestRegistry(t)
	deck := []*cards.Card{searchCorpusCard(t, reg, hero), searchCorpusCard(t, reg, "Ashad, the Lone Cyberman"), searchCorpusCard(t, reg, "Sol Ring"), searchCorpusCard(t, reg, "Howling Mine")}
	deck = append(deck, searchCorpusCard(t, reg, "Llanowar Elves"))
	for i := 0; i < 5; i++ {
		deck = append(deck, searchCorpusCard(t, reg, "Grizzly Bears"))
	}
	for len(deck) < 40 {
		deck = append(deck, searchCorpusCard(t, reg, "Mountain"))
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = searchCorpusCard(t, reg, "Siege Mastodon")
	}
	cfg := seatZeroStart(Config{Seed: 44761, Names: []string{"caster", "opponent"}, Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg, reg
}

func casualtyChoice(t *testing.T, e *Engine, eligible, ineligible state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "casualty" {
		t.Fatalf("expected casualty creature election, got %+v", d)
	}
	found := false
	for _, o := range d.Options {
		if o.Obj == ineligible {
			t.Fatalf("ineligible power creature offered: %+v", o)
		}
		if o.Obj == eligible {
			found = true
			submitChoices(t, e, o.Index)
			break
		}
	}
	if !found {
		t.Fatalf("power-qualified creature %d not offered: %+v", eligible, d.Options)
	}
}

func TestCasualtyPrintedSpellSacrificeCopiesTarget(t *testing.T) {
	t.Parallel()
	for _, pay := range []bool{false, true} {
		name := "decline"
		if pay {
			name = "pay"
		}
		t.Run(name, func(t *testing.T) {
			e, cfg, reg := casualtyEngine(t, "Light 'Em Up")
			low := seedBattlefield(t, e, reg, "Llanowar Elves")
			high := seedBattlefield(t, e, reg, "Grizzly Bears")
			target := moveSeededCard(t, e, 1, searchCorpusCard(t, reg, "Siege Mastodon"), state.ZBattlefield)
			if e.G.Obj(high).Zone != state.ZBattlefield || e.G.Obj(low).Zone != state.ZBattlefield || e.Power(high) != 2 || e.Power(low) != 1 || e.Power(target) == e.Power(high) {
				t.Fatalf("invalid casualty setup: zones/power high=%+v target=%+v", e.G.Obj(high), e.G.Obj(target))
			}
			spell := searchMoveByName(t, e, "Light 'Em Up", state.ZHand)
			if e.G.Obj(spell).Zone != state.ZHand {
				t.Fatal("spell must be announced from hand")
			}
			addMana(t, e, 0, "RR")
			mode := ""
			if pay {
				mode = "casualty"
			}
			opt := castOptMode(t, e.Pending().Options, spell, mode)
			submitChoices(t, e, opt.Index)
			if pay {
				d := e.Pending()
				if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "casualty" {
					t.Fatalf("missing casualty ask: %+v", d)
				}
				found := false
				for _, o := range d.Options {
					if o.Obj == target || o.Obj == low {
						t.Fatalf("ineligible creature offered for casualty: %+v", o)
					}
					if o.Obj == high {
						found = true
					}
				}
				for _, o := range d.Options {
					if o.Obj == high {
						submitChoices(t, e, o.Index)
						break
					}
				}
				if !found {
					t.Fatal("power-2 sacrifice not offered")
				}
			}
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("missing spell target: %+v", d)
			}
			found := false
			for _, o := range d.Options {
				if o.Obj == target {
					submitChoices(t, e, o.Index)
					found = true
					break
				}
			}
			if !found {
				t.Fatal("mastodon not targetable")
			}
			passUntilStackEmpty(t, e, 60)
			want := int32(2)
			copies := 0
			if pay {
				want = 4
				copies = 1
				if e.G.Obj(high).Zone != state.ZGraveyard {
					t.Fatal("casualty cost was not sacrificed")
				}
			}
			if !pay && e.G.Obj(high).Zone != state.ZBattlefield {
				t.Fatal("declined casualty sacrificed a creature")
			}
			if got := e.G.Obj(target).Damage; got != want {
				t.Fatalf("damage on original target = %d, want %d", got, want)
			}
			if copies != countStackCopies(e, spell) {
				t.Fatalf("copies = %d, want %d", countStackCopies(e, spell), copies)
			}
			if e.G.Obj(low).Zone != state.ZBattlefield {
				t.Fatal("unselected creature was sacrificed")
			}
			replayCheck(t, e, cfg)
		})
	}
}

func TestCasualtyAshadGrantOnlyFirstArtifactPerTurn(t *testing.T) {
	t.Parallel()
	e, cfg, reg := casualtyEngine(t, "Sol Ring")
	ashad := seedBattlefield(t, e, reg, "Ashad, the Lone Cyberman")
	sacrifice := seedBattlefield(t, e, reg, "Grizzly Bears")
	low := seedBattlefield(t, e, reg, "Llanowar Elves")
	if e.G.Obj(ashad).Zone != state.ZBattlefield || e.G.Obj(sacrifice).Zone != state.ZBattlefield || e.G.Obj(low).Zone != state.ZBattlefield || e.Power(low) != 1 || e.Power(sacrifice) != 2 {
		t.Fatal("Ashad or power-2 sacrifice is not on the battlefield")
	}
	first := searchMoveByName(t, e, "Sol Ring", state.ZHand)
	second := searchMoveByName(t, e, "Howling Mine", state.ZHand)
	if e.G.Obj(first).Zone != state.ZHand || e.G.Obj(second).Zone != state.ZHand || first == second {
		t.Fatal("artifact spell cast-path precondition failed")
	}
	addMana(t, e, 0, "RRRR")
	if n := len(e.spellsCastThisTurnMatching(0, "Artifact.nonLegendary+YouCtrl", 0)); n != 0 {
		t.Fatalf("Ashad EQ0 precondition: already cast %d artifacts", n)
	}
	opt := castOptMode(t, e.Pending().Options, first, "casualty")
	submitChoices(t, e, opt.Index)
	casualtyChoice(t, e, sacrifice, low)
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		choices := make([]int, len(d.Options))
		for i := range choices {
			choices[i] = d.Options[i].Index
		}
		submitChoices(t, e, choices...)
	}
	passUntilStackEmpty(t, e, 60)
	if e.G.Obj(sacrifice).Zone != state.ZGraveyard {
		t.Fatal("Ashad grant did not sacrifice the chosen bear")
	}
	if got := countStackCopies(e, first); got != 1 {
		t.Fatalf("Ashad grant made %d copies, want one", got)
	}
	tokens := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Sol Ring" {
			tokens++
		}
	}
	if tokens != 1 {
		t.Fatalf("artifact spell copy made %d Sol Ring tokens, want 1", tokens)
	}
	addMana(t, e, 0, "RR")
	if n := len(e.spellsCastThisTurnMatching(0, "Artifact.nonLegendary+YouCtrl", 0)); n != 1 {
		t.Fatalf("Ashad EQ0 comparison did not change: cast count = %d, want 1", n)
	}
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == second && o.Mode == "casualty" {
			t.Fatal("subsequent artifact offered casualty")
		}
	}
	opt = castOptMode(t, e.Pending().Options, second, "")
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 60)
	if got := countStackCopies(e, second); got != 0 {
		t.Fatalf("second artifact copied %d times", got)
	}
	if e.G.Obj(ashad).Zone != state.ZBattlefield {
		t.Fatal("Ashad was sacrificed instead of the bear")
	}
	replayCheck(t, e, cfg)
}
