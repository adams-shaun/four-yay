package cards

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

// multiMapFixtureSrc is a card whose map-bearing IR types each carry at least
// two entries: the spell ability and its SubAbility have multi-entry Params,
// the trigger and replacement have multi-entry Params, and the face has three
// SVars. encoding/gob walks maps in runtime-random order, so a fixture with a
// single entry per map would let the canonical-order assertion pass without
// exercising the ordering at all (see TestSaveIsByteIdenticalAcrossRuns).
const multiMapFixtureSrc = "Name:Canon Image Fixture\n" +
	"ManaCost:1 U\n" +
	"Types:Creature Human Wizard\n" +
	"PT:2/2\n" +
	"A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3 | SubAbility$ DBDraw\n" +
	"SVar:DBDraw:DB$ Draw | NumCards$ 1 | Defined$ You\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigPump | TriggerZones$ Battlefield\n" +
	"SVar:TrigPump:DB$ Pump | NumAtt$ 2\n" +
	"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidTarget$ Card.Self | ReplaceWith$ ReplFog\n" +
	"SVar:ReplFog:DB$ Fog\n" +
	"K:Flying\n" +
	"Oracle:x\n"

// imageCorpusFixture compiles a two-card, two-token synthetic corpus. The
// card carries multi-entry maps (precondition asserted in the test) and the
// registry carries two tokens, so cacheFile.Tokens — the other map the
// canonical encoder sorts — is multi-entry too.
func imageCorpusFixture(t *testing.T) *Registry {
	t.Helper()
	root := t.TempDir()
	writeCardFile(t, CorpusDir(root), "canon_image_fixture.txt", multiMapFixtureSrc)
	writeCardFile(t, CorpusDir(root), "canon_mountain.txt", "Name:Canon Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	writeCardFile(t, TokensDir(root), "r_1_1_goblin.txt", goblinTokenSrc)
	writeCardFile(t, TokensDir(root), "c_3_3_a_phyrexian_wurm_deathtouch.txt", wurmTokenSrc)
	r, diags, err := CompileDir(CorpusDir(root))
	if err != nil || len(diags) != 0 {
		t.Fatalf("CompileDir: %v, diags %v", err, diags)
	}
	return r
}

// TestImageCorpusHashIsTheCanonicalCacheGobDigest pins the Option-1
// unification: Image.Hdr.CorpusHash is derived from the ONE canonical corpus
// encoder (cache_canon.go's cacheFile GobEncode), not from a second gob walk
// over the pointer-free image rows. Before the unification CorpusHash hashed
// Image.CanonicalBytes — a different byte stream — so this equality fails.
//
// The multi-entry-map precondition is asserted loudly below: a single-entry
// map would make the key ordering unobservable and the test vacuous.
func TestImageCorpusHashIsTheCanonicalCacheGobDigest(t *testing.T) {
	r := imageCorpusFixture(t)

	// Precondition: the fixture must exercise every map whose order the
	// canonical encoder fixes. If a setup bug made any map single-entry the
	// ordering assertions below would prove nothing.
	card, ok := r.Lookup("Canon Image Fixture")
	if !ok {
		t.Fatal("fixture card missing from the compiled registry")
	}
	face := card.Faces[0]
	if len(face.SVars) < 2 {
		t.Fatalf("fixture Face.SVars = %v, want >= 2 entries", face.SVars)
	}
	sa := face.SpellAbility()
	if sa == nil || len(sa.Params) < 2 {
		t.Fatalf("fixture spell ability Params = %v, want >= 2 entries", sa)
	}
	if sa.Sub == nil || len(sa.Sub.Params) < 2 {
		t.Fatalf("fixture SubAbility Params = %v, want >= 2 entries", sa.Sub)
	}
	if len(face.Triggers) == 0 || len(face.Triggers[0].Params) < 2 {
		t.Fatalf("fixture trigger Params = %+v, want >= 2 entries", face.Triggers)
	}
	if len(face.Repls) == 0 || len(face.Repls[0].Params) < 2 {
		t.Fatalf("fixture replacement Params = %+v, want >= 2 entries", face.Repls)
	}
	if len(r.Tokens) < 2 {
		t.Fatalf("fixture Tokens = %d, want >= 2 for a multi-entry cache map", len(r.Tokens))
	}

	img, err := buildImage(r.AllCards(), r.Tokens)
	if err != nil {
		t.Fatalf("buildImage: %v", err)
	}

	// The image's corpus identity is the digest of the canonical cache gob.
	canon, err := canonicalCorpusGob(r.AllCards(), r.Tokens)
	if err != nil {
		t.Fatalf("canonicalCorpusGob: %v", err)
	}
	if len(canon) == 0 {
		t.Fatal("canonical corpus gob is empty")
	}
	if got, want := img.Hdr.CorpusHash, sha256.Sum256(canon); got != want {
		t.Fatalf("Image.Hdr.CorpusHash = %x, want canonical cache gob digest %x", got, want)
	}

	// It is NOT the old pointer-free image walk: the two byte streams differ,
	// so the assertion above is not vacuous on this fixture.
	rows, err := img.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	if bytes.Equal(rows, canon) {
		t.Fatal("image row bytes equal the canonical cache gob; the unification assertion proves nothing")
	}
	if sha256.Sum256(rows) == img.Hdr.CorpusHash {
		t.Fatal("CorpusHash still equals the image-row-walk digest; unification did not take effect")
	}

	// One canonical form: repeated encodes of the same corpus data yield
	// byte-identical canonical bytes and therefore the same digest. Two
	// independent CompileDir runs are NOT compared: Card.Path carries the
	// corpus root, so a second t.TempDir() would differ for that reason alone.
	canon2, err := canonicalCorpusGob(r.AllCards(), r.Tokens)
	if err != nil {
		t.Fatalf("canonicalCorpusGob (second): %v", err)
	}
	if !bytes.Equal(canon, canon2) {
		t.Fatal("canonical corpus gob differs between repeated encodes of one corpus")
	}
	img2, err := buildImage(r.AllCards(), r.Tokens)
	if err != nil {
		t.Fatalf("buildImage (second): %v", err)
	}
	if img.Hdr.CorpusHash != img2.Hdr.CorpusHash {
		t.Fatalf("CorpusHash differs between builds: %x vs %x", img.Hdr.CorpusHash, img2.Hdr.CorpusHash)
	}

	// The persisted cache and the image agree end to end: save the corpus,
	// reload it, and the reloaded image's corpus identity equals the identity
	// the in-memory image had. This is the drift the report named — a field or
	// ordering rule that changes the persisted bytes must change this digest.
	path := CachePath(t.TempDir())
	if err := r.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	loadedImg, err := buildImage(loaded.AllCards(), loaded.Tokens)
	if err != nil {
		t.Fatalf("buildImage (reloaded): %v", err)
	}
	if img.Hdr.CorpusHash != loadedImg.Hdr.CorpusHash {
		t.Fatalf("reloaded corpus identity = %x, want %x (image and persisted cache disagree)",
			loadedImg.Hdr.CorpusHash, img.Hdr.CorpusHash)
	}
}
