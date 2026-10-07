package cards

import (
	"os"
	"path/filepath"
	"testing"
)

const goblinTokenSrc = "Name:Goblin Token\nManaCost:no cost\nTypes:Creature Goblin\nColors:red\nPT:1/1\nOracle:\n"
const wurmTokenSrc = "Name:Phyrexian Wurm Token\nManaCost:no cost\nTypes:Artifact Creature Phyrexian Wurm\nColors:colorless\nPT:3/3\nK:Deathtouch\nOracle:Deathtouch\n"

func TestCompileDirLoadsTokenScriptsByStem(t *testing.T) {
	dir := t.TempDir()
	writeCardFile(t, CorpusDir(dir), "mountain.txt", "Name:Mountain\nTypes:Basic Land Mountain\nOracle:\n")
	writeCardFile(t, TokensDir(dir), "r_1_1_goblin.txt", goblinTokenSrc)
	writeCardFile(t, TokensDir(dir), "c_3_3_a_phyrexian_wurm_deathtouch.txt", wurmTokenSrc)
	r, diags, err := CompileDir(CorpusDir(dir))
	if err != nil || len(diags) != 0 {
		t.Fatalf("%v %v", err, diags)
	}
	if r.Len() != 1 {
		t.Fatalf("tokens leaked into Cards: %d", r.Len())
	}
	tok, ok := r.Token("r_1_1_goblin")
	if !ok || tok.Faces[0].Name != "Goblin Token" || tok.Faces[0].Power() != 1 || tok.Faces[0].Colors != "red" {
		t.Fatalf("goblin token %+v", tok)
	}
	if w, ok := r.Token("c_3_3_a_phyrexian_wurm_deathtouch"); !ok || !w.Faces[0].HasKeyword("Deathtouch") {
		t.Fatal("wurm token lacks Deathtouch")
	}
	if _, ok := r.Lookup("Goblin Token"); ok {
		t.Fatal("a token is not a card: Lookup must not find it")
	}
	cardID := r.Card(0).Faces[0].CompiledID()
	aID := r.Tokens["c_3_3_a_phyrexian_wurm_deathtouch"].Faces[0].CompiledID()
	zID := r.Tokens["r_1_1_goblin"].Faces[0].CompiledID()
	if cardID != 1 || aID != 2 || zID != 3 {
		t.Fatalf("compiled face IDs card/a-token/z-token = %d/%d/%d, want 1/2/3", cardID, aID, zID)
	}
}

func TestTokensSurviveTheCache(t *testing.T) {
	dir := t.TempDir()
	writeCardFile(t, CorpusDir(dir), "mountain.txt", "Name:Mountain\nTypes:Basic Land Mountain\nOracle:\n")
	writeCardFile(t, TokensDir(dir), "r_1_1_goblin.txt", goblinTokenSrc)
	r, _, _ := CompileDir(CorpusDir(dir))
	cache := filepath.Join(dir, "ir.gob.gz")
	if err := r.Save(cache); err != nil {
		t.Fatal(err)
	}
	back, err := LoadRegistry(cache)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := back.Token("r_1_1_goblin"); !ok {
		t.Fatal("token lost through Save/Load")
	}
	if back.Catalog() == nil || back.Tokens["r_1_1_goblin"].Faces[0].CompiledID() == 0 {
		t.Fatal("loaded token has no compiled binding")
	}
}

func TestCompileDirWithoutTokensStillCompiles(t *testing.T) {
	dir := t.TempDir()
	writeCardFile(t, CorpusDir(dir), "mountain.txt", "Name:Mountain\nTypes:Basic Land Mountain\nOracle:\n")
	if _, err := os.Stat(TokensDir(dir)); !os.IsNotExist(err) {
		t.Fatal("fixture has a tokens dir")
	}
	r, _, err := CompileDir(CorpusDir(dir))
	if err != nil || len(r.Tokens) != 0 {
		t.Fatalf("%v, %d tokens", err, len(r.Tokens))
	}
	if r.Catalog() == nil {
		t.Fatal("CompileDir without tokens returned no catalog")
	}
}
