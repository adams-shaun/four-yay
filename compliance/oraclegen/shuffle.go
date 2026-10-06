package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// CompareNoLibraryOrder opts a generated item out of comparing the shuffled
// order of a library. Library counts and all other snapshot fields remain
// compared.
const CompareNoLibraryOrder = "no_library_order"

// CanShuffleLibrary reports whether a face's effect graph can shuffle a
// library. It inspects linked APIs, SVar bodies and the prose-trigger fallback
// so every template for a face gets the same structural opt-in.
func CanShuffleLibrary(f *cards.Face) bool {
	if f == nil {
		return false
	}
	// Some owner-directed trigger bodies are represented in the corpus as a
	// prose trigger while their effect is carried by an unlinked SVar. Keep
	// those executable-text forms covered as a fallback to the API graph.
	oracle := strings.ToLower(f.Oracle)
	if strings.Contains(oracle, "shuffle") && strings.Contains(oracle, "library") {
		return true
	}
	seen := map[*cards.SA]bool{}
	var visits func(*cards.SA) bool
	visits = func(sa *cards.SA) bool {
		if sa == nil || seen[sa] {
			return false
		}
		seen[sa] = true
		api := strings.ToLower(sa.API)
		if api == "shuffle" {
			return true
		}
		if libraryShuffle(api, sa.ParamStr(cards.PKDestination), sa.ParamStr(cards.PKShuffle)) {
			return true
		}
		return visits(sa.Sub)
	}
	for _, sa := range f.Abilities {
		if visits(sa) {
			return true
		}
	}
	for i := range f.Triggers {
		if visits(f.Triggers[i].Effect) {
			return true
		}
	}
	for i := range f.Repls {
		if visits(f.Repls[i].With) {
			return true
		}
	}
	for _, body := range f.SVars {
		p := svarParams(body)
		api := strings.ToLower(p["DB"])
		if api == "shuffle" {
			return true
		}
		if libraryShuffle(api, p["Destination"], p["Shuffle"]) {
			return true
		}
	}
	return false
}

func libraryShuffle(api, destination, shuffle string) bool {
	api = strings.ToLower(api)
	if api == "changezone" || api == "changezoneall" {
		return strings.EqualFold(destination, "Library") && strings.EqualFold(shuffle, "True")
	}
	return api == "diguntil" && strings.EqualFold(shuffle, "True")
}
