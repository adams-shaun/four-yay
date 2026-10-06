package cards

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestImageCorpusOracle(t *testing.T) {
	full := compiledCorpus(t)
	dir := t.TempDir()
	cache := CachePath(dir)
	if err := full.Save(cache); err != nil {
		t.Fatal(err)
	}
	sf, err := openSegFile(SegmentPath(cache))
	if err != nil {
		t.Fatal(err)
	}
	defer sf.f.Close()
	ords := make([]int32, sf.nCards)
	for i := range ords {
		ords[i] = int32(i)
	}
	rawCards, err := sf.decodeCards(ords)
	if err != nil {
		t.Fatal(err)
	}
	rawTokens, err := sf.decodeTokens()
	if err != nil {
		t.Fatal(err)
	}
	img, err := buildImage(rawCards, rawTokens)
	if err != nil {
		t.Fatal(err)
	}
	if len(img.Cards) != sf.nCards {
		t.Fatalf("image cards %d segment cards %d", len(img.Cards), sf.nCards)
	}
	start := time.Now()
	raw, err := img.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildImage(rawCards, rawTokens)
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := second.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw, raw2) {
		t.Fatal("canonical image bytes differ between builds")
	}
	t.Logf("image: %d bytes; build+canonical checks: %s", len(raw), time.Since(start))
	for lo := 0; lo < sf.nCards; lo += 1000 {
		lo := lo
		hi := lo + 1000
		if hi > sf.nCards {
			hi = sf.nCards
		}
		t.Run(fmt.Sprintf("range-%05d", lo), func(t *testing.T) {
			ords := make([]int32, hi-lo)
			for j := range ords {
				ords[j] = int32(lo + j)
			}
			decoded, e := sf.decodeCards(ords)
			if e != nil {
				t.Fatal(e)
			}
			for j, want := range decoded {
				got := img.rawCard(lo + j)
				if !reflect.DeepEqual(cleanCopy(t, got), cleanCopy(t, want)) {
					t.Fatalf("ordinal %d raw image differs from segment decode", lo+j)
				}
				mini, e := finishDecoded([]*Card{got}, nil)
				if e != nil {
					t.Fatalf("finishDecoded ordinal %d: %v", lo+j, e)
				}
				sameCard(t, "ordinal", mini.Cards[0], full.Cards[lo+j])
			}
		})
	}
	tokens := rawTokens
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) != len(img.Tokens) {
		t.Fatalf("image tokens %d, want %d", len(img.Tokens), len(keys))
	}
	for i, k := range keys {
		got := img.rawToken(i)
		if !reflect.DeepEqual(cleanCopy(t, got), cleanCopy(t, tokens[k])) {
			t.Errorf("token %q differs from segment decode", k)
			break
		}
	}
	for i, r := range img.Cards {
		p := img.str(r.Path)
		if filepath.IsAbs(p) || p != full.Cards[i].Path {
			t.Fatalf("card %d path %q not relative round-trip of %q", i, p, full.Cards[i].Path)
		}
	}
}
