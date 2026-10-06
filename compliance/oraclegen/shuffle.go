package oraclegen

import (
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// CompareNoLibraryOrder opts a generated item out of comparing the shuffled
// order of a library. Library counts and all other snapshot fields remain
// compared.
const CompareNoLibraryOrder = "no_library_order"

// intoLibraryText matches prose that moves a card into a library ("shuffle it
// into its owner's library", "puts it into their library").
var intoLibraryText = regexp.MustCompile(`\binto (?:\S+ ){0,3}?library`)

// CanShuffleLibrary reports whether a face's effect graph can shuffle a card
// that is not the library's filler into a library. XMage shuffles at random
// and gorge deterministically, so only then does the library order after the
// shuffle differ by luck. A search-and-shuffle only removes a card from a
// uniform library, so it is not marked. It inspects linked APIs, SVar bodies
// and the prose fallback so every template for a face gets the same
// structural opt-in.
func CanShuffleLibrary(f *cards.Face) bool {
	if f == nil {
		return false
	}
	var w shuffleWalk
	// Some owner-directed trigger bodies are represented in the corpus as a
	// prose trigger while their effect is carried by an unlinked SVar. Keep
	// the oracle text covered as a fallback to the API graph.
	oracle := strings.ToLower(f.Oracle)
	w.shuffles = strings.Contains(oracle, "shuffle")
	w.inserts = intoLibraryText.MatchString(oracle)
	seen := map[*cards.SA]bool{}
	var visit func(*cards.SA)
	visit = func(sa *cards.SA) {
		if sa == nil || seen[sa] {
			return
		}
		seen[sa] = true
		w.api(sa.API, sa.ParamStr(cards.PKOrigin), sa.ParamStr(cards.PKDestination), sa.ParamStr(cards.PKShuffle))
		visit(sa.Sub)
	}
	for _, sa := range f.Abilities {
		visit(sa)
	}
	for i := range f.Triggers {
		visit(f.Triggers[i].Effect)
	}
	for i := range f.Repls {
		visit(f.Repls[i].With)
	}
	for _, body := range f.SVars {
		p := svarParams(body)
		w.api(p["DB"], p["Origin"], p["Destination"], p["Shuffle"])
	}
	return w.shuffles && w.inserts
}

// shuffleWalk accumulates the two facts CanShuffleLibrary needs: some effect
// shuffles a library, and some effect puts a card into a library from outside
// it.
type shuffleWalk struct{ shuffles, inserts bool }

func (w *shuffleWalk) api(api, origin, destination, shuffle string) {
	api = strings.ToLower(api)
	if api == "shuffle" {
		w.shuffles = true
	}
	if api != "changezone" && api != "changezoneall" {
		if api == "diguntil" && strings.EqualFold(shuffle, "True") {
			w.shuffles = true
		}
		return
	}
	if !strings.EqualFold(destination, "Library") {
		return
	}
	if !strings.EqualFold(origin, "Library") {
		w.inserts = true
		if strings.EqualFold(shuffle, "True") {
			w.shuffles = true
		}
	}
}
