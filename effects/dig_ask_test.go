package effects

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The dig1 look-and-take tests. The pinned primitives_test.go Dig tests pin
// the NO-CHOICE and no-host behaviour; these pin the ask itself, the look
// Note, the Optional$ Min, the re-entry contract and the strict-supersets
// gate, using the askHost double (primitives_test.go) whose Ask captures the
// posed decision and suspends -- the effects-package stand-in for
// rules.Engine.

// digAskFixture builds seat 0's library as [c0 bear, c1 land, c2 land, c3
// bear] and returns (host, ids).
func digAskFixture(t *testing.T) (*askHost, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(bear, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(bear, 0).ID,
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	return h, ids
}

const digAskSA = "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand"

// TestDigAsksWhenTheWindowHoldsMoreEligibleCardsThanChangeNum is the dig1
// core leaf: a window with STRICTLY more ChangeValid$-eligible cards than
// ChangeNum poses a real KChoose to the library's owner -- the look recorded
// first as a Secret Note carrying the window, the options the ELIGIBLE cards
// only, Min 0 (Optional$ honoured) and Max ChangeNum -- and the resolution
// suspends with nothing moved until the answer arrives.
func TestDigAsksWhenTheWindowHoldsMoreEligibleCardsThanChangeNum(t *testing.T) {
	h, ids := digAskFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, digAskSA))
	if h.asked == nil {
		t.Fatal("no decision was posed: a window with more eligible cards than ChangeNum must ask")
	}
	d := h.asked
	if d.Kind != decision.KChoose || d.Player != 0 || d.Min != 0 || d.Max != 1 {
		t.Fatalf("decision = %+v, want a Min==0/Max==1 KChoose for the library's owner", d)
	}
	if d.ResumeKind != "dig" {
		t.Fatalf("ResumeKind = %q, want \"dig\"", d.ResumeKind)
	}
	if !strings.Contains(d.Prompt, "Look at the top 3") {
		t.Fatalf("prompt = %q, want it to name the look in the card's own terms", d.Prompt)
	}
	// The options are the ELIGIBLE cards only, in library order: the bear
	// must not be pickable for a ChangeValid$ Land take.
	if len(d.Options) != 2 || d.Options[0].Kind != "dig" || d.Options[0].Obj != ids[1] || d.Options[1].Obj != ids[2] {
		t.Fatalf("options = %+v, want the two lands in library order, Kind \"dig\"", d.Options)
	}
	// The look is recorded BEFORE the ask, as a Secret Note to the owner
	// carrying the whole window, and nothing has moved yet.
	var look *events.Event
	for i := range h.log {
		e := &h.log[i]
		if e.Kind == events.Note && e.Text == "looks at the top of the library" {
			look = e
			break
		}
	}
	if look == nil {
		t.Fatalf("no look Note in %+v", h.log)
	}
	if !look.Secret || look.Player != 0 {
		t.Fatalf("look Note = %+v, want Secret to its owner (seat 0)", look)
	}
	if len(look.IDs) != 3 || look.IDs[0] != ids[0] || look.IDs[1] != ids[1] || look.IDs[2] != ids[2] {
		t.Fatalf("look Note IDs = %v, want the full 3-card window %v", look.IDs, ids[:3])
	}
	if len(h.g.Zone(state.ZHand, 0)) != 0 {
		t.Fatal("cards moved before the answer: the ask must suspend the resolution")
	}
	// Re-entry, the engine's contract: Ctx.Dig/DigDone carry the chosen
	// objects (here the SECOND land, to prove the choice is honoured, not a
	// first-eligible default), the move is Secret to the owner, and the
	// passed Ctx is consumed and cleared (the fx42 scoping discipline).
	ctx := &Ctx{Controller: 0, Dig: []state.ObjID{ids[2]}, DigDone: true}
	Resolve(h, ctx, sa(t, digAskSA))
	if ctx.Dig != nil || ctx.DigDone {
		t.Fatal("re-entry left Ctx.Dig/DigDone set: the answer field must be consumed and cleared")
	}
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 1 || hand[0] != ids[2] {
		t.Fatalf("hand = %v, want [%d] (the SECOND land, the one the player picked)", hand, ids[2])
	}
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 3 || lib[0] != ids[0] || lib[1] != ids[1] || lib[2] != ids[3] {
		t.Fatalf("library = %v, want [%d %d %d], the rest on top in their existing order", lib, ids[0], ids[1], ids[3])
	}
	found := false
	for _, e := range h.log {
		if e.Kind == events.MoveZone && e.Obj == ids[2] && e.Secret && e.Player == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("the answered take's MoveZone is not a Secret move naming the library's owner")
	}
}

// TestDigBottomChoiceExplainsBothOutcomes pins the one-card "may put it on
// the bottom" shape used by cards such as Jace, the Mind Sculptor.  It is a
// KChoose with an empty answer meaning keep, so the prompt and option must not
// leave a player guessing which result clicking a card produces.
func TestDigBottomChoiceExplainsBothOutcomes(t *testing.T) {
	h, ids := digAskFixture(t)
	effect := sa(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | Optional$ True | ChangeValid$ Land | DestinationZone$ Library | LibraryPosition$ -1 | SkipReorder$ True")
	Resolve(h, &Ctx{Controller: 0}, effect)
	if h.asked == nil {
		t.Fatal("no decision was posed")
	}
	d := h.asked
	if got, want := d.Prompt, "Look at the top 3 card(s) of your library: Select up to 1 matching card(s) to put on the bottom of your library. Leave unselected card(s) on top."; got != want {
		t.Fatalf("prompt = %q, want %q", got, want)
	}
	if len(d.Options) != 2 || d.Options[0].Label != "Put Isle on bottom" || d.Options[1].Label != "Put Isle on bottom" {
		t.Fatalf("options = %+v, want explicit bottom actions", d.Options)
	}

	// Selecting the second eligible card puts that card on the bottom.  The
	// first eligible card remains in its original top-of-library position.
	Resolve(h, &Ctx{Controller: 0, Dig: []state.ObjID{ids[2]}, DigDone: true}, effect)
	if got, want := h.g.Zone(state.ZLibrary, 0), []state.ObjID{ids[0], ids[1], ids[3], ids[2]}; !slices.Equal(got, want) {
		t.Fatalf("library = %v, want selected card on bottom and unselected card on top %v", got, want)
	}
}

// TestDigOptionalDeclineMovesNothing is the Optional$ leaf on the answer
// side: an answered ask carrying NO cards (Min 0 lets the player decline the
// take) moves nothing and leaves the whole window in place.
func TestDigOptionalDeclineMovesNothing(t *testing.T) {
	h, ids := digAskFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, digAskSA))
	if h.asked == nil {
		t.Fatal("no decision was posed")
	}
	Resolve(h, &Ctx{Controller: 0, DigDone: true}, sa(t, digAskSA))
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 4 || lib[0] != ids[0] || lib[3] != ids[3] {
		t.Fatalf("library = %v, want the decline to move nothing", lib)
	}
	if len(h.g.Zone(state.ZHand, 0)) != 0 {
		t.Fatal("a declined Optional take moved a card into hand")
	}
}

// TestDigMandatoryAskMinsAtChangeNum is the Optional$-absent leaf: without
// Optional$ the take is mandatory, so the ask's Min is ChangeNum (the
// answer must take that many) rather than 0.
func TestDigMandatoryAskMinsAtChangeNum(t *testing.T) {
	h, _ := digAskFixture(t)
	Resolve(h, &Ctx{Controller: 0},
		sa(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | ChangeValid$ Land | DestinationZone$ Hand"))
	if h.asked == nil {
		t.Fatal("no decision was posed")
	}
	if h.asked.Min != 1 || h.asked.Max != 1 {
		t.Fatalf("Min/Max = %d/%d, want 1/1 for a mandatory take of 1", h.asked.Min, h.asked.Max)
	}
}

// TestDigStaysSilentWhenTheWindowHoldsNoChoice pins the strict-supersets
// gate's TAKE half: a window whose eligible count EQUALS ChangeNum admits no
// real pick, so no take decision is posed and no look Note for the take ask is
// emitted (with only one untaken card there is also no ordered-bottom ask).
// The silent M1 take runs (first ChangeNum eligible in zone order move), but
// the default remainder STILL moves the untaken card to the bottom -- so this
// is not the byte-identical pre-dig1 engine: only a window whose remainder
// cannot move would replay that way.
func TestDigOptionalAsksEvenWhenTheEligibleSetFitsTheCap(t *testing.T) {
	h, ids := digAskFixture(t)
	saLine := "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 2 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand | SkipReorder$ True"
	Resolve(h, &Ctx{Controller: 0}, sa(t, saLine))
	if h.asked == nil || h.asked.Kind != decision.KChoose || h.asked.Min != 0 || h.asked.Max != 2 {
		t.Fatalf("decision = %+v, want optional 0..2 take ask", h.asked)
	}
	h.asked = nil
	Resolve(h, &Ctx{Controller: 0, DigDone: true}, sa(t, saLine))
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 0 {
		t.Fatalf("hand = %v, want empty after the optional decline", hand)
	}
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != len(ids) || lib[0] != ids[0] || lib[2] != ids[2] {
		t.Fatalf("library = %v, want the unchanged window after decline", lib)
	}
}

// TestDigNoHostFallbackKeepsTheFirstEligibleWithANote pins the R-9 fallback:
// a host that cannot answer keeps today's behaviour -- the first ChangeNum
// eligible cards in zone order -- with the Note that records why the richer
// path did not run (after the look Note the ask itself emitted).
func TestDigNoHostFallbackKeepsTheFirstEligibleWithANote(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(bear, 0).ID,
		h.g.AddObject(land, 0).ID,
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	Resolve(h, &Ctx{Controller: 0}, sa(t, digAskSA))
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 1 || hand[0] != ids[0] {
		t.Fatalf("hand = %v, want [%d] (the FIRST eligible card, the deterministic stand-in)", hand, ids[0])
	}
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 2 || lib[0] != ids[1] || lib[1] != ids[2] {
		t.Fatalf("library = %v, want [%d %d]", lib, ids[1], ids[2])
	}
	found := false
	for _, e := range h.log {
		if e.Kind == events.Note && strings.Contains(e.Text, "no engine host to ask") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no fallback Note in %+v", h.log)
	}
}

// TestDigNoHostFallbackNoteNamesTheLibraryOwner is the r2 minor finding: the
// fallback Note is Secret, so it must carry Player = the library's owner;
// without it a Dig of another player's library routed that private note to
// seat 0 (the zero value), the one reader the note must NOT reach.
func TestDigNoHostFallbackNoteNamesTheLibraryOwner(t *testing.T) {
	h := newHost(t, 2)
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(land, 1).ID,
		h.g.AddObject(bear, 1).ID,
		h.g.AddObject(land, 1).ID,
	}
	h.g.SetZone(state.ZLibrary, 1, ids)
	Resolve(h, &Ctx{Controller: 1}, sa(t, digAskSA))
	if hand := h.g.Zone(state.ZHand, 1); len(hand) != 1 || hand[0] != ids[0] {
		t.Fatalf("seat 1 hand = %v, want [%d] (the deterministic fallback take)", hand, ids[0])
	}
	var fallback *events.Event
	for i := range h.log {
		e := &h.log[i]
		if e.Kind == events.Note && strings.Contains(e.Text, "no engine host to ask") {
			fallback = e
			break
		}
	}
	if fallback == nil {
		t.Fatalf("no fallback Note in %+v", h.log)
	}
	if !fallback.Secret || fallback.Player != 1 {
		t.Fatalf("fallback Note = %+v, want Secret to the library's owner (seat 1)", fallback)
	}
}

// TestDigReentryIgnoresIdsOutsideTheWindow keeps a stray or stale answer
// from moving an object that is not (or no longer) in the target's look
// window -- the per-window filter, the mirror of effDiscard's per-hand
// filter on its own re-entry.
func TestDigReentryIgnoresIdsOutsideTheWindow(t *testing.T) {
	h, ids := digAskFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, digAskSA))
	if h.asked == nil {
		t.Fatal("no decision was posed")
	}
	// The answer names the bear at index 3 (outside DigNum$ 3's window) and
	// a bogus id; neither may move. The in-window land at index 1 does.
	Resolve(h, &Ctx{Controller: 0, Dig: []state.ObjID{ids[3], 999, ids[1]}, DigDone: true},
		sa(t, digAskSA))
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 1 || hand[0] != ids[1] {
		t.Fatalf("hand = %v, want [%d] (only the in-window pick moved)", hand, ids[1])
	}
}

// TestDigZeroChangeNumSkipsTheTakeAsk is the ChangeNum$ 0 regression (r2
// finding): a zero cap takes nothing, so a Dig with eligible cards in its
// window must not pose the Min==Max==0 KChoose (whose only legal answer is
// the empty one) nor emit the look Note FOR THE TAKE. The default bottom
// remainder is still a real order choice over the whole window (sanity
// grinding's own Oracle: "put the cards you revealed this way on the bottom
// of your library in any order"), so the ordered-bottom KArrange poses, and
// the simulated answer bottoms the window in the chosen order.
func TestDigZeroChangeNumSkipsTheTakeAsk(t *testing.T) {
	h, ids := digAskFixture(t)
	Resolve(h, &Ctx{Controller: 0},
		sa(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 0 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand"))
	if h.asked == nil || h.asked.Kind != decision.KArrange {
		t.Fatalf("decision = %+v, want the ordered-bottom KArrange (no take KChoose for ChangeNum$ 0)", h.asked)
	}
	if h.asked.Min != 3 || h.asked.Max != 3 || len(h.asked.Options) != 3 || h.asked.Options[0].Kind != "dig_bottom" {
		t.Fatalf("arrange = Min %d Max %d options %+v, want Min==Max==3 over the whole window with dig_bottom kinds",
			h.asked.Min, h.asked.Max, h.asked.Options)
	}
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 0 {
		t.Fatalf("hand = %v, want empty (ChangeNum$ 0 takes nothing)", hand)
	}
	// Simulate the engine: apply the answered order and re-enter.
	want := []state.ObjID{ids[3], ids[2], ids[0], ids[1]}
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: want, Secret: true})
	Resolve(h, &Ctx{Controller: 0, Arrange: true, ArrangeTarget: 0},
		sa(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 0 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand"))
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 4 || lib[0] != want[0] || lib[1] != want[1] || lib[2] != want[2] || lib[3] != want[3] {
		t.Fatalf("library = %v, want %v (the window bottomed in the answered order)", lib, want)
	}
}

// TestDigLookNoteIsSecretToItsOwner is the multi-seat leaf: a Dig resolved
// against seat 1's library records its look (when it asks) and its take as
// Secret events naming seat 1, never seat 0.
func TestDigLookNoteIsSecretToItsOwner(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(land, 1).ID,
		h.g.AddObject(bear, 1).ID,
		h.g.AddObject(land, 1).ID,
	}
	h.g.SetZone(state.ZLibrary, 1, ids)
	Resolve(h, &Ctx{Controller: 1},
		sa(t, "SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | Optional$ True | ChangeValid$ Land | DestinationZone$ Hand"))
	if h.asked == nil || h.asked.Player != 1 {
		t.Fatalf("decision = %+v, want one posed to seat 1, the library's owner", h.asked)
	}
	for _, e := range h.log {
		switch e.Kind {
		case events.Note:
			if !e.Secret {
				continue
			}
			if e.Player != 1 {
				t.Fatalf("Secret Note has Player %d, want the library's owner (1): %+v", e.Player, e)
			}
		case events.MoveZone:
			if !e.Secret {
				t.Fatalf("Dig MoveZone not Secret: %+v", e)
			}
			if e.Player != 1 {
				t.Fatalf("Secret MoveZone has Player %d, want the library's owner (1): %+v", e.Player, e)
			}
		}
	}
}

// TestDigMultiPlayerResumeKeepsEveryLibrary is the r3 multi-target regression.
// Seat 0's no-choice window completes before seat 1 poses the first real ask;
// re-entry must skip that completed library, apply the answer to seat 1, and
// preserve M1's deterministic first-eligible take for seat 2 rather than
// abandoning it. ResumeTarget/DigTarget are indices, not player guesses, so
// the same rule also covers multiple object targets mapping to one controller.
func TestDigMultiPlayerResumeKeepsEveryLibrary(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(3))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	libs := make([][]state.ObjID, 3)
	for p := state.PlayerID(0); p < 3; p++ {
		libs[p] = []state.ObjID{
			h.g.AddObject(land, p).ID,
			h.g.AddObject(bear, p).ID,
			h.g.AddObject(land, p).ID,
		}
		h.g.SetZone(state.ZLibrary, p, libs[p])
	}
	// Seat 0 has exactly one eligible card, so it completes without asking.
	// Seats 1 and 2 each have a strict-superset choice.
	h.g.SetZone(state.ZLibrary, 0, libs[0][:2])
	effect := sa(t, "SP$ Dig | Defined$ Player | DigNum$ 3 | ChangeNum$ 1 | ChangeValid$ Land | DestinationZone$ Hand")
	Resolve(h, &Ctx{Controller: 0}, effect)
	if h.asked == nil || h.asked.Player != 1 || h.asked.ResumeTarget != 1 {
		t.Fatalf("decision = %+v, want seat 1 at Defined$ target index 1", h.asked)
	}
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 1 || hand[0] != libs[0][0] {
		t.Fatalf("seat 0 hand = %v, want its completed no-choice take [%d]", hand, libs[0][0])
	}
	// Seat 0's one-card remainder took the default bottom destination without
	// an ask (one card has one possible order) and its library was already
	// the whole window, so the move changed nothing.
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 1 || lib[0] != libs[0][1] {
		t.Fatalf("seat 0 library = %v, want [%d] (the bear stayed, already at the bottom)", lib, libs[0][1])
	}
	if len(h.g.Zone(state.ZHand, 2)) != 0 {
		t.Fatal("seat 2 was processed before seat 1's ask suspended the effect")
	}

	picked := libs[1][2] // choose seat 1's SECOND eligible card.
	h.asked = nil
	ctx := &Ctx{Controller: 0, Dig: []state.ObjID{picked}, DigDone: true, DigTarget: 1}
	Resolve(h, ctx, effect)
	if ctx.Dig != nil || ctx.DigDone || ctx.DigTarget != 0 {
		t.Fatalf("re-entry fields were not consumed: Dig=%v Done=%v Target=%d", ctx.Dig, ctx.DigDone, ctx.DigTarget)
	}
	// The take ask is answered; the re-entry moves seat 1's pick, then seat
	// 1's own two-card remainder poses the ordered-bottom ask (a real order
	// choice), which suspends the walk BEFORE any later target is processed.
	if h.asked == nil || h.asked.Kind != decision.KArrange || h.asked.Player != 1 || h.asked.ResumeTarget != 1 {
		t.Fatalf("decision = %+v, want seat 1's ordered-bottom arrange after its take", h.asked)
	}
	if h.asked.Min != 2 || h.asked.Max != 2 {
		t.Fatalf("arrange Min/Max = %d/%d, want 2/2 (a full permutation over the two-card remainder)", h.asked.Min, h.asked.Max)
	}
	if hand := h.g.Zone(state.ZHand, 0); len(hand) != 1 || hand[0] != libs[0][0] {
		t.Fatalf("seat 0 was processed twice on re-entry: hand %v", hand)
	}
	if hand := h.g.Zone(state.ZHand, 1); len(hand) != 1 || hand[0] != picked {
		t.Fatalf("seat 1 hand = %v, want its answered second eligible card [%d]", hand, picked)
	}
	if len(h.g.Zone(state.ZHand, 2)) != 0 {
		t.Fatal("seat 2 was processed before seat 1's arrange suspended the effect")
	}

	// Simulate the engine: handleArrange applies the answered bottom order
	// (seat 1's untouched library is empty -- the window WAS the library),
	// then re-enters with the arrange cursor at seat 1's index; the walk
	// resumes at seat 2 and keeps ITS deterministic processing -- the greedy
	// take, then its own arrange ask (never dropped).
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: 1, IDs: []state.ObjID{libs[1][1], libs[1][0]}, Secret: true})
	h.asked = nil
	Resolve(h, &Ctx{Controller: 0, Arrange: true, ArrangeTarget: 1}, effect)
	if lib := h.g.Zone(state.ZLibrary, 1); len(lib) != 2 || lib[0] != libs[1][1] || lib[1] != libs[1][0] {
		t.Fatalf("seat 1 library = %v, want the answered bottom order [%d %d]", lib, libs[1][1], libs[1][0])
	}
	if h.asked == nil || h.asked.Kind != decision.KArrange || h.asked.Player != 2 || h.asked.ResumeTarget != 2 {
		t.Fatalf("decision = %+v, want seat 2's own ordered-bottom arrange on the resumed walk", h.asked)
	}
	if hand := h.g.Zone(state.ZHand, 2); len(hand) != 1 || hand[0] != libs[2][0] {
		t.Fatalf("seat 2 hand = %v, want deterministic later-target take [%d]", hand, libs[2][0])
	}
	// Seat 2's arrange answered the same way completes the effect.
	h.Emit(events.Event{Kind: events.LibraryOrder, Player: 2, IDs: []state.ObjID{libs[2][2], libs[2][1]}, Secret: true})
	Resolve(h, &Ctx{Controller: 0, Arrange: true, ArrangeTarget: 2}, effect)
	if lib := h.g.Zone(state.ZLibrary, 2); len(lib) != 2 || lib[0] != libs[2][2] || lib[1] != libs[2][1] {
		t.Fatalf("seat 2 library = %v, want the answered bottom order [%d %d]", lib, libs[2][2], libs[2][1])
	}
}

// TestDigTakeResumeContinuesToLaterLibrary is the multi-target take-resume
// continuation: a Dig over several libraries where an EARLIER target's take
// answer resumes the walk must continue to a LATER library and pose ITS own
// take ask -- never silently complete it with the deterministic
// first-eligible take. Each window is two cards so the answered target's
// remainder is a single card (exactly one possible order): that keeps the
// ordered-bottom arrange ask out of the path, leaving the take ask itself as
// the only continuation under test.
func TestDigTakeResumeContinuesToLaterLibrary(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(3))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	// Seats 1 and 2 each hold two eligible lands, so each poses its own take
	// ask; seat 0 holds one eligible land and a nonland (no choice), so the
	// first pass completes it deterministically and reaches seat 1.
	libs := make([][]state.ObjID, 3)
	for p := state.PlayerID(1); p < 3; p++ {
		libs[p] = []state.ObjID{
			h.g.AddObject(land, p).ID,
			h.g.AddObject(land, p).ID,
		}
		h.g.SetZone(state.ZLibrary, p, libs[p])
	}
	libs[0] = []state.ObjID{
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(bear, 0).ID,
	}
	h.g.SetZone(state.ZLibrary, 0, libs[0])
	// Precondition: the later libraries' two cards are distinct object ids, so
	// an assertion cannot pass by comparing identical values; seat 0 has
	// exactly one eligible card, so it presents no take ask.
	if libs[1][0] == libs[1][1] || libs[2][0] == libs[2][1] {
		t.Fatal("precondition: the later libraries must hold distinct eligible cards")
	}
	effect := sa(t, "SP$ Dig | Defined$ Player | DigNum$ 2 | ChangeNum$ 1 | ChangeValid$ Land | DestinationZone$ Hand")
	Resolve(h, &Ctx{Controller: 0}, effect)
	if h.asked == nil || h.asked.Player != 1 || h.asked.ResumeTarget != 1 {
		t.Fatalf("first decision = %+v, want seat 1's take ask at target index 1", h.asked)
	}
	if len(h.g.Zone(state.ZHand, 2)) != 0 {
		t.Fatal("seat 2 was processed before seat 1's ask suspended the effect")
	}
	picked := libs[1][1]
	h.asked = nil
	Resolve(h, &Ctx{Controller: 0, Dig: []state.ObjID{picked}, DigDone: true, DigTarget: 1}, effect)
	// The continuation: seat 2's library keeps its own take ask instead of
	// being completed with the deterministic first-eligible take.
	if h.asked == nil || h.asked.Player != 2 || h.asked.ResumeTarget != 2 {
		t.Fatalf("later library did not get its own take ask: %+v", h.asked)
	}
	if h.asked.Kind != decision.KChoose {
		t.Fatalf("later decision kind = %v, want the take KChoose", h.asked.Kind)
	}
	if hand := h.g.Zone(state.ZHand, 1); len(hand) != 1 || hand[0] != picked {
		t.Fatalf("seat 1 hand = %v, want its answered card [%d]", hand, picked)
	}
	if len(h.g.Zone(state.ZHand, 2)) != 0 {
		t.Fatal("seat 2 was completed before its own ask was answered")
	}
	laterPicked := libs[2][1]
	h.asked = nil
	Resolve(h, &Ctx{Controller: 0, Dig: []state.ObjID{laterPicked}, DigDone: true, DigTarget: 2}, effect)
	if h.asked != nil {
		t.Fatalf("after the last library answer, another ask remained: %+v", h.asked)
	}
	if hand := h.g.Zone(state.ZHand, 2); len(hand) != 1 || hand[0] != laterPicked {
		t.Fatalf("seat 2 hand = %v, want its answered card [%d]", hand, laterPicked)
	}
}
