package cards

import (
	"sort"
	"testing"
)

// Keyword expansion used to be one switch in keywords.go, which made every
// ticket touching a keyword edit the same file. It is now a table the
// per-keyword kw_*.go files register into. These tests pin the two properties
// that replace what the compiler used to give for free: no head silently loses
// its expander, and no two files claim the same head.

// expandedHeads is every keyword head the switch expanded before the split. A
// head dropping out of the table is a keyword that silently stops expanding --
// no test would otherwise fail, the carrier just quietly loses its ability.
var expandedHeads = []string{
	"etbCounter", "Devour", "ETBReplacement", "Vanishing",
	"Undying", "Persist", "Evolve", "Exalted", "Dethrone", "Afflict",
	"Flanking", "Hideaway", "Prowess", "Extort", "Soulbond", "Myriad",
	"Annihilator", "Ward", "Storm", "Gravestorm", "Replicate", "Conspire",
	"Living Weapon", "For Mirrodin", "Cumulative upkeep", "Echo",
	"Equip", "Transmute", "Cycling", "TypeCycling", "Level up", "Affinity",
	// Undaunted (CR 702.105) was added after the split; the pre-split switch never expanded it.
	"Undaunted",
	"Enchant", "Mobilize", "Afterlife", "Encore", "Embalm", "Eternalize",
	// Squad (CR 702.66) was added after the split: the corpus carries only
	// the K:Squad:<cost> line, so the expansion supplies the ETB trigger
	// that creates one token copy per squad payment (Count$SquadPaid).
	"Squad",
	// CR 702.70 Training: a genuinely new expansion added after the split
	// (not a head the old switch covered), so it is listed here to keep the
	// table equal to the registered set.
	"Training",
	// Melee (CR 702.121): an Attacks trigger pumped by the distinct
	// opponents captured from the whole declare-attackers batch.
	"Melee",
	// Mentor (CR 702.134): an Attacks self-trigger whose body is a targeted
	// PutCounter on another attacking creature with lesser power, added after
	// the split (cards/kw_mentor.go). The strict power restriction rides the
	// body's Mentor$ marker (rules/mentor.go), not a powerLTX spec.
	"Mentor",
	// Appended after the split (each is a keyword whose expansion the
	// pre-split switch never had): Exploit (CR 702.58, task exploit1).
	"Exploit",
	// Offspring (CR 702.175, task offspring1): the corpus carries only the
	// K:Offspring:<cost> line, so the expansion supplies the ETB trigger
	// that creates one 1/1 token copy when the additional cost was paid
	// (Count$OffspringPaid).
	"Offspring",
	// CR 702.118 Class: the level-up activator plus the level-gated granted
	// static/trigger/replacement, added after the split (kw-class).
	"Class",
	// Ravenous (CR 702.148): the {X} +1/+1-counter ETB plus the X>=5 draw,
	// expanded into an ETB trigger (task kw-ravenous).
	"Ravenous",
	// Fabricate (CR 702.121, task fabricate1): a ChangesZone ETB trigger
	// whose Charm elects N +1/+1 counters or N Servo tokens.
	"Fabricate",
	// Graft (CR 702.57): enters-with counters plus an optional move trigger.
	"Graft",
	// Reconfigure (CR 702.150, task kw-reconfigure): the attach/unattach
	// activated-ability pair per printed cost (an alternative second colon
	// field like Razorfield Ripper's PayEnergy<3> gets its own pair), both
	// sorcery-speed; the not-a-creature-while-attached switch is derived
	// state (state.Object.ReconfiguredAttached).
	"Reconfigure",
	// Demonstrate (CR 702.152): a SpellCast trigger on the card's own cast
	// whose DB$ Demonstrate body asks the may-copy election and the
	// opponent choice, the copies ordinary StackCopy mints (kw-demonstrate).
	"Demonstrate",
	// Partner with (CR 702.128): the ETB may-search for the named partner is
	// real rules text, not reminder, and no carrier's script prints it (task
	// kw-partner-with). The deck-construction designation half of the keyword
	// is read by deck.IsPartnerPair and is unchanged.
	"Partner with",
	// Backup (CR 702.70, task kw-backup): a ChangesZone ETB trigger whose
	// body is a targeted PutCounter chained to a grant built from the named
	// SVar, gated on the target being another creature. Added after the
	// split; the pre-split switch never had it.
	"Backup",
	// Fortify (CR 702.67): analogous to Equip but targets lands; it was added
	// after the split by kw-fortify and therefore belongs in this registry.
	"Fortify",
	// Battle cry (CR 702.33): an Attacks trigger whose PumpAll selects other
	// creatures that are attacking when the trigger resolves.
	"Battle cry",
	// The K:Prevent sentence keyword (task prevent-keyword-expansion): a
	// printed English sentence with no colon, so the head IS the sentence and
	// the registration is keyed on the three exact sentences the corpus
	// prints (kw-prevent). Added after the split; the pre-split switch never
	// expanded any of them.
	"Prevent all combat damage that would be dealt to CARDNAME.",
	"Prevent all combat damage that would be dealt to and dealt by CARDNAME.",
	"Prevent all damage that would be dealt to CARDNAME.",
	// Renown (CR 702.112, task agent-20260920T061844Z-01de2bee): the
	// combat-damage trigger places N +1/+1 counters and marks its source renowned.
	"Renown",
	// Sunburst (CR 702.47, task kw:Sunburst): a bare K:Sunburst line expanded
	// into the Moved -> Battlefield Updated Repl that puts Count$Converge
	// counters, kind decided on the printed face's types (+1/+1 for a
	// creature, charge otherwise). Added after the split; the pre-split
	// switch never expanded it (cards/kw_sunburst.go).
	"Sunburst",
	// Ninjutsu (CR 702.49, task kw-ninjutsu): the printed K:Ninjutsu:<cost>
	// line expanded into a hand-zone activated ability whose Cost$ carries
	// the printed mana cost plus Return<1/Creature.YouCtrl+attacking+unblocked>
	// and whose body puts the card onto the battlefield tapped and attacking
	// (cards/kw_ninjutsu.go). Added after the split; the pre-split switch
	// never had it.
	"Ninjutsu",
	// Crew (CR 702.122, task kw-crew): the printed K:Crew:<N> line expanded
	// into the Vehicle's own activated ability -- a tapXType<Any/
	// Creature.Other+withTotalPowerGE<N>> cost paying for an until-end-of-turn
	// Animate to artifact creature (cards/kw_crew.go). Added after the split;
	// the pre-split switch never had it.
	"Crew",
	// Cipher: printed spell encode appended as the resolving spell's own tail
	// instruction, plus a runtime encoded-creature trigger.
	"Cipher",
	// Unearth (CR 702.84, task kw-unearth): the printed K:Unearth:<cost>
	// line expanded into the card's own graveyard activated ability
	// (cards/kw_unearth.go), whose rules-side riders grant haste, register
	// the end-step exile, and redirect any departure to exile. Added after
	// the split; the pre-split switch never expanded it.
	"Unearth",
	// Outlast (CR 702.107, task agent-20260920T061844Z-8c4b9690): the
	// printed K:Outlast:<cost> line expanded into one ordinary sorcery-speed
	// PutCounter activation -- Cost$ T <mana> (the CR 702.107a {T} plus the
	// printed mana), one P1P1 counter on Self (cards/kw_outlast.go). Added
	// after the split; the pre-split switch never expanded it.
	"Outlast",
}

func TestEveryExpandedKeywordHasAnExpander(t *testing.T) {
	for _, head := range expandedHeads {
		if kwExpanders[head] == nil {
			t.Errorf("keyword head %q has no registered expander: it silently stops expanding", head)
		}
	}
}

func TestNoKeywordIsRegisteredThatTheSwitchNeverExpanded(t *testing.T) {
	// The mirror: a head in the table that was not in the switch means the
	// split invented an expansion, which is a behaviour change.
	known := map[string]bool{}
	for _, h := range expandedHeads {
		known[h] = true
	}
	var extra []string
	for head := range kwExpanders {
		if !known[head] {
			extra = append(extra, head)
		}
	}
	sort.Strings(extra)
	if len(extra) != 0 {
		t.Errorf("heads registered but not expanded before the split: %v", extra)
	}
}

func TestAnUnregisteredKeywordIsNotExpanded(t *testing.T) {
	// The switch had no default arm: a keyword whose meaning is a casting
	// option or a static property (Kicker, Flash, Protection) falls through
	// and is read directly by rules. The table must keep that, not panic.
	for _, head := range []string{"Kicker", "Flash", "Protection", "Indestructible"} {
		if kwExpanders[head] != nil {
			t.Errorf("%q must not be expanded: rules reads it directly", head)
		}
	}
	f := &Face{Keywords: []string{"Kicker:{2}"}}
	f.expandKeywords()
	if len(f.Triggers) != 0 || len(f.Repls) != 0 || len(f.Abilities) != 0 {
		t.Errorf("an unregistered head expanded anyway: %d T, %d R, %d A",
			len(f.Triggers), len(f.Repls), len(f.Abilities))
	}
}

func TestRegisteringOneKeywordTwicePanics(t *testing.T) {
	// Two files claiming one head is the merge accident the split makes
	// possible, so it must be loud at startup rather than an expansion that
	// quietly stopped being reached.
	const head = "TestOnlyDuplicateKeyword"
	fn := func(*Face, int, string, string, string, func(string, string) bool) {}
	registerKeyword(fn, head)
	defer delete(kwExpanders, head)

	defer func() {
		if recover() == nil {
			t.Error("registering a keyword head twice must panic")
		}
	}()
	registerKeyword(fn, head)
}
