package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func handLand(t *testing.T, e *Engine, p state.PlayerID) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, landSrc("Test Mountain")), p)
	o.Zone = state.ZHand
	ids := append([]state.ObjID(nil), e.G.Zone(state.ZHand, p)...)
	ids = append(ids, o.ID)
	e.G.SetZone(state.ZHand, p, ids)
	return o.ID
}

func TestCantPlayLandHandLocked(t *testing.T) {
	t.Parallel()
	e := mayPlayBase(t)
	land := handLand(t, e, 0)
	if got := countPlayLand(e, land); got != 1 {
		t.Fatalf("precondition: unlocked hand land offers = %d, want 1", got)
	}
	source := onBoardGrant(t, e, 0, "Name:Memory Vessel\nTypes:Artifact\n"+
		"SVar:NoLand:Mode$ CantPlayLand | Player$ Player | Origin$ Hand | Description$ Players can't play lands from their hand.\n"+
		"SVar:DBEffect:DB$ Effect | StaticAbilities$ NoLand | RememberObjects$ Remembered | Duration$ UntilYourNextTurn | SubAbility$ DBCleanup | ForgetOnMoved$ Exile\n"+
		"Oracle:x\n")
	vessel := e.G.Obj(source)
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0, SVars: vessel.Face().SVars},
		cards.ResolveSVar(vessel.Face().SVars, "DBEffect"))
	if !playLandForbidden(e, 0, state.ZHand, land) {
		t.Fatal("Memory Vessel DB$ Effect did not deliver an active CantPlayLand restriction")
	}
	if got := countPlayLand(e, land); got != 0 {
		t.Fatalf("hand-locked land offers = %d, want 0", got)
	}
	// The shared predicate is the bot/submit recheck as well as the offer gate.
	if !playLandForbidden(e, 0, state.ZHand, land) {
		t.Fatal("shared playLandForbidden predicate did not bind for bot/submit validation")
	}
}

func TestCantPlayLandScope(t *testing.T) {
	t.Parallel()
	e := mayPlayBase(t)
	land0 := handLand(t, e, 0)
	land1 := handLand(t, e, 1)
	source := onBoardGrant(t, e, 0, "Name:Land lock\nTypes:Enchantment\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: source, Controller: 0, Restriction: "CantPlayLand",
		RestrictParams: map[string]string{"Player": "Player.Opponent", "Origin": "Hand"}})
	if !playLandForbidden(e, 1, state.ZHand, land1) || playLandForbidden(e, 0, state.ZHand, land0) {
		t.Fatal("Player.Opponent must bind only the controller's opponent")
	}

	e2 := mayPlayBase(t)
	land0, land1 = handLand(t, e2, 0), handLand(t, e2, 1)
	onBoardGrant(t, e2, 0, "Name:You land lock\nTypes:Enchantment\nS:Mode$ CantPlayLand | Player$ You | Origin$ Hand | Description$ x\nOracle:x\n")
	if !playLandForbidden(e2, 0, state.ZHand, land0) || playLandForbidden(e2, 1, state.ZHand, land1) {
		t.Fatal("printed Player$ You must bind only the static's controller")
	}
}

func TestCantPlayLandGraveyardGrantUnaffected(t *testing.T) {
	t.Parallel()
	e := mayPlayBase(t)
	onBoardGrant(t, e, 0, conduitGrantSrc)
	grave := graveCard(e, card(t, landSrc("Graveyard Mountain")), 0, 0)
	source := onBoardGrant(t, e, 0, "Name:Hand lock\nTypes:Enchantment\nOracle:x\n")
	e.AddContinuous(ContinuousEffect{Source: source, Controller: 0, Restriction: "CantPlayLand",
		RestrictParams: map[string]string{"Player": "Player", "Origin": "Hand"}})
	if playLandForbidden(e, 0, state.ZGraveyard, grave) {
		t.Fatal("hand lock unexpectedly covers graveyard origin")
	}
	if got := countPlayLand(e, grave); got != 1 {
		t.Fatalf("MayPlay$ graveyard land offers under hand lock = %d, want 1", got)
	}
}

var cantPlayLandCarriers = []string{
	"Aggressive Mining", "City in a Bottle", "Conjurer's Ban", "Cornered Market",
	"Experimental Frenzy", "Limited Resources", "Memory Vessel", "Moonhold",
	"Null Chamber", "Pardic Miner", "Rock Jockey", "Shaman's Trance", "Solfatara",
	"Territorial Dispute", "Tomik, Distinguished Advokist", "Turf Wound", "Ward of Bones",
	"Worms of the Earth",
}

func TestCantPlayLandCensus(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["stat:CantPlayLand"] {
		t.Fatal("effects.Supported() lacks stat:CantPlayLand")
	}
	if !effects.CantPlayLandParamsReadable(map[string]string{cards.PKMode.String(): "CantPlayLand", cards.PKPlayer.String(): "Player", cards.PKOrigin.String(): "Hand"}) {
		t.Fatal("the supported Player$ Player | Origin$ Hand shape must register")
	}
	for _, params := range []map[string]string{
		{cards.PKMode.String(): "CantPlayLand", cards.PKPlayer.String(): "Player"},
		{cards.PKMode.String(): "CantPlayLand", cards.PKPlayer.String(): "Player", cards.PKOrigin.String(): "Graveyard"},
	} {
		if effects.CantPlayLandParamsReadable(params) {
			t.Errorf("out-of-scope CantPlayLand params registered: %v", params)
		}
	}
	reg := searchTestRegistry(t)
	handOrigins := 0
	for _, name := range cantPlayLandCarriers {
		c := searchCorpusCard(t, reg, name)
		found := false
		for _, f := range c.Faces {
			for _, st := range f.Statics {
				if st.Mode != "CantPlayLand" {
					continue
				}
				found = true
				if origin, _ := st.Param(cards.PKOrigin); origin == "Hand" {
					handOrigins++
				}
			}
			for _, body := range f.SVars {
				if !strings.Contains(body, "Mode$ CantPlayLand") {
					continue
				}
				found = true
				if strings.Contains(body, "Origin$ Hand") {
					handOrigins++
				}
			}
		}
		if !found {
			t.Errorf("%s no longer carries Mode$ CantPlayLand", name)
		}
	}
	if handOrigins != 2 {
		t.Errorf("Origin$ Hand carrier count = %d, want 2", handOrigins)
	}
}
