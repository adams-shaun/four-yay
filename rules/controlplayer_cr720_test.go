package rules

// CR 720 conformance for `api:ControlPlayer` (Mindslaver, Sorin Markov, The
// Dominion Bracelet's granted ability). The engine folds the grant into
// state.Game.ControlledBy/ControlArmedTurn, redirects the controlled player's
// decisions to their controller for the duration of that player's next turn,
// and expires both at the end of that turn.

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

// The fixture is deliberately a plain artifact with no tap cost: the test must
// exercise the API, the grant fold and the redirect, not a tap/payment shape
// another test already covers.
const controlPlayerFixture = "Name:Mindbender\nManaCost:4\nTypes:Artifact\n" +
	"A:AB$ ControlPlayer | Cost$ 4 | ValidTgts$ Opponent | SpellDescription$ You control target opponent during their next turn.\n" +
	"Oracle:x\n"

// TestControlPlayerGrantRedirectsThenExpires drives a real grant through the
// registered API: activate, target the opponent, resolve, then answer the
// controlled player's next-turn priority as the CONTROLLER, and confirm the
// control is gone the turn after.
func TestControlPlayerGrantRedirectsThenExpires(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 7, controlPlayerFixture)
	// Precondition: the fixture is on the battlefield, so its ability can be
	// offered at all (a hand-only card would make the offer assertion below
	// pass for the wrong reason).
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: fixture on battlefield, got %+v", o)
	}
	addMana(t, e, 0, "CCCC")
	e.Advance()

	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	// The ability's opponent target (CR 601.2c): a KTarget decision naming
	// seat 1, the only opponent in a two-seat game.
	d := passToAsk(t, e, 10)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target ask after activating the ability (got %+v)", d)
	}
	idx := indexOfPlayerOption(d, 1)
	if idx < 0 {
		t.Fatalf("target ask offered no opponent: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("target opponent: %v", err)
	}
	passUntilStackEmpty(t, e, 20)

	// The API ran: a real grant, not an "unimplemented API ControlPlayer" Note.
	if ctl, ok := e.G.ControlledBy[1]; !ok || ctl != 0 {
		t.Fatalf("after resolution ControlledBy = %+v, want seat 1 controlled by seat 0", e.G.ControlledBy)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ControlPlayer") {
			t.Fatalf("api:ControlPlayer is not registered: %q", ev.Text)
		}
	}

	// Seat 1's next turn: its opening Main1 priority must be posed to seat 0
	// (the controller). driveToTurn stops as soon as the turn/step is entered,
	// before any priority submission, so e.Pending() is the active player's
	// own first priority -- seat 1's, redirected.
	driveToTurn(t, e, 2, 1)
	if e.G.Active != 1 {
		t.Fatalf("precondition: seat 1's turn, active = %d", e.G.Active)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority at seat 1's Main1 (got %+v)", d)
	}
	if d.Player != 0 {
		t.Fatalf("controlled seat 1's priority posed to %d, want the controller 0", d.Player)
	}

	// The turn after seat 1's turn ends, the control is gone: seat 0's next
	// turn leaves no controller mapping and the seats answer for themselves.
	driveToTurn(t, e, 3, 0)
	if len(e.G.ControlledBy) != 0 {
		t.Fatalf("control outlived seat 1's next turn: %+v", e.G.ControlledBy)
	}
	replayCheck(t, e, cfg)
}

// TestDominionBraceletGrantedAbilityResolvesControl is the end-to-end card
// test: The Dominion Bracelet's granted ability both OFFERS and, once
// activated, delivers CR 720 control of the target opponent.
func TestDominionBraceletGrantedAbilityResolvesControl(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "The Dominion Bracelet"),
		lookup(t, reg, "Tapestry Warden"),
	}, nil)
	eq := moveByName(t, e, 0, "The Dominion Bracelet", state.ZBattlefield)
	cr := moveByName(t, e, 0, "Tapestry Warden", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: eq, IDs: []state.ObjID{cr}})
	e.G.Obj(cr).SummonSick = false
	if e.G.Obj(eq).AttachedTo != cr {
		t.Fatalf("precondition: Equipment attached to %d, want %d", e.G.Obj(eq).AttachedTo, cr)
	}
	addMana(t, e, 0, "CCCCCCCCCCCCCCC")
	e.priorityRound()
	d := e.Pending()
	opt := findGrantOption(t, d, cr, "DominionControlPlayer", eq)
	activateAndResolveGranted(t, e, opt, 1)
	if ctl, ok := e.G.ControlledBy[1]; !ok || ctl != 0 {
		t.Fatalf("the granted ability did not grant control: %+v", e.G.ControlledBy)
	}
	// The {15} Exile cost was paid by exiling the grantor Equipment from
	// seat 0's battlefield into its owner's exile.
	inExile := false
	for _, oid := range e.G.Zone(state.ZExile, 0) {
		if oid == eq {
			inExile = true
		}
	}
	if !inExile {
		t.Fatalf("the Equipment was not exiled as the cost: exile=%v", e.G.Zone(state.ZExile, 0))
	}
}

// TestDominionBraceletOpponentControlledGrantor is the regression the review
// named: a granted ability stays on a creature after the creature changes
// control, and its new controller can still activate it even when the
// grantor Equipment is controlled by an OPPONENT. Both the offer scan and
// the payment chooser must reach the bound referent regardless of controller.
func TestDominionBraceletOpponentControlledGrantor(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "Tapestry Warden"),
	}, []*cards.Card{
		lookup(t, reg, "The Dominion Bracelet"),
	})
	cr := moveByName(t, e, 0, "Tapestry Warden", state.ZBattlefield)
	eq := moveByName(t, e, 1, "The Dominion Bracelet", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: eq, IDs: []state.ObjID{cr}})
	e.G.Obj(cr).SummonSick = false
	// Precondition: the grantor is on an OPPONENT's battlefield, or this test
	// would degenerate to the same-controller case the offer test already
	// covers and prove nothing about the referent scan.
	if e.G.Obj(eq).Controller != 1 || e.G.Obj(eq).AttachedTo != cr {
		t.Fatalf("precondition: Equipment ctrl=%d attached=%d, want ctrl 1 attached %d",
			e.G.Obj(eq).Controller, e.G.Obj(eq).AttachedTo, cr)
	}
	addMana(t, e, 0, "CCCCCCCCCCCCCCC")
	e.priorityRound()
	d := e.Pending()
	opt := findGrantOption(t, d, cr, "DominionControlPlayer", eq)
	activateAndResolveGranted(t, e, opt, 1)
	// Payment really reached the opponent-controlled grantor: it left seat 1's
	// battlefield for its owner's exile.
	inExile := false
	for _, oid := range e.G.Zone(state.ZExile, 1) {
		if oid == eq {
			inExile = true
		}
	}
	if !inExile {
		t.Fatalf("the opponent-controlled Equipment was not exiled: exile=%v", e.G.Zone(state.ZExile, 1))
	}
}

// activateAndResolveGranted activates the offered granted ability and answers
// the asks the activation produces -- the Exile cost's battlefield-card pick
// (a KChoose; the {{15}} Exile<1/OriginalHost/...> cost) and the opponent
// target (KTarget) -- in whatever order they arrive, then drains the stack.
func activateAndResolveGranted(t *testing.T, e *Engine, opt decision.Option, targetPlayer state.PlayerID) {
	t.Helper()
	submitChoices(t, e, opt.Index)
	sawTarget := false
	for i := 0; i < 30; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision pending while resolving the granted ability")
		}
		switch d.Kind {
		case decision.KTarget:
			idx := indexOfPlayerOption(d, targetPlayer)
			if idx < 0 {
				t.Fatalf("no opponent target offered: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("target opponent: %v", err)
			}
			sawTarget = true
		case decision.KChoose:
			// The exile cost's card pick; the only option is the bound
			// referent (or its sibling battlefield cards).
			if len(d.Options) == 0 {
				t.Fatalf("exile cost ask offered no card: %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
		case decision.KPriority:
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("priority with no pass option: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
				t.Fatalf("pass: %v", err)
			}
		default:
			t.Fatalf("unexpected decision %s while resolving the granted ability", d.Kind)
		}
		if len(e.G.Stack) == 0 && sawTarget {
			return
		}
	}
	t.Fatal("granted ability did not resolve")
}

// findGrantOption locates the granted "ability" option for (obj, svar, grantor)
// in d, failing loudly when it is absent. It is the offer assertion both
// Dominion tests share, so an ability offered for a DIFFERENT grantor or SVar
// cannot satisfy either.
func findGrantOption(t *testing.T, d *decision.Decision, obj state.ObjID, svar string, grantor state.ObjID) decision.Option {
	t.Helper()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision (got %+v)", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == obj && o.SVar == svar && o.GrantSource == grantor {
			return o
		}
	}
	t.Fatalf("no granted ability %q of %d granted by %d (options %+v)", svar, obj, grantor, d.Options)
	return decision.Option{}
}

// TestControlPlayerCensusNamesTheOtherCarriers is the census the brief asks
// for: it enumerates every corpus card serving `api:ControlPlayer` (either as a
// printed A: ability or as an SVar body -- The Dominion Bracelet's granted
// shape) and every card whose Exile cost names OriginalHost, so a corpus pin
// that adds or drops a carrier is NAMED rather than silently absorbed. It is a
// presence ratchet, not a brittle count.
func TestControlPlayerCensusNamesTheOtherCarriers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ctrl := map[string]bool{}
	host := map[string]bool{}
	for _, c := range reg.AllCards() {
		if len(c.Faces) == 0 {
			continue
		}
		name := c.Faces[0].Name
		for _, f := range c.Faces {
			for _, a := range f.Abilities {
				if strings.TrimSpace(a.API) == "ControlPlayer" {
					ctrl[name] = true
				}
			}
			for _, body := range f.SVars {
				if censusSVarAPI(body) == "ControlPlayer" {
					ctrl[name] = true
				}
				if strings.Contains(body, "OriginalHost") {
					host[name] = true
				}
			}
		}
	}
	for _, want := range []string{"Mindslaver", "Sorin Markov", "The Dominion Bracelet"} {
		if !ctrl[want] {
			t.Errorf("api:ControlPlayer carrier %q missing from the census: %v", want, censusNames(ctrl))
		}
	}
	if !host["The Dominion Bracelet"] {
		t.Errorf("the OriginalHost exile-cost carrier is missing: %v", censusNames(host))
	}
}

// censusSVarAPI reads the API token from an SVar body ("AB$ ControlPlayer | ..."
// -> "ControlPlayer"), or "" when the body carries no API head.
func censusSVarAPI(body string) string {
	s := strings.TrimSpace(body)
	i := strings.IndexByte(s, '$')
	if i < 0 {
		return ""
	}
	s = s[i+1:]
	if j := strings.IndexByte(s, '|'); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}

func censusNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
