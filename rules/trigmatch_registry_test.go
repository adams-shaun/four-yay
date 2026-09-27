package rules

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The trigger-mode dispatch used to be one switch in trigger_match.go, which
// made every ticket touching a mode edit the same file. It is now a table the
// per-mode trigmatch_*.go files register into. These tests pin the two
// properties that replace what the compiler used to give us for free: no mode
// silently loses its matcher, and no two files claim the same mode.

// registeredModes is every Mode$ the switch dispatched before the split. A mode
// dropping out of the table is a mode that silently stops firing, which no
// other test would catch -- the trigger just never matches.
var registeredModes = []string{
	"ChangesZone", "ChangesZoneAll",
	"SpellCast", "SpellCastOrCopy", "SpellCopy", "AbilityCast", "SpellAbilityCast",
	"Attacks", "AttackersDeclared", "AttackersDeclaredOneTarget",
	"Cycled", "Explores", "Investigated", "RingTemptsYou",
	"CounterAdded", "CounterAddedOnce", "CounterRemoved",
	"Attached", "Exerted", "TokenCreated", "TokenCreatedOnce",
	"Sacrificed", "Discarded", "CommitCrime",
	"Taps", "TapsForMana",
	"DamageDone", "DamageDealtOnce", "DamageDoneOnce", "DamagePreventedOnce",
	"FlippedCoin", "Vote", "Drawn",
	"LifeLost", "LifeLostAll", "LifeGained",
	"BecomesTarget", "LandPlayed", "Phase", "Mutates", "Always",
}

// addedAfterTheSplit names every mode registered into the table since the
// split, each with the ticket that added it. The split did not invent these
// dispatches -- they are deliberate new trigger modes whose events did not
// exist before the split -- but a new mode must land here to be legal, which
// keeps the addition explicit and reviewable instead of silent. A mode NOT on
// either list is the merge accident the mirror test exists to catch.
var addedAfterTheSplit = []string{
	// trigdisc1: "Whenever you discover ..." (CR 701.57; Val, Marooned
	// Surveyor; Curator of Sun's Creation) and "Whenever you seek one or more
	// cards ..." (Vexyr, Ich-Tekik's Heir; Val; Lurker in the Deep). They
	// match the events.Discover / events.Seek marker Kinds, which were
	// appended for them, so neither mode could have been in the pre-split
	// switch.
	"Discover", "SeekAll",
	// rolled-die-trigger: "Whenever you roll a die ..." (CR 706; Mr. House,
	// Celebr-8000, Feywild Trickster). RolledDie fires once per die, each with
	// its own result, and its sibling RolledDieOnce fires once per roll action
	// however many dice it rolled. Neither name existed in the pre-split switch.
	"RolledDie", "RolledDieOnce",
	// connive1: "Whenever a creature you control connives ..." (CR 702.59;
	// Iron Monger Sadistic Tycoon, Glorious Purpose, Ultron Unlimited). It
	// matches the events.Connive marker Kind, which was appended for it, so
	// it could not have been in the pre-split switch.
	"Connives",
	// enlist1: "Whenever CARDNAME enlists a creature ..." (CR 702.160;
	// Goblin Morale Sergeant, Guardian of New Benalia). It matches the
	// events.Enlist marker Kind, appended for it (the Exerted shape), so it
	// could not have been in the pre-split switch.
	"Enlisted",
	// exploit1: "Whenever a creature exploits a creature ..." (CR 702.58c;
	// Graf Reaver, Colonel Autumn, Silumgar Scavenger and the rest of the
	// keyword's 24 trigger lines). It matches the events.Exploit marker Kind
	// the K:Exploit expansion emits, which was appended for it, so the mode
	// could not have been in the pre-split switch.
	"Exploited",
	// clash1: "Whenever you win/lose a clash ..." (CR 701.31: Marvo, Deep
	// Operative; Entangling Trap; Rebellion of the Flamekin; Sylvan Echoes --
	// 6 raw lines / 4 files at the pin). It matches the events.Clash marker
	// Kind effClash emits (one per clashing player), appended for it, so it
	// could not have been in the pre-split switch.
	"Clashed",
	// planar-verbs: "Whenever chaos ensues" (CR 901.9; 139 corpus files at
	// the pin, every plane card's chaos ability). It matches the
	// events.ChaosEnsues marker the planar-dice roll dispatch and the DB$
	// ChaosEnsues verb emit, appended for it, so it could not have been in
	// the pre-split switch.
	"ChaosEnsues",
	// planar-verbs: "When you planeswalk to CARDNAME" (CR 901.8; 46 corpus
	// files) and "When you planeswalk away from CARDNAME" (6 corpus files).
	// They match the events.PlanarWalk event's arriving/departing planes; no
	// new event Kind was needed, but neither mode existed in the pre-split
	// switch, so they could not have been dispatched there.
	"PlaneswalkedTo", "PlaneswalkedFrom",
	// manaexpend1: "Whenever you expend N ..." (the Bloomburrow Commander
	// expend keyword: Teapot Slinger, Trailtracker Scout, Wandertale Mentor,
	// Pyreswipe Hawk and 8 more corpus carriers). It matches rules/cast.go's
	// pay-time FlagManaExpendCast CastInfo emission, gated on a carrier being
	// on the caster's battlefield, so no new event Kind was needed -- but the
	// mode is new to the table, so it lands here.
	"ManaExpend",
	// specialize-alternate-mode-layer-unparsed: Specializes matches the newly
	// appended events.Specialize transition; the pre-split switch could not
	// dispatch an event kind that did not exist.
	"Specializes",
	// kw-class: "When this Class becomes level N" (CR 702.118c; 13 corpus
	// TriggerClassLevel SVar bodies). It matches the ordinary CounterChange
	// event the level-up activator's PutCounter emits, so the event existed
	// already but the MODE did not -- no pre-split switch arm could have
	// dispatched it.
	// phaseoutall1: "Whenever one or more other permanents phase out" (CR
	// 702.25b; The War Doctor). It matches the events.PhaseOut marker the
	// api:Phases primitive emits, which existed before the mode did, so no
	// pre-split switch arm could have dispatched it.
	"PhaseOutAll",
	"ClassLevelGained",
	// trig-damageall: "Whenever one or more <sources> deal damage to one or
	// more <targets>" (Contaminant Grafter, Malcolm Keen-Eyed Navigator,
	// Hordewing Skaab and 5 more corpus carriers). It matches the ordinary
	// Damage event the pre-split switch already carried for DamageDone -- the
	// response is one batch-level instance per damage batch (rules/
	// trigger_match.go's all-latch), but the MODE name is new, so no pre-split
	// switch arm could have dispatched it.
	"DamageAll",
	// trig-become-monarch: "Whenever a player becomes the monarch ..." (the
	// 5 corpus Mode$ BecomeMonarch carriers: Knights of the Black Rose,
	// Custodi Lich, Garland Royal Kidnapper, Starscream Power Hungry, and
	// Palace Jailer's command-zone SVar). It matches the events.MonarchChange
	// designation transition api:BecomeMonarch emits, which existed before
	// the mode did -- no pre-split switch arm dispatched BecomeMonarch.
	"BecomeMonarch",
	// counterchange-triggers: "Whenever one or more counters are removed from
	// CARDNAME" (Regenerations Restored, Chandra Fire Artisan, B.O.B. Bevy of
	// Beebles). The pre-split switch dispatched CounterRemoved but not its
	// Once variant; one CounterChange event already carries the whole removal
	// batch, which is the Once contract (matcher: counterRemovedMatches).
	"CounterRemovedOnce",
	// counterplayeraddedall: "Whenever you put one or more counters on a(n)
	// <spec>" (Generous Patron, Rikku Resourceful Guardian, Kros Defense
	// Contractor, All Will Be One; 8 corpus files). It matches the ordinary
	// CounterChange AND PlayerCounterChange placement events, so the events
	// existed already but the MODE did not -- no pre-split switch arm could
	// have dispatched it.
	"CounterPlayerAddedAll",
	// monstrosity (task agent-20260919T190014Z): "When CARDNAME becomes
	// monstrous, ..." (CR 701.31; Hydra Broodmaster, Fleecemane Lion,
	// Polukranos and the mode's 19 corpus carrier files). It matches the
	// events.AlterAttribute mark events.Apply folds for the Monstrosity$
	// param's Text "Monstrous" mark -- the event Kind existed already, but
	// the MODE did not -- so no pre-split switch arm could have dispatched it.
	"BecomeMonstrous",
	// trig-surveil: "Whenever you surveil ..." (CR 701.42; Mirko, Obsessive
	// Theorist; Dimir Spybug; Thoughtbound Phantasm; Whispering Snitch and 8
	// more corpus carriers). It matches the events.Surveil marker Kind,
	// which was appended for it, so it could not have been in the pre-split
	// switch.
	"Surveil",
	// agent-20260919T183016Z-886b1a86: "Whenever you give a gift ..."
	// (CR 702.168; Jolly Gerbils, the one corpus carrier). It matches the
	// events.GiveGift marker Kind, which was appended for it alongside the
	// GiftPromise election record, so it could not have been in the pre-split
	// switch -- no prior engine action produced a gift-completion event.
	"GiveGift",
	// agent-20260920T063816Z-abb68c8f: "Whenever CARDNAME becomes unattached
	// from a permanent ..." (CR 701.3b; Captain's Hook, Grafted Exoskeleton,
	// Grafted Wargear, Stitcher's Graft). It matches the events.Unattached
	// marker Kind, which was appended for it so that a detach fires this mode
	// while Mode$ Attached keeps ignoring it -- the detach SBAs previously
	// logged an empty-IDs events.Attach, so no pre-split switch arm could
	// have dispatched it.
	"Unattached",
	// trig-searched-library: a completed search marker was added because
	// ordinary library card moves cannot identify a search to the trigger.
	// The marker Kind is new, so the pre-split switch could not dispatch it.
	"SearchedLibrary",
	// TurnFaceUp (task agent-20260919T183249Z-0fb8ed97): "When
	// [this/that permanent] is turned face up" (CR 708.6 / CR 702.36e;
	// Printlifter Ooze, Woolly Loxodon and the mode's 125 corpus carriers).
	// It matches the events.TurnFaceUp Kind appended for it (effSetState's
	// Mode$ TurnFaceUp arm emits it), so no pre-split switch arm could have
	// dispatched it.
	"TurnFaceUp",
	// trig:Untaps (Key to the City): it matches the existing events.Untap
	// event although the old switch had no such mode.
	"Untaps",
	// trig-milled (task agent-20260919T183731Z-085022e9): the two
	// "whenever ... mills ..." modes (CR 701.17a; glowing_one and
	// infesting_radroach on Mode$ Milled, the_wise_mothman, mirelurk_queen
	// and screeching_scorchbeast on Mode$ MilledAll). They match the mill
	// MoveZone marker events.Mill emits -- the action marker effMill gained
	// with the mode, the IsDiscard/IsSacrifice discipline -- so no pre-split
	// switch arm existed for either name and no library->graveyard move
	// could be told from a mill without the marker.
	"Milled", "MilledAll",
	// DiscardedAll (agent-20260919T183145Z-c6610d46): "Whenever you discard
	// one or more [filtered] cards" (CR 701.8; 21 corpus T: lines plus Pure
	// Intentions' SVar body at the pin -- Veronica Dissident Scribe, Magmakin
	// Artillerist, Marauding Mako, Rielle the Everwise and the rest). It
	// matches the ordinary discard MoveZone event the pre-split switch already
	// carried for Discarded -- the response is one batch-level instance per
	// discard action (rules/trigger_match.go's discard latch) -- but the MODE
	// name is new, so no pre-split switch arm could have dispatched it.
	"DiscardedAll",
	// task scrybottom: "Whenever you choose to put one or more cards on the
	// bottom of your library while scrying, ..." (CR 701.18; The Temporal
	// Anchor, the corpus's one `ToBottom$ True` carrier at the pin -- 20
	// files carry `T:Mode$ Scry`). It matches the events.Scry marker Kind
	// appended for it, which rules' handleArrange emits only once the
	// KArrange answer is known (the count actually put on the bottom), so
	// no pre-split switch arm could have dispatched it.
	"Scry",
	// agent-20260919T183836Z-df01c8be: "Whenever this creature evolves"
	// (CR 702.99b; Watchful Radstag, Renegade Krasis -- the 2 corpus
	// carriers). It matches the events.Evolved marker Kind, which was
	// appended for it (resolveTop emits it once the Evolve keyword ability's
	// counter actually lands), so no pre-split switch arm could have
	// dispatched it -- evolve previously announced only its own counter
	// trigger and no separate mode existed.
	"Evolved",
	// trig:FullyUnlock (task agent-20260919T191104Z-95f1e316): CR 709.5's
	// "Eerie -- whenever ... you fully unlock a Room" (17 corpus carriers,
	// the Eerie enchantments e.g. Fear of Sleep Paralysis). It matches the
	// pre-existing events.DoorUnlock transition, but the MODE did not exist:
	// the pre-split switch dispatched the unlock door's own Mode$ UnlockDoor
	// through a dedicated face scan (rules/rooms.go), never a Mode$
	// FullyUnlock that lives on an unrelated battlefield permanent, so no
	// pre-split switch arm could have dispatched it.
	"FullyUnlock",
	// trig-proliferate: "Whenever you proliferate ..." (CR 701.27; the 6
	// corpus carrier files at the pin -- Voidwing Hybrid, Ezuri Stalker of
	// Spheres, Scheming Aspirant, Venser Corpse Puppet, Ichor Aberration,
	// Contagion Dispenser). It matches the events.Proliferate marker Kind,
	// which was appended for it because a CounterChange cannot be the signal
	// (a proliferate with no eligible recipient still happened, while an
	// ordinary counter addition is not a proliferate action), so it could not
	// have been in the pre-split switch.
	"Proliferate",
	// trig:BecomesTargetOnce (task agent-20260919T192459Z-2d5505d7):
	// Forge's TriggerBecomesTargetOnce, the BATCH sibling of Mode$
	// BecomesTarget ("whenever one or more creatures you control become the
	// target of an activated ability", Professor Hojo; Leyline of Combustion;
	// Psychic Battle). It matches the pre-existing events.TargetsChosen the
	// per-target sibling already matches -- no new event Kind -- but the
	// pre-split switch had only a BecomesTarget arm, so a Mode$
	// BecomesTargetOnce line fell off its end and never fired; the batch
	// cadence it needs (one instance per targeting ACTION) rides the
	// queue-time target-batch latch (rules/trigger_match.go).
	"BecomesTargetOnce",
	// foretell-trig (task agent-20260923T032009Z-3b9d3432): "Whenever you
	// foretell a card, ..." (CR 702.126b; Dream Devourer, the corpus's sole
	// carrier at the pin -- measured
	// `/usr/bin/grep -rlE 'T:Mode\$ Foretell' .cards/cardsfolder | wc -l`
	// => 1). It matches the {2} Foretell special action's pay-time
	// FlagForetold CastInfo (rules/cast.go's foretell branch, card still in
	// hand) and the effect-designation exile MoveZone markers
	// (applyFaceDownMarker's Foretold$ True composition), both of which
	// existed before the mode did -- but the pre-split switch had no
	// Foretell arm, so neither shape could have been dispatched through it.
	"Foretell",
}

func allRegisteredModeNames() []string {
	all := make([]string, 0, len(registeredModes)+len(addedAfterTheSplit))
	all = append(all, registeredModes...)
	all = append(all, addedAfterTheSplit...)
	return all
}

func TestEveryDispatchedTriggerModeHasAMatcher(t *testing.T) {
	for _, mode := range allRegisteredModeNames() {
		if trigMatchers[mode] == nil {
			t.Errorf("Mode$ %s has no registered matcher: it can never fire", mode)
		}
	}
}

func TestNoTriggerModeIsRegisteredThatTheSwitchNeverDispatched(t *testing.T) {
	// The mirror of the test above: a mode in the table that is on neither
	// the pre-split switch's list nor the documented post-split additions
	// means a dispatch appeared without review -- which is a behaviour
	// change. A legitimate new mode joins addedAfterTheSplit with its ticket
	// rather than broadening this test.
	known := map[string]bool{}
	for _, m := range allRegisteredModeNames() {
		known[m] = true
	}
	var extra []string
	for mode := range trigMatchers {
		if !known[mode] {
			extra = append(extra, mode)
		}
	}
	sort.Strings(extra)
	if len(extra) != 0 {
		t.Errorf("modes registered that the split never dispatched and no ticket added: %v", extra)
	}
}

func TestAnUnregisteredModeNeverFires(t *testing.T) {
	// The switch had no default arm: an unknown mode fell off the end with
	// matched still false. The table must keep that, not panic on a lookup miss.
	if trigMatchers["NoSuchModeExists"] != nil {
		t.Fatal("test precondition: NoSuchModeExists must not be registered")
	}
}

func TestRegisteringOneModeTwicePanics(t *testing.T) {
	// Two files claiming one mode is the merge accident the split makes
	// possible, so it must be loud at startup rather than a matcher that
	// quietly stopped being reached.
	const mode = "TestOnlyDuplicateMode"
	fn := func(*Engine, cards.Trigger, state.ObjID, events.Event, *state.Object) bool { return false }
	registerTrigMatcher(fn, mode)
	defer delete(trigMatchers, mode)

	defer func() {
		if recover() == nil {
			t.Error("registering a mode twice must panic")
		}
	}()
	registerTrigMatcher(fn, mode)
}
