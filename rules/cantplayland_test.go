package rules

// stat:CantPlayLand (CR 305.1) — "Players can't play lands from their hand."
// Memory Vessel (BIG) is the Standard carrier; 17 more exist corpus-wide.

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// memoryVesselSrc is the BIG carrier's shape: every player, hand only.
const memoryVesselSrc = "Name:Test Vessel\nManaCost:3\nTypes:Artifact\n" +
	"S:Mode$ CantPlayLand | Player$ Player | Origin$ Hand | Description$ Players can't play lands from their hand.\n" +
	"Oracle:x\n"

// graveyardOnlySrc grants a may-play-from-graveyard permission and a
// graveyard-scoped prohibition, so the helper's Origin$ scope can be pinned.
const graveyardOnlySrc = "Name:Test Trance\nManaCost:1\nTypes:Enchantment\n" +
	"S:Mode$ CantPlayLand | Player$ Player.Other | Origin$ Graveyard | Description$ Other players can't play lands from their graveyards.\n" +
	"Oracle:x\n"

// TestCantPlayLandHandBlocksTheOffer is the behavioural leaf: with a
// Memory-Vessel-shaped static live, a land in the controller's hand is not
// offered; without it, the same hand offers the play. The precondition (the
// static is live) is asserted so a vacuous setup fails.
func TestCantPlayLandHandBlocksTheOffer(t *testing.T) {
	t.Parallel()
	mtn := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")

	open := handEngine(t, mtn)
	if got := kinds(open.legalActions(0))["play_land"]; got != 1 {
		t.Fatalf("precondition: control hand did not offer the land play: play_land = %d", got)
	}

	e := handEngine(t, mtn)
	onBoard(t, e, 0, memoryVesselSrc)
	if got := len(e.activeStatics("CantPlayLand")); got != 1 {
		t.Fatalf("precondition: expected one live CantPlayLand static, got %d", got)
	}
	if got := kinds(e.legalActions(0))["play_land"]; got != 0 {
		t.Fatalf("CantPlayLand (Player$ Player | Origin$ Hand) did not withhold the hand play: play_land = %d", got)
	}
}

// TestCantPlayLandOriginScopesTheZone pins that Origin$ Hand does not reach a
// graveyard play and Origin$ Graveyard does not reach a hand play: the helper
// is zone-exact, not a blanket prohibition.
func TestCantPlayLandOriginScopesTheZone(t *testing.T) {
	t.Parallel()
	mtn := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")

	hand := handEngine(t, mtn)
	vessel := onBoard(t, hand, 0, memoryVesselSrc)
	landID := hand.G.Zone(state.ZHand, 0)[0]
	if !cantPlayLand(hand, 0, state.ZHand, landID) {
		t.Fatal("Origin$ Hand did not prohibit a hand land play")
	}
	if cantPlayLand(hand, 0, state.ZGraveyard, landID) {
		t.Fatal("Origin$ Hand wrongly prohibited a graveyard land play")
	}
	_ = vessel

	grave := handEngine(t, mtn)
	onBoard(t, grave, 0, graveyardOnlySrc)
	gid := grave.G.Zone(state.ZHand, 0)[0]
	// Player$ Player.Other excludes seat 0 (the active player) for its own
	// graveyard, so seat 0 is not prohibited there.
	if cantPlayLand(grave, 0, state.ZGraveyard, gid) {
		t.Fatal("Player$ Player.Other wrongly prohibited the active player")
	}
	if cantPlayLand(grave, 0, state.ZHand, gid) {
		t.Fatal("Origin$ Graveyard wrongly prohibited a hand land play")
	}
}

// TestCantPlayLandPrimitiveIsRegistered pins the support declaration the skip
// gate reads.
func TestCantPlayLandPrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["stat:CantPlayLand"] {
		t.Fatal(`effects.Supported() is missing "stat:CantPlayLand"`)
	}
}

// cantPlayLandCarriers is the corpus-wide class.
var cantPlayLandCarriers = []string{
	"Aggressive Mining",
	"City in a Bottle",
	"Conjurer's Ban",
	"Cornered Market",
	"Damping Engine",
	"Experimental Frenzy",
	"Limited Resources",
	"Memory Vessel",
	"Moonhold",
	"Null Chamber",
	"Pardic Miner",
	"Rock Jockey",
	"Shaman's Trance",
	"Solfatara",
	"Territorial Dispute",
	"Tomik, Distinguished Advokist",
	"Turf Wound",
	"Ward of Bones",
	"Worms of the Earth",
}

// TestCantPlayLandClassCensus pins the carrier set so a corpus-pin bump that
// adds a carrier fails loudly. It walks both delivery routes (printed S: and
// Effect-delivered bodies), the same shape the NoCleanupDamage ratchet uses.
func TestCantPlayLandClassCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	found := cantPlayLandCorpusCarriers(reg)
	list := make([]string, 0, len(found))
	for n := range found {
		list = append(list, n)
	}
	sort.Strings(list)
	want := append([]string(nil), cantPlayLandCarriers...)
	sort.Strings(want)
	if len(list) != len(want) {
		t.Fatalf("CantPlayLand carriers: got %d, want %d\ngot:  %s\nwant: %s", len(list), len(want), joinQuoted(list), joinQuoted(want))
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("CantPlayLand carrier %d = %q, want %q\ngot: %s", i, list[i], want[i], joinQuoted(list))
		}
	}
}

func cantPlayLandCorpusCarriers(reg *cards.Registry) map[string]bool {
	got := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, st := range f.Statics {
				if st.Mode == "CantPlayLand" {
					got[f.Name] = true
				}
			}
			f.EachRawEffectChild(func(ch cards.EffectChild) {
				if ch.Static != nil && ch.Static.Mode == "CantPlayLand" {
					got[f.Name] = true
				}
			})
		}
	}
	return got
}
