package testutil

import (
	"testing"
)

// repoCommanderDecks is the table of the m38 interim mono-colour Commander
// deck files and the commander each declares. It is the one place a new
// Commander deck must be listed: the driver below is table-driven over it,
// so a further deck added later is validated by construction, and the
// cross-check at the bottom fails any deck file that declares a commander
// the table does not name, so a Commander deck can never silently skip
// validation.
var repoCommanderDecks = []struct {
	file      string
	commander string
}{
	{"foundations-calling-all-angels", "Giada, Font of Hope"},
	{"foundations-keen-engineering", "Sai, Master Thopterist"},
	{"foundations-wretched-ranks", "Ghoulcaller Gisa"},
	{"foundations-reign-of-dragons", "Lathliss, Dragon Queen"},
	{"foundations-tramplesaurus-rex", "Ghalta, Primal Hunger"},
	{"hearthhull-worldseed-landfall", "Lord Windgrace"},
	{"valgavoth-endless-punishment", "Valgavoth, Harrower of Souls"},
	{"rakdos-muscle-scam-exe", "Rakdos, the Muscle"},
	{"ulalek-eldrazi", "Ulalek, Fused Atrocity"},
	{"avengers-assemble", "Captain America, Team Leader"},
	{"vivi-ornitier-cedh", "Vivi Ornitier"},
	{"pro-shaper", "Hearthhull, the Worldseed"},
	{"cavalry-charge", "Sidar Jabari of Zhalfir"},
	{"deadly-disguise", "Kaust, Eyes of the Glade"},
	{"science-pip", "Dr. Madison Li"},
}

// TestRepoCommanderDecksValidate is the m38 acceptance gate: every interim
// Commander deck file is a legal Commander deck by deck.ValidateCommander
// (CR 903.4: exactly 100 cards including the commander, singleton except
// basic lands; CR 903.5: every card's colour identity a subset of the
// commander's). Table-driven over the seven files so a further deck added
// later must be added to the table — the driver is the construction — and
// the cross-check at the end fails any deck file that declares a commander
// the table does not name.
func TestRepoCommanderDecksValidate(t *testing.T) {
	reg := CorpusRegistry(t)

	table := map[string]string{}
	for _, rcd := range repoCommanderDecks {
		this := rcd
		table[this.file] = this.commander
		t.Run(this.file, func(t *testing.T) {
			f := RepoDeckFile(t, this.file)
			if f.Commander != this.commander {
				t.Fatalf("%s: file declares commander %q, table says %q",
					this.file, f.Commander, this.commander)
			}
			if err := f.ValidateCommander(reg); err != nil {
				t.Fatalf("%s: %v", this.file, err)
			}
		})
	}

	// Every deck file that declares a commander must be in the table: a
	// additional Commander deck whose file landed in decks/ but not in
	// repoCommanderDecks is a fixture error, not a silent pass.
	for _, name := range RepoDeckNames() {
		f := RepoDeckFile(t, name)
		if f.Commander == "" {
			continue
		}
		if _, known := table[name]; !known {
			t.Errorf("%s declares commander %q but is not in repoCommanderDecks — add it to the table",
				name, f.Commander)
		}
	}
}
