package rules

// The vivi-ornitier-cedh Commander deck import (2026-09-17) closed the
// parameter-census gaps the deck's cards exposed — one real read per
// parameter, each pinned here on the real corpus card (never a .cards .txt,
// per the licensing rule):
//
//	api:Draw.OptionalDecider        Rhystic Study (Mystic Remora shares the shape)
//	stat:AlternativeCost.Announce$  Blazing Shoal (Disrupting Shoal shares it)
//	api:DelayedTrigger.NextTurn     Mishra's Bauble (Urza's/Lodestone share it)
//	api:PeekAndReveal.NoReveal      Mishra's Bauble
//	api:Reveal.Random               Urza's Bauble
//	api:PutCounter.RememberCostMana Jeweled Amulet (and its Produced$ Special LastNotedType)
//	api:ChangeZone.ReduceCost       Otawara, Soaring City
//	api:ChangeZone.TargetsWithSameController  Lodestone Bauble
//	api:DelayedTrigger.SpellCast    Mistrise Village (ThisTurn$/Static$/ValidCard$/ValidActivatingPlayer$)
//
// The same ticket fixed the class behind the first live livelock the deck
// exposed: an SA whose BODY poses a mid-resolution ask after its UnlessCost$
// gate resolved used to re-pose the pay ask on the answer's re-entry (a
// repeating pay/draw cycle). Host.SuspendUnless records the resolved
// outcome on the pending ask's resume point; TestUnlessBodyAskDoesNotRepose
// pins the class on Mystic Remora, the deck's live carrier.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// viviEngine builds a two-seat fixture whose decks are exactly seat0 and
// seat1 padded with Mountains, seeded so the toss starts seat 0, advanced to
// seat 0's Main1. The Config travels out because a replay must be handed the
// same seed/decks the live game was built with.
func viviEngine(t *testing.T, reg *cards.Registry, seat0, seat1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	pad := func(base []*cards.Card) []*cards.Card {
		out := append([]*cards.Card(nil), base...)
		return append(out, mountainDeck(t, 40-len(base))...)
	}
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{pad(seat0), pad(seat1)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// viviRefresh re-asks priority so the pending decision reflects every
// fixture move since the last real ask (moveByName emits logged MoveZones a
// stale decision cannot see).
func viviRefresh(t *testing.T, e *Engine) {
	t.Helper()
	addMana(t, e, 0, "")
}

// viviCard looks a corpus card up by name, failing the test when the corpus
// lacks it.
func viviCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus fixture: %s missing", name)
	}
	return c
}

// viviPass submits a pass for the pending priority decision.
func viviPass(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "pass" {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("priority without a pass option: %+v", d.Options)
}

// viviPick returns the option index whose Kind matches kind (first match).
func viviPick(d *decision.Decision, kind string) int {
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index
		}
	}
	return -1
}

// viviOption returns the option whose Kind is kind and (when obj != 0) whose
// Obj is obj — except for the player kind, where the option carries the seat
// in .Player (Obj is always 0 for a player option).
func viviOption(d *decision.Decision, kind string, obj state.ObjID) *decision.Option {
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind != kind {
			continue
		}
		if obj == 0 || o.Obj == obj || (kind == "player" && o.Player == state.PlayerID(obj)) {
			return o
		}
	}
	return nil
}

// viviUntap clears a fixture permanent's tapped state (a logged Untap, the
// same event an untap step emits) — a fixture that entered or activated
// tapped and needs a second {T} ability.
func viviUntap(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.Untap, Obj: id})
}

// viviDecline returns the unless ask's decline option index ("Don't pay").
func viviDecline(d *decision.Decision) int {
	for _, o := range d.Options {
		if strings.HasPrefix(o.Label, "Don't") {
			return o.Index
		}
	}
	return -1
}

// TestRhysticStudyPayDeclineAsksTheOptionalDraw pins api:Draw.
// OptionalDecider$ end to end, and with it the asking-body-under-UnlessCost$
// livelock fix: the payer declines {1}, the OptionalDecider$ player is asked
// ("draw?"), a yes draws one, and — the class pin — the gate does NOT
// re-pose the pay ask on the draw answer's re-entry (the pre-fix cycle
// repeated pay/choose 80 times before the livelock detector fired).
func TestRhysticStudyPayDeclineAsksTheOptionalDraw(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Rhystic Study")},
		[]*cards.Card{viviCard(t, reg, "Opt")})
	if moveByName(t, e, 0, "Rhystic Study", state.ZBattlefield) == 0 {
		t.Fatal("Rhystic Study not found")
	}
	if moveByName(t, e, 1, "Opt", state.ZHand) == 0 {
		t.Fatal("Opt not found")
	}
	addMana(t, e, 1, "CCU")
	// Seat 0 passes; seat 1 casts Opt (1 mana, no target).
	viviPass(t, e)
	d := e.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("expected seat 1 priority, got %+v", d)
	}
	co := viviOption(d, "cast", 0)
	if co == nil {
		t.Fatalf("seat 1 has no Opt cast option: %+v", d.Options)
	}
	submitChoices(t, e, co.Index)
	// The Rhystic trigger pushed on the cast resolves only once both seats
	// pass; it then asks seat 1 (the UnlessPayer$ TriggeredActivator) to pay
	// {1}: decline.
	viviPass(t, e)
	viviPass(t, e)
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes || d.Player != 1 {
		t.Fatalf("expected the unless pay ask for seat 1, got %+v", d)
	}
	decline := viviDecline(d)
	if decline < 0 {
		t.Fatalf("no decline option: %+v", d.Options)
	}
	submitChoices(t, e, decline)
	// THE pin: the OptionalDecider$ ask goes to seat 0 — and answering it
	// must not re-pose the pay ask (the livelock). A yes draws one.
	handBefore := len(e.G.Zone(state.ZHand, 0))
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.ResumeKind != "draw_optional" {
		t.Fatalf("expected the draw_optional ask for seat 0, got %+v", d)
	}
	submitChoices(t, e, 0) // "yes — draw"
	d = e.Pending()
	if d == nil || d.Player != 0 || d.Kind != decision.KPriority {
		t.Fatalf("expected the turn player's priority after the draw (the gate re-posed: %+v)", d)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("seat 0 hand %d, want %d (one Opt draw)", got, handBefore+1)
	}
	// (The replay-contract re-fold is pinned by the acceptance suite's bot
	// games; this fixture's addMana drive records helper-internal passes that
	// supersede an outstanding ask, which a bare replayFor cannot re-fold --
	// the same pre-existing limitation every fixture test shares.)
}

// TestRhysticStudyDrawDeclineDrawsNothing pins the "no" arm: the decider's
// decline draws nothing and the resolution completes (no re-ask).
func TestRhysticStudyDrawDeclineDrawsNothing(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Rhystic Study")},
		[]*cards.Card{viviCard(t, reg, "Opt")})
	if moveByName(t, e, 0, "Rhystic Study", state.ZBattlefield) == 0 {
		t.Fatal("Rhystic Study not found")
	}
	if moveByName(t, e, 1, "Opt", state.ZHand) == 0 {
		t.Fatal("Opt not found")
	}
	addMana(t, e, 1, "CCU")
	viviPass(t, e)
	d := e.Pending()
	co := viviOption(d, "cast", 0)
	if co == nil {
		t.Fatalf("seat 1 has no Opt cast option: %+v", d.Options)
	}
	submitChoices(t, e, co.Index)
	viviPass(t, e)
	viviPass(t, e)
	d = e.Pending()
	decline := viviDecline(d)
	if decline < 0 {
		t.Fatalf("no decline option: %+v", d.Options)
	}
	submitChoices(t, e, decline)
	d = e.Pending()
	if d == nil || d.ResumeKind != "draw_optional" {
		t.Fatalf("expected the draw_optional ask, got %+v", d)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, 1) // "no"
	d = e.Pending()
	if d == nil || d.Player != 0 {
		t.Fatalf("expected the turn player's priority after the decline, got %+v", d)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("seat 0 hand %d, want %d (a decline draws nothing)", got, handBefore)
	}
}

// TestBlazingShoalAnnouncesXFromExileCandidates pins
// stat:AlternativeCost.Announce$ end to end: the alternative cost's X ask
// offers exactly the mana values some exilable red card matches at, the
// announced value binds the exile filter's cmcEQX, and the spell's pump
// reads the announced X through Count$xPaid.
func TestBlazingShoalAnnouncesXFromExileCandidates(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{
			viviCard(t, reg, "Blazing Shoal"),
			viviCard(t, reg, "Ember Hauler"),  // red, mana value 2
			viviCard(t, reg, "Goblin Guide"),  // red, mana value 1
			viviCard(t, reg, "Grizzly Bears"), // the pump's target
			viviCard(t, reg, "Grizzly Bears"), // a second body on the board
			viviCard(t, reg, "Wall of Roots"), // NOT red — must never be a candidate
		},
		[]*cards.Card{viviCard(t, reg, "Grizzly Bears")})
	shoal := moveByName(t, e, 0, "Blazing Shoal", state.ZHand)
	bear2 := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	bear1 := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	wall := moveByName(t, e, 0, "Wall of Roots", state.ZBattlefield)
	hauler := moveByName(t, e, 0, "Ember Hauler", state.ZHand)
	guide := moveByName(t, e, 0, "Goblin Guide", state.ZHand)
	if bear1 == 0 || bear2 == 0 || wall == 0 || hauler == 0 || guide == 0 {
		t.Fatal("the fixtures were not found")
	}
	viviRefresh(t, e)
	d := e.Pending()
	alt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == shoal && o.AltCostIndex > 0 {
			alt = o.Index
		}
	}
	if alt < 0 {
		t.Fatalf("no alternative-cost cast option for the Shoal: %+v", d.Options)
	}
	submitChoices(t, e, alt)
	// CR 601.2b: the X ask offers exactly the exilable CMC values {1, 2}.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("the Shoal's alternative cost did not announce X: %+v", d)
	}
	vals := map[int32]bool{}
	for _, o := range d.Options {
		vals[int32(o.Amount)] = true
	}
	if !vals[1] || !vals[2] || vals[0] || vals[3] {
		t.Fatalf("X options %v, want exactly {1, 2} (the red cards' mana values)", vals)
	}
	// Announce X = 2, exile the 2-mana-value red card only.
	two := -1
	for _, o := range d.Options {
		if o.Amount == 2 {
			two = o.Index
		}
	}
	submitChoices(t, e, two)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 {
		t.Fatalf("expected the exile ask, got %+v", d)
	}
	if len(d.Options) != 1 || e.G.Obj(d.Options[0].Obj).Face().Name != "Ember Hauler" {
		t.Fatalf("exile candidates %+v, want exactly Ember Hauler (cmcEQ2 at the announced X)", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	// The pump's target ask: aim at bear2.
	d = e.Pending()
	ta := viviOption(d, "permanent", bear2)
	if ta == nil {
		t.Fatalf("no target option for the bear: %+v", d.Options)
	}
	submitChoices(t, e, ta.Index)
	// The spell resolves (both seats pass) and the bear reads +2 power.
	viviPass(t, e)
	viviPass(t, e)
	if got, want := e.Power(bear2), int32(2+2); got != want {
		t.Fatalf("bear power %d, want %d (the announced X pumped it)", got, want)
	}
	if o := e.G.Obj(shoal); o == nil || o.Zone == state.ZHand {
		t.Fatalf("the Shoal did not leave the hand: zone %v", o.Zone)
	}
}

// TestMishrasBaublePrivateLookAndNextTurnSlowtrip pins api:PeekAndReveal.
// NoReveal$ (the look is a Secret Note to the activator, never a public
// reveal) and api:DelayedTrigger.NextTurn$ (the slowtrip registration's
// MinTurn skips the current turn's own upkeep — the bauble was activated in
// THIS turn — and fires at the NEXT turn's upkeep).
func TestMishrasBaublePrivateLookAndNextTurnSlowtrip(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Mishra's Bauble")},
		[]*cards.Card{viviCard(t, reg, "Opt")})
	id := moveByName(t, e, 0, "Mishra's Bauble", state.ZBattlefield)
	viviRefresh(t, e)
	ao, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("the bauble's ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, ao.Index)
	// The target ask: seat 1 (ValidTgts$ Player).
	d := e.Pending()
	ta := viviOption(d, "player", 1)
	if ta == nil {
		t.Fatalf("no seat-1 target option: %+v", d.Options)
	}
	submitChoices(t, e, ta.Index)
	viviPass(t, e)
	viviPass(t, e)
	// The pacing gate (lookack, fb-20260917T232325Z-35cfca4b): the bare
	// private look now poses its one-option Continue ack to the activator
	// BEFORE the note lands — the reporter's defect was exactly this line
	// streaming past ungated.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "look_ack" || d.Player != 0 {
		t.Fatalf("expected the look ack, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Kind != "yes" || d.Options[0].Label != "Continue" {
		t.Fatalf("ack options = %+v, want a single Continue", d.Options)
	}
	if lib := e.G.Zone(state.ZLibrary, 1); len(lib) > 0 {
		if name := e.G.Obj(lib[0]).Face().Name; !strings.Contains(d.Prompt, name) {
			t.Fatalf("ack prompt %q does not name the top library card %q", d.Prompt, name)
		}
	}
	submitChoices(t, e, d.Options[0].Index)
	viviPass(t, e)
	viviPass(t, e)
	// One Secret look Note scoped to the activator (seat 0), naming the top
	// library card; NO public reveal.
	looks := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Secret && ev.Player == 0 {
			looks++
		}
		if ev.Kind == events.Note && !ev.Secret && len(ev.IDs) > 0 {
			t.Fatalf("a public reveal leaked: %+v", ev)
		}
	}
	if looks != 1 {
		t.Fatalf("%d secret look notes, want exactly 1", looks)
	}
	// The slowtrip registration carries MinTurn = this turn + 1.
	reg1 := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedRegister {
			reg1 = true
			if ev.Amount != e.G.Turn+1 {
				t.Fatalf("NextTurn$ MinTurn %d, want %d", ev.Amount, e.G.Turn+1)
			}
		}
	}
	if !reg1 {
		t.Fatal("no DelayedRegister event")
	}
	// Drive to the NEXT turn's upkeep: the slowtrip draws seat 0 a card
	// (ValidPlayer$ Player matches any seat's upkeep; the first upkeep the
	// game reaches is the next turn's).
	handBefore := len(e.G.Zone(state.ZHand, 0))
	driveToStepAll(t, e, e.G.Turn+1, 1-e.G.Active, state.StepUpkeep)
	// The trigger pushed at the step change; let it resolve (both seats
	// pass) before counting the draw.
	viviPass(t, e)
	viviPass(t, e)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("seat 0 hand %d, want %d (the slowtrip draw fired)", got, handBefore+1)
	}
}

// TestUrzasBaublesRandomRevealNamesOneCard pins api:Reveal.Random$: the
// pool narrows to ONE random card (the seeded engine pick), and exactly one
// Note names it. The corpus's Reveal$+Random$ carriers are genuine random
// reveals ("target player reveals a card at random from their hand" --
// Cursed Scroll, Wand of Ith, Ignite Memories), so the note is public; the
// engine's public note for Urza's Bauble's oracle-LOOK wording is the
// recorded divergence (the report's Issues section).
func TestUrzasBaublesRandomRevealNamesOneCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Urza's Bauble")},
		[]*cards.Card{viviCard(t, reg, "Opt"), viviCard(t, reg, "Giant Growth"), viviCard(t, reg, "Grizzly Bears")})
	id := moveByName(t, e, 0, "Urza's Bauble", state.ZBattlefield)
	viviRefresh(t, e)
	ao, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("the bauble's ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, ao.Index)
	d := e.Pending()
	ta := viviOption(d, "player", 1)
	if ta == nil {
		t.Fatalf("no seat-1 target option: %+v", d.Options)
	}
	submitChoices(t, e, ta.Index)
	viviPass(t, e)
	viviPass(t, e)
	look, leaked := 0, 0
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note || len(ev.IDs) != 1 {
			continue
		}
		if o := e.G.Obj(ev.IDs[0]); o == nil || o.Zone != state.ZHand || o.Owner != 1 {
			continue
		}
		look++
		if ev.Secret {
			leaked++
		}
	}
	if look != 1 {
		t.Fatalf("%d one-card notes over seat 1's hand, want exactly 1 (Random$ narrows the pool)", look)
	}
	if leaked != 0 {
		t.Fatalf("the random pick leaked as a secret note (a reveal is public)")
	}
}

// TestJeweledAmuletNotesAndProducesTheSpentType pins api:PutCounter.
// RememberCostMana$ and the Produced$ Special LastNotedType pair: the
// activation notes the {U} it was paid with; the mana ability then adds one
// blue mana.
func TestJeweledAmuletNotesAndProducesTheSpentType(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Jeweled Amulet")},
		[]*cards.Card{viviCard(t, reg, "Opt")})
	id := moveByName(t, e, 0, "Jeweled Amulet", state.ZBattlefield)
	addMana(t, e, 0, "U")
	ao, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("the amulet's PutCounter ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, ao.Index)
	noted := ""
	for _, ev := range e.L.Events {
		if ev.Kind == events.Choose && ev.Counter == "noted-mana" && ev.Obj == id {
			noted = ev.Text
		}
	}
	if noted != "U" {
		t.Fatalf("the activation noted %q, want \"U\" (the colour the {1} was paid with)", noted)
	}
	// The ability resolves (both seats pass) and places the charge counter.
	viviPass(t, e)
	viviPass(t, e)
	if o := e.G.Obj(id); o.Counter("CHARGE") != 1 {
		t.Fatalf("charge counters %d, want 1 (the PutCounter resolved)", o.Counter("CHARGE"))
	}
	// The mana ability (index 1) is a TAP-FOR-MANA action, not an "ability"
	// option: it must be offered untapped with a charge counter to remove
	// (the {1} {T} activation tapped the amulet).
	viviUntap(t, e, id)
	viviRefresh(t, e)
	d := e.Pending()
	act := viviOption(d, "activate", id)
	if act == nil {
		t.Fatalf("the amulet's mana ability is not offered: %+v", d.Options)
	}
	submitChoices(t, e, act.Index)
	produced := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.ManaAdd && ev.Counter == "U" && ev.Amount > 0 {
			produced = true
		}
	}
	if !produced {
		t.Fatal("the amulet did not produce the noted blue mana")
	}
}

// TestOtawaraChannelReduceCost pins api:ChangeZone.ReduceCost$: the Channel
// ability's offer-time cost reads the SVar-resolved reduction ({3}{U} minus
// one {1} per legendary creature you control), the client-visible
// AbilityCosts projection carries it, and the activation's charge agrees.
func TestOtawaraChannelReduceCost(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Otawara, Soaring City"), viviCard(t, reg, "Krenko, Baron of Tin Street")},
		[]*cards.Card{viviCard(t, reg, "Opt")})
	id := moveByName(t, e, 0, "Otawara, Soaring City", state.ZHand)
	ugin := moveByName(t, e, 0, "Krenko, Baron of Tin Street", state.ZBattlefield)
	viviRefresh(t, e)
	if ugin == 0 || id == 0 {
		t.Fatal("the fixtures were not found")
	}
	// Krenko is a legendary creature you control: the Channel costs {2}{U}.
	costs := e.AbilityCosts(0, id)
	if len(costs) != 1 || costs[0] != "2 U Discard<1/CARDNAME>" {
		t.Fatalf("Channel cost %v, want [2 U Discard<1/CARDNAME>] (one legendary creature reduces {1})", costs)
	}
	addMana(t, e, 0, "CCU")
	d := e.Pending()
	ao, ok := findAbilityOption(e, id, 1)
	if !ok {
		t.Fatalf("the Channel ability is not offered from hand: %+v", d)
	}
	submitChoices(t, e, ao.Index)
	// The Discard<1/CARDNAME> cost part asks first: the Otawara itself.
	d = e.Pending()
	da := viviOption(d, "discard", id)
	if da == nil {
		t.Fatalf("no discard cost option: %+v", d.Options)
	}
	submitChoices(t, e, da.Index)
	d = e.Pending()
	ta := viviOption(d, "permanent", ugin)
	if ta == nil {
		t.Fatalf("no Channel target option: %+v", d.Options)
	}
	submitChoices(t, e, ta.Index)
	// The ability resolves (both seats pass): Krenko returns to hand.
	viviPass(t, e)
	viviPass(t, e)
	if o := e.G.Obj(ugin); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Ugin zone %v, want hand (the Channel returned it)", o.Zone)
	}
}

// TestLodestoneBaubleTargetsShareOneOwner pins
// api:ChangeZone.TargetsWithSameController$ at the Submit gate: an answer
// naming graveyards of two different players is rejected (the pending
// decision survives), and an answer from ONE player's graveyard is accepted.
func TestLodestoneBaubleTargetsShareOneOwner(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Lodestone Bauble"), viviCard(t, reg, "Forest"), viviCard(t, reg, "Forest")},
		[]*cards.Card{viviCard(t, reg, "Forest")})
	id := moveByName(t, e, 0, "Lodestone Bauble", state.ZBattlefield)
	f0 := moveByName(t, e, 0, "Forest", state.ZGraveyard)
	f1 := moveByName(t, e, 0, "Forest", state.ZGraveyard)
	other := moveByName(t, e, 1, "Forest", state.ZGraveyard)
	addMana(t, e, 0, "CC")
	ao, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("the bauble's ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, ao.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the target ask, got %+v", d)
	}
	f0idx, f1idx, otherIdx := -1, -1, -1
	for _, o := range d.Options {
		switch o.Obj {
		case f0:
			f0idx = o.Index
		case f1:
			f1idx = o.Index
		case other:
			otherIdx = o.Index
		}
	}
	if f0idx < 0 || f1idx < 0 || otherIdx < 0 {
		t.Fatalf("expected both seat-0 graveyards plus seat 1's offered: %+v", d.Options)
	}
	// Mixing owners is rejected and the decision survives.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{f0idx, otherIdx}}); err == nil {
		t.Fatal("a mixed-owner answer was accepted (TargetsWithSameController unread)")
	}
	// Two from seat 0's graveyard are accepted.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{f0idx, f1idx}}); err != nil {
		t.Fatalf("a same-owner answer was rejected: %v", err)
	}
}

// TestMistriseVillageSpellCastPromise pins the event-matched DelayedTrigger:
// the {U},{T} activation registers a SpellCast delayed trigger bounded to
// this turn (ThisTurn$ True rides "|TT=<turn>" into MaxTurn), the next spell
// you cast fires it, the static delayed trigger's Effect resolves
// IMMEDIATELY (Forge's isStatic arm — never pushed on the stack), the
// AntiMagic CantHappen replacement registers remembering the cast spell, and
// a counterspell against THAT spell is stopped while the spell resolves.
func TestMistriseVillageSpellCastPromise(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{
			viviCard(t, reg, "Mistrise Village"),
			viviCard(t, reg, "Opt"),
		},
		[]*cards.Card{viviCard(t, reg, "Counterspell")})
	id := moveByName(t, e, 0, "Mistrise Village", state.ZBattlefield)
	optId := moveByName(t, e, 0, "Opt", state.ZHand)
	if optId == 0 {
		t.Fatal("Opt not found")
	}
	if moveByName(t, e, 1, "Counterspell", state.ZHand) == 0 {
		t.Fatal("Counterspell not found")
	}
	// Mistrise's own ETB replacement taps it on entry (see the report's
	// Issues: the Mountain/Forest condition does not withhold the tap) —
	// the fixture untaps it, the same thing an untap step would do.
	viviUntap(t, e, id)
	addMana(t, e, 0, "UU")
	ao, ok := findAbilityOption(e, id, 1)
	if !ok {
		t.Fatalf("Mistrise's DelayedTrigger ability is not offered: %+v", e.Pending())
	}
	submitChoices(t, e, ao.Index)
	// The ability resolves (both seats pass) and registers the promise.
	viviPass(t, e)
	viviPass(t, e)
	// The registration is event-matched, inline-bodied, this-turn-bounded.
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind != events.DelayedRegister {
			continue
		}
		found = true
		if !strings.HasPrefix(ev.Text, "SpellCast:Mode$ SpellCast") {
			t.Fatalf("the registration's Text %q is not an inline SpellCast body", ev.Text)
		}
		if !strings.Contains(ev.Text, "|TT=") || !strings.Contains(ev.Text, "Static$ True") {
			t.Fatalf("ThisTurn$/Static$ did not ride the body: %q", ev.Text)
		}
	}
	if !found {
		t.Fatal("no DelayedRegister event")
	}
	// Seat 0 casts Opt: the promise's trigger fires on the SpellCast (seat 0
	// casts, ValidCard$ Card, ValidActivatingPlayer$ You) and its Static$
	// Effect resolves immediately at the cast event — no static delayed
	// ability may sit on the stack above the Opt (CR 603.7's immediate arm)
	// — so the AntiMagic continuous is live while the Opt itself is still
	// on the stack.
	viviRefresh(t, e)
	d := e.Pending()
	co := viviOption(d, "cast", optId)
	if co == nil {
		t.Fatalf("seat 0 has no Opt cast option: %+v", d.Options)
	}
	submitChoices(t, e, co.Index)
	anti := 0
	for _, ce := range e.continuous {
		if ce.ReplacementEvent == "Counter" && strings.EqualFold(strings.TrimSpace(ce.ReplacementParams["Layer"]), "CantHappen") {
			anti++
			if len(ce.Remembered) != 1 || ce.Remembered[0] != optId {
				t.Fatalf("AntiMagic remembered %v, want the cast Opt %d", ce.Remembered, optId)
			}
		}
	}
	if anti != 1 {
		t.Fatalf("%d AntiMagic replacements active, want exactly 1", anti)
	}
	if o := e.G.Obj(optId); o == nil || o.Zone != state.ZStack {
		t.Fatalf("the Opt zone %v, want the stack (it was cast)", o.Zone)
	}
	// The opponent counters the Opt: the Counter event is stopped (the
	// CantHappen replacement) and the Counterspell resolves doing nothing;
	// the Opt then resolves normally (its scry still asks).
	addMana(t, e, 1, "UU")
	viviPass(t, e)
	cs := moveByName(t, e, 1, "Counterspell", state.ZHand)
	viviRefresh(t, e)
	d = e.Pending()
	co = viviOption(d, "cast", cs)
	if co == nil {
		t.Fatalf("seat 1 has no Counterspell cast option: %+v", d.Options)
	}
	submitChoices(t, e, co.Index)
	d = e.Pending()
	ta := viviOption(d, "spell", optId)
	if ta == nil {
		t.Fatalf("no stack-target option for the Opt: %+v", d.Options)
	}
	submitChoices(t, e, ta.Index)
	// Two passes resolve the prevented Counterspell, two more resolve the
	// Opt beneath it.
	viviPass(t, e)
	viviPass(t, e)
	viviPass(t, e)
	viviPass(t, e)
	d = e.Pending()
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("expected the Opt's scry arrange after the prevented counter, got %+v", d)
	}
	var scryChoices []int
	for i := range d.Options {
		scryChoices = append(scryChoices, i)
	}
	submitChoices(t, e, scryChoices...)
	if o := e.G.Obj(optId); o == nil || o.Zone == state.ZStack {
		t.Fatalf("the Opt zone %v, want resolved off the stack", o.Zone)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Obj == optId && strings.Contains(ev.Text, "countered") {
			t.Fatalf("the Opt appears countered: %+v", ev)
		}
	}
}

// TestUnlessBodyAskDoesNotRepose is the class pin for the livelock the
// vivi-ornitier-cedh import exposed: an SA whose BODY poses a mid-resolution
// ask after its UnlessCost$ gate resolved must never re-pose the pay ask on
// the body answer's re-entry. Mystic Remora is the live carrier; Rhystic
// Study shares the shape.
func TestUnlessBodyAskDoesNotRepose(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Mystic Remora")},
		[]*cards.Card{viviCard(t, reg, "Opt")})
	if moveByName(t, e, 0, "Mystic Remora", state.ZBattlefield) == 0 {
		t.Fatal("Mystic Remora not found")
	}
	if moveByName(t, e, 1, "Opt", state.ZHand) == 0 {
		t.Fatal("Opt not found")
	}
	addMana(t, e, 1, "CCU")
	viviPass(t, e)
	d := e.Pending()
	co := viviOption(d, "cast", 0)
	if co == nil {
		t.Fatalf("seat 1 has no Opt cast option: %+v", d.Options)
	}
	submitChoices(t, e, co.Index)
	// The Remora trigger pushed on the cast resolves once both seats pass.
	viviPass(t, e)
	viviPass(t, e)
	// Decline the {4}.
	d = e.Pending()
	decline := viviDecline(d)
	if decline < 0 {
		t.Fatalf("no decline option: %+v", d.Options)
	}
	submitChoices(t, e, decline)
	// The optional draw ask follows; answering it must reach seat 1's
	// priority, not another pay ask (the livelock detector would fire first).
	d = e.Pending()
	if d == nil || d.ResumeKind != "draw_optional" {
		t.Fatalf("expected the draw_optional ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = e.Pending()
	if d == nil || d.Player != 0 || d.Kind != decision.KPriority {
		t.Fatalf("expected the turn player's priority, got %+v", d)
	}
}
