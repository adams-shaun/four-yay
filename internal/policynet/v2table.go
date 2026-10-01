// The v2 entity/candidate table (spellbench policy-network build plan, S0,
// P§3.3-P§3.6): ONE table shape every front end populates, so a network can
// train on rows built from gorge's own view.View and serve on rows built
// from the spellbench protocol-v2 Observation, and P§3.9's parity test can
// hold the two to byte-identity.
//
// The table has three parts:
//
//   - one V2Card row per object the acting seat can see, in the fixed order
//     P§3.4 names: players in seat order, and per player the zones
//     battlefield, hand (the acting seat only), graveyard, exile, command;
//     then the stack, bottom to top. Both front ends iterate exactly this
//     order, so a pointer feature's row index means the same object on both
//     sides.
//   - one V2Player row per seat, seat order.
//   - one V2Global row.
//
// Every row is fixed-width dense scalars plus a sorted sparse bag of hashed
// name rows (hashID, the pinned feature hash). The row builders live HERE,
// shared by both front ends: a front end converts its own projection into
// the neutral V2CardDatum / V2PlayerDatum / V2GlobalDatum below, and the
// datum→row conversion — the part a checkpoint pins — is single-sourced.
// Parity (P§3.9) then proves the two projections agree on the datum level.
//
// S0 deliberately encodes only what BOTH projections supply. Fields only
// one side projects (the observation's colors, copy, poison, designations,
// progress, lands played, known entries; the view's Round, LibraryTop,
// Available, PotentialActions, AbilityCosts) are not encoded; S1 widens the
// layout after the projector work (H5) closes the gaps. Residues that are
// conventions rather than gaps are named at their datum field.

package policynet

import (
	"math"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// V2 zones, in table row order.
const (
	V2ZoneBattlefield uint8 = iota
	V2ZoneHand
	V2ZoneGraveyard
	V2ZoneExile
	V2ZoneCommand
	V2ZoneStack
	v2ZoneCount
)

// V2 stack-entry kinds for a stack row (0 elsewhere).
const (
	V2StackSpell uint8 = iota + 1
	V2StackTriggered
	V2StackActivated
)

// v2TypeWords is the fixed type vocabulary of the card row: a type word
// outside the table falls in the "other" bucket.
var v2TypeWords = []string{"creature", "artifact", "enchantment", "land", "planeswalker",
	"instant", "sorcery", "battle", "dungeon", "other"}

// v2Supertypes is the fixed supertype vocabulary.
var v2Supertypes = []string{"legendary", "basic", "snow"}

// v2Keywords is the fixed keyword vocabulary of the card row, in canonical
// snake_case (v2KeywordName). A keyword outside the table counts in the
// "other" bucket and in the total.
var v2Keywords = []string{"flying", "vigilance", "trample", "reach", "deathtouch",
	"lifelink", "haste", "indestructible", "first_strike", "double_strike",
	"protection", "hexproof", "landwalk", "ward", "menace", "deathtouch_other"}

// v2CounterKinds is the fixed counter vocabulary of the card row.
var v2CounterKinds = []string{"p1p1", "m1m1", "loyalty", "other"}

// V2Steps is the phase-step vocabulary (v2engine/names.go phaseStep), the
// one-hot order of the global row.
var V2Steps = []string{"untap", "upkeep", "draw", "precombat_main", "beginning_of_combat",
	"declare_attackers", "declare_blockers", "combat_damage", "end_of_combat",
	"postcombat_main", "end_step", "cleanup", "other"}

// V2PhaseStep maps a gorge step to the shared vocabulary. The G front end
// reads the view's step STRING (state.Step.String) and maps it back through
// state.ParseStep, so both front ends derive the word from the one engine
// state the observation also reads.
func V2PhaseStep(s state.Step) string {
	switch s {
	case state.StepUntap:
		return "untap"
	case state.StepUpkeep:
		return "upkeep"
	case state.StepDraw:
		return "draw"
	case state.StepMain1:
		return "precombat_main"
	case state.StepBeginCombat:
		return "beginning_of_combat"
	case state.StepDeclareAttackers:
		return "declare_attackers"
	case state.StepDeclareBlockers:
		return "declare_blockers"
	case state.StepCombatDamage:
		return "combat_damage"
	case state.StepEndCombat:
		return "end_of_combat"
	case state.StepMain2:
		return "postcombat_main"
	case state.StepEnd:
		return "end_step"
	case state.StepCleanup:
		return "cleanup"
	}
	return "precombat_main"
}

// V2StepIndex returns the one-hot offset of a phase-step word.
func V2StepIndex(step string) int {
	for i, s := range V2Steps {
		if s == step {
			return i
		}
	}
	return len(V2Steps) - 1 // "other"
}

// v2Snake mirrors v2engine/names.go's snake (lowercase, spaces and dashes
// to underscores, quotes dropped, other punctuation dropped, trimmed).
func v2Snake(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\'' || r == '’':
			continue
		case r == ' ' || r == '-':
			b.WriteByte('_')
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "_")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	return out
}

// V2CounterName normalizes a gorge counter kind (Forge's "P1P1") the way
// the v2 wire names counters.
func V2CounterName(k string) string { return v2Snake(k) }

// V2KeywordName normalizes a gorge keyword string to the canonical keyword
// word the v2 wire carries. Mirrors v2engine/names.go keywordName.
func V2KeywordName(k string) string {
	head, _, _ := strings.Cut(k, ":")
	head = strings.TrimSpace(head)
	low := strings.ToLower(head)
	switch {
	case strings.HasPrefix(low, "protection"):
		return "protection"
	case strings.HasPrefix(low, "hexproof"):
		return "hexproof"
	case strings.HasSuffix(low, "walk") && !strings.Contains(low, " "):
		return "landwalk"
	}
	if strings.Count(head, " ") > 2 || strings.ContainsAny(head, ".,$<>{}()") || strings.Contains(low, "cardname") {
		return ""
	}
	return v2Snake(head)
}

// v2KeywordRow maps a canonical keyword word to its feature row, or -1 when
// the word is outside the vocabulary (the "other" bucket takes the count).
func v2KeywordRow(word string) int {
	for i, k := range v2Keywords {
		if k == word {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Row widths. Every width is part of the pinned encoding contract; a change
// fails the golden test in v2table_test.go until it is re-pinned.

// V2CardWidth is the dense scalar width of one card row.
const V2CardWidth = 64

// Named offsets into the card row.
const (
	// 0..5 zone one-hot (v2ZoneCount)
	v2cControllerMine  = 6 // 1 if the controller is the acting seat
	v2cControllerOther = 7 // 1 if the controller is another seat
	v2cOwnerMine       = 8
	v2cOwnerOther      = 9
	// 10..19 type one-hot (v2TypeWords)
	v2cLegendary  = 20
	v2cBasic      = 21
	v2cSnow       = 22
	v2cManaValue  = 23 // printed mana value / 8, clamped [0, 3]
	v2cPower      = 24 // power / 8, clamped [-3, 3]
	v2cToughness  = 25
	v2cDamage     = 26
	v2cTapped     = 27
	v2cSick       = 28
	v2cAttacking  = 29
	v2cAttPlayer  = 30 // attacks a player (not a battle)
	v2cBlocking   = 31 // blocks an attacker
	v2cAttached   = 32
	v2cFacedown   = 33
	v2cCtlIsOwner = 34
	// 35..48 keyword vocabulary (v2Keywords), each its value
	v2cKwBase = 35 // 14 keyword slots
	// 49 other-keyword count / 2 clamped [0, 3]
	v2cKwOther = 49
	// 50..53 counter vocabulary p1p1, m1m1, loyalty, other, / 4 clamped [0, 3]
	v2cCounterBase = 50
	v2cStackKind   = 54 // stack kind / 3 (0 off the stack)
	v2cHasTargets  = 55
	v2cTgtPlayer   = 56
	// 57..63 reserved zero (S1 widens here)
)

// V2PlayerWidth is the dense scalar width of one player row.
const V2PlayerWidth = 11

// V2GlobalWidth is the dense scalar width of the global row.
const V2GlobalWidth = 17

// V2Card is one table card row: the fixed dense scalars and the sparse
// hashed name rows, sorted by row.
type V2Card struct {
	Zone uint8
	Raw  []float32
	Rows []Feature
}

// V2Player is one player row.
type V2Player struct {
	Raw []float32
}

// V2Global is the global row.
type V2Global struct {
	Raw []float32
}

// V2Table is one decision-time entity table.
type V2Table struct {
	Cards   []V2Card
	Players []V2Player
	Global  V2Global
}

// V2CardDatum is the neutral per-object fact set both front ends produce
// before the shared row builder runs. Seat fields are the RELATIVE seat of
// the object (v2SeatMine / v2SeatOther / v2SeatNone); zones and stack kinds
// use the table constants above.
type V2CardDatum struct {
	Zone     uint8
	StackKd  uint8
	Name     string // the visible name; "" when hidden (no name row)
	CtlSeat  uint8  // v2SeatMine / v2SeatOther
	OwnSeat  uint8
	Types    []string // lowercase type words
	Supers   []string // lowercase supertype words
	Keywords []string // canonical keyword words (V2KeywordName output)
	MV       int      // printed mana value
	Power    int32
	Tough    int32
	Damage   int32
	Tapped   bool
	Sick     bool
	Att      bool
	AttPlyr  bool // attacks a player
	BlockN   int  // number of attackers this permanent blocks / is blocked by
	Attached bool
	Facedown bool
	Counters map[string]int32 // canonical counter name -> n
	HasTgts  bool
	TgtPlyr  bool
}

// Relative seats inside a datum.
const (
	v2SeatNone uint8 = iota
	v2SeatMine
	v2SeatOther
)

// clamp01..clamp helpers are shared scalar encodings.
func clamp01(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func v2clamp(x, lo, hi float32) float32 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func v2satur(x, div float32) float32 { return v2clamp(x/div, -3, 3) }

// V2CardRow converts one datum into the fixed row shape. THE single home of
// the card encoding (P§3.9): both front ends call this.
func V2CardRow(d V2CardDatum) V2Card {
	raw := make([]float32, V2CardWidth)
	raw[d.Zone] = 1
	switch d.CtlSeat {
	case v2SeatMine:
		raw[v2cControllerMine] = 1
	case v2SeatOther:
		raw[v2cControllerOther] = 1
	}
	switch d.OwnSeat {
	case v2SeatMine:
		raw[v2cOwnerMine] = 1
	case v2SeatOther:
		raw[v2cOwnerOther] = 1
	}
	// Types. A facedown object's printed characteristics are NOT encoded in
	// S0 (the two projections disagree about a face-down permanent's
	// characteristics: the observation reads the derived face-down shape, the
	// view reads the printed face it may not redact for its looker), so the
	// datum producers leave Types/Keywords/MV empty for a facedown object.
	if !d.Facedown {
		other := 0
		for _, t := range d.Types {
			hit := false
			for i, w := range v2TypeWords {
				if w == t {
					if w != "other" {
						raw[10+i] = 1
					}
					hit = true
					break
				}
			}
			if !hit {
				other++
			}
		}
		if other > 0 {
			raw[10+9] = 1 // "other"
		}
		for _, s := range d.Supers {
			for i, w := range v2Supertypes {
				if w == s {
					raw[v2cLegendary+i] = 1
				}
			}
		}
		raw[v2cManaValue] = v2clamp(float32(d.MV)/8, 0, 3)
		// Keywords: vocabulary bits, an other-count and the total.
		otherKw := 0
		totalKw := 0
		for _, k := range d.Keywords {
			if k == "" {
				continue
			}
			totalKw++
			if r := v2KeywordRow(k); r >= 0 {
				raw[v2cKwBase+r] = 1
			} else {
				otherKw++
			}
		}
		raw[v2cKwOther] = v2clamp(float32(otherKw)/2, 0, 3)
		_ = totalKw
	}
	raw[v2cPower] = v2satur(float32(d.Power), 8)
	raw[v2cToughness] = v2satur(float32(d.Tough), 8)
	raw[v2cDamage] = v2satur(float32(d.Damage), 8)
	if d.Tapped {
		raw[v2cTapped] = 1
	}
	if d.Sick {
		raw[v2cSick] = 1
	}
	if d.Att {
		raw[v2cAttacking] = 1
	}
	if d.AttPlyr {
		raw[v2cAttPlayer] = 1
	}
	if d.BlockN > 0 {
		raw[v2cBlocking] = 1
	}
	if d.Attached {
		raw[v2cAttached] = 1
	}
	if d.Facedown {
		raw[v2cFacedown] = 1
	}
	if d.CtlSeat != v2SeatNone && d.CtlSeat == d.OwnSeat {
		raw[v2cCtlIsOwner] = 1
	}
	// Counters, canonical order.
	other := float32(0)
	for i, k := range v2CounterKinds {
		if k == "other" {
			continue
		}
		if n, ok := d.Counters[k]; ok {
			raw[v2cCounterBase+i] = v2clamp(float32(n)/4, 0, 3)
		}
	}
	for k, n := range d.Counters {
		if k != "p1p1" && k != "m1m1" && k != "loyalty" {
			other += float32(n)
		}
	}
	raw[v2cCounterBase+3] = v2clamp(other/4, 0, 3)
	raw[v2cStackKind] = float32(d.StackKd) / 3
	if d.HasTgts {
		raw[v2cHasTargets] = 1
	}
	if d.TgtPlyr {
		raw[v2cTgtPlayer] = 1
	}
	// Sparse name rows: the card's name (only when visible), in the shared
	// TableRows space with a v2-prefixed format.
	var rows []Feature
	if d.Name != "" {
		rows = append(rows, Feature{Row: hashID("v2|name|" + d.Name), Value: 1})
	}
	sortFeatures(rows)
	return V2Card{Zone: d.Zone, Raw: raw, Rows: rows}
}

// sortFeatures sorts a sparse bag by row (and value for stability), so the
// byte encoding never depends on construction order.
func sortFeatures(fs []Feature) {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Row != fs[j].Row {
			return fs[i].Row < fs[j].Row
		}
		return fs[i].Value < fs[j].Value
	})
}

// V2PlayerDatum is the neutral per-seat fact set.
type V2PlayerDatum struct {
	Life    int32
	Pool    [6]int32 // W U B R G C
	Hand    int
	Library int
	IsMe    bool
}

// V2PlayerRow converts one player datum into the fixed row shape.
func V2PlayerRow(d V2PlayerDatum) V2Player {
	raw := make([]float32, V2PlayerWidth)
	raw[0] = v2satur(float32(d.Life), 20)
	for i, n := range d.Pool {
		raw[1+i] = v2clamp(float32(n)/4, 0, 3)
	}
	raw[7] = v2clamp(float32(d.Hand)/8, 0, 3)
	raw[8] = v2clamp(float32(d.Library)/40, 0, 3)
	if d.IsMe {
		raw[9] = 1
	}
	return V2Player{Raw: raw}
}

// V2GlobalDatum is the neutral global fact set.
type V2GlobalDatum struct {
	Turn   int // 1 when the game has not begun a turn
	Step   string
	Stack  int
	ActMe  bool // the active seat is the acting seat
	PrioMe bool // priority is with the acting seat
}

// V2GlobalRow converts one global datum into the fixed row shape.
func V2GlobalRow(d V2GlobalDatum) V2Global {
	raw := make([]float32, V2GlobalWidth)
	t := d.Turn
	if t <= 0 {
		t = 1
	}
	raw[0] = v2clamp(float32(t)/10, 0, 3)
	raw[1+V2StepIndex(d.Step)] = 1
	raw[14] = v2clamp(float32(d.Stack)/4, 0, 3)
	if d.ActMe {
		raw[15] = 1
	}
	if d.PrioMe {
		raw[16] = 1
	}
	return V2Global{Raw: raw}
}

// ---------------------------------------------------------------------------
// Deterministic byte encoding. The parity test (P§3.9) compares tables via
// these bytes; a corpus dump stores the digest.

// V2TableBytes returns the canonical byte encoding of a table: little-endian
// float32s in fixed order (cards in row order, then players, then global),
// sparse rows as (row uint16, value float32) pairs.
func V2TableBytes(t V2Table) []byte {
	var b []byte
	put := func(f float32) { b = append(b, f32bytes(f)...) }
	for _, c := range t.Cards {
		b = append(b, c.Zone)
		for _, f := range c.Raw {
			put(f)
		}
		for _, r := range c.Rows {
			b = append(b, byte(r.Row), byte(r.Row>>8))
			put(r.Value)
		}
	}
	for _, p := range t.Players {
		for _, f := range p.Raw {
			put(f)
		}
	}
	for _, f := range t.Global.Raw {
		put(f)
	}
	return b
}

func f32bytes(f float32) []byte {
	u := math.Float32bits(f)
	return []byte{byte(u), byte(u >> 8), byte(u >> 16), byte(u >> 24)}
}

// V2TableDigest is a stable 8-byte hex digest of the table bytes, for
// corpus rows and logs.
func V2TableDigest(t V2Table) string {
	b := V2TableBytes(t)
	h := uint64(14695981039346656037)
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	const hexd = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 0; i < 8; i++ {
		out[2*i] = hexd[(h>>(60-i*8))&0xf]
		out[2*i+1] = hexd[(h>>(56-i*8))&0xf]
	}
	return string(out)
}

// V2Equal reports whether two tables are byte-identical.
func V2Equal(a, b V2Table) bool {
	return string(V2TableBytes(a)) == string(V2TableBytes(b))
}

// viewSeatOf maps a view seat to the relative seat of a table row: the
// acting seat is "mine", every other seat is "other".
func viewSeatOf(p, viewer state.PlayerID) uint8 {
	if p == viewer {
		return v2SeatMine
	}
	return v2SeatOther
}

// ---------------------------------------------------------------------------
// Front end G: view.View -> table.

// V2SupertypeWords is the canonical supertype vocabulary of the v2
// projection (single-sourced here; v2engine's splitTypes reads it).
var V2SupertypeWords = map[string]string{"Basic": "basic", "Legendary": "legendary", "Ongoing": "ongoing", "Snow": "snow", "World": "world"}

// V2CardTypeWords is the canonical card-type vocabulary of the v2
// projection (single-sourced here; v2engine's splitTypes reads it).
var V2CardTypeWords = map[string]string{
	"Artifact": "artifact", "Battle": "battle", "Conspiracy": "conspiracy", "Creature": "creature",
	"Dungeon": "dungeon", "Enchantment": "enchantment", "Instant": "instant", "Kindred": "kindred",
	"Tribal": "kindred", "Land": "land", "Phenomenon": "phenomenon", "Plane": "plane",
	"Planeswalker": "planeswalker", "Scheme": "scheme", "Sorcery": "sorcery", "Vanguard": "vanguard",
}

// V2SupertypeWord maps one type-line word to its canonical supertype.
func V2SupertypeWord(w string) (string, bool) {
	s, ok := V2SupertypeWords[w]
	return s, ok
}

// V2CardTypeWord maps one type-line word to its canonical card type.
func V2CardTypeWord(w string) (string, bool) {
	t, ok := V2CardTypeWords[w]
	return t, ok
}

// appendUniqueWord appends w when not already present (splitTypes's
// deduplication, in first-seen order).
func appendUniqueWord(words []string, w string) []string {
	for _, x := range words {
		if x == w {
			return words
		}
	}
	return append(words, w)
}

// V2CardFromView converts one view card into the neutral datum. nameOK
// gates the name row (the G front end hides a face-down object's name only
// when the view itself redacted it: cv.Name == "" on a face-down card).
func V2CardFromView(cv *view.CardView, zone uint8, viewer state.PlayerID, ctlSeat, ownSeat uint8) V2CardDatum {
	d := V2CardDatum{Zone: zone, CtlSeat: ctlSeat, OwnSeat: ownSeat, Name: cv.Name,
		Damage: cv.Damage,
		Tapped: cv.Tapped, Sick: cv.SummonSick, Att: cv.Attacking,
		Facedown: cv.FaceDown,
	}
	// The observation's zone records carry combat/tap state only for
	// battlefield objects (its Permanent field is nil elsewhere), so front
	// end G zeroes the derived per-object combat state off the battlefield
	// as well: a graveyard copy of a card played this turn reads not-sick,
	// a stack spell not-tapped.
	if zone != V2ZoneBattlefield {
		d.Damage, d.Tapped, d.Sick, d.Att, d.AttPlyr, d.BlockN, d.Attached = 0, false, false, false, false, 0, false
		d.Counters = nil
		// Power/Toughness survive: the observation's characteristics carry
		// the printed face PT of a creature in any zone, and the view's
		// derived PT agrees with the printed face off the battlefield.
	}
	// S0's facedown convention (shared with front end V): a face-down
	// object's printed face — name, types, keywords, mana value, and the
	// power/toughness the two projections read differently (the view reads
	// the derived shape, the observation the printed face for its looker) —
	// is not encoded.
	if d.Facedown {
		d.Name = ""
		d.Power = 0
		d.Tough = 0
	}
	if cv.AttackingPlayer != nil {
		d.AttPlyr = true
	}
	// Blocking: the view carries, on an attacker, the list of blockers
	// (BlockedBy); on a blocker the pair is visible from the attacker's own
	// row. The datum takes the attacker's view here; a blocker's Blocking
	// bit is set by the front end from the attacker rows it already read.
	d.BlockN = len(cv.BlockedBy)
	if cv.AttachedTo != 0 {
		d.Attached = true
	}
	if !cv.FaceDown && cv.Name != "" {
		if cv.Types != "" {
			for _, t := range strings.Fields(cv.Types) {
				if t == "-" || t == "—" {
					continue
				}
				if s, ok := V2SupertypeWord(t); ok {
					d.Supers = appendUniqueWord(d.Supers, s)
				} else if w, ok := V2CardTypeWord(t); ok {
					d.Types = appendUniqueWord(d.Types, w)
				}
				// A subtype is neither encoded nor counted (the observation
				// carries it in subtypes, which S0 does not read).
			}
		}
		for _, k := range cv.Keywords {
			if w := V2KeywordName(k); w != "" {
				d.Keywords = append(d.Keywords, w)
			}
		}
		d.MV = mvOf(cv.ManaCost)
	} else {
		d.Name = ""
		d.MV = 0
	}
	if len(cv.Counters) > 0 {
		d.Counters = make(map[string]int32, len(cv.Counters))
		for k, n := range cv.Counters {
			d.Counters[V2CounterName(k)] = n
		}
	}
	if ctlSeat == v2SeatNone {
		d.CtlSeat = viewSeatOf(cv.Controller, viewer)
	}
	if ownSeat == v2SeatNone {
		d.OwnSeat = viewSeatOf(cv.Owner, viewer)
	}
	return d
}
