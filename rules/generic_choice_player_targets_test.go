package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

var genericChoicePlayerTgtsClass = map[string]bool{
	"Bane, Lord of Darkness":    true,
	"Combustible Gearhulk":      true,
	"Covenant of Minds":         true,
	"Feed the Machine":          true,
	"Indulgent Tormentor":       true,
	"Liar's Pendulum":           true,
	"May Civilization Collapse": true,
	"Palantír of Orthanc":       true,
	"Soul Echo":                 true,
	"Starseer Mentor":           true,
	"Surrender Your Thoughts":   true,
	"Terrapact Intimidator":     true,
	"The Fate of the Flammable": true,
	"Thornplate Intimidator":    true,
}

// TestGenericChoicePlayerTgtsCensus is a two-direction ratchet over SVar
// bodies: a new no-Defined$ player-targeted GenericChoice needs review, and a
// pinned card leaving the shape is stale.
func TestGenericChoicePlayerTgtsCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	for _, c := range reg.AllCards() {
		if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
			continue
		}
		for _, face := range c.Faces {
			if face == nil {
				continue
			}
			for _, body := range face.SVars {
				if genericChoicePlayerTargetBody(body) {
					got[face.Name] = true
				}
			}
		}
	}
	var drift []string
	for name := range got {
		if !genericChoicePlayerTgtsClass[name] {
			drift = append(drift, "NEW "+name+": review player-targeted GenericChoice")
		}
	}
	for name := range genericChoicePlayerTgtsClass {
		if !got[name] {
			drift = append(drift, "STALE "+name+": no longer in the class")
		}
	}
	sort.Strings(drift)
	for _, d := range drift {
		t.Error(d)
	}
	t.Logf("no-Defined$ player-targeted GenericChoice class: %d cards", len(got))
}

func TestGenericChoicePlayerTargetBodyUsesBaseType(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{"DB$ GenericChoice | ValidTgts$ Player.Opponent", true},
		{"DB$ GenericChoice | ValidTgts$ Any.Opponent", true},
		{"DB$ GenericChoice | ValidTgts$ Opponent.Other", true},
		{"DB$ GenericChoice | ValidTgts$ You.Controller", true},
		{"DB$ GenericChoice | ValidTgts$ Creature.YouCtrl", false},
		{"DB$ GenericChoice | ValidTgts$ Creature", false},
	} {
		if got := genericChoicePlayerTargetBody(tc.body); got != tc.want {
			t.Errorf("genericChoicePlayerTargetBody(%q) = %t, want %t", tc.body, got, tc.want)
		}
	}
}

func genericChoicePlayerTargetBody(body string) bool {
	if !strings.Contains(body, "DB$ GenericChoice") || strings.Contains(body, "Defined$") {
		return false
	}
	for _, part := range strings.Split(body, "|") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "ValidTgts$") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(part, "ValidTgts$"))
		for _, alternative := range strings.Split(value, ",") {
			base, _, _ := strings.Cut(strings.TrimSpace(alternative), ".")
			switch base {
			case "Player", "Any", "Opponent", "You":
				return true
			}
		}
	}
	return false
}

func terrapactTrigger(t *testing.T) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card := mustCorpusCard(t, reg, "Terrapact Intimidator")
	cfg := seatZeroStart(Config{Seed: 310, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{append([]*cards.Card{card}, mountainDeck(t, 39)...), mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	id := placeInDeck(t, e, 0, card, state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Terrapact zone/controller = %+v, want Battlefield/0", o)
	}
	if len(e.G.Players) < 2 || e.G.Players[1].Life <= 0 || e.G.Players[1].Life == e.G.Players[0].Life && e.G.Players[0].Life <= 0 {
		t.Fatalf("precondition: seat 1 is not a live opponent: players=%+v", e.G.Players)
	}
	return e, cfg, id
}

func terrapactChoose(t *testing.T, e *Engine, mode int) *decision.Decision {
	t.Helper()
	target := passUntilAskKind(t, e, decision.KTarget, 40)
	if target.Player != 0 || len(target.Options) != 1 || target.Options[0].Player != 1 || target.Options[0].Label != "b" {
		t.Fatalf("target decision = %+v, want controller seat 0 selecting opponent seat 1 (b)", target)
	}
	submitChoices(t, e, target.Options[0].Index)
	d := passUntilAskKind(t, e, decision.KModes, 40)
	if d.Player != 1 || d.ResumeKind != "generic_players" || len(d.Options) != 2 {
		t.Fatalf("chooser decision = %+v, want two modes to target seat 1 with generic_players", d)
	}
	if len(d.ResumeModes) != 2 || d.ResumeModes[0] != "TerrapactToken" || d.ResumeModes[1] != "TerrapactPutCounter" {
		t.Fatalf("mode choices = %v, want [TerrapactToken, TerrapactPutCounter]", d.ResumeModes)
	}
	submitChoices(t, e, d.Options[mode].Index)
	return d
}

func assertTerrapactModeChooser(t *testing.T, e *Engine) {
	t.Helper()
	var choosers []state.PlayerID
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen {
			choosers = append(choosers, ev.Player)
		}
	}
	if len(choosers) != 1 || choosers[0] != 1 {
		t.Fatalf("ModeChosen players = %v, want exactly [1]", choosers)
	}
}

func TestTerrapactIntimidatorTargetedOpponentIsTheChooser(t *testing.T) {
	t.Parallel()
	e, _, _ := terrapactTrigger(t)
	d := passUntilAskKind(t, e, decision.KTarget, 40)
	if d.Player != 0 || len(d.Options) != 1 || d.Options[0].Player != 1 || d.Options[0].Label != "b" {
		t.Fatalf("first ask = %+v, want target ask to seat 0 naming opponent seat 1", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	m := passUntilAskKind(t, e, decision.KModes, 40)
	if m.Player != 1 || m.ResumeKind != "generic_players" || len(m.Options) != 2 || len(m.ResumeModes) != 2 || m.ResumeModes[0] != "TerrapactToken" || m.ResumeModes[1] != "TerrapactPutCounter" {
		t.Fatalf("mode ask = %+v, want target seat 1 choosing both branches", m)
	}
	submitChoices(t, e, m.Options[1].Index)
	passUntilStackEmpty(t, e, 40)
	assertTerrapactModeChooser(t, e)
}

func TestTerrapactIntimidatorTokenBranch(t *testing.T) {
	t.Parallel()
	e, cfg, _ := terrapactTrigger(t)
	terrapactChoose(t, e, 0)
	passUntilStackEmpty(t, e, 40)
	landers := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Lander Token" {
			landers++
		}
	}
	if landers != 2 {
		t.Fatalf("seat 0 Lander tokens = %d, want exactly 2", landers)
	}
	assertTerrapactModeChooser(t, e)
	replayCheck(t, e, cfg)
}

func TestTerrapactIntimidatorCounterBranch(t *testing.T) {
	t.Parallel()
	e, cfg, id := terrapactTrigger(t)
	terrapactChoose(t, e, 1)
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Terrapact object = %+v, want battlefield", o)
	}
	if got := counterCount(o, "P1P1"); got != 2 {
		t.Fatalf("Terrapact P1P1 counters = %d, want 2", got)
	}
	assertTerrapactModeChooser(t, e)
	replayCheck(t, e, cfg)
}
