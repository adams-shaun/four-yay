package effects

// The DigUntil withheld-rider tests (issue agent-20260923T042552Z-0936e140):
// the Amount$ SVar, Shuffle$/ShuffleCondition$, NoMoveFound$/
// FoundLibraryPosition$, ImprintFound$/ImprintRevealed$ and NoneFound*$
// riders effDigUntil used to log a loud Note for. Inline mechanics use the
// askHost/fakeHost doubles; where a real corpus carrier exists the test
// drives that carrier's own compiled SA through CorpusRegistry and asserts
// the carrier's params first (a vacuous setup must fail loudly).

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpusRiderSA returns the REAL compiled SA named by svarName (or the first
// ability when svarName is empty) together with the face's SVar table, which
// the resolving Ctx must carry for an SVar-amount rider to resolve.
func corpusRiderSA(t *testing.T, cardName, svarName string) (*cards.Card, *cards.SA, map[string]string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(cardName)
	if !ok {
		t.Fatalf("corpus has no %s", cardName)
	}
	for _, f := range c.Faces {
		if svarName == "" {
			if len(f.Abilities) > 0 {
				return c, f.Abilities[0], f.SVars
			}
			continue
		}
		if s := cards.ResolveSVar(f.SVars, svarName); s != nil {
			return c, s, f.SVars
		}
	}
	t.Fatalf("corpus %s has no %s", cardName, svarName)
	return nil, nil, nil
}

const (
	riderLand  = "Name:Isle\nTypes:Basic Land Island\nOracle:x\n"
	riderBear  = "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"
	riderHalo  = "Name:Halo\nManaCost:W\nTypes:Enchantment Aura\nK:Enchant:Creature\nOracle:x\n"
	riderSurge = "Name:Surge\nTypes:Sorcery\nOracle:x\n"
)

// riderBoard builds a board with a resolving source permanent on seat 0's
// battlefield and seat 0's library in the given card-source order. It returns
// the host, the board source id and the library ids (index 0 = top).
func riderBoard(t *testing.T, lib ...string) (*askHost, state.ObjID, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	src := h.g.AddObject(mkCard(t, "Name:Relic\nTypes:Artifact\nOracle:x\n"), 0).ID
	bearer := h.g.AddObject(mkCard(t, riderBear), 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src, bearer})
	var ids []state.ObjID
	for _, s := range lib {
		ids = append(ids, h.g.AddObject(mkCard(t, s), 0).ID)
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	return h, src, ids
}

// riderShuffles returns the Secret Shuffle events emitted to player p.
func riderShuffles(h *askHost, p state.PlayerID) []events.Event {
	var out []events.Event
	for _, e := range h.log {
		if e.Kind == events.Shuffle && e.Player == p {
			out = append(out, e)
		}
	}
	return out
}

// riderImprints returns every Imprint event emitted on obj.
func riderImprints(h *askHost, obj state.ObjID) []events.Event {
	var out []events.Event
	for _, e := range h.log {
		if e.Kind == events.Imprint && e.Obj == obj {
			out = append(out, e)
		}
	}
	return out
}

func riderMoved(h *askHost, id state.ObjID) bool {
	for _, e := range h.log {
		if e.Kind == events.MoveZone && e.Obj == id {
			return true
		}
	}
	return false
}

func riderSet(ids []state.ObjID) map[state.ObjID]int {
	m := map[state.ObjID]int{}
	for _, id := range ids {
		m[id]++
	}
	return m
}

// --- Amount$ non-literal SVar -------------------------------------------

// TestDigUntilAmountSVarCountsMatchesToTheTally drives the REAL Mass
// Polymorph DBMassReveal SA (Amount$ MassX, SVar:MassX:Remembered$Amount):
// with two remembered objects the scan must find TWO creatures, not one.
func TestDigUntilAmountSVarCountsMatchesToTheTally(t *testing.T) {
	_, sa, svars := corpusRiderSA(t, "Mass Polymorph", "DBMassReveal")
	if got := sa.API; got != "DigUntil" || sa.Params["Amount"] != "MassX" {
		t.Fatalf("precondition: Mass Polymorph DBMassReveal = API %q Amount %q, want DigUntil/MassX", got, sa.Params["Amount"])
	}
	h, _, ids := riderBoard(t, riderLand, riderBear, riderLand, riderBear, riderLand)
	// Two remembered creatures: the tally the rider must read.
	rem1 := h.g.AddObject(mkCard(t, riderBear), 0).ID
	rem2 := h.g.AddObject(mkCard(t, riderBear), 0).ID
	c := &Ctx{Controller: 0, SVars: svars, Remembered: []state.Target{{Obj: rem1}, {Obj: rem2}}}
	if n, ok := EvalCountOK(h, c, "Remembered$Amount"); !ok || n != 2 {
		t.Fatalf("precondition: Remembered$Amount = (%d,%v), want (2,true): the tally must actually be two", n, ok)
	}
	Resolve(h, c, sa)
	// Both creatures found and moved to the battlefield; every land stays.
	for _, want := range []state.ObjID{ids[1], ids[3]} {
		if o := h.g.Obj(want); o.Zone != state.ZBattlefield {
			t.Fatalf("found creature %d zone = %s, want battlefield (Amount$ MassX = 2, not 1)", want, o.Zone)
		}
	}
	for _, want := range []state.ObjID{ids[0], ids[2], ids[4]} {
		if o := h.g.Obj(want); o.Zone != state.ZLibrary {
			t.Fatalf("revealed land %d zone = %s, want library (RevealedDestination$ Library)", want, o.Zone)
		}
	}
}

// TestDigUntilAmountSVarZeroRevealsNothing drives the REAL Selvala's Stampede
// DBVoteWild SA (Amount$ VoteNum, Shuffle$ True): a zero wild-vote tally must
// reveal nothing at all rather than stopping on the first creature.
func TestDigUntilAmountSVarZeroRevealsNothing(t *testing.T) {
	_, sa, svars := corpusRiderSA(t, "Selvala's Stampede", "DBVoteWild")
	if got := sa.API; got != "DigUntil" || sa.Params["Amount"] != "VoteNum" {
		t.Fatalf("precondition: Selvala's Stampede DBVoteWild = API %q Amount %q, want DigUntil/VoteNum", got, sa.Params["Amount"])
	}
	h, _, ids := riderBoard(t, riderBear, riderBear)
	c := &Ctx{Controller: 0, SVars: svars}
	if n, ok := EvalCountOK(h, c, "Number$0"); !ok || n != 0 {
		t.Fatalf("precondition: Number$0 = (%d,%v), want (0,true)", n, ok)
	}
	// The runtime VoteNum SVar the vote SP writes, here the zero tally.
	c.SVars = map[string]string{"VoteNum": "Number$0"}
	Resolve(h, c, sa)
	for _, id := range ids {
		if o := h.g.Obj(id); o.Zone != state.ZLibrary {
			t.Fatalf("creature %d zone = %s, want library: a zero tally reveals nothing", id, o.Zone)
		}
		if riderMoved(h, id) {
			t.Fatalf("creature %d moved with a zero Amount$ tally", id)
		}
	}
	// The effect still ran: Shuffle$ True shuffled after the (empty) scan.
	if got := len(riderShuffles(h, 0)); got != 1 {
		t.Fatalf("Shuffle events = %d, want 1 (the DigUntil ran; Amount$ 0 only empties the scan)", got)
	}
}

// TestDigUntilAmountSVarUnresolvableStillWithholds pins the fail-safe: a
// non-literal Amount$ naming no SVar in the resolving context keeps its loud
// Note and the amount-1 core move.
func TestDigUntilAmountSVarUnresolvableStillWithholds(t *testing.T) {
	h, _, ids := riderBoard(t, riderLand, riderHalo, riderLand)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DigUntil | Valid$ Aura | Amount$ X | FoundDestination$ Hand | RevealedDestination$ Library | RevealedLibraryPosition$ -1"))
	if o := h.g.Obj(ids[1]); o.Zone != state.ZHand {
		t.Fatalf("found Aura zone = %s, want hand: an unresolvable Amount$ must keep the core amount-1 move", o.Zone)
	}
	var got []string
	for _, e := range h.log {
		if strings.HasPrefix(e.Text, "DigUntil withholds ") {
			got = append(got, e.Text)
		}
	}
	want := []string{"DigUntil withholds Amount$ X; the core move runs without it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("withheld Notes = %v, want %v", got, want)
	}
}

// --- Shuffle$ / ShuffleCondition$ ---------------------------------------

// TestDigUntilShuffleShufflesTheDugLibrary: Shuffle$ True shuffles the dug
// player's library once, after the moves, as a Secret events.Shuffle.
func TestDigUntilShuffleShufflesTheDugLibrary(t *testing.T) {
	h, _, _ := riderBoard(t, riderLand, riderHalo, riderLand, riderLand)
	before := append([]state.ObjID(nil), h.g.Zone(state.ZLibrary, 0)...)
	if len(before) < 2 {
		t.Fatalf("precondition: library has %d cards, want >1 so a shuffle can reorder it", len(before))
	}
	Resolve(h, &Ctx{Controller: 0},
		sa(t, "SP$ DigUntil | Valid$ Aura | FoundDestination$ Hand | RevealedDestination$ Library | RevealedLibraryPosition$ -1 | Shuffle$ True"))
	shs := riderShuffles(h, 0)
	if len(shs) != 1 || !shs[0].Secret {
		t.Fatalf("Shuffle events = %+v, want exactly one Secret Shuffle", shs)
	}
	lib := h.g.Zone(state.ZLibrary, 0)
	if !reflect.DeepEqual(shs[0].IDs, lib) {
		t.Fatalf("Shuffle event order = %v, want the resulting library %v", shs[0].IDs, lib)
	}
	if reflect.DeepEqual(before, lib) {
		t.Fatalf("library still %v after Shuffle$ True: the shuffle must actually reorder", lib)
	}
}

// TestDigUntilShuffleConditionNoneFoundOnlyShufflesOnAnEmptyScan pins both
// branches: a scan that finds nothing shuffles, one that finds a card does
// not.
func TestDigUntilShuffleConditionNoneFoundOnlyShufflesOnAnEmptyScan(t *testing.T) {
	// Found: no shuffle.
	hFound, _, _ := riderBoard(t, riderLand, riderHalo, riderLand)
	saFound := sa(t, "SP$ DigUntil | Valid$ Aura | FoundDestination$ Hand | RevealedDestination$ Library | RevealedLibraryPosition$ -1 | Shuffle$ True | ShuffleCondition$ NoneFound")
	if n, ok := EvalCountOK(hFound, &Ctx{Controller: 0}, "Number$1"); !ok || n != 1 {
		t.Fatalf("precondition: Number$1 = (%d,%v), want (1,true)", n, ok)
	}
	Resolve(hFound, &Ctx{Controller: 0}, saFound)
	if got := len(riderShuffles(hFound, 0)); got != 0 {
		t.Fatalf("Shuffle events = %d, want 0 when the scan found a card", got)
	}
	// Nothing found: shuffle.
	hNone, _, _ := riderBoard(t, riderLand, riderLand)
	saNone := sa(t, "SP$ DigUntil | Valid$ Dragon | FoundDestination$ Hand | RevealedDestination$ Library | RevealedLibraryPosition$ -1 | Shuffle$ True | ShuffleCondition$ NoneFound")
	Resolve(hNone, &Ctx{Controller: 0}, saNone)
	if got := len(riderShuffles(hNone, 0)); got != 1 {
		t.Fatalf("Shuffle events = %d, want 1 when the scan found nothing (ShuffleCondition$ NoneFound)", got)
	}
}

// --- NoMoveFound$ / FoundLibraryPosition$ -------------------------------

// TestDigUntilNoMoveFoundKeepsTheFoundCardInTheLibrary: the found card is not
// moved to its (non-library) destination and stays on top with no event; the
// revealed rest still take RevealedDestination$.
func TestDigUntilNoMoveFoundKeepsTheFoundCardInTheLibrary(t *testing.T) {
	h, _, ids := riderBoard(t, riderLand, riderHalo, riderLand)
	if o := h.g.Obj(ids[1]); o.Zone != state.ZLibrary {
		t.Fatalf("precondition: found Aura is not in the library: %s", o.Zone)
	}
	// FoundDestination$ Hand is deliberately NOT the library: without the
	// rider the found card would move, so the assertion below can fail.
	Resolve(h, &Ctx{Controller: 0},
		sa(t, "SP$ DigUntil | Valid$ Aura | NoMoveFound$ True | FoundDestination$ Hand | FoundLibraryPosition$ 0 | RevealedDestination$ Graveyard"))
	if o := h.g.Obj(ids[1]); o.Zone != state.ZLibrary {
		t.Fatalf("found Aura zone = %s, want library (NoMoveFound$ True)", o.Zone)
	}
	if riderMoved(h, ids[1]) {
		t.Fatal("NoMoveFound$ True emitted a move for the found card")
	}
	if o := h.g.Obj(ids[0]); o.Zone != state.ZGraveyard {
		t.Fatalf("revealed rest zone = %s, want graveyard (RevealedDestination$)", o.Zone)
	}
}

// TestDigUntilFoundLibraryPositionPlacesOrKeepsTheFoundCard pins
// FoundLibraryPosition$ for a library-destination found card: "-1" is a real
// library-to-library move that lands it at the bottom, while "0" (and an
// absent value) is the stay-in-place top default, so the card never left and
// no event is emitted.
func TestDigUntilFoundLibraryPositionPlacesOrKeepsTheFoundCard(t *testing.T) {
	// Bottom: one library-to-library move, found card last.
	hBottom, _, idsBottom := riderBoard(t, riderLand, riderHalo, riderLand, riderLand)
	Resolve(hBottom, &Ctx{Controller: 0},
		sa(t, "SP$ DigUntil | Valid$ Aura | FoundDestination$ Library | FoundLibraryPosition$ -1 | RevealedDestination$ Library | RevealedLibraryPosition$ 0"))
	lib := hBottom.g.Zone(state.ZLibrary, 0)
	if len(lib) != 4 || lib[len(lib)-1] != idsBottom[1] {
		t.Fatalf("library = %v, want the found Aura %d at the bottom (FoundLibraryPosition$ -1)", lib, idsBottom[1])
	}
	// Top ("0"): the found card keeps its place and nothing moves it.
	hTop, _, idsTop := riderBoard(t, riderLand, riderHalo, riderLand, riderLand)
	Resolve(hTop, &Ctx{Controller: 0},
		sa(t, "SP$ DigUntil | Valid$ Aura | FoundDestination$ Library | FoundLibraryPosition$ 0 | RevealedDestination$ Library | RevealedLibraryPosition$ 0"))
	if riderMoved(hTop, idsTop[1]) {
		t.Fatal("FoundLibraryPosition$ 0 emitted a move: the stay-in-place top default is a no-op")
	}
	top := hTop.g.Zone(state.ZLibrary, 0)
	if len(top) != 4 || top[0] != idsTop[0] || top[1] != idsTop[1] {
		t.Fatalf("library = %v, want the found Aura %d still at the top (FoundLibraryPosition$ 0)", top, idsTop[1])
	}
}

// --- ImprintFound$ / ImprintRevealed$ -----------------------------------

// TestDigUntilImprintFoundFeedsTheExileReader drives the REAL Venture Forth
// DigUntil SA (ImprintFound$ True, FoundDestination$ Exile): the found land
// must join the resolving source's imprint association and resolve through
// the card's own `Defined$ Imprinted | Origin$ Exile` continuation.
func TestDigUntilImprintFoundFeedsTheExileReader(t *testing.T) {
	_, sa, svars := corpusRiderSA(t, "Venture Forth", "")
	if sa.API != "DigUntil" || sa.Params["ImprintFound"] != "True" || sa.Params["FoundDestination"] != "Exile" {
		t.Fatalf("precondition: Venture Forth = API %q ImprintFound %q FoundDestination %q, want DigUntil/True/Exile",
			sa.API, sa.Params["ImprintFound"], sa.Params["FoundDestination"])
	}
	// The card's own continuation is the reader this rider feeds.
	toPlay := cards.ResolveSVar(svars, "DBToPlay")
	if toPlay == nil || toPlay.API != "ChangeZone" || toPlay.Params["Defined"] != "Imprinted" || toPlay.Params["Origin"] != "Exile" {
		t.Fatalf("precondition: DBToPlay = %+v, want ChangeZone Defined$ Imprinted Origin$ Exile", toPlay)
	}
	h, src, ids := riderBoard(t, riderSurge, riderLand, riderLand)
	if !MatchesSpecCtx(h.g, permanentCardSpec("Permanent.Land"), ids[1], (&Ctx{Controller: 0, Source: src}).SpecContext(0)) {
		t.Fatalf("precondition: second library card %d is not a Permanent.Land", ids[1])
	}
	if MatchesSpecCtx(h.g, permanentCardSpec("Permanent.Land"), ids[0], (&Ctx{Controller: 0, Source: src}).SpecContext(0)) {
		t.Fatalf("precondition: first library card %d is a Land, so the scan would stop immediately", ids[0])
	}
	Resolve(h, &Ctx{Controller: 0, Source: src, SVars: svars}, sa)
	// The chained DBToPlay moved the found land out of exile onto the
	// battlefield via `Defined$ Imprinted | Origin$ Exile` -- the reader the
	// imprint rider exists to feed. The non-land turned over before it came
	// back to the library through DBRestRandomOrder.
	if o := h.g.Obj(ids[1]); o.Zone != state.ZBattlefield {
		t.Fatalf("found land zone = %s, want battlefield via DBToPlay's Defined$ Imprinted reader", o.Zone)
	}
	if o := h.g.Obj(ids[0]); o.Zone != state.ZLibrary {
		t.Fatalf("non-land revealed card zone = %s, want library (DBRestRandomOrder)", o.Zone)
	}
}

// TestDigUntilImprintRevealedRecordsEveryRevealedCard drives the REAL Part
// in Friendship TrigDigUntil SA (ImprintRevealed$ True, NoMoveRevealed$ True,
// FoundDestination$ Library, FoundLibraryPosition$ 0): every revealed card is
// associated with the resolving source, and the card's own chained
// `Cleanup | ClearImprinted$ True` (DBBranch) consumes that association -- an
// empty post-resolution list is the cleanup's own trace, not an absent write.
func TestDigUntilImprintRevealedRecordsEveryRevealedCard(t *testing.T) {
	_, sa, svars := corpusRiderSA(t, "Part in Friendship", "TrigDigUntil")
	if sa.API != "DigUntil" || sa.Params["ImprintRevealed"] != "True" || sa.Params["NoMoveRevealed"] != "True" {
		t.Fatalf("precondition: Part in Friendship = API %q ImprintRevealed %q NoMoveRevealed %q, want DigUntil/True/True",
			sa.API, sa.Params["ImprintRevealed"], sa.Params["NoMoveRevealed"])
	}
	if !strings.Contains(strings.ToLower(sa.Params["SubAbility"]), "dbbranch") {
		t.Fatalf("precondition: Part in Friendship SubAbility = %q, want the DBBranch continuation", sa.Params["SubAbility"])
	}
	h, src, ids := riderBoard(t, riderLand, riderBear, riderLand)
	if len(h.g.Obj(src).SeekFound) != 0 {
		t.Fatalf("precondition: source already carries an imprint association")
	}
	Resolve(h, &Ctx{Controller: 0, Source: src, SVars: svars}, sa)
	// The scan stops at the first creature: the two cards turned over are the
	// land and the creature, and both stay in the library.
	wantRevealed := []state.ObjID{ids[0], ids[1]}
	imps := riderImprints(h, src)
	var found []events.Event
	for _, e := range imps {
		if e.Text == "seek-found" {
			found = append(found, e)
		}
	}
	if len(found) != 1 || !reflect.DeepEqual(found[0].IDs, wantRevealed) {
		t.Fatalf("seek-found Imprint events = %+v, want one association of the revealed %v", found, wantRevealed)
	}
	if len(imps) < 2 {
		t.Fatalf("Imprint events = %+v, want the association plus the card's own ClearImprinted$ cleanup event", imps)
	}
	if o := h.g.Obj(ids[0]); o.Zone != state.ZLibrary {
		t.Fatalf("revealed non-found land zone = %s, want library (NoMoveRevealed$ True)", o.Zone)
	}
	// The card's own DBBranch continuation moved the found creature on (its
	// "if its mana value is <= lands you control" branch) and then ran a
	// ClearImprinted$ cleanup: an empty association here proves the rider's
	// write reached the source object.
	if o := h.g.Obj(ids[1]); o.Zone == state.ZLibrary {
		t.Fatalf("found creature %d still in the library: Part in Friendship's DBBranch continuation did not run", ids[1])
	}
	if got := h.g.Obj(src).SeekFound; len(got) != 0 {
		t.Fatalf("source imprint association = %v, want cleared by the card's own ClearImprinted$ cleanup", got)
	}
}

// --- NoneFoundDestination$ / NoneFoundLibraryPosition$ ------------------

// TestDigUntilNoneFoundBranchSwapsTheRevealedDestination pins the
// nothing-found branch: when the scan finds nothing the revealed cards take
// NoneFoundDestination$ at NoneFoundLibraryPosition$ instead of the ordinary
// revealed destination.
func TestDigUntilNoneFoundBranchSwapsTheRevealedDestination(t *testing.T) {
	ability := "SP$ DigUntil | Valid$ Aura | NoMoveFound$ True | FoundDestination$ Library | FoundLibraryPosition$ 0 | RevealedDestination$ Graveyard | NoneFoundDestination$ Library | NoneFoundLibraryPosition$ 0"
	// Found: the revealed rest take RevealedDestination$ Graveyard.
	hFound, _, idsFound := riderBoard(t, riderLand, riderHalo, riderLand)
	Resolve(hFound, &Ctx{Controller: 0}, sa(t, ability))
	if o := hFound.g.Obj(idsFound[0]); o.Zone != state.ZGraveyard {
		t.Fatalf("revealed rest zone = %s, want graveyard (the found branch keeps RevealedDestination$)", o.Zone)
	}
	// Nothing found: the revealed cards stay in the library instead.
	hNone, _, idsNone := riderBoard(t, riderLand, riderLand)
	Resolve(hNone, &Ctx{Controller: 0}, sa(t, ability))
	for _, id := range idsNone {
		if o := hNone.g.Obj(id); o.Zone != state.ZLibrary {
			t.Fatalf("revealed card %d zone = %s, want library (NoneFoundDestination$ Library)", id, o.Zone)
		}
		if riderMoved(hNone, id) {
			t.Fatalf("revealed card %d moved: NoneFoundLibraryPosition$ 0 is the stay-in-place default", id)
		}
	}
}

// --- No double-emit across a suspension re-entry ------------------------

// TestDigUntilRidersEmitOnceAcrossTheOptionalAsk pins the suspension
// guarantee: an OptionalFoundMove$ ask suspends and re-enters, and the
// ImprintFound$ association and Shuffle$ must be recorded exactly once.
func TestDigUntilRidersEmitOnceAcrossTheOptionalAsk(t *testing.T) {
	h, src, ids := riderBoard(t, riderLand, riderHalo, riderLand)
	ability := sa(t, "SP$ DigUntil | Valid$ Aura | FoundDestination$ Battlefield | OptionalFoundMove$ True | OptionalNoDestination$ Hand | RevealedDestination$ Library | RevealedLibraryPosition$ -1 | ImprintFound$ True | Shuffle$ True")
	// First pass: poses the ask, records nothing yet.
	Resolve(h, &Ctx{Controller: 0, Source: src}, ability)
	if h.asked == nil {
		t.Fatal("precondition: OptionalFoundMove$ True must pose the ask (the two-step continuation)")
	}
	if got := len(riderImprints(h, src)); got != 0 {
		t.Fatalf("Imprint events before the answer = %d, want 0 (the ask suspends first)", got)
	}
	if got := len(riderShuffles(h, 0)); got != 0 {
		t.Fatalf("Shuffle events before the answer = %d, want 0 (the ask suspends first)", got)
	}
	// Second pass: the answered continuation completes the walk once.
	ctx := &Ctx{Controller: 0, Source: src, DigUntilMove: "yes", DigUntilMoveDone: true}
	Resolve(h, ctx, ability)
	if o := h.g.Obj(ids[1]); o.Zone != state.ZBattlefield {
		t.Fatalf("answered found Aura zone = %s, want battlefield", o.Zone)
	}
	if got := len(riderImprints(h, src)); got != 1 {
		t.Fatalf("Imprint events across both passes = %d, want exactly 1", got)
	}
	if got := len(riderShuffles(h, 0)); got != 1 {
		t.Fatalf("Shuffle events across both passes = %d, want exactly 1", got)
	}
	if imp := riderImprints(h, src)[0]; len(imp.IDs) != 1 || imp.IDs[0] != ids[1] {
		t.Fatalf("Imprint payload = %v, want the found Aura [%d]", imp.IDs, ids[1])
	}
}

// --- Real-carrier coverage: kindred_summons, empty_the_laboratory,
// tunnel_vision ----------------------------------------------------------

const (
	riderBearCreature = "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	riderZombie       = "Name:Zombie\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n"
	riderRing         = "Name:Ring\nTypes:Artifact\nOracle:x\n"
)

// TestDigUntilKindredSummonsAmountSVarCountsChosenTypeCreatures drives the
// REAL Kindred Summons DBDigUntil SA (Amount$ X,
// SVar:X:Count$Valid Creature.ChosenType+YouCtrl): with two chosen-type
// creatures controlled, the scan must find TWO, not one.
func TestDigUntilKindredSummonsAmountSVarCountsChosenTypeCreatures(t *testing.T) {
	_, sa, svars := corpusRiderSA(t, "Kindred Summons", "DBDigUntil")
	if sa.API != "DigUntil" || sa.Params["Amount"] != "X" {
		t.Fatalf("precondition: Kindred Summons DBDigUntil = API %q Amount %q, want DigUntil/X", sa.API, sa.Params["Amount"])
	}
	h, src, ids := riderBoard(t, riderBearCreature, riderBearCreature, riderBearCreature, riderLand)
	// The chosen creature type lives on the resolving source; two Bears on
	// seat 0's battlefield make the tally 2.
	h.g.Obj(src).ChosenType = "Bear"
	b1 := h.g.AddObject(mkCard(t, riderBearCreature), 0).ID
	b2 := h.g.AddObject(mkCard(t, riderBearCreature), 0).ID
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src, b1, b2})
	c := &Ctx{Controller: 0, Source: src, SVars: svars}
	if n, ok := EvalCountOK(h, c, "Valid Creature.ChosenType+YouCtrl"); !ok || n != 2 {
		t.Fatalf("precondition: chosen-type creature count = (%d,%v), want (2,true)", n, ok)
	}
	Resolve(h, c, sa)
	for _, want := range []state.ObjID{ids[0], ids[1]} {
		if o := h.g.Obj(want); o.Zone != state.ZBattlefield {
			t.Fatalf("found Bear %d zone = %s, want battlefield (Amount$ X = 2, not 1)", want, o.Zone)
		}
	}
	if o := h.g.Obj(ids[2]); o.Zone != state.ZLibrary {
		t.Fatalf("third Bear zone = %s, want library (the scan stops after two matches)", o.Zone)
	}
	if got := len(riderShuffles(h, 0)); got != 1 {
		t.Fatalf("Shuffle events = %d, want 1 (Kindred Summons' Shuffle$ True)", got)
	}
}

// TestDigUntilEmptyTheLaboratoryAmountSVarCountsRemembered drives the REAL
// Empty the Laboratory DBDigUntil SA (Amount$ Y, SVar:Y:Remembered$Amount):
// the number of Zombies found must equal the sacrificed-and-remembered count.
func TestDigUntilEmptyTheLaboratoryAmountSVarCountsRemembered(t *testing.T) {
	_, sa, svars := corpusRiderSA(t, "Empty the Laboratory", "DBDigUntil")
	if sa.API != "DigUntil" || sa.Params["Amount"] != "Y" {
		t.Fatalf("precondition: Empty the Laboratory DBDigUntil = API %q Amount %q, want DigUntil/Y", sa.API, sa.Params["Amount"])
	}
	h, src, ids := riderBoard(t, riderZombie, riderZombie, riderZombie, riderLand)
	rem1 := h.g.AddObject(mkCard(t, riderZombie), 0).ID
	rem2 := h.g.AddObject(mkCard(t, riderZombie), 0).ID
	c := &Ctx{Controller: 0, Source: src, SVars: svars, Remembered: []state.Target{{Obj: rem1}, {Obj: rem2}}}
	if n, ok := EvalCountOK(h, c, "Remembered$Amount"); !ok || n != 2 {
		t.Fatalf("precondition: Remembered$Amount = (%d,%v), want (2,true)", n, ok)
	}
	Resolve(h, c, sa)
	for _, want := range []state.ObjID{ids[0], ids[1]} {
		if o := h.g.Obj(want); o.Zone != state.ZBattlefield {
			t.Fatalf("found Zombie %d zone = %s, want battlefield (Amount$ Y = 2, not 1)", want, o.Zone)
		}
	}
	if o := h.g.Obj(ids[2]); o.Zone != state.ZLibrary {
		t.Fatalf("third Zombie zone = %s, want library (the scan stops after two matches)", o.Zone)
	}
}

// TestDigUntilTunnelVisionNoneFoundShufflesAndKeepsLibrary drives the REAL
// Tunnel Vision FindThePrecious SA (NoMoveFound$ True, NoneFoundDestination$
// Library, Shuffle$ True, ShuffleCondition$ NoneFound) through both branches:
// a named card that is not in the library keeps the revealed cards in the
// library and shuffles; one that is found stays put and does not shuffle.
func TestDigUntilTunnelVisionNoneFoundShufflesAndKeepsLibrary(t *testing.T) {
	_, sa, svars := corpusRiderSA(t, "Tunnel Vision", "FindThePrecious")
	if sa.API != "DigUntil" || sa.Params["NoMoveFound"] != "True" || sa.Params["ShuffleCondition"] != "NoneFound" {
		t.Fatalf("precondition: FindThePrecious = API %q NoMoveFound %q ShuffleCondition %q, want DigUntil/True/NoneFound",
			sa.API, sa.Params["NoMoveFound"], sa.Params["ShuffleCondition"])
	}
	// Nothing named: the whole library is revealed, stays in the library (the
	// NoneFoundDestination$ Library override) and the library shuffles.
	h, src, _ := riderBoard(t)
	h.g.Obj(src).ChosenName = "Ring"
	var lib []state.ObjID
	for range 4 {
		lib = append(lib, h.g.AddObject(mkCard(t, riderLand), 1).ID)
	}
	h.g.SetZone(state.ZLibrary, 1, lib)
	Resolve(h, &Ctx{Controller: 0, Source: src, SVars: svars,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}, sa)
	if got := len(riderShuffles(h, 1)); got != 1 {
		t.Fatalf("Shuffle events = %d, want 1 (ShuffleCondition$ NoneFound with nothing found)", got)
	}
	for _, id := range lib {
		if o := h.g.Obj(id); o.Zone != state.ZLibrary {
			t.Fatalf("revealed card %d zone = %s, want library (NoneFoundDestination$ Library)", id, o.Zone)
		}
	}
	// Found: the named card is not moved (NoMoveFound$ True) and no shuffle
	// runs under ShuffleCondition$ NoneFound.
	h2, src2, _ := riderBoard(t)
	h2.g.Obj(src2).ChosenName = "Ring"
	other := h2.g.AddObject(mkCard(t, riderLand), 1).ID
	ring := h2.g.AddObject(mkCard(t, riderRing), 1).ID
	h2.g.SetZone(state.ZLibrary, 1, []state.ObjID{other, ring})
	Resolve(h2, &Ctx{Controller: 0, Source: src2, SVars: svars,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}, sa)
	if o := h2.g.Obj(ring); o.Zone != state.ZLibrary {
		t.Fatalf("found named card zone = %s, want library (NoMoveFound$ True)", o.Zone)
	}
	if riderMoved(h2, ring) {
		t.Fatal("NoMoveFound$ True emitted a move for the found card")
	}
	if got := len(riderShuffles(h2, 1)); got != 0 {
		t.Fatalf("Shuffle events = %d, want 0 when the named card was found", got)
	}
	if o := h2.g.Obj(other); o.Zone != state.ZGraveyard {
		t.Fatalf("revealed rest zone = %s, want graveyard (RevealedDestination$ Graveyard)", o.Zone)
	}
}

// TestDigUntilFoundCardFollowsRevealedDestination drives the REAL Consuming
// Aberration Grind SA (Defined$ Player.Opponent | Valid$ Land |
// RevealedDestination$ Graveyard, no FoundDestination$): "reveals cards ...
// until they reveal a land card, then puts THOSE cards into their graveyard"
// -- the land included. The found card used to default to Hand.
func TestDigUntilFoundCardFollowsRevealedDestination(t *testing.T) {
	_, sa, svars := corpusRiderSA(t, "Consuming Aberration", "Grind")
	if sa.API != "DigUntil" || sa.Params["FoundDestination"] != "" || sa.Params["RevealedDestination"] != "Graveyard" {
		t.Fatalf("precondition: Grind = API %q Found %q Revealed %q, want DigUntil with only RevealedDestination$ Graveyard",
			sa.API, sa.Params["FoundDestination"], sa.Params["RevealedDestination"])
	}
	h, src, _ := riderBoard(t)
	var ids []state.ObjID
	for _, s := range []string{riderBear, riderSurge, riderLand, riderBear} {
		ids = append(ids, h.g.AddObject(mkCard(t, s), 1).ID)
	}
	h.g.SetZone(state.ZLibrary, 1, ids)
	Resolve(h, &Ctx{Source: src, Controller: 0, SVars: svars}, sa)
	for _, id := range ids[:3] {
		if o := h.g.Obj(id); o.Zone != state.ZGraveyard {
			t.Fatalf("revealed card %d zone = %s, want graveyard (the found land too)", id, o.Zone)
		}
	}
	if o := h.g.Obj(ids[3]); o.Zone != state.ZLibrary {
		t.Fatalf("card after the land zone = %s, want library", o.Zone)
	}
	if n := len(h.g.Zone(state.ZHand, 1)); n != 0 {
		t.Fatalf("opponent hand = %d cards, want 0", n)
	}
}
