package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// triggerChainPreAskCarriers is every corpus card with a printed trigger whose
// chain triggerChainPreAsks announces at placement (CR 603.3d): a non-modal
// trigger body with at least one SubAbility$ link the cast flow's own
// collectSubTargetPreAsks walk recognises as a target declaration. Measured
// 2026-10-03 at FORGE_REF over every face's T: lines. The whole point of the
// widening is that every one of these now asks its link targets while the
// ability is put on the stack -- before any player gets priority -- instead of
// mid-resolution. The one chain the walk finds but the scope excludes is
// pinned in triggerChainPreAskExclusions with its reason.
var triggerChainPreAskCarriers = []string{
	"A-Elderfang Ritualist",
	"Absolving Lammasu",
	"Aetherstorm Roc",
	"All Shall Smolder in My Wake",
	"Amy Rose",
	"Aria of Flame",
	"Assassin Gauntlet",
	"Axavar, Fate Thief",
	"Axelrod Gunnarson",
	"Barret, Avalanche Leader",
	"Betor, Ancestor's Voice",
	"Big Boa Constrictor",
	"Blacksmith's Talent",
	"Blue Dragon",
	"Body Snatcher",
	"Boomflinger",
	"Boros Battleshaper",
	"Brink of Madness",
	"Burning Sun's Avatar",
	"Caldera Pyremaw",
	"Capricious Efreet",
	"Captain Ripley Vance",
	"Celestial Gatekeeper",
	"Cloak and Dagger, Entwined",
	"Clockwork Hydra",
	"Coveted Falcon",
	"Cragganwick Cremator",
	"Dai Li Agents",
	"Dalek Intensive Care",
	"Decimator Beetle",
	"Descendant of Masumaro",
	"Don & Leo, Problem Solvers",
	"Dragon Turtle",
	"Drakuseth, Maw of Flames",
	"Evolutionary Escalation",
	"Favored Enemy",
	"Feisty Stegosaurus",
	"Fiendlash",
	"Filigree Vector",
	"Gimli's Reckless Might",
	"Gloom Ripper",
	"Goblin Grenadiers",
	"Greatsword of Tyr",
	"Gurmag Rakshasa",
	"Gut, Fanatical Priestess",
	"Halvar, God of Battle",
	"Hidetsugu and Kairi",
	"Hotel of Fears",
	"Hunter's Bow",
	"Hunter's Talent",
	"Huntmaster of the Fells",
	"Illicit Masquerade",
	"Impetuous Lootmonger",
	"Iname, Life Aspect",
	"Into the Earthen Maw",
	"Invasion of Muraganda",
	"Invasion of Regatha",
	"Iron Hills Stalwart",
	"Jaheira, Harper Emissary",
	"Jon Irenicus, Shattered One",
	"Karlov's Crossbow",
	"Kharasha Foothills",
	"Kimahri, Valiant Guardian",
	"Kinetic Ooze",
	"Knollspine Dragon",
	"Know Evil",
	"Kor Outfitter",
	"Kraul Harpooner",
	"Loaming Shaman",
	"Lord of Tresserhorn",
	"Lucius the Eternal",
	"Manticore of the Gauntlet",
	"Mechanical Mobster",
	"Mogg Bombers",
	"Monoist Circuit-Feeder",
	"Ms. Bumbleflower",
	"Nantuko Slicer",
	"Necron Deathmark",
	"Nightshade Assassin",
	"Niv-Mizzet, Guildpact",
	"Noctis, Heir Apparent",
	"Numbing Jellyfish",
	"Omo, Queen of Vesuva",
	"Palantír of Orthanc",
	"Persistent Constrictor",
	"Phantom Blade",
	"Portal Manipulator",
	"Prime Minister's Cabinet Room",
	"Prismwake Merrow",
	"Psychic Symbiont",
	"Rakdos Firewheeler",
	"Raubahn, Bull of Ala Mhigo",
	"Raven's Run",
	"Ravener",
	"Restless Cottage",
	"Rhino, Terrible Trampler",
	"Rolling Hamsphere",
	"Sinister Concierge",
	"Soul Echo",
	"Soul Seizer",
	"Sphinx-Bone Wand",
	"Sting, Bilbo's Sword",
	"Strategic Intervention",
	"Sword of Light and Shadow",
	"Sword of Sinew and Steel",
	"Swordsman, Sharp Scoundrel",
	"Sylvan Hierophant",
	"Syrix, Carrier of the Flame",
	"Tataru Taru",
	"Thalakos Deceiver",
	"The Great Aerie",
	"The Spot, Living Portal",
	"The Watcher in the Water",
	"Thorin, Mountain-king",
	"Thud-for-Duds",
	"Tolsimir, Friend to Wolves",
	"Trygon Prime",
	"Uldaros Theorix",
	"Vanille, Cheerful l'Cie",
	"Viconia, Nightsinger's Disciple",
	"Volatile Arsonist",
	"Volcano Hellion",
	"Winter Soldier, Reborn Avenger",
	"Witch of the Moors",
	"Yosei, the Morning Star",
	"Yoshimaru, Scrappy Stray",
	"Éomer, King of Rohan",
}

// triggerChainPreAskExclusions is every corpus card whose trigger body carries
// a targeting SubAbility$ link but stays out of the placement announcement,
// with the reason it does. Each reason is a shape the census test can detect:
// a card that leaves this set, or a card that enters it for any OTHER reason,
// fails the build rather than silently changing which triggers announce their
// link targets at placement.
var triggerChainPreAskExclusions = map[string]string{
	// Modal roots are out whole: CR 603.3c's mode election owns that target
	// ask, and the cast-side collector excludes a Charm root for the same
	// reason (collectSubTargetPreAsks). ModeTargets carries the per-mode
	// groups instead, so the two bindings stay disjoint.
	"My Will Is Irresistible": "modal root (Choices$): CR 603.3c mode election owns the ask",
	// These Defined$ target-reuse links use the previous target rather than
	// asking for a second one; chosenTargetsFor suppresses their duplicate
	// ask, so placement must not invent one.
	"Chromeshell Crab": "Defined$ target reuse: chosenTargetsFor suppresses the duplicate ask",
	"Daring Thief":     "Defined$ target reuse: chosenTargetsFor suppresses the duplicate ask",
	"Glen Elendra":     "Defined$ target reuse: chosenTargetsFor suppresses the duplicate ask",
	"Iroh, Tea Master": "Defined$ target reuse: chosenTargetsFor suppresses the duplicate ask",
	"Power Struggle":   "Defined$ target reuse: chosenTargetsFor suppresses the duplicate ask",
	"Puca's Mischief":  "Defined$ target reuse: chosenTargetsFor suppresses the duplicate ask",
	"Spawnbroker":      "Defined$ target reuse: chosenTargetsFor suppresses the duplicate ask",
	"Vedalken Plotter": "Defined$ target reuse: chosenTargetsFor suppresses the duplicate ask",
}

// TestTriggerChainPreAskCensus pins the scoped class both ways: a carrier
// that drifts out of the shape, or a new corpus chain that enters it, fails
// here rather than silently changing which triggers announce link targets at
// placement. The exclusions are pinned too, so a card cannot quietly join the
// unscoped set.
func TestTriggerChainPreAskCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := &Engine{}
	seen := map[string]bool{}
	excluded := map[string]string{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, tr := range f.Triggers {
				if tr.Effect == nil {
					continue
				}
				if len(e.triggerChainPreAsks(tr.Effect)) > 0 {
					seen[c.Faces[0].Name] = true
					continue
				}
				if !chainHasTargetingLink(tr.Effect) {
					continue
				}
				excluded[c.Faces[0].Name] = exclusionReason(tr.Effect)
			}
		}
	}
	got := make([]string, 0, len(seen))
	for name := range seen {
		got = append(got, name)
	}
	sort.Strings(got)
	want := append([]string(nil), triggerChainPreAskCarriers...)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("placement chain pre-ask carriers:\n  got  %q\n  want %q", got, want)
	}
	for name, reason := range triggerChainPreAskExclusions {
		gotReason, ok := excluded[name]
		if !ok {
			t.Errorf("%q is pinned as an exclusion but no trigger chain excludes it", name)
			continue
		}
		if gotReason != reason {
			t.Errorf("%q exclusion reason = %q, want %q", name, gotReason, reason)
		}
	}
	for name := range excluded {
		if _, ok := triggerChainPreAskExclusions[name]; !ok {
			t.Errorf("%q carries a targeting chain link but is neither in scope nor a pinned exclusion (reason %q)",
				name, excluded[name])
		}
	}
}

// chainHasTargetingLink reports whether ANY SubAbility$ link of the trigger
// body declares a ValidTgts$. The census pins the full raw class, including
// links excluded because Defined$ reuses an earlier target or because a
// TargetingPlayer$ chooser needs a placement continuation not implemented
// here; ChangeZone's dedicated chooser is now included in scope.
func chainHasTargetingLink(root *cards.SA) bool {
	for sa := root.Sub; sa != nil; sa = sa.Sub {
		if strings.TrimSpace(sa.Params["ValidTgts"]) != "" {
			return true
		}
	}
	return false
}

// exclusionReason classifies why a trigger body with a targeting chain link
// is out of the placement scope. It returns the machine-checkable reason the
// census pins; an unrecognised shape returns a reason no pinned entry matches,
// which fails the census above.
func exclusionReason(root *cards.SA) string {
	if strings.TrimSpace(root.Params["Choices"]) != "" {
		return "modal root (Choices$): CR 603.3c mode election owns the ask"
	}
	for sa := root.Sub; sa != nil; sa = sa.Sub {
		if strings.TrimSpace(sa.Params["TargetingPlayer"]) != "" {
			return "TargetingPlayer$ chooser: cast/placement-root opponent-pick machinery owns the ask"
		}
	}
	// TargetingPlayer$ above excludes a whole chain because placement cannot
	// currently hand that link's decision to the named chooser. Classify the
	// remaining recognized exclusions by the first target declaration.
	for sa := root.Sub; sa != nil; sa = sa.Sub {
		if strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
			continue
		}
		if (sa.CompiledAPI() == cards.APIChangeZone || sa.API == "ChangeZone") && effects.DefinedRefOf(sa).Set() {
			return "ChangeZone Defined$: the effect uses its named referent instead of asking"
		}
		if effects.DefinedIsTargetReuse(sa.Params["Defined"]) && sa.API != "Fight" {
			return "Defined$ target reuse: chosenTargetsFor suppresses the duplicate ask"
		}
		return "unrecognised exclusion shape"
	}
	return "unrecognised exclusion shape"
}
