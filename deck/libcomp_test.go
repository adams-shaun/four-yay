package deck

import (
	"slices"
	"testing"
)

func TestLibraryCompositionFold(t *testing.T) {
	m := &Manifest{Main: []ManifestRow{{Name: "Bolt", Count: 4}, {Name: "Goblin", Count: 3}, {Name: "Mountain", Count: 5, Land: true}}}
	var lc LibraryComposition

	// Ordinary: seen cards subtract, an unlisted name (a token, a wished
	// card) is ignored, the total closes against the library size.
	lc.Begin(m)
	lc.See("Bolt")
	lc.See("Mountain")
	lc.See("Mountain")
	lc.See("Goblin Token")
	lc.Finish(9)
	if !lc.Known || !slices.Equal(lc.Counts, []int32{3, 3, 3}) || lc.Lands != 3 || lc.Nonlands != 6 || lc.Size != 9 {
		t.Fatalf("ordinary fold = %+v", lc)
	}

	cases := []struct {
		name string
		run  func()
		want LibraryUnknown
	}{
		{"no manifest", func() { lc.Begin(nil); lc.Finish(0) }, UnknownNoManifest},
		{"size mismatch", func() { lc.Begin(m); lc.Finish(11) }, UnknownSizeMismatch},
		{"overdrawn", func() {
			lc.Begin(m)
			for i := 0; i < 4; i++ {
				lc.See("Goblin")
			}
			lc.Finish(8)
		}, UnknownOverdrawn},
		{"hidden own card", func() { lc.Begin(m); lc.SeeHidden(); lc.Finish(11) }, UnknownHiddenOwnCard},
	}
	for _, tc := range cases {
		tc.run()
		if lc.Known || lc.Unknown != tc.want || len(lc.Counts) != 0 || lc.Lands != 0 || lc.Nonlands != 0 {
			t.Fatalf("%s: %+v, want Unknown %s", tc.name, lc, tc.want)
		}
	}

	// A hand-built unsorted list still resolves every name.
	un := &Manifest{Main: []ManifestRow{{Name: "Zebra", Count: 1}, {Name: "Apple", Count: 2}}}
	lc.Begin(un)
	lc.See("Apple")
	lc.See("Zebra")
	lc.Finish(1)
	if !lc.Known || !slices.Equal(lc.Counts, []int32{0, 1}) {
		t.Fatalf("unsorted fold = %+v", lc)
	}
	if got := (UnknownOverdrawn | UnknownSizeMismatch).String(); got != "overdrawn,size-mismatch" {
		t.Fatalf("reason string %q", got)
	}
}
