package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// unless_reveal_cost_test.go pins the Reveal<N/Spec> unless-cost component
// (the hideaway-family ETB lands: Primal Beyond, Port Town, ..., plus the
// three switched carriers Xyru Specter, Priest of the Wakening Sun and
// Invasion of the Giants). The cost is choice-bearing like Sac/Discard — the
// payer reveals N hand cards matching the spec, the cards STAY in hand — and
// it pays through the beginUnlessPayment continuation, never synchronously
// (payUnlessCost refuses it). Before this grammar existed, ParseUnlessCost
// hard-declined every one of the 22 corpus carriers, so the ETB lands were
// permanently tapped and the switched bodies never ran.

// playLandFromHand plays the named card from seat 0's hand through the
// ordinary play_land option, exactly the Blood Crypt fixture does.
func playLandFromHand(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	d := e.Pending()
	land := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && e.G.Obj(o.Obj) != nil && e.G.Obj(o.Obj).Face().Name == name {
			land = o.Index
			break
		}
	}
	if land < 0 {
		t.Fatalf("no play_land option for %s: %+v", name, d.Options)
	}
	id := d.Options[land].Obj
	submitChoices(t, e, land)
	return id
}

// seekUnlessPayAsk passes every priority decision until a mid-resolution
// unless_pay ask becomes pending.
func seekUnlessPayAsk(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision kind %v (seat %d) while seeking the unless-pay ask: %+v", d.Kind, d.Player, d)
		}
		castFirst(t, e, "pass")
	}
	return nil
}

// TestUnlessRevealPrimalBeyondPayEntersUntapped is leaf 1: Primal Beyond
// entering with exactly one Elemental in hand — the exact-candidate shape —
// records the reveal silently, announces it with the cast flow's public Note,
// leaves the card in hand, and the land enters UNTAPPED.
func TestUnlessRevealPrimalBeyondPayEntersUntapped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	fern := card(t, "Name:Fern\nTypes:Creature Elemental\nPT:1/1\nOracle:x\n")
	e := handEngine(t, mustCorpusCard(t, reg, "Primal Beyond"), fern)
	e.askPriority(0)
	land := playLandFromHand(t, e, "Primal Beyond")

	ask := seekUnlessPayAsk(t, e, 40)
	if ask == nil {
		t.Fatal("no unless-pay ask posed for the entering Primal Beyond")
	}
	if ask.ResumeSA == nil || ask.ResumeSA.API != "Tap" {
		t.Fatalf("ask SA = %+v, want the DB$ Tap body", ask.ResumeSA)
	}
	submitChoices(t, e, ask.Options[0].Index) // pay

	o := e.G.Obj(land)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("paid Primal Beyond zone = %+v, want battlefield", o)
	}
	if o.Tapped {
		t.Fatal("paid Primal Beyond entered tapped; the reveal pay must spare the tap")
	}
	notes := revealNotes(e)
	if len(notes) != 1 {
		t.Fatalf("reveal Notes = %+v, want exactly one", notes)
	}
	if notes[0].Text != "revealed Fern as a cost" {
		t.Fatalf("reveal Note text = %q", notes[0].Text)
	}
	if len(notes[0].IDs) != 1 || notes[0].IDs[0] != e.G.Zone(state.ZHand, 0)[0] {
		t.Fatalf("reveal Note IDs = %v, want the Elemental's id %v", notes[0].IDs, e.G.Zone(state.ZHand, 0))
	}
	if len(e.G.Zone(state.ZHand, 0)) != 1 {
		t.Fatalf("revealed card left the hand: hand = %d cards", len(e.G.Zone(state.ZHand, 0)))
	}
}

// TestUnlessRevealPortTownAsksRevealCost pins the strictly-more shape AND the
// ";" OR-alternation fold: Port Town's UnlessCost$ is Reveal<1/Plains;Island>
// (display text dropped), so a hand holding two matching cards poses a real
// KChoose (Min=Max=1, ResumeKind "unless_cost", option kind "revealcost"),
// the chosen card is revealed publicly, and the land enters untapped.
func TestUnlessRevealPortTownAsksRevealCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	plains := card(t, "Name:Steppe\nTypes:Land Plains\nOracle:x\n")
	island := card(t, "Name:Lagoon\nTypes:Land Island\nOracle:x\n")
	e := handEngine(t, mustCorpusCard(t, reg, "Port Town"), plains, island)
	e.askPriority(0)
	land := playLandFromHand(t, e, "Port Town")

	ask := seekUnlessPayAsk(t, e, 40)
	if ask == nil {
		t.Fatal("no unless-pay ask posed for the entering Port Town")
	}
	submitChoices(t, e, ask.Options[0].Index) // pay

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "unless_cost" {
		t.Fatalf("pending = %+v, want the unless_cost reveal KChoose", d)
	}
	if d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("reveal choose = Min %d Max %d options %+v, want 1..1 over 2 hand cards", d.Min, d.Max, d.Options)
	}
	for _, o := range d.Options {
		if o.Kind != "revealcost" {
			t.Fatalf("option kind = %q, want revealcost", o.Kind)
		}
	}
	// Reveal the Island (the second option) and verify exactly that id was
	// announced, in the payer's answer order.
	pick := d.Options[1].Index
	submitChoices(t, e, pick)
	o := e.G.Obj(land)
	if o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("paid Port Town = zone %+v tapped %v, want untapped battlefield", o, o != nil && o.Tapped)
	}
	notes := revealNotes(e)
	if len(notes) != 1 {
		t.Fatalf("reveal Notes = %+v, want exactly one", notes)
	}
	if len(notes[0].IDs) != 1 || notes[0].IDs[0] != d.Options[1].Obj {
		t.Fatalf("reveal Note IDs = %v, want the chosen card %d", notes[0].IDs, d.Options[1].Obj)
	}
}

// TestUnlessRevealFurycalmSnarlPayEntersUntapped is the brief's named carrier
// (Furycalm Snarl's UnlessCost$ Reveal<1/Mountain;Plains/...>): paying with a
// Mountain in hand reveals it (one public Note, the card stays in hand) and
// the land enters UNTAPPED; a decline keeps the old tapped entry.
func TestUnlessRevealFurycalmSnarlPayEntersUntapped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	mountain := card(t, "Name:Ridge\nTypes:Land Mountain\nOracle:x\n")
	for _, tc := range []struct {
		name  string
		mount bool
	}{
		{name: "pay", mount: true},
		{name: "decline", mount: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hand := []*cards.Card{mustCorpusCard(t, reg, "Furycalm Snarl")}
			if tc.mount {
				hand = append(hand, mountain)
			}
			e := handEngine(t, hand...)
			e.askPriority(0)
			land := playLandFromHand(t, e, "Furycalm Snarl")

			ask := seekUnlessPayAsk(t, e, 40)
			if ask == nil {
				t.Fatal("no unless-pay ask posed for the entering Furycalm Snarl")
			}
			if tc.mount {
				submitChoices(t, e, ask.Options[0].Index) // pay: reveal the Mountain
			} else {
				submitChoices(t, e, ask.Options[len(ask.Options)-1].Index) // decline
			}
			o := e.G.Obj(land)
			if tc.mount {
				if o == nil || o.Tapped {
					t.Fatalf("paid Furycalm Snarl zone/tap = %+v, want untapped on the battlefield", o)
				}
				notes := revealNotes(e)
				if len(notes) != 1 || notes[0].Text != "revealed Ridge as a cost" {
					t.Fatalf("reveal Notes = %+v, want exactly one 'revealed Ridge as a cost'", notes)
				}
				if len(e.G.Zone(state.ZHand, 0)) != 1 {
					t.Fatalf("revealed card left the hand: hand = %d cards", len(e.G.Zone(state.ZHand, 0)))
				}
			} else {
				if o == nil || !o.Tapped {
					t.Fatalf("declined Furycalm Snarl = %+v, want tapped on the battlefield", o)
				}
				if notes := revealNotes(e); len(notes) != 0 {
					t.Fatalf("reveal Notes = %+v, want none", notes)
				}
			}
		})
	}
}

// TestUnlessRevealDeclineEntersTapped is leaf 2: with no matching card in
// hand the Reveal<i> component is unsatisfiable, so the offer gate no longer
// poses the Pay branch at all (it used to offer it and then decline). The
// sole option is the decline and the land enters tapped with no reveal Note.
func TestUnlessRevealDeclineEntersTapped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name  string
		embed bool
	}{
		{name: "decline", embed: false},
		{name: "pay-with-no-match", embed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := handEngine(t, mustCorpusCard(t, reg, "Primal Beyond"))
			e.askPriority(0)
			land := playLandFromHand(t, e, "Primal Beyond")

			ask := seekUnlessPayAsk(t, e, 40)
			if ask == nil {
				t.Fatal("no unless-pay ask posed for the entering Primal Beyond")
			}
			// Precondition: nothing is left in hand to reveal, so the Pay
			// branch must not be offered; the decline is the only option
			// whether the test meant to decline or to try an impossible pay.
			if len(e.G.Zone(state.ZHand, 0)) != 0 {
				t.Fatalf("hand precondition failed: %d cards", len(e.G.Zone(state.ZHand, 0)))
			}
			if len(ask.Options) != 1 {
				t.Fatalf("unpayable Reveal Pay offered anyway: %+v", ask.Options)
			}
			submitChoices(t, e, ask.Options[0].Index) // decline
			o := e.G.Obj(land)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("Primal Beyond zone = %+v, want battlefield (it still enters)", o)
			}
			if !o.Tapped {
				t.Fatal("Primal Beyond entered untapped; the tap body must run")
			}
			if notes := revealNotes(e); len(notes) != 0 {
				t.Fatalf("reveal Notes = %+v, want none", notes)
			}
		})
	}
}

// drivePriestTrigger fires Priest of the Wakening Sun's real upkeep trigger
// (SVar:ABGainLife: DB$ GainLife | UnlessCost$ Reveal<1/Creature.Dinosaur> |
// UnlessPayer$ You | UnlessSwitched$ True | LifeAmount$ 2) by seeding the
// ordinary stack events — the same harness driveKurokiTrig uses, since the
// engine's Phase$ matcher cannot reach the trigger text naturally here.
func drivePriestTrigger(t *testing.T, dinos int) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	priest := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Priest of the Wakening Sun"))
	sa := cards.ResolveSVar(e.G.Obj(priest).Face().SVars, "ABGainLife")
	if sa == nil || sa.Params["UnlessCost"] != "Reveal<1/Creature.Dinosaur>" || sa.Params["UnlessPayer"] != "You" || sa.Params["UnlessSwitched"] != "True" {
		t.Fatalf("Priest ABGainLife = %+v, want the switched reveal shape", sa)
	}
	// Seed the reveal candidates BEFORE the ask is posed: the option list is
	// built at pose time from the hand, so a card injected afterwards would
	// not (and should not) change what was offered.
	dinoNames := []string{"Rexy", "Brutus", "Cera"}
	for i := 0; i < dinos; i++ {
		name := dinoNames[i%len(dinoNames)]
		dino := card(t, fmt.Sprintf("Name:%s\nTypes:Creature Dinosaur\nPT:3/3\nOracle:x\n", name))
		o := e.G.AddObject(dino, 0)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: priest, Player: 0, Amount: 0})
	e.resolveTop()
	// OptionalDecider$ You makes the trigger itself an optional election;
	// accept it so the unless-pay gate on the body is reached.
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOptional && d.ResumeKind == "optional" {
		submitChoices(t, e, d.Options[0].Index) // yes
	}
	if d := e.Pending(); d == nil || d.ResumeKind != "unless_pay" || d.Player != 0 {
		t.Fatalf("pending = %+v, want the unless-pay ask for seat 0", d)
	}
	return e, priest
}
