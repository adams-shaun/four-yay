package rules

// Kernel-era restorations of the tests W3 removed from play_hideaway_params_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMosswortBridgeGateAndCostedFreePlay walks the REAL card end to end.
// Leg 1: with zero power the AB$ Play activates (the offer has no
// condition gate -- the SVar gate lives at resolution), the GE10 gate fails,
// and no play ask ever appears. Leg 2: after two Craw Wurms (12 power) the
// same activation poses the play ask, the answered free cast moves the
// exiled Bears from exile onto the battlefield, and the printed {1}{G} is
// never charged (the pool carries only what the activations spent).
func TestMosswortBridgeGateAndCostedFreePlayKernel(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, bridgeID, bearID, playIdx := kr7MosswortEngine(t, reg)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: the Hideaway-exiled card is %v, want face down in exile", o)
	}

	// Leg 1: activate with zero power. The gate (Count$Valid
	// Creature.YouCtrl$CardPower GE 10) fails, so the resolution dispatch
	// skips effPlay and no play ask is posed.
	addMana(t, e, 0, "G")
	activatePlay := func() {
		t.Helper()
		d := e.Pending()
		opt := abilityOption(t, e, bridgeID, playIdx)
		if d == nil {
			t.Fatal("no decision pending before the activation")
		}
		submitChoices(t, e, opt.Index)
	}
	activatePlay()
	passUntilStackEmpty(t, e, 20)
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the zero-power activation the pending decision is %+v, want plain priority (the GE10 gate skipped the play)", d)
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the zero-power activation moved the exiled card: %v, want still in exile", o)
	}

	// Leg 2: 12 power, untap, fund, activate again -- the play ask appears.
	wurms := 0
	for i := 0; i < 2; i++ {
		searchMoveByName(t, e, "Craw Wurm", state.ZBattlefield)
	}
	for _, w := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(w); o != nil && o.Face() != nil && o.Face().Name == "Craw Wurm" {
			wurms++
		}
	}
	if wurms != 2 {
		t.Fatalf("precondition: %d Craw Wurms on the battlefield, want 2 (12 total power, past the GE10 gate)", wurms)
	}
	e.emit(events.Event{Kind: events.Untap, Obj: bridgeID})
	addMana(t, e, 0, "G")
	activatePlay()
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("after the 12-power activation: %+v, want the DB$ Play KModes ask", d)
	}
	// Controller$ You is an exact identity: the ask stays with the
	// resolving controller (seat 0).
	if d.Player != 0 {
		t.Fatalf("play ask player = %d, want 0 (Controller$ You == the resolving controller)", d.Player)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bearID || d.Options[0].Player != 0 {
		t.Fatalf("play options = %+v, want exactly the exiled Bears %d offered to seat 0", d.Options, bearID)
	}
	poolBefore := poolTotal(e.G.Players[0].Pool)
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the played card is %v, want on the battlefield (the free cast committed)", o)
	}
	if got := poolTotal(e.G.Players[0].Pool); got != poolBefore {
		t.Fatalf("pool after the play = %d (was %d), want unchanged: WithoutManaCost$ True charged nothing", got, poolBefore)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("pending after the play = %+v, want plain priority (a charged {1}{G} would have parked a mana ask instead)", d)
	}
	replayCheck(t, e, cfg)
}

// TestPlayControllerUnresolvableNamesNobody pins the fail-closed arm on the
// real compiled Mosswort ability with the Controller$ value swapped for one
// the defined-player machinery cannot bind: effPlay emits the loud Note and
// poses no ask at all -- it never silently routes the play back to the
// resolving controller. The synthetic-context drive follows
// choose_control_regression_test.go's Vial Smasher precedent (the real
// corpus SA resolved against a hand-built ctx whose Source is a real
// battlefield permanent).
func TestPlayControllerUnresolvableNamesNobodyKernel(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _, bridgeID, bearID, playIdx := kr7MosswortEngine(t, reg)
	// The gate's count body needs the real board: two Craw Wurms make the
	// GE10 gate pass so the walk dispatches effPlay and the exercise is
	// genuinely about the Controller$ read.
	for i := 0; i < 2; i++ {
		searchMoveByName(t, e, "Craw Wurm", state.ZBattlefield)
	}
	before := len(e.L.Events)
	bridge := e.G.Obj(bridgeID)
	if bridge == nil || playIdx < 0 || playIdx >= len(bridge.Face().Abilities) {
		t.Fatalf("bridge face %v ability idx %d unusable", bridge, playIdx)
	}
	sa := bridge.Face().Abilities[playIdx]
	bad := *sa
	bad.Params = make(map[string]string, len(sa.Params))
	for k, v := range sa.Params {
		bad.Params[k] = v
	}
	bad.Params["Controller"] = "Bogus.Nobody"
	ctx := &effects.Ctx{Source: bridgeID, Controller: 0}
	effects.Resolve(e, ctx, &bad)
	if len(e.L.Events) != before+1 {
		t.Fatalf("the unresolvable Controller$ emitted %d events, want exactly the one loud Note", len(e.L.Events)-before)
	}
	note := e.L.Events[len(e.L.Events)-1]
	if note.Kind != events.Note || note.Obj != bridgeID {
		t.Fatalf("emitted event = %+v, want the loud Note on the source", note)
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the failed Controller$ moved the exiled card: %v, want untouched in exile", o)
	}
}

// kr7MosswortEngine is mosswortEngine with the Bridge PLAYED (a real Submit)
// instead of raw-emitted onto the battlefield: a raw MoveZone outside any
// resolution no longer poses the Hideaway ask.
func kr7MosswortEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID, int) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	wurm := searchCorpusCard(t, reg, "Craw Wurm")
	deck0 := []*cards.Card{
		searchCorpusCard(t, reg, "Mosswort Bridge"), wurm, wurm, bear,
	}
	for i := 0; i < 18; i++ {
		deck0 = append(deck0, forest, mountain)
	}
	deck1 := make([]*cards.Card, 0, 40)
	for i := 0; i < 40; i++ {
		deck1 = append(deck1, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 7421, Names: []string{"bridge", "opponent"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	// The Bridge must start in hand: a library copy is bridged up (logged
	// MoveZone) like newFixtureDeck's own setup does.
	findIn := func(z state.Zone) state.ObjID {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Mosswort Bridge" {
				return id
			}
		}
		return 0
	}
	bridgeID := findIn(state.ZHand)
	if bridgeID == 0 {
		bridgeID = findIn(state.ZLibrary)
		if bridgeID == 0 {
			t.Fatal("Mosswort Bridge is in neither hand nor library")
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: bridgeID, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.priorityRound()
	}
	// Park the Bears at the very top, so the Hideaway window (top 4) offers
	// it first and the pick answer is unambiguous.
	bearID := seatLibraryTop(t, e, 0, "Grizzly Bears")
	// Play the Bridge through the priority decision's play_land option (a
	// real Submit): the Hideaway ask is posed by the kernel-driven entry.
	{
		pd := e.Pending()
		play := -1
		for _, o := range pd.Options {
			if o.Kind == "play_land" && o.Obj == bridgeID {
				play = o.Index
			}
		}
		if play < 0 {
			t.Fatalf("no play_land option for the Bridge: %+v", pd.Options)
		}
		submitChoices(t, e, play)
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KPriority {
		passUntilNonPriority(t, e, 10)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "hideaway" {
		t.Fatalf("after the Bridge entered: %+v, want the Hideaway pick ask", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the parked Bears %d is not among the Hideaway options: %+v", bearID, d.Options)
	}
	submitChoices(t, e, pick)
	// The arrange ask over the remaining three window cards (Min == Max == 3).
	d = e.Pending()
	if d == nil || d.Kind != decision.KArrange {
		t.Fatalf("after the Hideaway pick: %+v, want the arrange ask", d)
	}
	rest := []int{}
	for _, o := range d.Options {
		rest = append(rest, o.Index)
	}
	submitChoices(t, e, rest...)
	// The Bridge entered tapped (ETBTapped). Untap it so the AB$ Play's
	// {T} component is payable in the first leg too -- leg 2 untaps again
	// after its own activation tapped it back.
	e.emit(events.Event{Kind: events.Untap, Obj: bridgeID})

	playIdx := -1
	if o := e.G.Obj(bridgeID); o == nil || o.Face() == nil {
		t.Fatal("the Bridge is not on the battlefield")
	} else {
		for i, sa := range o.Face().Abilities {
			if sa.Kind == "AB" && sa.API == "Play" {
				playIdx = i
			}
		}
	}
	if playIdx < 0 {
		t.Fatal("Mosswort Bridge's compiled face carries no AB$ Play ability")
	}
	return e, cfg, bridgeID, bearID, playIdx
}
