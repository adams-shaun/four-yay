package cards

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// determinismFixtureSrc is a synthetic card whose every map-bearing type
// carries at least two entries: SA.Params on the spell ability and its
// SubAbility, Trigger.Params, two Static.Params, Repl.Params, and three
// Face.SVars. The maps are what encoding/gob used to walk in runtime-random
// order, so a fixture with multi-entry maps reproduces the nondeterminism
// without loading the corpus.
const determinismFixtureSrc = "Name:Determinism Fixture\n" +
	"ManaCost:1 U\n" +
	"Types:Creature Human Wizard\n" +
	"PT:2/2\n" +
	"A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3 | SubAbility$ DBDraw\n" +
	"SVar:DBDraw:DB$ Draw | NumCards$ 1 | Defined$ You\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigPump | TriggerZones$ Battlefield\n" +
	"SVar:TrigPump:DB$ Pump | NumAtt$ 2\n" +
	"S:Mode$ Continuous | Affected$ Card.Self | AddPower$ 1 | AddToughness$ 1\n" +
	"S:Mode$ CantAttack | ValidCard$ Card.Self | Description$ CARDNAME can't attack.\n" +
	"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidTarget$ Card.Self | ReplaceWith$ ReplFog\n" +
	"SVar:ReplFog:DB$ Fog\n" +
	"K:Flying\n" +
	"K:Trample\n" +
	"Oracle:x\n"

// TestSaveIsByteIdenticalAcrossRuns pins the determinism promise CompileDir's
// doc makes: for one on-disk corpus, repeated CompileDir + Save runs must
// write byte-identical cache and segment files. encoding/gob walks maps with
// reflect.MapRange() (unordered) and does not sort keys, so before the
// canonical GobEncode/GobDecode pairs in cache_canon.go the Params/SVars and
// cacheFile.Tokens maps made both artifacts differ on every run — on the real
// corpus the file size itself moved by thousands of bytes.
//
// Four saves each of the .gob.gz and the .seg must be equal. The synthetic
// fixture needs no corpus: every map under test has >= 2 entries, asserted
// loudly below, and the registry carries two tokens so cacheFile.Tokens is
// multi-entry too.
func TestSaveIsByteIdenticalAcrossRuns(t *testing.T) {
	root := t.TempDir()
	writeCardFile(t, CorpusDir(root), "determinism.txt", determinismFixtureSrc)
	// Two token scripts: cacheFile.Tokens and the segment file's segTokens
	// both encode this map, and one entry would hide map-order variance.
	writeCardFile(t, TokensDir(root), "r_1_1_goblin.txt", goblinTokenSrc)
	writeCardFile(t, TokensDir(root), "c_3_3_a_phyrexian_wurm_deathtouch.txt", wurmTokenSrc)

	r, diags, err := CompileDir(CorpusDir(root))
	if err != nil || len(diags) != 0 {
		t.Fatalf("CompileDir: %v, diags %v", err, diags)
	}

	// Preconditions: the fixture must exercise every map whose unordered
	// encoding caused the defect. A setup that failed to produce a
	// multi-entry map would let the test pass without testing anything.
	card, ok := r.Lookup("Determinism Fixture")
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
	multiStatic := false
	for _, st := range face.Statics {
		if len(st.Params) >= 2 {
			multiStatic = true
		}
	}
	if !multiStatic {
		t.Fatalf("fixture statics = %+v, want one with >= 2 Params", face.Statics)
	}
	if len(face.Repls) == 0 || len(face.Repls[0].Params) < 2 {
		t.Fatalf("fixture replacement Params = %+v, want >= 2 entries", face.Repls)
	}
	if len(r.Tokens) < 2 {
		t.Fatalf("fixture Tokens = %d, want >= 2 for a multi-entry cache map", len(r.Tokens))
	}

	const saves = 4
	var gobFirst, segFirst []byte
	for i := 0; i < saves; i++ {
		cache := filepath.Join(t.TempDir(), "ir.gob.gz")
		if err := r.Save(cache); err != nil {
			t.Fatalf("save #%d: %v", i, err)
		}
		gotGob, err := os.ReadFile(cache)
		if err != nil {
			t.Fatalf("read cache #%d: %v", i, err)
		}
		gotSeg, err := os.ReadFile(SegmentPath(cache))
		if err != nil {
			t.Fatalf("read segment #%d: %v", i, err)
		}
		if len(gotGob) == 0 || len(gotSeg) == 0 {
			t.Fatalf("save #%d wrote an empty artifact", i)
		}
		if i == 0 {
			gobFirst, segFirst = gotGob, gotSeg
			continue
		}
		if !bytes.Equal(gotGob, gobFirst) {
			t.Fatalf("cache file differs between runs 0 and %d (%d vs %d bytes)", i, len(gobFirst), len(gotGob))
		}
		if !bytes.Equal(gotSeg, segFirst) {
			t.Fatalf("segment file differs between runs 0 and %d (%d vs %d bytes)", i, len(segFirst), len(gotSeg))
		}
	}
}
