package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The api:Play parameter-family pin (ticket agent-20260918T221252Z-d504b33b)
// on REAL compiled corpus cards. The brief's four parameters:
//
//   - WithoutManaCost$ True gates the free cast end to end (rules'
//     resumeResolution "play" arm -> beginPlay): the played card's printed
//     mana cost is never charged. Both tests exercise the flag; Mosswort's
//     leg also proves the NEGATIVE direction implicitly -- the pool carries
//     no mana beyond the activation's pip, so a wrongly-charged {1}{G} cast
//     could not commit and the Bears would never reach the battlefield.
//   - Controller$ routes the ask (effPlay) and the begun cast (the resume
//     arm reads the answer's Option.Player). The corpus's dominant spelling
//     is `Controller$ You` (125 raw lines), which must be an exact identity:
//     the ask stays with the resolving controller. The fail-closed arm (an
//     unresolvable value) is pinned synthetically in
//     TestPlayControllerUnresolvableNamesNobody.
//   - ShowCards$ (Sunbird's Invocation, the corpus's one carrier) emits the
//     play's public reveal Note before the cast moves the card out of its
//     hidden zone.
//   - ForgetPlayed$ (Vaan, Street Thief; Sunbird's Invocation) drops the
//     played card from the remembered set so the chained
//     "unplayed remainder" arm (Sunbird's DBRestRandomOrder) moves only
//     what was NOT played.
//
// Supporting fix in the same ticket: Sunbird's Invocation's own
// `SVar:X:TriggeredSpellAbility$CardManaCostLKI` (51 raw corpus lines carry
// this ref-property spelling) evaluated to 0 before the alias read in
// effects/count.go's evalRefProperty, so the whole real-card chain was inert
// (PeekAmount 0 -> empty window). The alias makes X the cast spell's mana
// value and this file pins the chain end to end on the real card.
//
// Both protagonists (Mosswort Bridge, Sunbird's Invocation) are in NO repo
// deck and NO legacy golden deck (measured against internal/testutil/decks),
// so no chain head depends on them and TestHeads is safe by construction.
//
// The helpers come from cast_test.go / search_library_test.go /
// vaan_forget_played_test.go (same package): the decks are built from
// compiled corpus cards only, so no Forge script text is committed here.

// mosswortEngine deals seat 0 a 40-card deck holding Mosswort Bridge, two
// Craw Wurms (6/4 each -- 12 total power, past the bridge's GE10 gate) and
// one Grizzly Bears, parks the Bears at the top of the library, plays the
// Bridge onto the battlefield (the raw emit keeps the Hideaway asks alive),
// and answers both of them -- the Bears is exiled face down with the Bridge
// as its exiling source, the rest of the window is arranged to the bottom.
// It returns everything the two legs need: the engine, the config, the
// Bridge id, the exiled Bears id, and the AB$ Play ability's index.
func mosswortEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID, int) {
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
	// Play the Bridge: the raw emit (searchMoveByName would clear the
	// pending ask the Hideaway ETB replacement immediately poses). The
	// enter is tapped (ETBTapped) and the Hideaway ask is pending.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bridgeID, From: state.ZHand, To: state.ZBattlefield})
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

// TestMosswortBridgeGateAndCostedFreePlay walks the REAL card end to end.
// Leg 1: with zero power the AB$ Play activates (the offer has no
// condition gate -- the SVar gate lives at resolution), the GE10 gate fails,
// and no play ask ever appears. Leg 2: after two Craw Wurms (12 power) the
// same activation poses the play ask, the answered free cast moves the
// exiled Bears from exile onto the battlefield, and the printed {1}{G} is
// never charged (the pool carries only what the activations spent).
func TestMosswortBridgeGateAndCostedFreePlay(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, bridgeID, bearID, playIdx := mosswortEngine(t, reg)
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
func TestPlayControllerUnresolvableNamesNobody(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _, bridgeID, bearID, playIdx := mosswortEngine(t, reg)
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

// sunbirdEngine deals seat 0 a 40-card deck holding Sunbird's Invocation,
// two Grizzly Bears and one Mountain among the Forests, casts the
// Invocation, parks one Bears (the play candidate) and the Mountain at the
// library top in that exact order, and keeps the other Bears in hand for
// the trigger's cast. It returns the engine, config, the cast Bears id, the
// library-top Bears id, and the Mountain id.
func sunbirdEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck0 := []*cards.Card{searchCorpusCard(t, reg, "Sunbird's Invocation"), bear, bear, mountain}
	for i := 0; i < 18; i++ {
		deck0 = append(deck0, forest)
	}
	deck1 := make([]*cards.Card, 0, 40)
	for i := 0; i < 40; i++ {
		deck1 = append(deck1, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 7427, Names: []string{"sunbird", "opponent"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	findAll := func(names ...string) map[string][]state.ObjID {
		out := map[string][]state.ObjID{}
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil {
					continue
				}
				for _, n := range names {
					if o.Face().Name == n {
						out[n] = append(out[n], id)
						if z == state.ZHand {
							out[n+"@hand"] = append(out[n+"@hand"], id)
						}
					}
				}
			}
		}
		return out
	}
	moveToHand := func(name string) state.ObjID {
		for _, id := range e.G.Zone(state.ZLibrary, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
		t.Fatalf("no %q left in the library to move to hand", name)
		return 0
	}
	// The Invocation in hand, one Bears in hand (the triggering cast), and
	// the second Bears plus the Mountain parked at the library top in that
	// exact order -- the peek window (X = the cast spell's mana value 2) is
	// exactly those two, one of which is not a spell and must be filtered
	// out of the play candidates by Valid$ Card.nonLand.
	loc := findAll("Sunbird's Invocation", "Grizzly Bears", "Mountain")
	if len(loc["Sunbird's Invocation@hand"]) == 0 {
		moveToHand("Sunbird's Invocation")
		loc = findAll("Sunbird's Invocation", "Grizzly Bears", "Mountain")
	}
	if len(loc["Grizzly Bears@hand"]) == 0 {
		moveToHand("Grizzly Bears")
		loc = findAll("Sunbird's Invocation", "Grizzly Bears", "Mountain")
	}
	bearCast := loc["Grizzly Bears@hand"][0]
	bearTop := state.ObjID(0)
	for _, id := range loc["Grizzly Bears"] {
		if id != bearCast {
			bearTop = id
		}
	}
	if bearTop == 0 {
		t.Fatal("no second Grizzly Bears for the library window")
	}
	mtnID := state.ObjID(0)
	for _, id := range loc["Mountain"] {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZLibrary {
			mtnID = id
			break
		}
	}
	if mtnID == 0 {
		t.Fatal("no Mountain in the library for the window")
	}
	// Exact library order: [bearTop, mtn, everything else]. Any window card
	// stranded in hand first moves back (logged).
	if o := e.G.Obj(bearTop); o.Zone != state.ZLibrary {
		e.emit(events.Event{Kind: events.MoveZone, Obj: bearTop, From: o.Zone, To: state.ZLibrary})
	}
	if o := e.G.Obj(mtnID); o.Zone != state.ZLibrary {
		e.emit(events.Event{Kind: events.MoveZone, Obj: mtnID, From: o.Zone, To: state.ZLibrary})
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	want := []state.ObjID{bearTop, mtnID}
	in := map[state.ObjID]bool{bearTop: true, mtnID: true}
	for _, id := range lib {
		if !in[id] {
			want = append(want, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: want, Secret: true})
	return e, cfg, bearCast, bearTop, mtnID
}

// castDrain submits the cast option for id and passes until the stack is
// empty, leaving the game at the same step's plain priority (castFixture's
// passUntilNonPriority would pass clean turns into the next cleanup step,
// where an 8-card hand poses a discard ask this test does not want).
func castDrain(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 40)
}

// TestSunbirdInvocationPlayThenRandomRemainder walks the REAL card end to
// end: cast the Invocation, cast a Bears from hand (the trigger fires, X =
// the Bears' mana value 2), the peek reveals and remembers the top two
// library cards [Bears, Mountain], the chained DB$ Play offers only the
// spell-shaped candidate, the answered free cast moves it onto the
// battlefield, ShowCards$ emits the play's own public reveal Note,
// ForgetPlayed$ drops the played card, and the DBRestRandomOrder tail moves
// only the unplayed Mountain to the library bottom.
func TestSunbirdInvocationPlayThenRandomRemainder(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, bearCast, bearTop, mtnID := sunbirdEngine(t, reg)
	addMana(t, e, 0, "RRRRRRGG")
	sunbirdID := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Sunbird's Invocation" {
			sunbirdID = id
		}
	}
	if sunbirdID == 0 {
		t.Fatal("precondition: Sunbird's Invocation is not in hand")
	}
	castDrain(t, e, sunbirdID)
	if o := e.G.Obj(sunbirdID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the Invocation is %v, want on the battlefield", o)
	}
	// The triggering cast: the second Bears, from hand, for X = 2. The
	// trigger resolves right after the cast (the ask arrives while the
	// Bears spell is still on the stack below it), so the drain and the
	// catch are one passUntilNonPriority.
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bearCast {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the triggering Bears %d: %+v", bearCast, d.Options)
	}
	submitChoices(t, e, idx)
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("after the triggering cast: %+v, want the DB$ Play KModes ask", d)
	}
	if d.Player != 0 || len(d.Options) != 1 || d.Options[0].Obj != bearTop {
		t.Fatalf("play ask = player %d options %+v, want seat 0 offered exactly the library-top Bears %d", d.Player, d.Options, bearTop)
	}
	// Precondition for the ShowCards comparison: the peek already emitted
	// exactly one public reveal Note carrying the window [Bears, Mountain].
	window := []state.ObjID{bearTop, mtnID}
	sameIDs := func(a []state.ObjID) bool {
		if len(a) != len(window) {
			return false
		}
		for i := range a {
			if a[i] != window[i] {
				return false
			}
		}
		return true
	}
	peekNotes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && !ev.Secret && ev.Player == 0 && sameIDs(ev.IDs) {
			peekNotes++
		}
	}
	if peekNotes != 1 {
		t.Fatalf("public window-reveal Notes before the play answer = %d, want exactly 1 (the peek's own)", peekNotes)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(bearTop); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the played Bears is %v, want on the battlefield (cast free from the library)", o)
	}
	// ShowCards$ emitted the play's own reveal: exactly one MORE public
	// Note carrying the played card than the peek's (the play's population
	// is the whole walk Remembered -- the triggering cast card rides in it
	// beside the peek's window -- so the note is a superset of the window,
	// not an exact copy), and it landed while the cards were still in the
	// library (before the cast's moves in the log).
	total := 0
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note || ev.Secret || ev.Player != 0 {
			continue
		}
		for _, id := range ev.IDs {
			if id == bearTop {
				total++
				break
			}
		}
	}
	if total != 2 {
		t.Fatalf("public Notes carrying the played card after the play = %d, want 2 (the peek's + ShowCards$'s)", total)
	}
	// The unplayed remainder: the Mountain is now the BOTTOM card of the
	// library (LibraryPosition$ -1), and the played Bears is gone from it.
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 || lib[len(lib)-1] != mtnID {
		t.Fatalf("library tail = %v..., want the unplayed Mountain %d at the bottom (DBRestRandomOrder's remainder move)", lib[maxInt(0, len(lib)-3):], mtnID)
	}
	for _, id := range lib {
		if id == bearTop {
			t.Fatal("the played Bears is still in the library; the cast never moved it")
		}
	}
	replayCheck(t, e, cfg)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
