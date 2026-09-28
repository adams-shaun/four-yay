package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// conduitGrantSrc is Conduit of Worlds' real compiled may-play static: a
// top-level S:Mode$ Continuous carrying Affected$ Land.YouOwn, MayPlay$ True
// and AffectedZone$ Graveyard -- the exact grant the reported card's "You may
// play lands from your graveyard." compiles to. The card's A:AB$ Play
// activated ability (api:Play, still in knownUnsupported) is deliberately
// absent here; it is separate, unwired feature work and out of this task's
// scope.
const conduitGrantSrc = "Name:Conduit of Worlds\nManaCost:2 G G\nTypes:Artifact\n" +
	"S:Mode$ Continuous | Affected$ Land.YouOwn | MayPlay$ True | AffectedZone$ Graveyard | Description$ You may play lands from your graveyard.\nOracle:x\n"

func landSrc(name string) string {
	return "Name:" + name + "\nTypes:Basic Land Mountain\nOracle:x\n"
}

func creatureSrc(name string) string {
	return "Name:" + name + "\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
}

// mayPlayBase builds a 2-seat engine sitting at seat 0's main phase with both
// hands and graveyards empty, so a fixture can place a may-play grant on the
// battlefield and lands in a graveyard and assert the resulting offer.
func mayPlayBase(t *testing.T) *Engine {
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

// onBoardGrant places a may-play granting permanent onto seat owner's
// battlefield THROUGH a MoveZone event, because staticEffects() results are
// memoized on the event-log length and a direct zone write would not refresh
// the memo -- a test of whether a live battlefield static applies must not
// depend on a stale cache. The object starts in the library so the MoveZone
// has a real origin.
func onBoardGrant(t *testing.T, e *Engine, owner state.PlayerID, src string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, src), owner)
	o.Zone = state.ZLibrary
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, owner)...)
	ids = append(ids, o.ID)
	e.G.SetZone(state.ZLibrary, owner, ids)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return o.ID
}

// graveCard places c into putIn's graveyard but owned by owner, so a filter
// test can park an opponent's land or a non-land permanent in the same zone
// as the grant's own land and check the grant does not reach it.
func graveCard(e *Engine, c *cards.Card, owner, putIn state.PlayerID) state.ObjID {
	o := e.G.AddObject(c, owner)
	o.Zone = state.ZGraveyard
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZGraveyard, putIn)...)
	ids = append(ids, o.ID)
	e.G.SetZone(state.ZGraveyard, putIn, ids)
	return o.ID
}

func countPlayLand(e *Engine, want state.ObjID) int {
	n := 0
	for _, o := range e.legalActions(0) {
		if o.Kind == "play_land" && o.Obj == want {
			n++
		}
	}
	return n
}

// TestConduitGrantsPlayingLandsFromGraveyard pins Conduit of Worlds' headline
// static end to end: a land in the graveyard is offered as play_land while the
// grant permanent is on the battlefield, and playing it moves the land From
// the graveyard to the battlefield and raises LandsPlayed (so the once-per-turn
// gate holds for the next land).
func TestConduitGrantsPlayingLandsFromGraveyard(t *testing.T) {
	t.Parallel()
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, conduitGrantSrc)
	grave := graveCard(e, card(t, landSrc("Mountain")), 0, 0)

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == grave {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for the graveyard land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	if o := e.G.Obj(grave); o.Zone != state.ZBattlefield {
		t.Fatalf("played land zone = %s, want battlefield", o.Zone)
	}
	if e.G.Players[0].LandsPlayed != 1 {
		t.Fatalf("LandsPlayed = %d, want 1", e.G.Players[0].LandsPlayed)
	}
}

// TestConduitMayPlayExpiresWhenSourceLeaves pins CR 611.3b: the grant is only
// live while its source is on the battlefield. Once Conduit leaves, the same
// graveyard land is no longer offered.
func TestConduitMayPlayExpiresWhenSourceLeaves(t *testing.T) {
	t.Parallel()
	e := mayPlayBase(t)
	conduit := onBoardGrant(t, e, 0, conduitGrantSrc)
	grave := graveCard(e, card(t, landSrc("Mountain")), 0, 0)

	if n := countPlayLand(e, grave); n != 1 {
		t.Fatalf("want the graveyard land offered while Conduit is on the battlefield, got %d", n)
	}
	// Conduit leaves the battlefield (CR 611.3b): the grant expires.
	e.emit(events.Event{Kind: events.MoveZone, Obj: conduit, From: state.ZBattlefield, To: state.ZGraveyard})
	if n := countPlayLand(e, grave); n != 0 {
		t.Fatalf("want no play_land after Conduit leaves the battlefield, got %d", n)
	}
}

// TestConduitMayPlayRespectsOncePerTurn pins the land-drop gate under the new
// grant: playing one graveyard land this turn leaves no further play_land for
// the other graveyard land.
func TestConduitMayPlayRespectsOncePerTurn(t *testing.T) {
	t.Parallel()
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, conduitGrantSrc)
	first := graveCard(e, card(t, landSrc("Mountain")), 0, 0)
	second := graveCard(e, card(t, landSrc("Island")), 0, 0)

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Player != 0 {
		t.Fatalf("expected seat 0's priority, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == first {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land for the first graveyard land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if n := countPlayLand(e, second); n != 0 {
		t.Fatalf("second land dropped this turn should not be playable, got %d", n)
	}
}

// TestConduitGraveyardLandEtbChoicePlaysFromGraveyard pins that the
// one-stage "as this enters" land flow (Cavern of Souls' ChooseType)
// behaves identically for a graveyard land under a may-play grant: the land
// is offered from the graveyard, asks its as-enters type choice, and enters
// the battlefield From the graveyard with the choice recorded.
func TestConduitGraveyardLandEtbChoicePlaysFromGraveyard(t *testing.T) {
	t.Parallel()
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, conduitGrantSrc)
	cavern := graveCard(e, card(t, "Name:Cavern\nManaCost:no cost\nTypes:Land\n"+
		"K:ETBReplacement:Other:ChooseCT\n"+
		"SVar:ChooseCT:DB$ ChooseType | Defined$ You | Type$ Creature | SpellDescription$ x\nOracle:x\n"), 0, 0)

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Player != 0 {
		t.Fatalf("expected seat 0's priority, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == cavern {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land for the ETB graveyard land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	dd := e.Pending()
	if dd == nil || dd.Kind != decision.KChoose || len(dd.Options) == 0 || dd.Options[0].Kind != "type" {
		t.Fatalf("expected the as-enters type choice, got %+v", dd)
	}
	if err := e.Submit(decision.Intent{Seq: dd.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("submit type: %v", err)
	}
	if o := e.G.Obj(cavern); o.Zone != state.ZBattlefield {
		t.Fatalf("ETB land zone = %s, want battlefield", o.Zone)
	}
	if e.G.Players[0].LandsPlayed != 1 {
		t.Fatalf("LandsPlayed = %d, want 1", e.G.Players[0].LandsPlayed)
	}
}

// TestMayPlayRichGrantFailsClosed pins the rich-grant boundary on Muldrotha's
// real script shape: the once-per-turn MayPlayLimit$ and the Condition$
// PlayerTurn gate are IMPLEMENTED (mayPlayLimitReached and the Condition$
// PlayerTurn arm; TestKessGraveyardInstantCastsOnItsControllerTurn and
// TestKessGraveyardInstantNotOfferedOnOpponentTurn pin both on Kess's real
// compiled script), while the per-TYPE MayPlayText$ rider stays unread (the
// grant still applies -- the kw-mayplay branch's reviewed stance) and an
// unimplemented CheckSVar$ gate fails closed. A CheckSVar$ may-play grant's
// withholding is a recognition, not a consumption.
func TestMayPlayRichGrantFailsClosed(t *testing.T) {
	t.Parallel()
	t.Run("MayPlayLimit grants once and then withholds", func(t *testing.T) {
		e := mayPlayBase(t)
		// Muldrotha's real uncapped-per-type land grant: MayPlay$ True,
		// once-per-turn (MayPlayLimit$ 1) and Condition$ PlayerTurn. The
		// merged engine implements both gates, so the graveyard land IS
		// offered this turn -- and after the land drop is spent it is not
		// offered again.
		muldrotha := "Name:Muldrotha, the Gravetide\nManaCost:1 G U B\nTypes:Legendary Creature\nPT:6/6\n" +
			"S:Mode$ Continuous | Affected$ Land.YouOwn | Condition$ PlayerTurn | MayPlay$ True | MayPlayLimit$ 1 | MayPlayText$ Land | EffectZone$ Battlefield | AffectedZone$ Graveyard | Description$ x\nOracle:x\n"
		onBoardGrant(t, e, 0, muldrotha)
		grave := graveCard(e, card(t, landSrc("Mountain")), 0, 0)
		if n := countPlayLand(e, grave); n != 1 {
			t.Fatalf("a MayPlayLimit$ grant is implemented (once per turn), want exactly one play_land, got %d", n)
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			e.priorityRound()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no pending decision after priorityRound")
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "play_land" && o.Obj == grave {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("no play_land option under the MayPlayLimit$ grant: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit play_land: %v", err)
		}
		if n := countPlayLand(e, grave); n != 0 {
			t.Fatalf("after the once-per-turn play, no further play_land may be offered, got %d", n)
		}
	})

	t.Run("Condition PlayerTurn grants on the controller's turn", func(t *testing.T) {
		e := mayPlayBase(t)
		gated := "Name:Gated Grant\nManaCost:2\nTypes:Artifact\n" +
			"S:Mode$ Continuous | Affected$ Land.YouOwn | Condition$ PlayerTurn | MayPlay$ True | AffectedZone$ Graveyard | Description$ x\nOracle:x\n"
		onBoardGrant(t, e, 0, gated)
		grave := graveCard(e, card(t, landSrc("Mountain")), 0, 0)
		// mayPlayBase sits at seat 0's main phase on seat 0's turn, so the
		// PlayerTurn-conditioned grant applies and the land is offered. The
		// opponent-turn side of the same gate is pinned on Kess's real script
		// by TestKessGraveyardInstantNotOfferedOnOpponentTurn.
		if n := countPlayLand(e, grave); n != 1 {
			t.Fatalf("a Condition$ PlayerTurn grant applies on the controller's turn, want exactly one play_land, got %d", n)
		}
	})

	t.Run("unimplemented CheckSVar gate forces closed", func(t *testing.T) {
		e := mayPlayBase(t)
		gated := "Name:SVar Gated Grant\nManaCost:2\nTypes:Artifact\n" +
			"S:Mode$ Continuous | Affected$ Land.YouOwn | MayPlay$ True | AffectedZone$ Graveyard | CheckSVar$ X | SVarCompare$ EQ1 | Description$ x\nOracle:x\n"
		onBoardGrant(t, e, 0, gated)
		grave := graveCard(e, card(t, landSrc("Mountain")), 0, 0)
		if n := countPlayLand(e, grave); n != 0 {
			t.Fatalf("a CheckSVar$ grant must fail closed (no play_land), got %d", n)
		}
	})
}

// Land.YouOwn grant must not offer an opponent's land or a non-land permanent,
// and a Land.YouCtrl grant must not offer a land its controller does not
// control.
func TestMayPlayFilterRespectsAffects(t *testing.T) {
	t.Parallel()
	t.Run("YouOwn excludes opponent's land and non-land", func(t *testing.T) {
		e := mayPlayBase(t)
		onBoardGrant(t, e, 0, conduitGrantSrc)
		mine := graveCard(e, card(t, landSrc("Mountain")), 0, 0)
		graveCard(e, card(t, landSrc("Swamp")), 1, 0)    // an opponent's land
		graveCard(e, card(t, creatureSrc("Bear")), 0, 0) // a non-land permanent
		if n := countPlayLand(e, mine); n != 1 {
			t.Fatalf("want exactly my own land offered, got %d", n)
		}
		// No other land is offered.
		for _, o := range e.legalActions(0) {
			if o.Kind != "play_land" {
				continue
			}
			if o.Obj != mine {
				t.Fatalf("offered play_land for %q, which the Land.YouOwn grant must not reach",
					e.G.Obj(o.Obj).Face().Name)
			}
		}
	})

	t.Run("YouCtrl excludes a land the player does not control", func(t *testing.T) {
		e := mayPlayBase(t)
		ctrlSrc := "Name:Conduit of Worlds\nManaCost:2 G G\nTypes:Artifact\n" +
			"S:Mode$ Continuous | Affected$ Land.YouCtrl | MayPlay$ True | AffectedZone$ Graveyard | Description$ x\nOracle:x\n"
		onBoardGrant(t, e, 0, ctrlSrc)
		ctrl := graveCard(e, card(t, landSrc("Mountain")), 0, 0) // owned AND controlled by seat 0
		notMine := graveCard(e, card(t, landSrc("Swamp")), 1, 0) // owned by seat 1, controlled by seat 1
		if n := countPlayLand(e, ctrl); n != 1 {
			t.Fatalf("want the controlled land offered, got %d", n)
		}
		if n := countPlayLand(e, notMine); n != 0 {
			t.Fatalf("a land the player does not control must not be offered, got %d", n)
		}
	})
}
