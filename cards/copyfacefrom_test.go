package cards

// CopyFaceFrom resolution tests (ticket agent-20260928T215303Z-ab37989c).
//
// A `CopyFaceFrom:<Card>` face directive carries no printed characteristics of
// its own: the face IS the named card's front face. The Forge corpus uses it
// for 21 of the 54 AlternateMode:Prepare inset spells and for both faces of
// two AlternateMode:Split cards (bind_liberate.txt, start_fire.txt). Before
// this ticket the parser dropped the key, so those backs parsed as a
// zero-value Face (no name, cost, types, abilities or SVars) and the two
// split cards had no named face at all.

import (
	"path/filepath"
	"testing"
)

// TestParseCopyFaceFromRecordsTheDirective pins the parse half: the raw
// `CopyFaceFrom:` value is retained on the face that carries it, per face, and
// ALTERNATE starts a new face so the second directive lands on Faces[1]. A
// parser that dropped the key (the pre-ticket behaviour) leaves both empty and
// this fails.
func TestParseCopyFaceFromRecordsTheDirective(t *testing.T) {
	src := "Name:Stub Host\nTypes:Creature\nPT:2/2\nOracle:x\n" +
		"ALTERNATE\nCopyFaceFrom:  Zap  \n"
	c, diags := ParseBytes("stub.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("diags = %+v, want none (CopyFaceFrom is a known directive)", diags)
	}
	if len(c.Faces) != 2 {
		t.Fatalf("faces = %d, want 2", len(c.Faces))
	}
	if got := c.Faces[1].CopyFaceFrom; got != "Zap" {
		t.Fatalf("Faces[1].CopyFaceFrom = %q, want %q (trimmed)", got, "Zap")
	}
	if got := c.Faces[1].Name; got != "" {
		t.Fatalf("Faces[1].Name = %q before resolution, want empty (ParseBytes does not resolve)", got)
	}
}

// zapSrc is a minimal spell fixture whose ability the resolve test can read
// back: a 3-damage instant. Fixtures are authored inline, never copied from
// the GPL corpus.
const zapSrc = "Name:Zap\nManaCost:R\nTypes:Instant\n" +
	"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 3 | SpellDescription$ Deal 3 damage.\n"

// TestCompileDirResolvesCopyFaceFrom is the resolve half: a face carrying
// CopyFaceFrom takes the referenced card's front-face printed characteristics,
// its ability tree is functional, and the referenced card is found even though
// it compiles LATER in sorted path order (a_stub sorts before z_zap, which is
// exactly the ordering the second pass exists to survive).
func TestCompileDirResolvesCopyFaceFrom(t *testing.T) {
	dir := t.TempDir()
	writeCardFile(t, dir, "a_stub.txt",
		"Name:Stub Host\nManaCost:2 R\nTypes:Creature Wizard\nPT:2/2\nOracle:x\nALTERNATE\nCopyFaceFrom:Zap\n")
	writeCardFile(t, dir, "z_zap.txt", zapSrc)

	r, diags, err := CompileDir(dir)
	if err != nil {
		t.Fatalf("CompileDir: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diags = %+v, want none", diags)
	}
	host, ok := r.Lookup("Stub Host")
	if !ok {
		t.Fatal("Stub Host missing from the registry")
	}
	if len(host.Faces) != 2 || host.Faces[1] == nil {
		t.Fatalf("Stub Host faces = %d, want a named back", len(host.Faces))
	}
	back := host.Faces[1]
	if back.Name != "Zap" {
		t.Fatalf("back Name = %q, want Zap", back.Name)
	}
	if back.ManaCost != "R" {
		t.Fatalf("back ManaCost = %q, want R", back.ManaCost)
	}
	if !back.IsInstant() {
		t.Fatalf("back Types = %v, want Instant", back.Types)
	}
	sa := back.SpellAbility()
	if sa == nil || sa.API != "DealDamage" || sa.Params["NumDmg"] != "3" {
		t.Fatalf("back spell ability = %+v, want DealDamage NumDmg 3 (the referenced spell's)", sa)
	}
	// The referenced name is what Lookup resolves, and the back is reachable
	// through the same card (the front face wins the name index).
	if got, ok := r.Lookup("Zap"); !ok || got != host {
		t.Fatalf("Lookup(Zap) = %v, %v; want the Stub Host card via its resolved back", got, ok)
	}
}

// TestCompileDirResolvesBothFacesOfASplitCopy pins the AlternateMode:Split
// shape the corpus spells as bind_liberate.txt/start_fire.txt: BOTH faces are
// CopyFaceFrom, so before resolution the card had no named face at all and was
// dropped from Coverage. After resolution both faces are named and the
// registry reports the card as a real (named) card.
func TestCompileDirResolvesBothFacesOfASplitCopy(t *testing.T) {
	dir := t.TempDir()
	writeCardFile(t, dir, "split.txt",
		"CopyFaceFrom:Zap\nAlternateMode:Split\nALTERNATE\nCopyFaceFrom:Zing\n")
	writeCardFile(t, dir, "zap.txt", zapSrc)
	writeCardFile(t, dir, "zing.txt",
		"Name:Zing\nManaCost:1 U\nTypes:Sorcery\nA:SP$ Draw | Defined$ You | Amount$ 1 | SpellDescription$ Draw a card.\n")

	r, diags, err := CompileDir(dir)
	if err != nil {
		t.Fatalf("CompileDir: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diags = %+v, want none (both references resolve)", diags)
	}
	split, ok := r.Lookup("Zap")
	if !ok {
		t.Fatal("resolved split card missing")
	}
	if split.Faces[0].Name != "Zap" || split.Faces[1].Name != "Zing" {
		t.Fatalf("split faces = %q / %q, want Zap / Zing", split.Faces[0].Name, split.Faces[1].Name)
	}
	if !split.Faces[0].IsInstant() || !split.Faces[1].IsSorcery() {
		t.Fatalf("split face types = %v / %v, want Instant / Sorcery", split.Faces[0].Types, split.Faces[1].Types)
	}
	cv := r.Coverage(map[string]bool{})
	if cv.Cards != 3 {
		t.Errorf("Coverage.Cards = %d, want 3 (the split card now has a named face)", cv.Cards)
	}
}

// TestCopyFaceFromResolutionSurvivesCacheRoundTrip covers the second
// construction route: a resolved face must round-trip through Save/LoadRegistry
// with its printed characteristics and linked ability tree intact, and the load
// pass must be idempotent (a face already named is not re-copied).
func TestCopyFaceFromResolutionSurvivesCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeCardFile(t, dir, "a_stub.txt",
		"Name:Stub Host\nManaCost:2 R\nTypes:Creature Wizard\nPT:2/2\nOracle:x\nALTERNATE\nCopyFaceFrom:Zap\n")
	writeCardFile(t, dir, "z_zap.txt", zapSrc)

	r, _, err := CompileDir(dir)
	if err != nil {
		t.Fatalf("CompileDir: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ir.gob.gz")
	if err := r.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	host, ok := back.Lookup("Stub Host")
	if !ok || len(host.Faces) != 2 {
		t.Fatalf("loaded Stub Host = %v, want a two-face card", ok)
	}
	f := host.Faces[1]
	if f.Name != "Zap" || f.ManaCost != "R" || !f.IsInstant() {
		t.Fatalf("loaded back = %q %q %v, want Zap R Instant", f.Name, f.ManaCost, f.Types)
	}
	if sa := f.SpellAbility(); sa == nil || sa.Params["NumDmg"] != "3" {
		t.Fatalf("loaded back spell ability = %+v, want DealDamage NumDmg 3", sa)
	}
}
