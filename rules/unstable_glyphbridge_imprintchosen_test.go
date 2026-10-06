package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestUnstableGlyphbridgeImprintChosen(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	glyph := lookup(t, reg, "Unstable Glyphbridge")
	t.Run("empty seat zero and one bear", func(t *testing.T) {
		e, _ := corpusEngineCfg(t, reg,
			[]*cards.Card{glyph},
			[]*cards.Card{card(t, "Name:Glyphbridge Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")})
		bear := moveByName(t, e, 1, "Glyphbridge Bear", state.ZBattlefield)
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || e.Power(bear) != 2 {
			t.Fatalf("precondition Bear = %+v power %d, want battlefield 2/2", o, e.Power(bear))
		}
		sourceID := castUnstableGlyphbridge(t, e)
		d := passUntilAskKind(t, e, decision.KChoose, 200)
		assertGlyphbridgeAsk(t, d, e, []state.ObjID{bear})
		submitCardChoice(t, e, d, bear)
		passUntilStackEmpty(t, e, 200)
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("selected seat-1 Bear ended in %+v, want battlefield", o)
		}
		if src := e.G.Obj(sourceID); src == nil || len(src.SeekFound) != 0 || len(src.Imprinted) != 0 {
			t.Fatalf("Glyphbridge cleanup left imprint association: %+v", src)
		}
	})

	t.Run("each player's earlier pick survives", func(t *testing.T) {
		creature := func(name, pt string) *cards.Card {
			return card(t, "Name:"+name+"\nTypes:Creature\nPT:"+pt+"\nOracle:x\n")
		}
		e, _ := corpusEngineCfg(t, reg,
			[]*cards.Card{glyph, creature("Gly Zero A", "2/2"), creature("Gly Zero B", "1/1"), creature("Gly Zero Giant", "3/3")},
			[]*cards.Card{creature("Gly One A", "2/2"), creature("Gly One B", "1/1"), creature("Gly One Giant", "3/3")})
		ids := [2][3]state.ObjID{}
		for p, names := range [2][3]string{{"Gly Zero A", "Gly Zero B", "Gly Zero Giant"}, {"Gly One A", "Gly One B", "Gly One Giant"}} {
			for i, name := range names {
				ids[p][i] = moveByName(t, e, state.PlayerID(p), name, state.ZBattlefield)
				o := e.G.Obj(ids[p][i])
				if o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition %s not on battlefield: %+v", name, o)
				}
			}
			if e.Power(ids[p][0]) != 2 || e.Power(ids[p][2]) != 3 {
				t.Fatalf("precondition powers seat %d = %d/%d, want qualifying 2 and excluded 3", p, e.Power(ids[p][0]), e.Power(ids[p][2]))
			}
		}
		castUnstableGlyphbridge(t, e)
		for p := range ids {
			d := passUntilAskKind(t, e, decision.KChoose, 200)
			want := []state.ObjID{ids[p][0], ids[p][1]}
			assertGlyphbridgeAsk(t, d, e, want)
			// The trigger's controller (seat zero) chooses for each player;
			// choose the second option on the first iteration to prove the
			// later iteration accumulates rather than replaces it.
			pick := ids[p][1]
			if p == 1 {
				pick = ids[p][0]
			}
			submitCardChoice(t, e, d, pick)
		}
		passUntilStackEmpty(t, e, 200)
		for p := range ids {
			kept := ids[p][1]
			if p == 1 {
				kept = ids[p][0]
			}
			for i, id := range ids[p] {
				want := state.ZGraveyard
				if id == kept {
					want = state.ZBattlefield
				}
				if got := e.G.Obj(id).Zone; got != want {
					t.Errorf("seat %d creature %d zone=%s, want %s", p, i, got, want)
				}
			}
		}
	})
}

func castUnstableGlyphbridge(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	id := moveByName(t, e, 0, "Unstable Glyphbridge", state.ZHand)
	addMana(t, e, 0, "WWWWW")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("before cast pending=%+v, want priority", d)
	}
	index := -1
	for _, option := range d.Options {
		if option.Kind == "cast" && option.Obj == id {
			index = option.Index
		}
	}
	if index < 0 {
		t.Fatalf("Glyphbridge cast option absent: %+v", d.Options)
	}
	submitChoices(t, e, index)
	for i := 0; i < 200; i++ {
		d = e.Pending()
		if d != nil && d.Kind == decision.KChoose {
			return id
		}
		if d == nil {
			t.Fatalf("cast resolved without the Glyphbridge trigger choice")
		}
		pass := -1
		for _, option := range d.Options {
			if option.Kind == "pass" {
				pass = option.Index
			}
		}
		if pass < 0 {
			t.Fatalf("unexpected decision while casting: %+v", d)
		}
		submitChoices(t, e, pass)
	}
	t.Fatal("no Glyphbridge choice ask after cast")
	return 0
}

func assertGlyphbridgeAsk(t *testing.T, d *decision.Decision, e *Engine, want []state.ObjID) {
	t.Helper()
	if d.ResumeKind != "choice" || d.Player != 0 || d.Source == 0 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("Glyphbridge ask=%+v, want controller's mandatory single choice", d)
	}
	if len(d.Options) != len(want) {
		t.Fatalf("offered options=%+v want IDs=%v", d.Options, want)
	}
	for i, id := range want {
		if d.Options[i].Obj != id {
			t.Fatalf("option %d=%d, want %d", i, d.Options[i].Obj, id)
		}
		if e.G.Obj(id).Zone != state.ZBattlefield || e.Power(id) > 2 {
			t.Fatalf("offered object %d zone/power=%s/%d invalid", id, e.G.Obj(id).Zone, e.Power(id))
		}
	}
}
