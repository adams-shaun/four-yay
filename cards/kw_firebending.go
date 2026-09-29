// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import "strings"

func kwFirebending(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// CR 702.189a: "Firebending N" is a triggered ability — "Whenever this
	// creature attacks, add N {R}. Until end of combat, you don't lose this
	// mana as steps and phases end." The expansion supplies all three parts:
	//
	//   - the attack trigger is the ordinary Mode$ Attacks self-trigger the
	//     other combat keywords use (Annihilator/Dethrone/Melee);
	//   - the mana is a DB$ Mana body producing N red (effMana, effects/
	//     misc.go), whose Amount$ resolves N through the ordinary Num
	//     grammar so the power-derived forms work: the corpus prints both
	//     K:Firebending:<literal> (Fire Sages) and K:Firebending:X with the
	//     SVar X naming a Count$ (Firebending Student's Count$CardPower,
	//     Zuko's Count$YourCountersExperience), and Num resolves a bare SVar
	//     name from the resolving face's table;
	//   - PersistentUntilEndOfCombat$ True is the end-of-combat exception
	//     (effects' PersistentMana$ sibling): the units survive the step
	//     boundaries within the combat phase and are demoted as the
	//     end-of-combat step is left.
	//
	// The parameter is the printed "[N][:, where X is ...]" tail; the
	// value is everything before the first colon, which is the bare number
	// for every fixed carrier and the SVar name for every derived one. A
	// blank parameter is not a Firebending line this build can price, so it
	// is left unexpanded (the switch had no default arm either) rather than
	// adding an unknown amount of mana.
	n := strings.TrimSpace(param)
	if j := strings.IndexByte(n, ':'); j >= 0 {
		n = strings.TrimSpace(n[:j])
	}
	if n == "" {
		return
	}
	bendSVar := "__kwFirebendingMarker"
	f.setSVar(bendSVar, "DB$ ElementalBend | Verb$ fire")
	f.addKeywordTrigger(head, k, "Mode$ Attacks | ValidCard$ Card.Self | TriggerDescription$ Firebending",
		"DB$ Mana | Produced$ R | Amount$ "+n+" | PersistentUntilEndOfCombat$ True | SubAbility$ "+bendSVar, has)
}

func init() { registerKeyword(kwFirebending, "Firebending") }
