package cards

import (
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// cleanCopy is c's exported value tree (a gob round trip), so two cards
// compare by content without their catalog bindings.
func cleanCopy(t *testing.T, c *Card) *Card {
	t.Helper()
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(c); err != nil {
		t.Fatal(err)
	}
	var out Card
	if err := gob.NewDecoder(&b).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return &out
}

type faceDerived struct {
	Power, Toughness     int
	Cmc                  int32
	Colour               uint8
	CDA, AllTypes        bool
	Mana                 ManaProduction
	Interests            TriggerInterest
	InterestsOK          bool
	TypeMask             TypeMask
	OffBF, EffectZone    bool
	AbilityAPIs, AbKinds []int
}

func derivedOf(c *Card) []faceDerived {
	var out []faceDerived
	for _, f := range c.Faces {
		if f == nil {
			out = append(out, faceDerived{})
			continue
		}
		d := faceDerived{Power: f.Power(), Toughness: f.Toughness(), Cmc: f.Cmc(), Colour: f.ColourIdentity(),
			CDA: f.CharacteristicDefining(), AllTypes: f.AllCreatureTypesCDA(), Mana: f.ManaProduction(),
			TypeMask: f.compiledTypeMask, OffBF: f.ContinuousStaticsMayFunctionOffBattlefield(),
			EffectZone: f.StaticsMayNameEffectZone()}
		d.Interests, d.InterestsOK = f.CompiledTriggerInterests()
		for _, sa := range f.Abilities {
			for s := sa; s != nil; s = s.Sub {
				d.AbilityAPIs = append(d.AbilityAPIs, int(s.CompiledAPI()))
				d.AbKinds = append(d.AbKinds, int(s.CompiledKind()))
			}
		}
		out = append(out, d)
	}
	return out
}

func sameCard(t *testing.T, what string, got, want *Card) {
	t.Helper()
	if !reflect.DeepEqual(cleanCopy(t, got), cleanCopy(t, want)) {
		t.Errorf("%s: subset card's exported value differs from the full registry's", what)
	}
	if g, w := derivedOf(got), derivedOf(want); !reflect.DeepEqual(g, w) {
		t.Errorf("%s: derived/compiled values differ:\n got %+v\nwant %+v", what, g, w)
	}
}

// TestSubsetRegistryMatchesFull opens a subset of the real corpus through the
// segment file Save wrote and checks it against LoadRegistry over the same
// cache: every requested card, a card faulted in through Lookup, every token,
// and the segment file's name index against the full registry's.
func TestSubsetRegistryMatchesFull(t *testing.T) {
	full := compiledCorpus(t)
	dir := t.TempDir()
	cache := CachePath(dir)
	if err := full.Save(cache); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SegmentPath(cache)); err != nil {
		t.Fatalf("Save wrote no segment file: %v", err)
	}
	loaded, err := OpenCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Every 97th card, plus a split card, a transforming card, a
	// CopyFaceFrom card and an alias, by the names a decklist would use.
	var names []string
	for i := 0; i < loaded.Len(); i += 97 {
		names = append(names, loaded.Card(i).Faces[0].Name)
	}
	var special []string
	for _, c := range loaded.AllCards() {
		if len(special) >= 8 {
			break
		}
		for _, f := range c.Faces {
			if f != nil && (f.CopyFaceFrom != "" || len(f.Aliases) > 0) {
				special = append(special, c.Faces[0].Name)
				if len(f.Aliases) > 0 {
					special = append(special, f.Aliases[0])
				}
				break
			}
		}
	}
	names = append(names, special...)
	names = append(names, "Fire // Ice", "Delver of Secrets", "no such card anywhere")
	sub, err := OpenCorpusSubset(dir, names)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.IsSubset() || loaded.IsSubset() {
		t.Fatalf("IsSubset: subset %v full %v", sub.IsSubset(), loaded.IsSubset())
	}
	t.Logf("%d names -> %d subset cards (full %d), %d special", len(names), sub.Len(), loaded.Len(), len(special))
	if n := sub.Len(); n == 0 || n > len(names) || n >= loaded.Len() {
		t.Fatalf("subset holds %d cards for %d names", n, len(names))
	}
	for _, n := range names {
		want, wok := loaded.Lookup(n)
		got, gok := sub.Lookup(n)
		if wok != gok {
			t.Fatalf("Lookup(%q): subset ok=%v full ok=%v", n, gok, wok)
		}
		if wok {
			sameCard(t, n, got, want)
		}
	}
	// Subset order is corpus order.
	pos := map[string]int{}
	for i, c := range loaded.AllCards() {
		if _, ok := pos[c.Path]; !ok {
			pos[c.Path] = i
		}
	}
	if !sort.SliceIsSorted(sub.AllCards(), func(i, j int) bool { return pos[sub.Card(i).Path] < pos[sub.Card(j).Path] }) {
		t.Error("subset Cards is not in corpus order")
	}
	// A name outside the subset faults in, once.
	outside := loaded.Card(loaded.Len()/2 + 1).Faces[0].Name
	got, ok := sub.Lookup(outside)
	want, _ := loaded.Lookup(outside)
	if !ok {
		t.Fatalf("Lookup(%q) outside the subset missed", outside)
	}
	sameCard(t, outside, got, want)
	if again, _ := sub.Lookup(outside); again != got {
		t.Error("a faulted-in card is decoded again on a second Lookup")
	}
	if len(sub.Tokens) != len(loaded.Tokens) {
		t.Fatalf("subset tokens %d, full %d", len(sub.Tokens), len(loaded.Tokens))
	}
	for k, w := range loaded.Tokens {
		sameCard(t, "token "+k, sub.Tokens[k], w)
	}
	// The segment file's index is the full registry's tiered index, and the
	// imaged registry's name index agrees with it name for name.
	sf := sub.sub.sf
	if sf.nNames != len(loaded.lazy.img.NameKeys) {
		t.Fatalf("segment index has %d names, image index %d", sf.nNames, len(loaded.lazy.img.NameKeys))
	}
	for i := 0; i < sf.nNames; i++ {
		k := sf.name(i)
		o, ok := sf.ordinal(k)
		if c, hit := loaded.Lookup(k); !ok || !hit || c != loaded.Card(int(o)) {
			t.Fatalf("segment index %q -> %d, image index disagrees", k, o)
		}
	}
}

// TestSubsetRebuildsMissingSegments: a cache with no segment file beside it
// (a cache written before segments existed, or a failed segment write) gets
// one rebuilt from the cache on the first subset open.
func TestSubsetRebuildsMissingSegments(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry()
	for _, src := range []string{"Name:Alpha\nManaCost:R\nTypes:Instant\n", "Name:Beta\nManaCost:G\nTypes:Creature Bear\nPT:2/2\n"} {
		c, _ := ParseBytes(filepath.Join(dir, "x.txt"), []byte(src))
		c.Link()
		r.Add(c)
	}
	cache := CachePath(dir)
	if err := r.saveGob(cache); err != nil {
		t.Fatal(err)
	}
	sub, err := OpenCorpusSubset(dir, []string{"beta"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Len() != 1 || sub.Card(0).Faces[0].Name != "Beta" {
		t.Fatalf("subset cards = %v", sub.AllCards())
	}
	if _, err := os.Stat(SegmentPath(cache)); err != nil {
		t.Fatalf("no segment file rebuilt: %v", err)
	}
	if c, ok := sub.Lookup("Alpha"); !ok || c.Faces[0].Name != "Alpha" {
		t.Fatal("Lookup outside the subset failed")
	}
	if err := sub.Save(filepath.Join(dir, "other.gob.gz")); err == nil {
		t.Fatal("a subset registry saved as a corpus cache")
	}
}

func TestNeedsFullNameUniverse(t *testing.T) {
	mk := func(src string) *Card {
		c, _ := ParseBytes("x.txt", []byte(src))
		c.Link()
		return c
	}
	plain := mk("Name:Plain\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\n")
	needle := mk("Name:Needle\nManaCost:1\nTypes:Artifact\nK:ETBReplacement:Other:ChooseName\nSVar:ChooseName:DB$ NameCard | Defined$ You\n")
	moon := mk("Name:Moon\nManaCost:1\nTypes:Enchantment\nS:Mode$ Continuous | Affected$ Land | AddType$ AllNonBasicLandType\n")
	mountain := mk("Name:Mountain\nTypes:Basic Land Mountain\n")
	if NeedsFullNameUniverse([]*Card{plain, mountain}, nil) {
		t.Error("a burn spell and a Mountain need the full universe")
	}
	if !NeedsFullNameUniverse([]*Card{plain}, nil) {
		t.Error("a universe with no basic-land-typed land was accepted")
	}
	if !NeedsFullNameUniverse([]*Card{plain, mountain, needle}, nil) {
		t.Error("a NameCard SVar was missed")
	}
	if !NeedsFullNameUniverse([]*Card{mountain}, map[string]*Card{"t": moon}) {
		t.Error("an AllNonBasicLandType static on a token was missed")
	}
}
