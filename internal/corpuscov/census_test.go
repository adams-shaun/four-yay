package corpuscov

import "testing"

func TestCostHasMana(t *testing.T) {
	for cost, want := range map[string]bool{
		"4": true, "3 U": true, "T": false, "T Sac<1/CARDNAME>": false,
		"SubCounter<1/P1P1>": false, "1 B R T": true, "0": false, "PayEnergy<2>": false,
		"X X": true, "R/P": true,
	} {
		if got := costHasMana(cost); got != want {
			t.Errorf("costHasMana(%q) = %v, want %v", cost, got, want)
		}
	}
}

func TestBaseKey(t *testing.T) {
	for in, want := range map[string]string{
		"cast": "cast", "cast/alt1": "cast", "cast/flashback": "cast",
		"play_land/modal_land": "play_land", "ability#2": "ability#2", "mana@X": "mana@X",
	} {
		if got := baseKey(in); got != want {
			t.Errorf("baseKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		r    Row
		want string
	}{
		{Row{Universe: true}, ClassNeverInDeck},
		{Row{Universe: true, InDeckGames: 1}, ClassStructural},
		{Row{Universe: true, InDeckGames: 1, Potential: 3}, ClassPoolPriced},
		{Row{Universe: true, InDeckGames: 1, Offered: 2}, ClassAvoided},
		{Row{Universe: true, Offered: 2, ExploreChosen: 1}, ClassExploredOnly},
		{Row{Universe: true, Offered: 2, Chosen: 1}, ClassCovered},
		{Row{Offered: 2}, ClassDynamic},
	} {
		if got := classify(&tc.r); got != tc.want {
			t.Errorf("classify(%+v) = %q, want %q", tc.r, got, tc.want)
		}
	}
}
