package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The additional-land-drops grant (fb-20260916T201051Z): an S:Mode$
// Continuous static carrying a plain integer AdjustLandPlays$ (Azusa, Lost
// but Seeking's "You may play two additional lands on each of your turns",
// Oracle of Mul Daya, Exploration, Icetill Explorer) registers a rules-mod
// on the ContinuousEffect, and the land-play gates in rules/legal.go offer
// 1+adjust drops per turn instead of the hardcoded one. The feedback
// snapshot this task came from DIVERGES against the current corpus (it was
// captured before a corpus pin bump), so every fixture here is synthetic:
// real compiled statics copied out of the named cards' scripts, hand-built
// decks, no recorded replay.

// azusaSrc is Azusa, Lost but Seeking's real compiled static: the +2 shape,
// top-level S: with Affected$ You.
const azusaSrc = "Name:Azusa, Lost but Seeking\nManaCost:2 G\nTypes:Legendary Creature Human Monk\nPT:1/2\n" +
	"S:Mode$ Continuous | Affected$ You | AdjustLandPlays$ 2 | Description$ You may play two additional lands on each of your turns.\nOracle:x\n"

// oracleSrc is Oracle of Mul Daya's AdjustLandPlays static: the +1 shape.
const oracleSrc = "Name:Oracle of Mul Daya\nManaCost:3 G\nTypes:Creature Elf Wizard\nPT:0/4\n" +
	"S:Mode$ Continuous | Affected$ You | AdjustLandPlays$ 1 | Description$ You may play an additional land on each of your turns.\nOracle:x\n"

// explorationSrc is Exploration's static, used for the stacking test.
const explorationSrc = "Name:Exploration\nManaCost:G\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ You | AdjustLandPlays$ 1 | Description$ You may play an additional land on each of your turns.\nOracle:x\n"

// azusaUnlimitedSrc is Fastbond's shape: a non-literal value this build
// cannot price as a count. It must fail closed, never silently become a
// smaller grant.
const azusaUnlimitedSrc = "Name:Fastbond\nManaCost:G\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ You | AdjustLandPlays$ Unlimited | Description$ You may play any number of lands on each of your turns.\nOracle:x\n"

// azusaIsPresentSrc is Thranduil's Company's shape: the grant carries an
// IsPresent$ qualifier. It must fail closed until that grammar exists.
const azusaIsPresentSrc = "Name:Thranduil's Company\nManaCost:5 G\nTypes:Creature Elf Noble\nPT:3/4\n" +
	"S:Mode$ Continuous | Affected$ You | AdjustLandPlays$ 1 | IsPresent$ Elf.YouCtrl+Other | Description$ As long as you control another Elf, you may play an additional land on each of your turns.\nOracle:x\n"

// landBase is a 2-seat engine at seat 0's turn-1 main phase, hands
// emptied, so a fixture can place a granting permanent on the battlefield
// and a chosen number of lands in the hand.
func landBase(t *testing.T) *Engine {
	t.Helper()
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
		e.G.SetZone(state.ZGraveyard, p, nil)
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	return e
}

// handCard places c into p's hand (the graveCard pattern, with the hand).
func handCard(e *Engine, c *cards.Card, p state.PlayerID) state.ObjID {
	o := e.G.AddObject(c, p)
	o.Zone = state.ZHand
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZHand, p)...)
	ids = append(ids, o.ID)
	e.G.SetZone(state.ZHand, p, ids)
	return o.ID
}

// answerReplacementOrderAsk answers one pending CR 616.1 order ask with
// option 0 -- the deterministic scan order the pre-choice engine composed
// in -- so harnesses written around that behavior keep it verbatim. Tests
// that assert the ORDER-dependence answer deliberately instead.
func answerReplacementOrderAsk(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		return
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("answer replacement order ask: %v", err)
	}
}

// playOneLand submits the play_land option for id at the current priority
// decision and returns whether one was found and played.
func playOneLand(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) bool {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != p {
		t.Fatalf("expected seat %d's priority, got %+v", p, d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		return false
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: p, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	answerReplacementOrderAsk(t, e)
	if e.Pending() == nil {
		e.priorityRound()
	}
	return true
}

// playAllLands drives priority rounds, playing id whenever it is offered,
// and returns how many times it was played.
func playAllLands(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID, limit int) int {
	t.Helper()
	played := 0
	for played < limit {
		if !playOneLand(t, e, p, id) {
			break
		}
		played++
	}
	return played
}

// TestAzusaGrantsTwoAdditionalLandDrops pins the reported card end to end:
// with Azusa on the battlefield seat 0 plays THREE lands in one turn (the
// ordinary drop plus Azusa's two), each through its own priority window,
// and the fourth drop is not offered -- the cap read is 1+adjust, not
// "always offered".
func TestAzusaGrantsTwoAdditionalLandDrops(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	onBoardGrant(t, e, 0, azusaSrc)
	lands := []state.ObjID{
		handCard(e, card(t, landSrc("Mountain")), 0),
		handCard(e, card(t, landSrc("Forest")), 0),
		handCard(e, card(t, landSrc("Plains")), 0),
		handCard(e, card(t, landSrc("Island")), 0),
	}
	e.priorityRound()
	for i, id := range lands[:3] {
		if !playOneLand(t, e, 0, id) {
			t.Fatalf("land %d not offered despite Azusa's grant: %+v", i, e.Pending().Options)
		}
		if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
			t.Fatalf("land %d zone = %s, want battlefield", i, got)
		}
	}
	if got := e.G.Players[0].LandsPlayed; got != 3 {
		t.Fatalf("LandsPlayed = %d, want 3 after the ordinary drop plus Azusa's two", got)
	}
	// The budget is spent: the fourth land is not offered.
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == lands[3] {
			t.Fatalf("fourth land drop offered beyond Azusa's grant: %+v", d.Options)
		}
	}
}

// TestOracleGrantsOneAdditionalLandDrop pins the +1 shape (the value is
// data the gate now reads, so a distinct size gets its own test): two drops
// in the turn, the third not offered.
func TestOracleGrantsOneAdditionalLandDrop(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	onBoardGrant(t, e, 0, oracleSrc)
	first := handCard(e, card(t, landSrc("Mountain")), 0)
	second := handCard(e, card(t, landSrc("Forest")), 0)
	third := handCard(e, card(t, landSrc("Plains")), 0)
	e.priorityRound()
	if !playOneLand(t, e, 0, first) || !playOneLand(t, e, 0, second) {
		t.Fatalf("two drops not offered under Oracle's +1 grant")
	}
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == third {
			t.Fatalf("third drop offered beyond Oracle's +1 grant: %+v", d.Options)
		}
	}
}

// TestAdjustLandPlaysGrantsSumNotMax pins the CR 305.2a sum: two grants on
// the battlefield give their TOTAL, not the max -- Azusa (+2) plus
// Exploration (+1) is four drops, not three.
func TestAdjustLandPlaysGrantsSumNotMax(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	onBoardGrant(t, e, 0, azusaSrc)
	onBoardGrant(t, e, 0, explorationSrc)
	var lands []state.ObjID
	for _, n := range []string{"Mountain", "Forest", "Plains", "Island", "Swamp"} {
		lands = append(lands, handCard(e, card(t, landSrc(n)), 0))
	}
	e.priorityRound()
	played := 0
	for _, id := range lands {
		if !playOneLand(t, e, 0, id) {
			break
		}
		played++
	}
	if played != 4 {
		t.Fatalf("played %d lands, want 4 (the drop plus the summed +3)", played)
	}
	if got := e.G.Players[0].LandsPlayed; got != 4 {
		t.Fatalf("LandsPlayed = %d, want 4", got)
	}
}

// TestAdjustLandPlaysResetsAtUntap pins the turn boundary: the extra drops
// do not survive into the next turn -- the TurnChange reset still zeroes
// LandsPlayed, so seat 0's next turn offers the ordinary drop (plus the
// grant) again. Drives two real turns.
func TestAdjustLandPlaysResetsAtUntap(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	onBoardGrant(t, e, 0, azusaSrc)
	lands := []state.ObjID{
		handCard(e, card(t, landSrc("Mountain")), 0),
		handCard(e, card(t, landSrc("Forest")), 0),
		handCard(e, card(t, landSrc("Plains")), 0),
		handCard(e, card(t, landSrc("Island")), 0),
		handCard(e, card(t, landSrc("Swamp")), 0),
		handCard(e, card(t, landSrc("Mountain")), 0),
		handCard(e, card(t, landSrc("Forest")), 0),
	}
	e.priorityRound()
	played := 0
	for _, id := range lands {
		if !playOneLand(t, e, 0, id) {
			break
		}
		played++
	}
	if played != 3 {
		t.Fatalf("turn 1 played %d lands, want 3", played)
	}
	// Pass around the table to seat 0's next turn (turn 3).
	for i := 0; i < 100 && (e.G.Turn < 3 || e.G.Active != 0 || e.G.Step != state.StepMain1); i++ {
		submitPass(t, e)
	}
	if e.G.Turn != 3 || e.G.Active != 0 || e.G.Step != state.StepMain1 {
		t.Fatalf("did not reach seat 0's turn-3 main phase (turn %d active %d step %s)", e.G.Turn, e.G.Active, e.G.Step)
	}
	if got := e.G.Players[0].LandsPlayed; got != 0 {
		t.Fatalf("LandsPlayed = %d at the new turn, want 0 (the TurnChange reset)", got)
	}
	// The remaining hand lands are playable again -- up to the full 1+2
	// cap (the turn-3 draw added one more, which the spent budget withholds).
	if !playOneLand(t, e, 0, lands[3]) || !playOneLand(t, e, 0, lands[4]) || !playOneLand(t, e, 0, lands[5]) {
		t.Fatalf("turn 3 did not re-offer the full Azusa budget")
	}
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "play_land" {
			t.Fatalf("a play_land option survived the spent turn-3 budget: %+v", d.Options)
		}
	}
}

// TestAdjustLandPlaysExpiresWhenSourceLeaves pins CR 611.3b: Azusa leaving
// the battlefield mid-turn removes the extra drops immediately -- after one
// ordinary drop and the source's departure, the remaining hand land is not
// offered.
func TestAdjustLandPlaysExpiresWhenSourceLeaves(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	azusa := onBoardGrant(t, e, 0, azusaSrc)
	first := handCard(e, card(t, landSrc("Mountain")), 0)
	second := handCard(e, card(t, landSrc("Forest")), 0)
	e.priorityRound()
	if !playOneLand(t, e, 0, first) {
		t.Fatalf("ordinary drop not offered with Azusa on the battlefield")
	}
	// Before the departure the grant is live: another hand land is offered.
	if n := countPlayLand(e, second); n != 1 {
		t.Fatalf("want the second hand land offered while Azusa is on the battlefield, got %d", n)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: azusa, From: state.ZBattlefield, To: state.ZGraveyard})
	if n := countPlayLand(e, second); n != 0 {
		t.Fatalf("want no extra drop after Azusa left the battlefield, got %d", n)
	}
}

// TestAdjustLandPlaysUnlimitedFailsClosed pins the fail-closed rule for a
// value this build cannot price: Fastbond's AdjustLandPlays$ Unlimited
// grants NOTHING (matching the pre-grant behaviour for that shape), so the
// cap stays at one and a second drop is not offered.
func TestAdjustLandPlaysUnlimitedFailsClosed(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	onBoardGrant(t, e, 0, azusaUnlimitedSrc)
	first := handCard(e, card(t, landSrc("Mountain")), 0)
	second := handCard(e, card(t, landSrc("Forest")), 0)
	e.priorityRound()
	if !playOneLand(t, e, 0, first) {
		t.Fatalf("ordinary drop not offered")
	}
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == second {
			t.Fatalf("Unlimited grant silently became a real drop: %+v", d.Options)
		}
	}
}

// TestAdjustLandPlaysIsPresentFailsClosed pins the fail-closed rule for an
// IsPresent$ rider: the conditional grant is not applied (matching today's
// behaviour for that shape) even though its value is a literal.
func TestAdjustLandPlaysIsPresentFailsClosed(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	onBoardGrant(t, e, 0, azusaIsPresentSrc)
	first := handCard(e, card(t, landSrc("Mountain")), 0)
	second := handCard(e, card(t, landSrc("Forest")), 0)
	e.priorityRound()
	if !playOneLand(t, e, 0, first) {
		t.Fatalf("ordinary drop not offered")
	}
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == second {
			t.Fatalf("IsPresent$ rider silently became an unconditional drop: %+v", d.Options)
		}
	}
}

// TestAdjustLandPlaysFollowsControl pins whose "You" the Affected$ spec
// resolves against: the grant's Affects is evaluated against the granting
// effect's live controller, so an Azusa handed to seat 1 mid-turn grants
// SEAT 1 -- seat 0's extra drops vanish and seat 1's appear.
func TestAdjustLandPlaysFollowsControl(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	azusa := onBoardGrant(t, e, 0, azusaSrc)
	seat0First := handCard(e, card(t, landSrc("Mountain")), 0)
	seat0Second := handCard(e, card(t, landSrc("Swamp")), 0)
	seat1First := handCard(e, card(t, landSrc("Forest")), 1)
	seat1Second := handCard(e, card(t, landSrc("Plains")), 1)
	e.priorityRound()
	if !playOneLand(t, e, 0, seat0First) {
		t.Fatalf("ordinary drop not offered with Azusa on the battlefield")
	}
	// Before the handover seat 0 still has drops left.
	if n := countPlayLand(e, seat0Second); n != 1 {
		t.Fatalf("want seat 0's grant live before the control change, got %d", n)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: azusa, Player: 1})
	// Seat 0's budget is back to the ordinary one (already spent).
	if n := countPlayLand(e, seat0Second); n != 0 {
		t.Fatalf("want no play_land for seat 0 after losing Azusa, got %d", n)
	}
	// Seat 1 now holds the grant: at seat 1's main phase three drops are
	// offered.
	e.G.Active, e.G.Priority = 1, 1
	e.priorityRound()
	if !playOneLand(t, e, 1, seat1First) || !playOneLand(t, e, 1, seat1Second) {
		t.Fatalf("seat 1 did not gain Azusa's extra drops after the control change")
	}
	if got := e.G.Players[1].LandsPlayed; got != 2 {
		t.Fatalf("seat 1 LandsPlayed = %d, want 2", got)
	}
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "play_land" {
			t.Fatalf("a play_land option survived seat 1's spent budget: %+v", d.Options)
		}
	}
}

// TestAdjustLandPlaysSharesOneBudgetWithMayPlayGrants pins that a hand
// land, a may-play graveyard land and Azusa's two extra drops all draw from
// ONE pool: Azusa (+2) plus a Conduit of Worlds grant puts a total of three
// drops in the turn, spendable across the hand walk and the graveyard walk
// alike, and the fourth is not offered through either.
func TestAdjustLandPlaysSharesOneBudgetWithMayPlayGrants(t *testing.T) {
	t.Parallel()
	e := landBase(t)
	onBoardGrant(t, e, 0, azusaSrc)
	onBoardGrant(t, e, 0, conduitGrantSrc)
	grave := graveCard(e, card(t, landSrc("Mountain")), 0, 0)
	hand1 := handCard(e, card(t, landSrc("Forest")), 0)
	hand2 := handCard(e, card(t, landSrc("Plains")), 0)
	hand3 := handCard(e, card(t, landSrc("Island")), 0)
	e.priorityRound()
	// Three plays across both walks: the grave land plus two hand lands.
	if !playOneLand(t, e, 0, grave) || !playOneLand(t, e, 0, hand1) || !playOneLand(t, e, 0, hand2) {
		t.Fatalf("the shared budget did not offer three drops across hand and graveyard")
	}
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "play_land" && (o.Obj == hand3 || o.Obj == grave) {
			t.Fatalf("fourth drop offered beyond the shared 1+2 budget: %+v", d.Options)
		}
	}
	if got := e.G.Players[0].LandsPlayed; got != 3 {
		t.Fatalf("LandsPlayed = %d, want 3", got)
	}
}
