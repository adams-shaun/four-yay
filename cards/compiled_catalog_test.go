package cards

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func compiledCatalogFixture(reverse bool, changedParam bool) (*Registry, *Face, *Face, *Face, *Face) {
	params := make(map[string]string, 3)
	svars := make(map[string]string, 2)
	if reverse {
		params["Zed"] = "last"
		params["SubAbility"] = "Tail"
		params["Alpha"] = "first"
		svars["Zed"] = "Count$RememberedSize"
		svars["Tail"] = "DB$ FutureAPI | Amount$ 2"
	} else {
		params["Alpha"] = "first"
		params["SubAbility"] = "Tail"
		params["Zed"] = "last"
		svars["Tail"] = "DB$ FutureAPI | Amount$ 2"
		svars["Zed"] = "Count$RememberedSize"
	}
	if changedParam {
		params["Alpha"] = "changed"
	}
	tail := &SA{Kind: "DB", API: "FutureAPI", Params: map[string]string{"Amount": "2"}, Line: "DB$ FutureAPI | Amount$ 2"}
	spell := &SA{Kind: "SP", API: "Draw", Params: params, Sub: tail, Line: "SP$ Draw | Alpha$ first | SubAbility$ Tail | Zed$ last"}
	unknownKind := &SA{Kind: "FX", API: "Draw", Params: map[string]string{}, Line: "FX$ Draw"}
	cardFace := &Face{
		Name:           "Alpha",
		ManaCost:       "1 W",
		Types:          []string{"Legendary", "Creature", "Wizard"},
		PT:             "2/3",
		Keywords:       []string{"Flying", "Ward:2"},
		Abilities:      []*SA{spell, unknownKind},
		Triggers:       []Trigger{{Mode: "FutureTrigger", Params: map[string]string{"Mode": "FutureTrigger", "Phase": "Upkeep"}, Effect: tail}},
		Statics:        []Static{{Mode: "FutureStatic", Params: map[string]string{"Mode": "FutureStatic"}}},
		Repls:          []Repl{{Event: "FutureReplacement", Params: map[string]string{"Event": "FutureReplacement"}, With: tail}},
		SVars:          svars,
		power:          2,
		toughness:      3,
		cmc:            2,
		colourIdentity: ColourWhite,
	}
	secondFace := &Face{Name: "Beta", Types: []string{"Basic", "Land", "Forest"}}
	aTokenFace := &Face{Name: "A Token", Types: []string{"Token", "Creature"}, PT: "1/1", power: 1, toughness: 1}
	zTokenFace := &Face{Name: "Z Token", Types: []string{"Token", "Artifact"}}
	r := NewRegistry()
	r.Add(&Card{Path: "alpha.txt", Faces: []*Face{cardFace}})
	r.Add(&Card{Path: "beta.txt", Faces: []*Face{secondFace}})
	if reverse {
		r.Tokens["z_token"] = &Card{Path: "z_token.txt", Faces: []*Face{zTokenFace}}
		r.Tokens["a_token"] = &Card{Path: "a_token.txt", Faces: []*Face{aTokenFace}}
	} else {
		r.Tokens["a_token"] = &Card{Path: "a_token.txt", Faces: []*Face{aTokenFace}}
		r.Tokens["z_token"] = &Card{Path: "z_token.txt", Faces: []*Face{zTokenFace}}
	}
	return r, cardFace, secondFace, aTokenFace, zTokenFace
}

func TestCompiledCatalogDeterministicLayout(t *testing.T) {
	a, af, ab, aa, az := compiledCatalogFixture(false, false)
	b, bf, bb, ba, bz := compiledCatalogFixture(true, false)
	if err := a.CompileMetadata(); err != nil {
		t.Fatalf("compile first catalog: %v", err)
	}
	if err := b.CompileMetadata(); err != nil {
		t.Fatalf("compile reordered catalog: %v", err)
	}
	ac, bc := a.Catalog(), b.Catalog()
	if ac == nil || bc == nil {
		t.Fatal("compiled registry has no catalog")
	}
	if !bytes.Equal(ac.CanonicalBytes(), bc.CanonicalBytes()) {
		t.Fatal("equivalent registries produced different canonical bytes")
	}
	if ac.Identity != bc.Identity {
		t.Fatalf("equivalent identities differ: %x vs %x", ac.Identity.CorpusHash, bc.Identity.CorpusHash)
	}
	for _, tc := range []struct {
		name string
		a, b *Face
		want FaceID
	}{
		{"first card", af, bf, 1},
		{"second card", ab, bb, 2},
		{"lexically first token", aa, ba, 3},
		{"lexically second token", az, bz, 4},
	} {
		if got := tc.a.CompiledID(); got != tc.want {
			t.Errorf("%s first ID = %d, want %d", tc.name, got, tc.want)
		}
		if got := tc.b.CompiledID(); got != tc.want {
			t.Errorf("%s reordered ID = %d, want %d", tc.name, got, tc.want)
		}
	}
	if ac.Identity.Schema != CompiledCatalogSchema {
		t.Fatalf("schema = %d, want %d", ac.Identity.Schema, CompiledCatalogSchema)
	}
	assertCatalogSpansInRange(t, ac)
}

func TestCompiledCatalogBindsRecursiveAbilitiesAndUnknownText(t *testing.T) {
	r, face, _, _, _ := compiledCatalogFixture(false, false)
	if err := r.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	c := r.Catalog()
	fr := c.Faces[face.CompiledID()-1]
	if fr.Abilities.Count != 2 {
		t.Fatalf("face ability count = %d, want 2", fr.Abilities.Count)
	}
	rootID := c.FaceAbilities[fr.Abilities.Start]
	root := c.Abilities[rootID-1]
	if root.Kind != SAKindSpell || root.API != APIDraw || root.Sub == 0 {
		t.Fatalf("compiled root = %+v", root)
	}
	child := c.Abilities[root.Sub-1]
	if child.Kind != SAKindDrawback || child.API != APIUnknown {
		t.Fatalf("compiled child = %+v", child)
	}
	if got, ok := c.String(child.UnknownAPI); !ok || got != "FutureAPI" {
		t.Fatalf("unknown API text = %q, %v", got, ok)
	}
	unknownID := c.FaceAbilities[fr.Abilities.Start+1]
	unknown := c.Abilities[unknownID-1]
	if unknown.Kind != SAKindUnknown || unknown.UnknownKind == 0 {
		t.Fatalf("unknown kind row = %+v", unknown)
	}
	if got, _ := c.String(unknown.UnknownKind); got != "FX" {
		t.Fatalf("unknown kind text = %q", got)
	}
	tr := c.Triggers[fr.Triggers.Start]
	if tr.Mode != TriggerModeUnknown || tr.UnknownMode == 0 || tr.Effect != root.Sub {
		t.Fatalf("unknown trigger row = %+v", tr)
	}
	if interest, ok := face.CompiledTriggerInterests(); !ok || interest != TriggerInterestAny {
		t.Fatalf("compiled interests = %x, %v; want catch-all", interest, ok)
	}
	if face.Abilities[0].CompiledKind() != SAKindSpell || face.Abilities[0].CompiledAPI() != APIDraw {
		t.Fatal("root ability binding missing")
	}
	if face.Abilities[0].Sub.CompiledKind() != SAKindDrawback || face.Abilities[0].Sub.CompiledAPI() != APIUnknown {
		t.Fatal("recursive ability binding missing")
	}
}

func TestCompiledCatalogBindsDirectTriggerInterests(t *testing.T) {
	r, face, _, _, _ := compiledCatalogFixture(false, false)
	if err := r.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	if got, want := face.compiledTriggerInterests, TriggerInterestAny; got != want {
		t.Fatalf("direct trigger interests = %x, want %x", got, want)
	}
	r.invalidateCatalog()
	if got := face.compiledTriggerInterests; got != 0 {
		t.Fatalf("direct trigger interests after invalidation = %x, want 0", got)
	}
}

func TestCompiledCatalogIdentityChangesWithParameter(t *testing.T) {
	a, _, _, _, _ := compiledCatalogFixture(false, false)
	b, _, _, _, _ := compiledCatalogFixture(false, true)
	if err := a.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	if err := b.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	if a.Catalog().Identity.CorpusHash == b.Catalog().Identity.CorpusHash {
		t.Fatal("changing one parameter value did not change the corpus hash")
	}
}

func TestWholeCorpusCompilesMetadata(t *testing.T) {
	r := compiledCorpus(t)
	if err := r.CompileMetadata(); err != nil {
		t.Fatalf("CompileMetadata: %v", err)
	}
	c := r.Catalog()
	if c == nil || len(c.Faces) < 35_000 || len(c.Abilities) < 50_000 {
		t.Fatalf("catalog rows: faces=%d abilities=%d", len(c.Faces), len(c.Abilities))
	}
	t.Logf("catalog rows: faces=%d abilities=%d triggers=%d statics=%d replacements=%d strings=%d bytes=%d",
		len(c.Faces), len(c.Abilities), len(c.Triggers), len(c.Statics), len(c.Replacements), len(c.Strings), len(c.CanonicalBytes()))
}

func textualHasType(face *Face, want string) bool {
	for _, typ := range face.Types {
		if strings.EqualFold(typ, want) {
			return true
		}
	}
	return false
}

func textualHasKeyword(face *Face, want string) bool {
	for _, keyword := range face.Keywords {
		if strings.EqualFold(KeywordHead(keyword), want) {
			return true
		}
	}
	return false
}

func textualKeywordParam(face *Face, want string) (string, bool) {
	for _, keyword := range face.Keywords {
		if strings.EqualFold(KeywordHead(keyword), want) {
			if i := strings.IndexByte(keyword, ':'); i >= 0 {
				return strings.TrimSpace(keyword[i+1:]), true
			}
			return "", true
		}
	}
	return "", false
}

func textualSpellAbility(face *Face) *SA {
	for _, ability := range face.Abilities {
		if ability.Kind == "SP" {
			return ability
		}
	}
	return nil
}

func textualManaAbilities(face *Face) []*SA {
	var out []*SA
	for _, ability := range face.Abilities {
		if ability.Kind == "AB" && ability.API == "Mana" {
			out = append(out, ability)
		}
	}
	return out
}

// TestCompiledFaceQueryParity compares the compiled metadata queries against
// the textual fallback over the whole corpus. It loads the running compiler's
// own fingerprint-keyed IR (cards.OpenCorpus), compiling from cardsfolder when
// that cache is absent — so the parity check always exercises THIS build's
// parser rather than skipping or reading another parser's IR.
func TestCompiledFaceQueryParity(t *testing.T) {
	r, err := OpenCorpus(filepath.Join("..", ".cards"))
	if err != nil {
		t.Skipf("compiled corpus unavailable: %v", err)
	}
	typeNames := []string{
		"Artifact", "Battle", "Conspiracy", "Creature", "Dungeon", "Enchantment", "Instant",
		"Kindred", "Land", "Phenomenon", "Plane", "Planeswalker", "Scheme", "Sorcery", "Tribal",
		"Vanguard", "Basic", "Legendary", "Ongoing", "Snow", "World", "Spacecraft", "Vehicle", "Room",
	}
	keywordNames := []string{
		"AlternateAdditionalCost", "Buyback", "Chapter", "Cycling", "Dethrone", "Devoid",
		"Dredge", "Enchant", "Flash", "Flashback", "Harmonize", "Kicker", "Madness",
		"MayEffectFromOpeningHand", "Miracle", "Riot", "Surge", "Suspend",
	}
	checked := 0
	checkCard := func(card *Card) {
		for _, face := range card.Faces {
			if face.CompiledID() == 0 {
				t.Fatalf("corpus face %q has no compiled ID", face.Name)
			}
			for _, name := range typeNames {
				if got, want := face.hasType(name), textualHasType(face, name); got != want {
					t.Fatalf("%q type %q = %v, want %v", face.Name, name, got, want)
				}
			}
			for _, name := range keywordNames {
				if got, want := face.HasKeyword(name), textualHasKeyword(face, name); got != want {
					t.Fatalf("%q keyword %q = %v, want %v", face.Name, name, got, want)
				}
				gotParam, gotOK := face.KeywordParam(name)
				wantParam, wantOK := textualKeywordParam(face, name)
				if gotParam != wantParam || gotOK != wantOK {
					t.Fatalf("%q keyword param %q = %q,%v; want %q,%v", face.Name, name, gotParam, gotOK, wantParam, wantOK)
				}
			}
			if got, want := face.SpellAbility(), textualSpellAbility(face); got != want {
				t.Fatalf("%q spell ability pointer differs", face.Name)
			}
			gotMana, wantMana := face.ManaAbilities(), textualManaAbilities(face)
			if len(gotMana) != len(wantMana) {
				t.Fatalf("%q mana abilities = %d, want %d", face.Name, len(gotMana), len(wantMana))
			}
			for i := range gotMana {
				if gotMana[i] != wantMana[i] {
					t.Fatalf("%q mana ability %d pointer differs", face.Name, i)
				}
			}
			checked++
		}
	}
	for _, card := range r.Cards {
		checkCard(card)
	}
	for _, key := range sortedKeys(r.Tokens) {
		checkCard(r.Tokens[key])
	}
	if checked < 35_000 {
		t.Fatalf("checked only %d compiled faces", checked)
	}

	spell := &SA{Kind: "SP", API: "FutureAPI"}
	manaA := &SA{Kind: "AB", API: "Mana"}
	manaB := &SA{Kind: "AB", API: "Mana"}
	bound := &Face{
		Types:    []string{"cReAtUrE", "FutureSubtype"},
		Keywords: []string{"kIcKeR:2", "FutureKeyword:value"},
		Abilities: []*SA{
			{Kind: "AB", API: "Draw"}, spell, manaA, manaB,
		},
	}
	fixture := NewRegistry()
	fixture.Add(&Card{Faces: []*Face{bound, {}}})
	if err := fixture.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	if !bound.hasType("CREATURE") || !bound.hasType("futuresubtype") || bound.hasType("Land") {
		t.Fatal("bound type lookup lost mixed-case or unknown fallback")
	}
	if !bound.HasKeyword("KICKER") || !bound.HasKeyword("futurekeyword") || bound.HasKeyword("Madness") {
		t.Fatal("bound keyword lookup lost mixed-case or unknown fallback")
	}
	if got, ok := bound.KeywordParam("kicker"); !ok || got != "2" {
		t.Fatalf("bound Kicker param = %q, %v", got, ok)
	}
	if bound.SpellAbility() != spell {
		t.Fatal("bound spell pointer differs")
	}
	gotMana := bound.ManaAbilities()
	if len(gotMana) != 2 || gotMana[0] != manaA || gotMana[1] != manaB {
		t.Fatal("bound mana pointers or order differ")
	}
	if empty := fixture.Cards[0].Faces[1]; empty.SpellAbility() != nil || empty.ManaAbilities() != nil {
		t.Fatal("empty bound face lookup changed")
	}
}

func TestUnboundFaceFallback(t *testing.T) {
	spell := &SA{Kind: "SP", API: "FutureAPI"}
	manaA := &SA{Kind: "AB", API: "Mana"}
	manaB := &SA{Kind: "AB", API: "Mana"}
	face := &Face{
		Types:    []string{"cReAtUrE", "FutureSubtype"},
		Keywords: []string{"wArD:2", "FutureKeyword:value"},
		Abilities: []*SA{
			{Kind: "AB", API: "Draw"}, spell, manaA, manaB,
		},
	}
	if face.CompiledID() != 0 {
		t.Fatal("synthetic face unexpectedly bound")
	}
	if !face.hasType("Creature") || !face.hasType("futuresubtype") || face.hasType("Land") {
		t.Fatal("unbound type fallback changed")
	}
	if !face.HasKeyword("WARD") || !face.HasKeyword("futurekeyword") || face.HasKeyword("Haste") {
		t.Fatal("unbound keyword fallback changed")
	}
	if got, ok := face.KeywordParam("ward"); !ok || got != "2" {
		t.Fatalf("unbound Ward param = %q, %v", got, ok)
	}
	if face.SpellAbility() != spell {
		t.Fatal("unbound spell lookup changed")
	}
	gotMana := face.ManaAbilities()
	if len(gotMana) != 2 || gotMana[0] != manaA || gotMana[1] != manaB {
		t.Fatal("unbound mana lookup changed")
	}
	if (&Face{}).SpellAbility() != nil || (&Face{}).ManaAbilities() != nil {
		t.Fatal("empty unbound face lookup changed")
	}
}

func assertCatalogSpansInRange(t *testing.T, c *CompiledCatalog) {
	t.Helper()
	check := func(name string, span Span, n int) {
		t.Helper()
		if uint64(span.Start)+uint64(span.Count) > uint64(n) {
			t.Errorf("%s span %+v exceeds length %d", name, span, n)
		}
	}
	for i, f := range c.Faces {
		check("face types", f.Types, len(c.TypeTokens))
		check("face keywords", f.Keywords, len(c.Keywords))
		check("face abilities", f.Abilities, len(c.FaceAbilities))
		check("face mana abilities", f.ManaAbilities, len(c.ManaAbilityIDs))
		check("face triggers", f.Triggers, len(c.Triggers))
		check("face statics", f.Statics, len(c.Statics))
		check("face replacements", f.Replacements, len(c.Replacements))
		check("face SVars", f.SVars, len(c.SVars))
		if f.SpellAbility != 0 && int(f.SpellAbility) > len(c.Abilities) {
			t.Errorf("face %d spell ability %d exceeds %d abilities", i, f.SpellAbility, len(c.Abilities))
		}
	}
	for _, a := range c.Abilities {
		check("ability params", a.Params, len(c.Params))
		if a.Sub != 0 && int(a.Sub) > len(c.Abilities) {
			t.Errorf("ability sub %d exceeds %d abilities", a.Sub, len(c.Abilities))
		}
	}
	for _, tr := range c.Triggers {
		check("trigger params", tr.Params, len(c.Params))
	}
	for _, st := range c.Statics {
		check("static params", st.Params, len(c.Params))
	}
	for _, repl := range c.Replacements {
		check("replacement params", repl.Params, len(c.Params))
	}
}
