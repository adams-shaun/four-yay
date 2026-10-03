package rules

// api:Empower (Reality Fracture's Jace mechanic; 35 corpus carriers, every
// one `Empower | Type$ Jace | Num$ <n|X|SVar>`). The reminder text every
// carrier prints: "Put N loyalty counters on a Jace token you control. If you
// don't control one, first create a blue Jace planeswalker token with
// "[-1]: Surveil 1" and "[-3]: Draw a card."" These tests drive the REAL
// corpus carriers through the cast/activate path; nothing embeds a script.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// jaceTokens returns seat p's battlefield tokens whose DERIVED types carry
// the Jace planeswalker type, in battlefield order.
func jaceTokens(e *Engine, p state.PlayerID) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || !o.IsToken {
			continue
		}
		if slices.Contains(e.Derived(id).Types, "Jace") {
			out = append(out, id)
		}
	}
	return out
}

// castAmphisbaena casts one Arcane Amphisbaena from seat 0's hand and drains
// the stack (the creature spell, then its "When this creature enters,
// empower Jace 2" trigger).
func castAmphisbaena(t *testing.T, e *Engine) {
	t.Helper()
	moveByName(t, e, 0, "Arcane Amphisbaena", state.ZHand)
	addMana(t, e, 0, "GG")
	castNamed(t, e, "Arcane Amphisbaena")
	passUntilStackEmpty(t, e, 40)
}

// TestEmpowerCreatesBlueJaceTokenThenGrowsIt: with no Jace token, empower
// first creates a blue Jace planeswalker token (CR 111.4 names it "Jace
// Token") and then puts the loyalty counters on it; a second empower puts its
// counters on that SAME token instead of creating another.
func TestEmpowerCreatesBlueJaceTokenThenGrowsIt(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	amph := lookup(t, reg, "Arcane Amphisbaena")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{amph, amph}, nil)
	if got := jaceTokens(e, 0); len(got) != 0 {
		t.Fatalf("precondition: seat 0 already controls Jace tokens %v", got)
	}

	castAmphisbaena(t, e)
	toks := jaceTokens(e, 0)
	if len(toks) != 1 {
		t.Fatalf("after one empower Jace 2 seat 0 controls %d Jace tokens, want 1", len(toks))
	}
	tok := toks[0]
	o := e.G.Obj(tok)
	d := e.Derived(tok)
	if !slices.Contains(d.Types, "Planeswalker") {
		t.Fatalf("Jace token types = %v, want a Planeswalker", d.Types)
	}
	if d.Name != "Jace Token" {
		t.Fatalf("Jace token name = %q, want %q (CR 111.4)", d.Name, "Jace Token")
	}
	if c := e.ObjectColors(o); c != "U" {
		t.Fatalf("Jace token colours = %q, want blue", c)
	}
	if got := o.Counter("LOYALTY"); got != 2 {
		t.Fatalf("Jace token loyalty = %d, want 2 (empower Jace 2)", got)
	}
	// The token's own loyalty abilities ([-1]: Surveil 1, [-3]: Draw a card)
	// are real: with two loyalty the [-1] is offered and the [-3] is not.
	if _, ok := findAbilityOption(e, tok, 0); !ok {
		t.Fatalf("the Jace token's [-1] loyalty ability is not offered: %+v", e.Pending())
	}
	if _, ok := findAbilityOption(e, tok, 1); ok {
		t.Fatal("the Jace token's [-3] loyalty ability is offered with only two loyalty")
	}

	castAmphisbaena(t, e)
	toks = jaceTokens(e, 0)
	if len(toks) != 1 || toks[0] != tok {
		t.Fatalf("second empower: Jace tokens = %v, want only the original %d", toks, tok)
	}
	if got := e.G.Obj(tok).Counter("LOYALTY"); got != 4 {
		t.Fatalf("Jace token loyalty after a second empower Jace 2 = %d, want 4", got)
	}
	replayCheck(t, e, cfg)
}

// TestEmpowerIgnoresNonTokenJace: Jace, Reality Sculptor is a Jace but not a
// token, so its own [+1] ("Empower Jace X, where X is the number of Islands
// you control") creates a token rather than growing itself, and X reads the
// Islands at resolution.
func TestEmpowerIgnoresNonTokenJace(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	jace := lookup(t, reg, "Jace, Reality Sculptor")
	island := lookup(t, reg, "Island")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{jace, island, island, island}, nil)
	for range 3 {
		moveByName(t, e, 0, "Island", state.ZBattlefield)
	}
	sculptor := moveByName(t, e, 0, "Jace, Reality Sculptor", state.ZBattlefield)
	if got := e.G.Obj(sculptor).Counter("LOYALTY"); got != 5 {
		t.Fatalf("precondition: Jace, Reality Sculptor entered with %d loyalty, want 5", got)
	}
	e.priorityRound()
	opt, ok := findAbilityOption(e, sculptor, 0)
	if !ok {
		t.Fatalf("Jace, Reality Sculptor's [+1] is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Obj(sculptor).Counter("LOYALTY"); got != 6 {
		t.Fatalf("Jace, Reality Sculptor loyalty = %d, want 6 (only its +1 cost; it is not a token)", got)
	}
	toks := jaceTokens(e, 0)
	if len(toks) != 1 {
		t.Fatalf("seat 0 controls %d Jace tokens, want 1", len(toks))
	}
	if got := e.G.Obj(toks[0]).Counter("LOYALTY"); got != 3 {
		t.Fatalf("Jace token loyalty = %d, want 3 (X = three Islands)", got)
	}
	replayCheck(t, e, cfg)
}

// TestEmpowerChoosesAmongSeveralJaceTokens: under Anointed Procession the
// create step mints TWO Jace tokens, so "a Jace token you control" is a real
// choice; the counters land only on the chosen one and the other, at zero
// loyalty, is put into the graveyard by state-based actions (CR 704.5i).
func TestEmpowerChoosesAmongSeveralJaceTokens(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	amph := lookup(t, reg, "Arcane Amphisbaena")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{amph, lookup(t, reg, "Anointed Procession")}, nil)
	moveByName(t, e, 0, "Anointed Procession", state.ZBattlefield)
	moveByName(t, e, 0, "Arcane Amphisbaena", state.ZHand)
	e.priorityRound()
	addMana(t, e, 0, "GG")
	castNamed(t, e, "Arcane Amphisbaena")

	var pick *decision.Decision
	for range 40 {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving the empower")
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "counter_pick" {
			pick = d
			break
		}
		if d.Kind != decision.KPriority || len(e.G.Stack) == 0 {
			t.Fatalf("unexpected decision before the Jace pick: %+v", d)
		}
		submitChoices(t, e, passIndex(t, d))
	}
	if pick == nil {
		t.Fatal("empower never asked which Jace token takes the counters")
	}
	if len(pick.Options) != 2 {
		t.Fatalf("Jace pick offers %d tokens, want the 2 Anointed Procession minted", len(pick.Options))
	}
	chosen := pick.Options[1].Obj
	other := pick.Options[0].Obj
	submitChoices(t, e, pick.Options[1].Index)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Obj(chosen).Counter("LOYALTY"); got != 2 {
		t.Fatalf("chosen Jace token loyalty = %d, want 2", got)
	}
	if o := e.G.Obj(other); o != nil && o.Zone == state.ZBattlefield {
		t.Fatalf("the unchosen zero-loyalty Jace token is still on the battlefield (loyalty %d)", o.Counter("LOYALTY"))
	}
	if toks := jaceTokens(e, 0); len(toks) != 1 || toks[0] != chosen {
		t.Fatalf("Jace tokens after SBAs = %v, want only the chosen %d", toks, chosen)
	}
	replayCheck(t, e, cfg)
}

// TestEmpowerVariableAmounts drives the two SVar-valued Num$ shapes: Violent
// Echoes' "If excess damage was dealt to that permanent this way, empower
// Jace X, where X is that excess damage" (Num$ Excess, gated by
// ConditionCheckSVar$ Excess) and Overwrite the Multiverse's "Exile all
// creatures. Empower Jace X, where X is the number of creatures exiled this
// way" (Num$ X = Remembered$Amount).
func TestEmpowerVariableAmounts(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bears := lookup(t, reg, "Grizzly Bears")
	t.Run("ViolentEchoesExcess", func(t *testing.T) {
		t.Parallel()
		e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Violent Echoes")}, []*cards.Card{bears})
		bear := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
		e.priorityRound()
		castCorpusSpell(t, e, "Violent Echoes", "RRRR", bear)
		if o := e.G.Obj(bear); o != nil && o.Zone == state.ZBattlefield {
			t.Fatal("precondition: Grizzly Bears survived 6 damage")
		}
		toks := jaceTokens(e, 0)
		if len(toks) != 1 {
			t.Fatalf("Violent Echoes on a 2-toughness creature: %d Jace tokens, want 1", len(toks))
		}
		if got := e.G.Obj(toks[0]).Counter("LOYALTY"); got != 4 {
			t.Fatalf("Jace token loyalty = %d, want 4 (6 damage, 4 excess)", got)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("ViolentEchoesNoExcess", func(t *testing.T) {
		t.Parallel()
		e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Violent Echoes")},
			[]*cards.Card{lookup(t, reg, "Colossal Dreadmaw")})
		big := moveByName(t, e, 1, "Colossal Dreadmaw", state.ZBattlefield)
		e.priorityRound()
		castCorpusSpell(t, e, "Violent Echoes", "RRRR", big)
		if toks := jaceTokens(e, 0); len(toks) != 0 {
			t.Fatalf("Violent Echoes with no excess damage empowered: Jace tokens %v", toks)
		}
		replayCheck(t, e, cfg)
	})
	t.Run("OverwriteTheMultiverse", func(t *testing.T) {
		t.Parallel()
		e, cfg := corpusEngineCfg(t, reg,
			[]*cards.Card{lookup(t, reg, "Overwrite the Multiverse"), bears},
			[]*cards.Card{bears, bears})
		moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
		moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
		moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
		moveByName(t, e, 0, "Overwrite the Multiverse", state.ZHand)
		e.priorityRound()
		addMana(t, e, 0, "BBBBBB")
		castNamed(t, e, "Overwrite the Multiverse")
		passUntilStackEmpty(t, e, 40)
		toks := jaceTokens(e, 0)
		if len(toks) != 1 {
			t.Fatalf("Overwrite the Multiverse: %d Jace tokens, want 1", len(toks))
		}
		if got := e.G.Obj(toks[0]).Counter("LOYALTY"); got != 3 {
			t.Fatalf("Jace token loyalty = %d, want 3 (three creatures exiled)", got)
		}
		replayCheck(t, e, cfg)
	})
}
