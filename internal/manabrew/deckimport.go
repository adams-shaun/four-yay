package manabrew

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// DeckImportReport is the census ImportDeck returns alongside every import
// (spec §9 MB-15): the wire fields this importer intentionally drops from
// the result (counted, never silent), every deck-card name no registry
// lookup could resolve, and every resolved card the engine cannot fully
// play yet.
type DeckImportReport struct {
	// Ignored tallies, by field name, how many times a ManaBrew Deck/DeckCard
	// field this importer has no room for was seen. A key present with a
	// positive count means "this many entries carried that field and it was
	// dropped" -- never a silent drop with no trace.
	Ignored map[string]int
	// Unresolved is every DeckCardIdentity.Name (main deck, sideboard and
	// commanders) that no registry lookup -- direct or via the §6.1
	// fallbacks -- could resolve, in first-encounter order, deduplicated.
	Unresolved []string
	// Unsupported maps a resolved card's canonical (front-face) name to the
	// primitives and value heads cards.Registry.Unsupported reports it
	// needs, for every deck card the engine cannot fully play. A card that
	// resolved cleanly and is fully playable is absent from this map.
	Unsupported map[string][]string
}

func newDeckImportReport() *DeckImportReport {
	return &DeckImportReport{Ignored: map[string]int{}, Unsupported: map[string][]string{}}
}

func (r *DeckImportReport) ignore(field string, n int) {
	if n <= 0 {
		return
	}
	r.Ignored[field] += n
}

// diacriticFold strips the Latin-1 diacritics a ManaBrew client's name field
// may carry that cards.NormalizeName does not (it folds case, whitespace and
// punctuation, but a marked vowel is still a distinct letter to it). It
// mirrors internal/spellbench/v2shadow's fold helper (spec §6.1: "borrow ...
// the diacritic fold"); duplicated here rather than imported, since
// internal/spellbench is an unrelated tier this package has no business
// depending on.
var diacriticFold = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c", "ý", "y", "ÿ", "y",
	"Á", "A", "À", "A", "Â", "A", "Ä", "A", "Ã", "A", "Å", "A",
	"É", "E", "È", "E", "Ê", "E", "Ë", "E",
	"Í", "I", "Ì", "I", "Î", "I", "Ï", "I",
	"Ó", "O", "Ò", "O", "Ô", "O", "Ö", "O", "Õ", "O",
	"Ú", "U", "Ù", "U", "Û", "U", "Ü", "U",
	"Ñ", "N", "Ç", "C", "Ý", "Y",
)

// combinedFold is cards.NormalizeName plus the diacritic fold, applied in
// that order: NormalizeName already lowercases (which also lowercases an
// accented letter, e.g. "É" -> "é"), so the fold table only needs to carry
// the lowercase forms to match either casing.
func combinedFold(s string) string {
	return diacriticFold.Replace(cards.NormalizeName(s))
}

// buildFoldIndex is the §6.1 diacritic-fold fallback index: every face name
// in reg, keyed by its combinedFold spelling. cards.Registry itself indexes
// by NormalizeName only (no diacritic folding -- a corpus card keeps its
// printed accents, e.g. "Lim-Dûl's Vault"), so a ManaBrew client that sends
// the unaccented spelling would otherwise never resolve. Built fresh per
// import rather than cached on the registry: a deck import happens once per
// game, not per decision, and reg.Cards here is not large enough (tens of
// thousands of faces) to matter next to loading the corpus itself.
func buildFoldIndex(reg *cards.Registry) map[string]*cards.Card {
	idx := make(map[string]*cards.Card, reg.Len())
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f.Name == "" {
				continue
			}
			k := combinedFold(f.Name)
			if _, exists := idx[k]; !exists {
				idx[k] = c
			}
		}
	}
	return idx
}

// lookupCard resolves one ManaBrew deck-card name against reg, applying the
// §6.1 fallback chain: reg.Lookup itself already folds an "A // B" name to
// its front face (cards.NormalizeName truncates at "//"), so the only
// fallback this adds is the diacritic fold (via foldIndex), tried only when
// the direct lookup misses.
func lookupCard(reg *cards.Registry, foldIndex map[string]*cards.Card, name string) (*cards.Card, bool) {
	if c, ok := reg.Lookup(name); ok {
		return c, true
	}
	c, ok := foldIndex[combinedFold(name)]
	return c, ok
}

// canonicalName is the name a resolved card's own front face prints -- the
// spelling every other gorge deck file uses, and the one deck.File.Resolve
// will itself re-derive via the identical registry lookup. Falls back to raw
// when, somehow, a resolved card carries no faces (never true for a real
// corpus card; only a defensive guard).
func canonicalName(c *cards.Card, raw string) string {
	if len(c.Faces) == 0 || c.Faces[0].Name == "" {
		return raw
	}
	return c.Faces[0].Name
}

// tallyIgnoredIdentity records, in rep, every per-card ManaBrew field this
// importer drops: setCode, cardNumber, oracleId, tokenScript and foil. A
// deck's cards resolve by NAME alone (§6.1); none of these participate.
func tallyIgnoredIdentity(id mb.DeckCardIdentity, rep *DeckImportReport) {
	if id.SetCode != "" {
		rep.ignore("setCode", 1)
	}
	if id.CardNumber != "" {
		rep.ignore("cardNumber", 1)
	}
	if id.OracleID != "" {
		rep.ignore("oracleId", 1)
	}
	if id.TokenScript != "" {
		rep.ignore("tokenScript", 1)
	}
	if id.Foil {
		rep.ignore("foil", 1)
	}
}

// cardGroup is one ManaBrew DeckCard list (the main deck, the sideboard, or
// the commanders list) resolved and grouped by raw name: a ManaBrew Deck
// carries one entry per physical copy (there is no per-entry quantity
// field), so a 4-of is four identical DeckCard values, and this is where
// they are folded back into a single deck.Entry{Count: 4}.
type cardGroup struct {
	order    []string               // raw DeckCardIdentity.Name, first-seen order
	counts   map[string]int         // raw name -> copies seen
	resolved map[string]*cards.Card // raw name -> resolved card, absent if unresolved
}

func groupCards(list []mb.DeckCard, reg *cards.Registry, foldIndex map[string]*cards.Card, rep *DeckImportReport) cardGroup {
	g := cardGroup{counts: map[string]int{}, resolved: map[string]*cards.Card{}}
	for _, dc := range list {
		tallyIgnoredIdentity(dc.Identity, rep)
		name := dc.Identity.Name
		if _, seen := g.counts[name]; !seen {
			g.order = append(g.order, name)
			if c, ok := lookupCard(reg, foldIndex, name); ok {
				g.resolved[name] = c
			} else {
				rep.Unresolved = append(rep.Unresolved, name)
			}
		}
		g.counts[name]++
	}
	return g
}

// entries renders a resolved cardGroup as deck.File entries, in the group's
// first-seen order, using each card's canonical (front-face) spelling so the
// resulting deck.File reads exactly like a hand-authored one.
func (g cardGroup) entries() []deck.Entry {
	out := make([]deck.Entry, 0, len(g.order))
	for _, name := range g.order {
		n := name
		if c, ok := g.resolved[name]; ok {
			n = canonicalName(c, name)
		}
		out = append(out, deck.Entry{Name: n, Count: g.counts[name]})
	}
	return out
}

// reportUnsupported adds, for every resolved-but-not-fully-playable card in
// g, its canonical name and reg.Unsupported's finding to rep.Unsupported. A
// card already recorded (seen in an earlier group, e.g. the same card in
// both the main deck and the commanders list) is not re-computed.
func reportUnsupported(g cardGroup, reg *cards.Registry, supported map[string]bool, rep *DeckImportReport) {
	for _, name := range g.order {
		c, ok := g.resolved[name]
		if !ok {
			continue
		}
		cn := canonicalName(c, name)
		if _, already := rep.Unsupported[cn]; already {
			continue
		}
		if missing := reg.Unsupported(c, supported); len(missing) > 0 {
			rep.Unsupported[cn] = missing
		}
	}
}

// dedupeStable removes repeats from in, keeping first occurrence and
// original order.
func dedupeStable(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ImportDeck converts a ManaBrew Deck (protocol/manabrew, §A.5, the
// https://docs.manabrew.app/protocol/deck/ format) into a gorge deck.File,
// matching every card by name (spec §6.1: "Match by name") against reg.
//
// setCode, cardNumber, oracleId, tokenScript, foil, and every whole-deck
// section a gorge deck.File has no room for (tokens, attractions,
// contraptions, schemes, planes, companion, maybeboard, labels, customTags,
// cardTags, draft, playmat*, stackPositions) are ignored -- dropped from the
// result, but tallied in the returned report's Ignored map so nothing is
// dropped silently.
//
// A commander deck's commander(s) (d.Commanders) become the returned File's
// Commanders. A ManaBrew deck-builder may keep a commander's DeckCard out of
// the `cards` list entirely (deck-builder convention, not settled by the
// protocol text -- INFERRED); any commander not already present among
// d.Cards is folded into the returned File's Cards too, at one copy, so
// File.CommanderIndices keeps finding it the way every other gorge deck file
// expects.
//
// The returned error is non-nil, and names every name that would not
// resolve, exactly when the report's Unresolved list is non-empty; the
// returned File is still populated with every entry that DID resolve (an
// unresolved entry keeps its raw ManaBrew name, so a caller that ignores the
// error gets the same "not in the registry" failure downstream, from
// deck.File.Resolve, that ImportDeck itself detected). The report is always
// fully populated regardless of the error, so a caller can choose to block
// import on it or only surface it.
//
// Custom-deck play itself -- handing this File to a running table --
// is explicitly out of scope (spec §9 MB-15, Q11): ImportDeck only builds
// the deck.File value.
func ImportDeck(reg *cards.Registry, d mb.Deck, supported map[string]bool) (deck.File, DeckImportReport, error) {
	rep := newDeckImportReport()
	foldIndex := buildFoldIndex(reg)

	f := deck.File{Name: d.Name, Format: d.Format}

	main := groupCards(d.Cards, reg, foldIndex, rep)
	f.Cards = main.entries()
	haveMain := make(map[string]bool, len(main.order))
	for _, e := range f.Cards {
		haveMain[cards.NormalizeName(e.Name)] = true
	}

	if len(d.Sideboard) > 0 {
		sb := groupCards(d.Sideboard, reg, foldIndex, rep)
		f.Sideboard = sb.entries()
		reportUnsupported(sb, reg, supported, rep)
	}

	if len(d.Commanders) > 0 {
		cmdrs := groupCards(d.Commanders, reg, foldIndex, rep)
		names := make([]string, 0, len(cmdrs.order))
		for _, raw := range cmdrs.order {
			n := raw
			if c, ok := cmdrs.resolved[raw]; ok {
				n = canonicalName(c, raw)
			}
			names = append(names, n)
			if !haveMain[cards.NormalizeName(n)] {
				f.Cards = append(f.Cards, deck.Entry{Name: n, Count: 1})
				haveMain[cards.NormalizeName(n)] = true
			}
		}
		f.Commanders = names
		reportUnsupported(cmdrs, reg, supported, rep)
	}

	reportUnsupported(main, reg, supported, rep)

	// Whole-deck sections this importer never carries into a gorge deck.File.
	rep.ignore("tokens", len(d.Tokens))
	rep.ignore("attractions", len(d.Attractions))
	rep.ignore("contraptions", len(d.Contraptions))
	rep.ignore("schemes", len(d.Schemes))
	rep.ignore("planes", len(d.Planes))
	rep.ignore("maybeboard", len(d.Maybeboard))
	rep.ignore("labels", len(d.Labels))
	rep.ignore("customTags", len(d.CustomTags))
	if len(d.CardTags) > 0 {
		rep.ignore("cardTags", len(d.CardTags))
	}
	if d.Companion != nil {
		rep.ignore("companion", 1)
	}
	if len(d.Draft) > 0 {
		rep.ignore("draft", 1)
	}
	if d.PlaymatURL != "" || d.PlaymatAssetID != "" || len(d.PlaymatSettings) > 0 {
		rep.ignore("playmat", 1)
	}
	if len(d.StackPositions) > 0 {
		rep.ignore("stackPositions", 1)
	}

	rep.Unresolved = dedupeStable(rep.Unresolved)

	var err error
	if len(rep.Unresolved) > 0 {
		err = fmt.Errorf("manabrew: deck %q: %d card(s) not in the registry: %s", d.Name, len(rep.Unresolved), strings.Join(rep.Unresolved, ", "))
	}
	return f, *rep, err
}
