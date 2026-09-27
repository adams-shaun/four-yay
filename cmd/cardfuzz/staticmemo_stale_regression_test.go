package main

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestStaticMemoRefreshAtUnmovedLogHeadIsNotServedStale is the regression for
// the 2026-09-27 perf-sweep verify-mode panic: cardfuzz in verify mode
// aborted this seed (9870940514099297810) with
//
//	rules: layer-inert active() reuse at log 2135 disagrees with a rebuild
//	(1 vs 2 effects)
//
// and the same stale-layer result lets the payment planner's cached source
// census read a board the engine has already left.
//
// Root cause: active()'s cached, CR-613-sorted effect list (activeBuf, built
// from the staticContinuous memo) was keyed on (activeEpoch, activeVersion)
// alone. staticControlWants (rules/control_static.go) refreshes
// staticContinuous outside active() through refreshStaticContinuous, and on a
// content-changing rebuild the memo advances while the log head and
// continuousVersion do not -- so active() served a stale one-effect buffer
// after the memo had become two (Relic Vial's IsPresent$ Cleric.YouCtrl grant
// going live). The fix couples activeBuf to the static memo's build sequence
// (Engine.staticBuildSeq / activeStaticSeq).
//
// This runs the recorded failure through cardfuzz's own playGame, so the
// decks, seed and seats are identical to the report. Run with verify mode:
//
// go test -ldflags '-X github.com/adams-shaun/gorge/rules.derivedMemoVerifyFlag=1 -X github.com/adams-shaun/gorge/rules.layerInertVerifyFlag=1' -run '^TestStaticMemoRefreshAtUnmovedLogHeadIsNotServedStale$' ./cmd/cardfuzz/
//
// Verify mode is essential: without it the stale list can leave this game
// seemingly successful. The other derived-memo verifiers are set up from the
// link-time flag too; flipping only the layer verifier at runtime would not
// reproduce the failure.
func TestStaticMemoRefreshAtUnmovedLogHeadIsNotServedStale(t *testing.T) {
	if !rules.CacheVerificationEnabled() {
		t.Skip("requires layer and derived memo verify-mode ldflags (see above)")
	}
	reg := testutil.CorpusRegistry(t)
	decks := []genDeck{
		{Colour: "B", Cards: []string{
			"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp",
			"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Blood Crypt", "Cabal Pit", "Undiscovered Paradise",
			"Temp of the Damned", "Cabal Initiate", "Technodrome", "Tourach's Chant", "Gonti's Machinations",
			"Liliana Vess", "Chittering Harvester", "Vanquisher's Banner", "Canal Dredger", "Dakmor Lancer",
			"Crimson Cowl, Master of Evil", "Relentless Drednok", "Bushmeat Poacher", "Sigil of Distinction",
			"Painful Memories", "Demon's Jester", "Stonework Packbeast", "Teferi's Sentinel", "Cordial Vampire",
			"Wei Night Raiders", "Fetid Horror", "Grave Scrabbler", "Stratadon", "Gnarlbark Elm",
			"Ugin, the Ineffable", "Mind Extraction", "Ornithopter of Paradise", "Lord of the Undead", "Marsh Gas",
			"Bringer of the Last Gift", "Nettlecyst", "Gravecrawler", "Undead Servant", "Deadly Wanderings",
			"Daring Demolition", "Myojin of Grim Betrayal", "Rotting Fensnake", "Black Mana Battery",
			"Season of the Witch", "Rotted Hulk",
		}},
		{Colour: "B", Cards: []string{
			"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp",
			"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Emergence Zone", "Volcanic Island", "Steam Vents",
			"Xiahou Dun, the One-Eyed", "Clockwork Droid", "Nuisance Engine", "Hexplate Golem", "Grave Robbers",
			"Earthblighter", "Hullcarver", "Raiders' Wake", "Solar Array", "Wirefly Hive", "Relic Vial",
			"Team Transmitter", "Dragon's Claw", "Ghoulish Procession", "Ritual of Soot", "Manor Skeleton",
			"Unbreakable Bond", "Dark Sphere", "Black Knight", "Relentless X-ATM092", "Renegade Demon",
			"Stonecoil Serpent", "Putrid Imp", "Flowstone Thopter", "Relentless Drednok", "Birthing Boughs",
			"Azra Smokeshaper", "Phantasmagorian", "Midnight Charm", "Grim Reaper's Scythe", "Acquisitions Expert",
			"Prying Blade", "Skulking Ghost", "Rot Farm Mortipede", "Gravestorm", "Venomous Hierophant",
			"Ur-Golem's Eye", "Hammerhead, Maggia Boss", "Black Market Connections", "Zuko's Exile",
		}},
	}
	const seed = uint64(9870940514099297810)
	apc := autoPay{mode: "mixed"}
	if seats := seatList(apc.seats(seed, len(decks), exploreIndex(seed, true))); len(seats) != 1 || seats[0] != 1 {
		t.Fatalf("repro no longer has auto-pay seat 1: %v", seats)
	}
	fail, gc := playGame(reg, decks, seed, 100, 20000, 0, true, true, apc)
	if fail != nil {
		t.Fatalf("game at seed %d aborted: %s\n%s", seed, fail.Sig, fail.Diag)
	}
	if gc == nil || gc.ap == nil || gc.ap.Planned == 0 {
		t.Fatalf("repro never exercised auto-pay: coverage %+v", gc)
	}
}
