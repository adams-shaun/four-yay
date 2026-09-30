// Package deck reads the repo's deck-list JSON — a bare {name, count} card
// list — and resolves it against a cards.Registry into the flat, repeated
// []*cards.Card that rules.Config.Decks wants. It is the one parser both
// the test fixtures (internal/testutil) and the match host use, so the two
// can never disagree about what a deck file means. A deck list carries no
// card text and is not the licensing hazard cards/boundary_test.go guards.
package deck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// File is the on-disk shape. Name and Format are authoring metadata,
// Commander/Commanders mark a deck as a Commander deck (see
// ValidateCommander), and only Cards decides what is dealt. Both commander
// fields are optional and additive: a deck file written before they existed
// parses identically (both empty, which means "constructed" — nothing
// changes for the repo's existing deck files), so adding them never makes an
// old list invalid. Commanders is the two-commander list (one or two
// names): a CR 903.13 partner pair, or a Doctor Who cycle Doctor's-companion
// pair. Commander is the legacy singular field, still read for every
// deck file that carries only it — CommanderNames() is the one accessor
// every reader should use, so the two spellings can never disagree.
type File struct {
	Name       string   `json:"name"`
	Format     string   `json:"format"`
	Archetype  string   `json:"archetype"`
	Commander  string   `json:"commander"`
	Commanders []string `json:"commanders,omitempty"`
	Cards      []Entry  `json:"cards"`
	Sideboard  []Entry  `json:"sideboard,omitempty"`
	// Policies is the deck's named bot policies, keyed by policy name. A
	// value is one policy DOCUMENT, kept as raw JSON here deliberately: this
	// package is deck data and knows nothing about bot behaviour, so the
	// weights are parsed by whoever owns them (botpolicy.ParseDeckPolicy)
	// rather than imported into the deck schema. That keeps the dependency
	// pointing one way -- botpolicy may read a deck, a deck never reads
	// botpolicy -- and it means a policy class this build does not implement
	// yet is carried through untouched instead of failing the deck load.
	//
	// The map is nil for every deck that declares none, which is every deck
	// in the repo today. A nil map must stay behaviourally inert: nothing
	// applies a deck's policy unless a caller names one, because the golden
	// acceptance games in rules/heads_test.go are bot-answered and three of
	// the twelve legacy golden decks are also botbench decks. Auto-applying a
	// deck policy would move all four pinned chain heads.
	Policies map[string]json.RawMessage `json:"policies,omitempty"`
}

// CommanderNames is the deck's commander designation as a list: the plural
// Commanders field when the file carries one (a two-commander partner pair
// or Doctor's companion pair), else the legacy singular Commander wrapped,
// else nil for a
// constructed deck. Every gate that used to read f.Commander == "" reads
// len(f.CommanderNames()) == 0 instead, so a plural-only file is a commander
// deck too.
func (f File) CommanderNames() []string {
	if len(f.Commanders) > 0 {
		return f.Commanders
	}
	if f.Commander != "" {
		return []string{f.Commander}
	}
	return nil
}

// CommanderIndices returns every commander's flat index in the deck Resolve
// produces, in CommanderNames order: the position each single copy (CR 903.4
// singleton) lands at when entries are expanded by count in file order.
// These are the indices rules.Config.Commanders expects — genesis moves
// each named object to the command zone. A name that is not one of the
// deck's entries contributes nothing (the result only makes sense for a deck
// that passed ValidateCommander, which guarantees presence), and a deck with
// no commander at all returns nil — callers gate on
// len(f.CommanderNames()) == 0 before calling it.
func (f File) CommanderIndices() []int {
	names := f.CommanderNames()
	if len(names) == 0 {
		return nil
	}
	pos := make(map[string]int, len(names))
	idx := 0
	for _, e := range f.Cards {
		key := cards.NormalizeName(e.Name)
		if _, wanted := pos[key]; !wanted {
			for _, n := range names {
				if cards.NormalizeName(n) == key {
					pos[key] = idx
					break
				}
			}
		}
		idx += e.Count
	}
	var out []int
	for _, n := range names {
		if p, ok := pos[cards.NormalizeName(n)]; ok {
			out = append(out, p)
		}
	}
	return out
}

// Entry is one line of a deck list.
type Entry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ManifestRow is one canonical card-name/count pair in an own-deck manifest.
type ManifestRow struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Manifest is the immutable, unordered genesis list assigned to one seat.
// Call Clone before publishing or retaining it outside its owner.
type Manifest struct {
	Name string `json:"name"`
	// Archetype is the deck's authoring archetype (File.Archetype), carried so
	// the seat's own prior survives into the projection the bot reads as
	// view.View.OwnDeck. Empty for a deck file that declares none, which
	// marshals away (omitempty) and preserves the pre-field wire shape.
	Archetype  string        `json:"archetype,omitempty"`
	Main       []ManifestRow `json:"main"`
	Sideboard  []ManifestRow `json:"sideboard,omitempty"`
	Commanders []string      `json:"commanders,omitempty"`
	// Curve is the derived mana curve over Main (CurveOf): one row per
	// front-face CMC in ascending order, each carrying the copies at that
	// cost. Derived observation data like the rows themselves, not game state.
	Curve []CurveRow `json:"curve,omitempty"`
}

// NewManifest canonicalizes the configured card lists without retaining their
// slices. Commander identities preserve the declared index order.
// archetype is the deck file's authoring archetype ("" when it declares
// none).
func NewManifest(name, archetype string, main, sideboard []*cards.Card, commanderIndices []int) Manifest {
	m := Manifest{Name: name, Archetype: archetype, Main: manifestRows(main), Curve: CurveOf(main)}
	if len(sideboard) > 0 {
		m.Sideboard = manifestRows(sideboard)
	}
	for _, index := range commanderIndices {
		if index >= 0 && index < len(main) && main[index] != nil && len(main[index].Faces) > 0 {
			m.Commanders = append(m.Commanders, main[index].Faces[0].Name)
		}
	}
	return m
}

// Clone returns a manifest with independently-owned slices.
func (m Manifest) Clone() Manifest {
	m.Main = append([]ManifestRow{}, m.Main...)
	if len(m.Sideboard) > 0 {
		m.Sideboard = append([]ManifestRow{}, m.Sideboard...)
	} else {
		m.Sideboard = nil
	}
	if len(m.Commanders) > 0 {
		m.Commanders = append([]string{}, m.Commanders...)
	} else {
		m.Commanders = nil
	}
	if len(m.Curve) > 0 {
		m.Curve = append([]CurveRow{}, m.Curve...)
	} else {
		m.Curve = nil
	}
	return m
}

func manifestRows(cardsIn []*cards.Card) []ManifestRow {
	counts := make(map[string]int)
	for _, c := range cardsIn {
		if c != nil && len(c.Faces) > 0 && c.Faces[0].Name != "" {
			counts[c.Faces[0].Name]++
		}
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]ManifestRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, ManifestRow{Name: name, Count: counts[name]})
	}
	return rows
}

// commanderOraclePhrase is the only way the pinned corpus marks a card that
// may be a commander despite not being a legendary creature: it appears as
// prose in the Oracle text, never as a structured field or type. This is
// checked in the deck validator (not here) so cards itself stays free of
// Commander rules; the substring is the literal printed phrase on those
// scripts.
const commanderOraclePhrase = "can be your commander"

// CommanderIndex returns the flat index of the deck's FIRST commander in
// the deck Resolve produces. It is the singular compatibility shim over
// CommanderIndices: every existing caller (and the tests that pin it) keeps
// working unchanged, while partner-pair decks seat every commander through
// CommanderIndices. The fallback for a deck whose first commander is not an
// entry — the count-expanded length — matches the original walk exactly.
func (f File) CommanderIndex() int {
	if names := f.CommanderNames(); len(names) > 0 {
		if idxs := f.CommanderIndices(); len(idxs) > 0 {
			return idxs[0]
		}
	}
	return f.firstCommanderIndex()
}

// firstCommanderIndex is the original singular walk over the legacy
// Commander field: the position the first entry whose name matches lands at,
// or the count-expanded length when no entry matches (which is why callers
// gate on a commander designation being present).
func (f File) firstCommanderIndex() int {
	cmdr := cards.NormalizeName(f.Commander)
	idx := 0
	for _, e := range f.Cards {
		if cards.NormalizeName(e.Name) == cmdr {
			return idx
		}
		idx += e.Count
	}
	return idx
}

// IsCommanderEligible reports whether a card may be a commander: CR 903.3 —
// a legendary card that is a creature card, a Vehicle card, or a Spacecraft
// card with one or more power/toughness boxes — or one of the cards the
// corpus marks as saying it can be a commander (planeswalker-legends,
// Partner/choose-a-background cases, which the corpus carries only as Oracle
// prose — see the report). The P/T-box carve-out is what keeps the two
// classes apart: every Vehicle is printed with a power/toughness box, while
// Spacecraft normally carry a defense box instead (The Eternity Elevator has
// neither power nor toughness and is not commander-legal), but the starship
// cycle's legendary Spacecraft with printed P/T (Dawnsire, The Seriema,
// Hearthhull) are.
func IsCommanderEligible(c *cards.Card) bool {
	for _, f := range c.Faces {
		if !f.IsLegendary() {
			continue
		}
		if f.IsCreature() || f.IsVehicle() || (f.IsSpacecraft() && hasPTBox(f)) {
			return true
		}
	}
	for _, f := range c.Faces {
		if strings.Contains(f.Oracle, commanderOraclePhrase) {
			return true
		}
	}
	return false
}

// hasPTBox reports whether a face is printed with a power/toughness box: any
// non-empty PT field (a characteristic-defining "*/*" is still a box).
func hasPTBox(f *cards.Face) bool { return f.PT != "" }

// Parse decodes a deck file and rejects the shapes that would otherwise
// fail later in a less obvious place: no cards, an unnamed entry, a
// non-positive count.
func Parse(raw []byte) (File, error) {
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, fmt.Errorf("deck: %w", err)
	}
	if len(f.Cards) == 0 {
		return File{}, fmt.Errorf("deck: no cards")
	}
	for i, e := range append(append([]Entry(nil), f.Cards...), f.Sideboard...) {
		if e.Name == "" {
			return File{}, fmt.Errorf("deck: entry %d has no name", i)
		}
		if e.Count <= 0 {
			return File{}, fmt.Errorf("deck: %q has count %d", e.Name, e.Count)
		}
	}
	// A declared policy is rejected here only for the shapes that would fail
	// later in a less obvious place -- an unnamed policy (unselectable, since
	// selection is by name) and a value that is not a JSON object (every
	// policy document is an object). The WEIGHTS are not validated here: this
	// package does not own them, and a policy class this build cannot read
	// yet must still load. PolicyNames() is the sorted accessor; ranging the
	// map directly is a determinism bug waiting to happen.
	for name, raw := range f.Policies {
		if strings.TrimSpace(name) == "" {
			return File{}, fmt.Errorf("deck: a policy has an empty name")
		}
		if !isJSONObject(raw) {
			return File{}, fmt.Errorf("deck: policy %q is not a JSON object", name)
		}
	}
	return f, nil
}

// isJSONObject reports whether raw is a JSON object, ignoring leading
// whitespace. json.RawMessage keeps the bytes verbatim, so the check is on
// the first significant byte rather than a full decode: a malformed body is
// the weight parser's error to report, with its own message.
func isJSONObject(raw json.RawMessage) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}

// Policy returns the named policy document. A name the deck does not declare
// is a HARD ERROR listing what it does declare, never a silent fallback to a
// default: a fallback would make two bench sides identical for exactly the
// decks missing the name under test, which reads as "the policy had no
// effect" when the truth is "the policy was never loaded". The caller parses
// the document (botpolicy.ParseDeckPolicy); this package only finds it.
func (f File) Policy(name string) (json.RawMessage, error) {
	raw, ok := f.Policies[name]
	if !ok {
		have := f.PolicyNames()
		if len(have) == 0 {
			return nil, fmt.Errorf("deck %q: no policy %q (the deck declares none)", f.Name, name)
		}
		return nil, fmt.Errorf("deck %q: no policy %q (declared: %s)", f.Name, name, strings.Join(have, ", "))
	}
	return raw, nil
}

// PolicyNames is the deck's declared policy names, sorted. Every reader uses
// it instead of ranging Policies: a map range whose order can reach a
// decision, an event or an error message is a replay bug (AGENTS.md's
// no-nondeterminism rule), and an error listing the available names is
// exactly such a message.
func (f File) PolicyNames() []string {
	if len(f.Policies) == 0 {
		return nil
	}
	out := make([]string, 0, len(f.Policies))
	for name := range f.Policies {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Resolve looks every entry up in r (which normalises names itself) and
// expands it by its count, in file order. The first unknown card is named
// in the error.
func (f File) Resolve(r *cards.Registry) ([]*cards.Card, error) {
	return resolveEntries(r, f.Name, f.Cards)
}

// CardNames lists every card name the file names -- main deck, sideboard
// and commanders, in file order, duplicates kept -- the card set a run that
// seats this deck needs from the corpus (cards.OpenCorpusFor).
func (f File) CardNames() []string {
	out := make([]string, 0, len(f.Cards)+len(f.Sideboard)+len(f.Commanders))
	for _, e := range f.Cards {
		out = append(out, e.Name)
	}
	for _, e := range f.Sideboard {
		out = append(out, e.Name)
	}
	return append(out, f.Commanders...)
}

// ResolveSideboard resolves the optional sideboard list independently of the
// main deck. A nil list remains nil, preserving the old genesis shape.
func (f File) ResolveSideboard(r *cards.Registry) ([]*cards.Card, error) {
	if len(f.Sideboard) == 0 {
		return nil, nil
	}
	return resolveEntries(r, f.Name+" sideboard", f.Sideboard)
}

func resolveEntries(r *cards.Registry, name string, entries []Entry) ([]*cards.Card, error) {
	var out []*cards.Card
	for _, e := range entries {
		c, ok := r.Lookup(e.Name)
		if !ok {
			return nil, fmt.Errorf("deck %q: card %q is not in the registry", name, e.Name)
		}
		for i := 0; i < e.Count; i++ {
			out = append(out, c)
		}
	}
	return out, nil
}

// Load reads, parses and resolves one deck file.
func Load(r *cards.Registry, path string) (File, []*cards.Card, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return File{}, nil, fmt.Errorf("deck: %w", err)
	}
	f, err := Parse(raw)
	if err != nil {
		return File{}, nil, fmt.Errorf("%s: %w", path, err)
	}
	cs, err := f.Resolve(r)
	if err != nil {
		return File{}, nil, err
	}
	return f, cs, nil
}

// Stem is the deck's short name: the file name without directory or
// extension ("decks/mono-red-goblins.json" -> "mono-red-goblins"). The host
// names seats after it.
func Stem(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// ValidateCommander enforces the Commander deck-construction rules (CR 903.4:
// exactly 100 cards including the commander and, except basic lands, no two
// cards with the same English name; CR 903.5: every card's colour identity is
// a subset of the commander's) for a deck file that names a commander. It is
// called explicitly — never from Parse — and a constructed deck (a File with
// no commander designation) is not subject to any of it and returns nil, so
// wiring the validator in never changes how an existing deck file is read.
// Every violation is reported together and each names the offending card (or
// the commander).
func (f File) ValidateCommander(r *cards.Registry) error {
	names := f.CommanderNames()
	if len(names) == 0 {
		return nil // constructed: no Commander rules apply
	}

	// A Commander deck has one commander, or a partner pair of two (CR
	// 903.13); anything more is not a deck shape the rules know.
	if len(names) > 2 {
		return fmt.Errorf("commander deck invalid:\n  %d commander designations (%s); a Commander deck has exactly one commander, or a partner pair of two (CR 903.13)", len(names), strings.Join(names, ", "))
	}

	// Resolve every commander first: the whole check hangs off their
	// identities and eligibility, so a missing or illegal commander is the
	// first thing named. The colour identity is the CR 903.5 UNION over the
	// commanders — what makes a partner pair's shared colours legal for the
	// whole deck (each half alone would reject the other's colours).
	var cmdrs []*cards.Card
	var errs []string
	cmdrID := uint8(0)
	for _, n := range names {
		c, ok := r.Lookup(n)
		if !ok {
			return fmt.Errorf("commander %q is not in the registry", n)
		}
		if !IsCommanderEligible(c) {
			return fmt.Errorf("commander %q is not a legendary creature, a Vehicle, or a Spacecraft with a power/toughness box, or a card that says it can be your commander", n)
		}
		cmdrs = append(cmdrs, c)
		cmdrID |= c.ColourIdentity()
	}
	if len(cmdrs) == 2 && cards.NormalizeName(names[0]) == cards.NormalizeName(names[1]) {
		return fmt.Errorf("commander deck invalid:\n  the commanders list names %q twice; a Commander deck's commanders are one or two DISTINCT cards (CR 903.3) — a duplicated designation would seat the same object twice", names[0])
	}
	if len(cmdrs) == 2 && !IsPartnerPair(cmdrs[0], cmdrs[1]) {
		return fmt.Errorf("commander pair %q and %q is not a legal commander pair: each must carry Partner, each must name the other with Partner with (CR 903.13), or one must carry Doctor's companion and the other must be the Doctor", names[0], names[1])
	}

	// The label the per-card messages name: the single commander, or the
	// pair joined — both keep the "outside commander" message shape the
	// existing assertions match.
	label := names[0]
	if len(names) == 2 {
		label = names[0] + " & " + names[1]
	}

	// Resolve every entry once and, keyed by normalised name, remember it so
	// the singleton and colour checks run once per distinct card.
	byName := map[string]*cards.Card{}
	for _, e := range f.Cards {
		k := cards.NormalizeName(e.Name)
		c, ok := r.Lookup(e.Name)
		if !ok {
			return fmt.Errorf("deck %q: card %q is not in the registry", f.Name, e.Name)
		}
		byName[k] = c
	}

	// CR 903.4 — exactly 100 cards including the commander.
	total := 0
	for _, e := range f.Cards {
		total += e.Count
	}
	if total != 100 {
		errs = append(errs, fmt.Sprintf("%s: %d cards, but a Commander deck must have exactly 100 including the commander", f.Name, total))
	}

	// CR 903.4 — every commander must actually be one of the deck's 100 cards.
	for _, n := range names {
		if _, isEntry := byName[cards.NormalizeName(n)]; !isEntry {
			errs = append(errs, fmt.Sprintf("commander %q is not in the deck's card list", n))
		}
	}

	// CR 903.4 singleton (basic lands excepted), CR 903.5 colour identity,
	// and CR 903.5d could-produce. The last is what stops a basic land from
	// being free: a basic land's colour IDENTITY is empty (its "{T}: Add {R}"
	// is granted by the land type, not printed in non-reminder text), but a
	// card WITH a basic land type may be in the deck only if EVERY colour of
	// mana it could produce — the colour each basic land type's mana ability
	// adds plus every colour an explicit production (including "any colour")
	// could yield — is in the commander's identity. The same check is what
	// makes the typed nonbasics (Badlands, the snow duals, Dryad Arbor)
	// belong to matching decks: they produce through their basic land types,
	// and their parenthesised mana ability is not scanned into the identity.
	for _, e := range f.Cards {
		c := byName[cards.NormalizeName(e.Name)]
		basic := isBasicLand(c)
		if e.Count > 1 && !basic && !AnyNumberAllowed(c) {
			errs = append(errs, fmt.Sprintf("card %q appears %d times; a Commander deck is singleton except basic lands", e.Name, e.Count))
		}
		if id := c.ColourIdentity(); id&^cmdrID != 0 {
			errs = append(errs, fmt.Sprintf("card %q has colour identity {%s}, outside commander %q's {%s}",
				e.Name, colourNames(id), label, colourNames(cmdrID)))
		}
		if prod := basicLandCouldProduce(c); prod&^cmdrID != 0 {
			errs = append(errs, fmt.Sprintf("card %q could produce {%s} mana, outside commander %q's {%s} (CR 903.5d)",
				e.Name, colourNames(prod), label, colourNames(cmdrID)))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("commander deck invalid:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// basicLandColour maps a basic land type to the colour its granted mana
// ability adds (the same map cards/intrinsic.go's basicLandMana layer uses;
// restated here because deck cannot reach it). Wastes is deliberately absent:
// it is a Basic LAND but not a basic LAND TYPE (CR 205.3b lists Plains,
// Island, Swamp, Mountain and Forest), and the mana it produces is
// colourless, which is not a colour at all — so it contributes nothing and
// a Wastes fits every deck.
var basicLandColour = map[string]uint8{
	"Plains":   cards.ColourWhite,
	"Island":   cards.ColourBlue,
	"Swamp":    cards.ColourBlack,
	"Mountain": cards.ColourRed,
	"Forest":   cards.ColourGreen,
}

// basicLandCouldProduce is the CR 903.5d "each colour of mana it could
// produce" set for a card with a basic land type. CR 903.5d: a card with a
// basic land type may be included only if every colour of mana it could
// produce is in the commander's colour identity — a stricter rule than the
// 903.5c identity subset, which a basic land passes on an empty identity
// (its "{T}: Add {R}" is granted by the land type, not printed rules text).
// The set is the union, over every face that has a basic land type, of (a)
// the colour each basic land type's granted mana ability adds and (b) every
// colour the face's own mana abilities could produce — including an
// "add one mana of any colour" production, because a card that can produce
// any colour can produce every one, so it needs a full WUBRG commander to
// be legal. A card with NO basic land type produces nothing this rule
// constrains, so a colourless mana rock that taps for any colour keeps an
// empty identity and stays legal in any deck (CR 903.5d applies only to
// basic-typed cards); Wastes, which is not a basic land type, is covered by
// the same return-0 path.
func basicLandCouldProduce(c *cards.Card) uint8 {
	var m uint8
	for _, f := range c.Faces {
		if !hasBasicLandType(f) {
			continue
		}
		m |= landTypeColours(f)
		m |= manaAbilityColours(f)
	}
	return m
}

// hasBasicLandType reports whether a face is printed with one of the five
// basic land types (Plains, Island, Swamp, Mountain, Forest — CR 205.3b). It
// is the gate CR 903.5d keys off: an ordinary nonbasic land, or a Wastes
// (Basic Land with no basic land subtype), is not a card "with a basic land
// type" and is not constrained by the could-produce rule.
func hasBasicLandType(f *cards.Face) bool {
	for _, t := range f.Types {
		if _, ok := basicLandColour[t]; ok {
			return true
		}
	}
	return false
}

// landTypeColours unions the colour each of a face's basic land types grants.
func landTypeColours(f *cards.Face) uint8 {
	var m uint8
	for _, t := range f.Types {
		if col, ok := basicLandColour[t]; ok {
			m |= col
		}
	}
	return m
}

// manaAbilityColours is the colour set a face's own printed mana abilities
// could produce. An "any colour" production (Produced$ Any / Combo Any —
// "{T}: Add one mana of any color.") expands to all five, because a card that
// could produce one mana of any colour could produce every colour, so CR
// 903.5d binds it to a full-WUBRG commander. A plain colour ("Produced$ W")
// or a choice of specific colours ("Combo W B", Murmuring Bosk's "{T}: Add
// {W} or {B}") contributes exactly those colours. A production whose colour
// is contingent on game context that a basic land's could-produce read must
// not widen — "Combo ColorIdentity" (Command Tower's "any color in your
// commander's color identity", which can only ever yield colours already in
// the commander's identity) and colourless ("C") — contributes nothing,
// because 903.5d never constrains a colour that is always inside the
// commander's identity anyway.
func manaAbilityColours(f *cards.Face) uint8 {
	var m uint8
	for _, a := range f.ManaAbilities() {
		m |= producedColours(a.Params["Produced"])
	}
	return m
}

// producedColours maps one Produced$ value to the colours the ability could
// yield. It is deliberately narrower than cards.ManaProduction's Any flag,
// which is set for ANY non-plain production — including "Combo W B" — and so
// would over-constrain a basic-typed card like Murmuring Bosk to all five
// colours when it really produces only white, black (and green via its basic
// land types).
//
// The token reading itself is cards.ProducedCounts, the one Produced$ parse
// every projection shares: a plain symbol token contributes its listed
// colours ("RR" and "R G" alike), and a token that names a script-level
// choice ("Chosen", "ColorIdentity", a "Special ..." word) contributes
// nothing -- never a phantom colour counted from the word's own runes. Only
// the any-colour widening above is local: the deck builder WANTS "Any" to
// bind a full-WUBRG commander (CR 903.5d), where the face projection
// conservatively resolves Any to colourless.
func producedColours(p string) uint8 {
	switch p {
	case "Any", "Combo Any":
		return cards.ColourWhite | cards.ColourBlue | cards.ColourBlack | cards.ColourRed | cards.ColourGreen
	}
	counts, _ := cards.ProducedCounts(p)
	var m uint8
	for i := 0; i < 5; i++ {
		if counts[i] > 0 {
			m |= 1 << uint(i)
		}
	}
	return m
}

// AnyNumberKeyword is the deck-construction keyword line Relentless Rats,
// Hare Apparent and their kin print (CR 100.2a's exception): "A deck can
// have any number of cards named CARDNAME."
const AnyNumberKeyword = "A deck can have any number of cards named CARDNAME."

// AnyNumberAllowed reports whether c's front face carries AnyNumberKeyword,
// which exempts the card from the Commander singleton rule (CR 903.5b) as it
// does from constructed's four-copy limit. Only the front face counts: a
// card outside the game has its front face's characteristics.
func AnyNumberAllowed(c *cards.Card) bool {
	if len(c.Faces) == 0 {
		return false
	}
	for _, k := range c.Faces[0].Keywords {
		if k == AnyNumberKeyword {
			return true
		}
	}
	return false
}

// isBasicLand reports whether a card is a basic land — the singleton rule's
// sole exemption (CR 903.4). A card is basic if any face carries the Basic
// land type.
func isBasicLand(c *cards.Card) bool {
	for _, f := range c.Faces {
		if f.IsBasic() && f.IsLand() {
			return true
		}
	}
	return false
}

// colourNames renders a colour-identity bitmask as the human set, e.g.
// "white/blue" or "colourless" for the empty set, for violation messages.
func colourNames(m uint8) string {
	order := []struct {
		bit  uint8
		name string
	}{
		{cards.ColourWhite, "white"},
		{cards.ColourBlue, "blue"},
		{cards.ColourBlack, "black"},
		{cards.ColourRed, "red"},
		{cards.ColourGreen, "green"},
	}
	var parts []string
	for _, o := range order {
		if m&o.bit != 0 {
			parts = append(parts, o.name)
		}
	}
	if len(parts) == 0 {
		return "colourless"
	}
	return strings.Join(parts, "/")
}
