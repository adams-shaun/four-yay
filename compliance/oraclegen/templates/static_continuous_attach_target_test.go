package templates

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

type staticAttachTargetRow struct {
	set, card, key string
}

func TestStaticContinuousAttachClusterTarget(t *testing.T) {
	rows := []staticAttachTargetRow{
		{"BIG", "Lotus Ring", "static#0.0"},
		{"BIG", "Sword of Wealth and Power", "static#0.0"},
		{"BLB", "Short Bow", "static#0.0"},
		{"BLB", "Starforged Sword", "static#0.0"},
		{"BLB", "Sword of Vengeance", "static#0.0"},
		{"DFT", "Rover Blades", "static#0.0"},
		{"DSK", "Chainsaw", "static#0.0"},
		{"DSK", "Conductive Machete", "static#0.0"},
		{"DSK", "Cursed Windbreaker", "static#0.0"},
		{"DSK", "Dissection Tools", "static#0.0"},
		{"DSK", "Glimmerlight", "static#0.0"},
		{"DSK", "Killer's Mask", "static#0.0"},
		{"DSK", "Saw", "static#0.0"},
		{"ECL", "Bark of Doran", "static#0.0"},
		{"ECL", "Stalactite Dagger", "static#0.0"},
		{"EOE", "Atomic Microsizer", "static#0.0"},
		{"EOE", "Auxiliary Boosters", "static#0.0"},
		{"EOE", "Hardlight Containment", "static#0.0"},
		{"EOE", "Hylderblade", "static#0.0"},
		{"EOE", "The Dominion Bracelet", "static#0.0"},
		{"FDN", "Basilisk Collar", "static#0.0"},
		{"FDN", "Fireshrieker", "static#0.0"},
		{"FDN", "Pirate's Cutlass", "static#0.0"},
		{"FIN", "Aettir and Priwen", "static#0.0"},
		{"FIN", "Astrologian's Planisphere", "static#0.0"},
		{"FIN", "Bard's Bow", "static#0.0"},
		{"FIN", "Black Mage's Rod", "static#0.0"},
		{"FIN", "Buster Sword", "static#0.0"},
		{"FIN", "Crystal Fragments", "static#0.0"},
		{"FIN", "Dark Knight's Greatsword", "static#0.0"},
		{"FIN", "Dragoon's Lance", "static#0.0"},
		{"FIN", "Dragoon's Lance", "static#0.1"},
		{"FIN", "Excalibur II", "static#0.0"},
		{"FIN", "Genji Glove", "static#0.0"},
		{"FIN", "Lion Heart", "static#0.0"},
		{"FIN", "Machinist's Arsenal", "static#0.0"},
		{"FIN", "Monk's Fist", "static#0.0"},
		{"FIN", "Ninja's Blades", "static#0.0"},
		{"FIN", "Paladin's Arms", "static#0.0"},
		{"FIN", "Red Mage's Rapier", "static#0.0"},
		{"FIN", "Sage's Nouliths", "static#0.0"},
		{"FIN", "Samurai's Katana", "static#0.0"},
		{"FIN", "Summoner's Grimoire", "static#0.0"},
		{"FIN", "The Masamune", "static#0.0"},
		{"FIN", "Thief's Knife", "static#0.0"},
		{"FIN", "Ultima Weapon", "static#0.0"},
		{"FIN", "Warrior's Sword", "static#0.0"},
		{"FIN", "White Mage's Staff", "static#0.0"},
		{"FRA", "Hunter's Axe", "static#0.0"},
		{"FRA", "Lich's Relic", "static#0.0"},
		{"FRA", "Medic's Kitesail", "static#0.0"},
		{"FRA", "Puppet Crafting", "static#0.0"},
		{"FRA", "Warrior's Blades", "static#0.0"},
		{"HOB", "Crude Bent Blade", "static#0.0"},
		{"HOB", "Dwarven Mattock", "static#0.0"},
		{"HOB", "Dwarven Shortsword", "static#0.0"},
		{"HOB", "Goblin Plate Mail", "static#0.0"},
		{"HOB", "My Precious", "static#0.0"},
		{"HOB", "Orcrist, Goblin-cleaver", "static#0.0"},
		{"HOB", "The Black Arrow", "static#0.0"},
		{"HOB", "Well-Worn Spatula", "static#0.0"},
		{"LCI", "Bloodthorn Flail", "static#0.0"},
		{"LCI", "Dead Weight", "static#0.0"},
		{"LCI", "Deconstruction Hammer", "static#0.0"},
		{"LCI", "Diamond Pick-Axe", "static#0.0"},
		{"LCI", "Dire Flail", "static#0.0"},
		{"LCI", "Glowcap Lantern", "static#0.0"},
		{"LCI", "Hunter's Blowgun", "static#0.0"},
		{"LCI", "Hunter's Blowgun", "static#0.1"},
		{"LCI", "Hunter's Blowgun", "static#0.2"},
		{"LCI", "Pirate Hat", "static#0.0"},
		{"LCI", "Sunfire Torch", "static#0.0"},
		{"LCI", "Swashbuckler's Whip", "static#0.0"},
		{"LCI", "Tarrian's Soulcleaver", "static#0.0"},
		{"LCI", "Zoetic Glyph", "static#0.0"},
		{"MKM", "Candlestick", "static#0.0"},
		{"MKM", "Concealed Weapon", "static#0.0"},
		{"MKM", "Cryptic Coat", "static#0.0"},
		{"MKM", "Knife", "static#0.0"},
		{"MKM", "Krovod Haunch", "static#0.0"},
		{"MKM", "Lead Pipe", "static#0.0"},
		{"MKM", "Rope", "static#0.0"},
		{"MKM", "Thinking Cap", "static#0.0"},
		{"MKM", "Wrench", "static#0.0"},
		{"MSH", "Captain America's Shield", "static#0.0"},
		{"MSH", "Hawkeye's Bow", "static#0.0"},
		{"MSH", "S.H.I.E.L.D. Spy Kit", "static#0.0"},
		{"MSH", "Secret Invasion", "static#0.0"},
		{"MSH", "Vibranium Energy Daggers", "static#0.0"},
		{"OTJ", "Gold Pan", "static#0.0"},
		{"OTJ", "Lavaspur Boots", "static#0.0"},
		{"SPM", "Doc Ock's Tentacles", "static#0.0"},
		{"SPM", "Friendly Neighborhood", "static#0.0"},
		{"SPM", "Rocket-Powered Goblin Glider", "static#0.0"},
		{"SPM", "Spider-Suit", "static#0.0"},
		{"SPM", "Web-Shooters", "static#0.0"},
		{"TDM", "Cori-Steel Cutter", "static#0.0"},
		{"TDM", "Dragonfire Blade", "static#0.0"},
		{"TDM", "Ringing Strike Mastery", "static#0.0"},
		{"TDM", "Stormbeacon Blade", "static#0.0"},
		{"TLA", "Avatar Destiny", "static#0.0"},
		{"TLA", "Glider Staff", "static#0.0"},
		{"TLA", "Honest Work", "static#0.0"},
		{"TLA", "Kyoshi Battle Fan", "static#0.0"},
		{"TLA", "Meteor Sword", "static#0.0"},
		{"TLA", "Swampsnare Trap", "static#0.1"},
		{"TLA", "Trusty Boomerang", "static#0.0"},
		{"TMT", "Bespoke Bō", "static#0.0"},
		{"TMT", "Hard-Won Jitte", "static#0.0"},
		{"TMT", "Improvised Arsenal", "static#0.0"},
		{"TMT", "Quintessential Katana", "static#0.0"},
		{"TMT", "Skateboard", "static#0.0"},
		{"WOE", "A Tale for the Ages", "static#0.0"},
		{"WOE", "Archon of the Wild Rose", "static#0.0"},
		{"WOE", "Bespoke Battlegarb", "static#0.0"},
	}

	if len(rows) != 115 {
		t.Fatalf("attachment cluster table has %d rows, want 115", len(rows))
	}
	reg := testutil.CorpusRegistry(t)
	type tally struct {
		served, skipped int
		cards           []string
	}
	perSet := map[string]*tally{}
	served, skipped := 0, 0
	for _, row := range rows {
		counts := perSet[row.set]
		if counts == nil {
			counts = &tally{}
			perSet[row.set] = counts
		}
		c, ok := reg.Lookup(row.card)
		if !ok {
			t.Errorf("%s: card %q absent from corpus", row.set, row.card)
			continue
		}
		var req *levelb.Requirement
		for _, candidate := range levelb.Requirements(c) {
			if candidate.Key == row.key {
				candidateCopy := candidate
				req = &candidateCopy
				break
			}
		}
		if req == nil {
			t.Errorf("%s %s: requirement %s absent", row.set, row.card, row.key)
			continue
		}
		if req.Sub != "static.continuous" {
			t.Errorf("%s %s %s: Sub = %q, want static.continuous", row.set, row.card, row.key, req.Sub)
			continue
		}
		_, skip := GenerateB(reg, row.card, *req)
		if skip == nil {
			counts.served++
			served++
			continue
		}
		// The generic reason, or the named one for an Aura that removes the
		// vanilla target's abilities (Honest Work): both are observation gaps.
		// A granted ability (levelb-static-granted-abilities) is named by shape
		// instead of the generic reason.
		if skip.Reason != "static effect not observable on a probe or the card" &&
			skip.Reason != "static removes the abilities of a permanent the fixture gives none" &&
			!strings.HasPrefix(skip.Reason, "static grants a") && !strings.HasPrefix(skip.Reason, "static adds an SVar") &&
			!strings.HasPrefix(skip.Reason, "static gains the") {
			t.Errorf("%s %s %s: unexpected skip reason %q", row.set, row.card, row.key, skip.Reason)
			continue
		}
		counts.skipped++
		counts.cards = append(counts.cards, fmt.Sprintf("%s %s", row.card, row.key))
		skipped++
	}

	sets := make([]string, 0, len(perSet))
	for set := range perSet {
		sets = append(sets, set)
	}
	sort.Strings(sets)
	var table strings.Builder
	for _, set := range sets {
		counts := perSet[set]
		sort.Strings(counts.cards)
		fmt.Fprintf(&table, "%s %d/%d", set, counts.served, counts.skipped)
		if len(counts.cards) > 0 {
			fmt.Fprintf(&table, " skipped=[%s]", strings.Join(counts.cards, ", "))
		}
		table.WriteByte('\n')
	}
	t.Logf("static attachment cluster: served=%d skip=%d (rows=%d)\n%s", served, skipped, len(rows), table.String())
	if served < 90 || served+skipped != len(rows) {
		t.Errorf("static attachment cluster target failed: served=%d skip=%d, want served >= 90 and all %d rows classified\nper-set served/skip and skipped cards:\n%s", served, skipped, len(rows), table.String())
	}
}
