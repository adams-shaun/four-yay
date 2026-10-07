package cards

import (
	"compress/gzip"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, src := range []string{
		"Name:Lightning Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n",
		"Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n",
		"Name:Delver of Secrets\nManaCost:U\nTypes:Creature Human Wizard\nPT:1/1\nOracle:x\nAlternateMode:DoubleFaced\nALTERNATE\nName:Insectile Aberration\nTypes:Creature Human Insect\nPT:3/2\nK:Flying\nOracle:x\n",
	} {
		c, _ := ParseBytes("fixture.txt", []byte(src))
		c.Link()
		for _, f := range c.Faces {
			f.ApplyIntrinsics()
		}
		r.Add(c)
	}
	return r
}

func TestRegistryLookupNormalisation(t *testing.T) {
	r := fixtureRegistry(t)
	for _, name := range []string{
		"Lightning Bolt", "lightning bolt", "  Lightning  Bolt ",
		"Delver of Secrets", "Delver of Secrets // Insectile Aberration",
	} {
		if _, ok := r.Lookup(name); !ok {
			t.Errorf("Lookup(%q) missed", name)
		}
	}
	if _, ok := r.Lookup("Not A Card"); ok {
		t.Error("Lookup matched a card that does not exist")
	}
}

// aliasFixture parses a card carrying a Universes-Within flavour name plus one
// Attraction-style Variant: line, which must stay ignored.
func aliasFixture(t *testing.T) *Card {
	t.Helper()
	src := "Name:Mind Flayer, the Shadow\n" +
		"Variant:UniversesWithin:FlavorName:Arvinox, the Mind Flail\n" +
		"Variant:C:Lights:2 5 6\n" +
		"ManaCost:4 B B B\nTypes:Legendary Enchantment Creature Horror\nPT:9/9\nOracle:x\n"
	c, diags := ParseBytes("alias.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %+v", diags)
	}
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	return c
}

func TestVariantFlavorNameAliasResolves(t *testing.T) {
	r := NewRegistry()
	r.Add(aliasFixture(t))

	// The canonical name still resolves and still names the same card.
	canon, ok := r.Lookup("Mind Flayer, the Shadow")
	if !ok || canon.Faces[0].Name != "Mind Flayer, the Shadow" {
		t.Fatalf("canonical lookup = %v %v", canon, ok)
	}
	// The flavour name resolves to that card, exact and normalised.
	for _, name := range []string{
		"Arvinox, the Mind Flail", "arvinox the mind flail", "  Arvinox,  the Mind Flail ",
	} {
		got, ok := r.Lookup(name)
		if !ok || got != canon {
			t.Errorf("Lookup(%q) = %v, %v; want the canonical card", name, got, ok)
		}
	}
	// The flavour name is NOT the card's printed name: nothing but the byName
	// index may know it. Name-characteristic matching reads Face.Name.
	if canon.Faces[0].Name == "Arvinox, the Mind Flail" {
		t.Fatal("flavour name leaked into Face.Name")
	}
	// The Attraction Variant: line produced no alias and no diag.
	if len(canon.Faces[0].Aliases) != 1 || canon.Faces[0].Aliases[0] != "Arvinox, the Mind Flail" {
		t.Fatalf("aliases = %q", canon.Faces[0].Aliases)
	}
}

func TestFlavorNameAliasSurvivesCacheRoundTrip(t *testing.T) {
	r := NewRegistry()
	r.Add(aliasFixture(t))
	if err := r.CompileMetadata(); err != nil {
		t.Fatalf("CompileMetadata: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ir.gob.gz")
	if err := r.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	got, ok := back.Lookup("Arvinox, the Mind Flail")
	if !ok || got.Faces[0].Name != "Mind Flayer, the Shadow" {
		t.Fatalf("alias lookup after round trip = %v, %v", got, ok)
	}
	if _, ok := back.Lookup("Mind Flayer, the Shadow"); !ok {
		t.Fatal("canonical name lost in round trip")
	}
}

func TestRegistryCacheRoundTrip(t *testing.T) {
	r := fixtureRegistry(t)
	if err := r.CompileMetadata(); err != nil {
		t.Fatalf("CompileMetadata: %v", err)
	}
	wantIdentity := r.Catalog().Identity
	path := filepath.Join(t.TempDir(), "ir.gob.gz")
	if err := r.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if back.Len() != r.Len() {
		t.Fatalf("cards = %d, want %d", back.Len(), r.Len())
	}
	if back.Catalog() == nil || back.Catalog().Identity != wantIdentity {
		t.Fatalf("loaded catalog identity = %+v, want %+v", back.Catalog(), wantIdentity)
	}
	delver, ok := back.Lookup("Delver of Secrets")
	if !ok || delver.AlternateMode != "DoubleFaced" {
		t.Fatalf("Delver AlternateMode = %q, want DoubleFaced", delver.AlternateMode)
	}
	// Structure must survive, not just names: the sub-ability tree and the
	// intrinsic mana ability are what the engine actually consumes.
	bolt, ok := back.Lookup("Lightning Bolt")
	if !ok {
		t.Fatal("bolt missing after round trip")
	}
	if sa := bolt.Faces[0].SpellAbility(); sa == nil || sa.Params["NumDmg"] != "3" {
		t.Fatalf("bolt spell ability = %+v", sa)
	}
	mtn, _ := back.Lookup("Mountain")
	if len(mtn.Faces[0].ManaAbilities()) != 1 {
		t.Fatal("intrinsic mana ability lost in round trip")
	}
}

func TestRegistryAddInvalidatesAndRebuildsCatalogBindings(t *testing.T) {
	r := fixtureRegistry(t)
	oldFace := r.Card(0).Faces[0]
	oldAbility := oldFace.Abilities[0]
	if err := r.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	if oldFace.CompiledID() == 0 || oldAbility.CompiledAPI() == APIUnknown {
		t.Fatal("initial metadata bindings missing")
	}
	card, _ := ParseBytes("new.txt", []byte("Name:New Card\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"))
	card.Link()
	r.Add(card)
	if r.Catalog() != nil {
		t.Fatal("Add left a stale catalog published")
	}
	if oldFace.CompiledID() != 0 || oldAbility.CompiledAPI() != APIUnknown {
		t.Fatal("Add left old runtime bindings active")
	}
	if err := r.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	if oldFace.CompiledID() == 0 || card.Faces[0].CompiledID() == 0 || card.Faces[0].Abilities[0].CompiledAPI() != APIDraw {
		t.Fatal("rebuild did not bind old and new faces")
	}
}

func TestRegistryRejectsTruncatedCache(t *testing.T) {
	r := fixtureRegistry(t)
	tests := []struct {
		name    string
		mutate  func(path string) error // corrupts the cache file
		wantErr bool
	}{
		{
			name:    "intact_cache",
			mutate:  func(path string) error { return nil }, // no mutation
			wantErr: false,
		},
		{
			name: "truncate_by_1_byte",
			mutate: func(path string) error {
				fi, err := os.Stat(path)
				if err != nil {
					return err
				}
				return os.Truncate(path, fi.Size()-1)
			},
			wantErr: true,
		},
		{
			name: "truncate_by_4_bytes",
			mutate: func(path string) error {
				fi, err := os.Stat(path)
				if err != nil {
					return err
				}
				return os.Truncate(path, fi.Size()-4)
			},
			wantErr: true,
		},
		{
			name: "truncate_by_8_bytes_whole_trailer",
			mutate: func(path string) error {
				fi, err := os.Stat(path)
				if err != nil {
					return err
				}
				return os.Truncate(path, fi.Size()-8)
			},
			wantErr: true,
		},
		{
			name: "truncate_by_10_bytes",
			mutate: func(path string) error {
				fi, err := os.Stat(path)
				if err != nil {
					return err
				}
				return os.Truncate(path, fi.Size()-10)
			},
			wantErr: true,
		},
		{
			name: "flip_byte_in_trailer",
			mutate: func(path string) error {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if len(data) < 8 {
					return nil // skip if file too small
				}
				// Flip a bit in the last 8 bytes (the trailer)
				data[len(data)-3] ^= 0x01
				return os.WriteFile(path, data, 0o644)
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpdir := t.TempDir()
			path := filepath.Join(tmpdir, "ir.gob.gz")
			if err := r.Save(path); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if err := tt.mutate(path); err != nil {
				t.Fatalf("mutate: %v", err)
			}
			_, err := LoadRegistry(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadRegistry error = %v, want error = %v", err, tt.wantErr)
			}
			// For the intact case, verify structure survives
			if !tt.wantErr {
				back, _ := LoadRegistry(path)
				bolt, ok := back.Lookup("Lightning Bolt")
				if !ok || bolt.Faces[0].SpellAbility() == nil {
					t.Fatal("intact cache lost structure")
				}
			}
		})
	}
}

// writeCardFile drops one Forge-shaped script into dir for CompileDir to pick
// up, creating dir (and any missing parents, e.g. a fixture's cardsfolder or
// tokenscripts sibling) first.
func writeCardFile(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestCompileDirDiagnosesCardWithNoNamedFace covers the real corpus defect
// found in bind_liberate.txt/start_fire.txt: a script built entirely from
// CopyFaceFrom (a directive this parser doesn't resolve) parses without
// error into a Card whose only Face has every field, including Name, at its
// zero value. That must not vanish silently — it should surface as exactly
// one diagnostic per card, and Coverage must still treat the card as absent
// rather than "supported".
func TestCompileDirDiagnosesCardWithNoNamedFace(t *testing.T) {
	dir := t.TempDir()
	writeCardFile(t, dir, "nameless.txt", "CopyFaceFrom:Bind\nAlternateMode:Split\n")

	r, diags, err := CompileDir(dir)
	if err != nil {
		t.Fatalf("CompileDir: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("diags = %+v, want exactly 1", diags)
	}
	if !strings.HasSuffix(diags[0].Path, "nameless.txt") {
		t.Errorf("diag path = %q, want it to name nameless.txt", diags[0].Path)
	}
	if !strings.Contains(diags[0].Msg, "no named face") {
		t.Errorf("diag msg = %q, want it to say the card has no named face", diags[0].Msg)
	}

	if r.Len() != 1 {
		t.Fatalf("Cards = %d, want 1 (the card still compiles, just flagged)", r.Len())
	}
	if r.Catalog() == nil || r.Card(0).Faces[0].CompiledID() == 0 {
		t.Fatal("CompileDir returned an uncompiled registry")
	}
	cv := r.Coverage(map[string]bool{})
	if cv.Cards != 0 {
		t.Errorf("Coverage.Cards = %d, want 0 (nameless card excluded)", cv.Cards)
	}
	if cv.Supported != 0 {
		t.Errorf("Coverage.Supported = %d, want 0 (nameless card must not count as playable)", cv.Supported)
	}
}

// TestCompileDirIgnoresNamelessAlternateFace is the regression guard for the
// distinction the fix must preserve: a card is only diagnosed when NO face
// has a name. A normal single-face card, and a two-face card whose primary
// face is named and whose ALTERNATE is a nameless CopyFaceFrom stub (the
// shape used by ~20 legitimate split/transform cards in the real corpus),
// must both produce zero "no named face" diagnostics — otherwise the check
// would blow through the diagnostic budget on cards that are working as
// designed.
func TestCompileDirIgnoresNamelessAlternateFace(t *testing.T) {
	dir := t.TempDir()
	writeCardFile(t, dir, "normal.txt", "Name:Normal Card\nTypes:Sorcery\nOracle:x\n")
	writeCardFile(t, dir, "splitcard.txt",
		"Name:Primary Face\nTypes:Creature\nOracle:x\nALTERNATE\nCopyFaceFrom:SomeAlt\n")

	r, diags, err := CompileDir(dir)
	if err != nil {
		t.Fatalf("CompileDir: %v", err)
	}
	for _, d := range diags {
		if strings.Contains(d.Msg, "no named face") {
			t.Errorf("unexpected nameless-face diagnostic: %+v", d)
		}
	}
	if r.Len() != 2 {
		t.Fatalf("Cards = %d, want 2", r.Len())
	}
	cv := r.Coverage(map[string]bool{})
	if cv.Cards != 2 {
		t.Errorf("Coverage.Cards = %d, want 2 (both cards have a named face)", cv.Cards)
	}
}

// TestLoadRegistryRelinksStaleCacheTransmuteWithItsManaValue pins the
// derive-before-link order in LoadRegistry. A gob cache stores only printed
// fields (the derived ones are unexported), so a cache compiled BEFORE a
// keyword-expansion case existed decodes with cmc == 0 — and Transmute's
// expansion reads f.Cmc() to build its Card.cmcEQ<N> search spec. Relinking
// before deriving therefore expanded such a cache as cmcEQ0 (every search
// would only ever find zero-cost cards); deriving first repairs cmc from the
// printed ManaCost the cache does carry, so the relink expands with the
// source's real mana value (Dizzy Spell: 1). The corpus cache itself already
// carries the correct expansion (it was compiled through Parse→derive→Link),
// which is exactly why the ordinary round-trip cannot catch this: this test
// strips the expanded ability to reconstruct the pre-expansion cache bytes.
func TestLoadRegistryRelinksStaleCacheTransmuteWithItsManaValue(t *testing.T) {
	src := "Name:Dizzy Spell\nManaCost:U\nTypes:Instant\n" +
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ -3 | IsCurse$ True | SpellDescription$ Target creature gets -3/-0 until end of turn.\n" +
		"K:Transmute:1 U U\nOracle:x\n"
	c, _ := ParseBytes("dizzy_spell.txt", []byte(src))
	c.Link()
	// Strip the expanded Transmute ability: the exact face bytes a cache
	// compiled before the Transmute expansion case existed would carry.
	for _, f := range c.Faces {
		kept := f.Abilities[:0]
		for _, ab := range f.Abilities {
			if ab.Params["Keyword"] == "Transmute" {
				continue
			}
			kept = append(kept, ab)
		}
		f.Abilities = kept
	}
	r := NewRegistry()
	r.Add(c)
	path := filepath.Join(t.TempDir(), "ir.gob.gz")
	if err := r.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	dizzy, ok := back.Lookup("Dizzy Spell")
	if !ok {
		t.Fatal("Dizzy Spell missing after stale-cache load")
	}
	f := dizzy.Faces[0]
	if got := f.Cmc(); got != 1 {
		t.Fatalf("relaid face cmc = %d, want 1", got)
	}
	var found []string
	for _, ab := range f.Abilities {
		if ab.Params["Keyword"] == "Transmute" {
			found = append(found, ab.Params["ChangeType"])
		}
	}
	if len(found) != 1 || found[0] != "Card.cmcEQ1" {
		t.Fatalf("stale-cache relink expanded Transmute as %v, want exactly [Card.cmcEQ1]", found)
	}
}

// TestLoadRegistryWrongVersion fabricates a cache one version behind by
// gob-encoding a cacheFile directly (cacheFile is package-internal, so only a
// cards test can build the exact stale shape) and pins the typed error: the
// failure is a *CacheVersionError carrying Got/Want, and its text is the
// historical wording so every existing printer keeps its output.
func TestLoadRegistryWrongVersion(t *testing.T) {
	r := fixtureRegistry(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "ir.gob.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	if err := gob.NewEncoder(zw).Encode(cacheFile{Version: cacheVersion - 1, Cards: r.AllCards(), Tokens: r.Tokens}); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = LoadRegistry(path)
	var cve *CacheVersionError
	if !errors.As(err, &cve) {
		t.Fatalf("LoadRegistry error = %T(%v), want *CacheVersionError", err, err)
	}
	if cve.Got != cacheVersion-1 || cve.Want != cacheVersion {
		t.Fatalf("CacheVersionError = %d/%d, want %d/%d", cve.Got, cve.Want, cacheVersion-1, cacheVersion)
	}
	want := fmt.Sprintf("IR cache version %d, want %d — run `make compile-cards`", cacheVersion-1, cacheVersion)
	if cve.Error() != want {
		t.Fatalf("Error() = %q, want %q", cve.Error(), want)
	}
	// LoadRegistry must return the typed error itself (not wrapped) so a
	// bare type assertion keeps working too.
	var direct *CacheVersionError
	if !errors.As(err, &direct) || direct != cve {
		t.Fatal("LoadRegistry wrapped or copied the typed error")
	}
}
