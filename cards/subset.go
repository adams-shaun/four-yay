package cards

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// The subset loader.
//
// A process that plays a known card pool (a spellbench catalog, a deck-file
// bench) needs a few hundred of the corpus's ~34k cards, but LoadRegistry
// decodes and compiles all of them: 1.0 s and 525 MB allocated, 189 MB
// retained (measured 2026-09-30 with a throwaway cards test over the pinned
// corpus). The gob cache is one value, a []*Card, and gob cannot skip a
// slice element without decoding it, so a subset needs a second layout:
// the segment file, written beside every cache by Save.
//
// A segment file holds each card as its own flate-compressed gob value
// message, all encoded by ONE encoder after a type prelude (the encoding of
// an empty Card, which sends every type descriptor the Card tree needs), so
// any card decodes from prelude+segment alone. It also holds every token in
// one segment and the whole corpus's tiered name index (nameIndexOf) as
// sorted (name, card ordinal) pairs. Decoded segments go through the same
// finishDecoded pipeline LoadRegistry runs, so a subset card is the value the
// full registry would hold.
//
// Why segments and not the alternatives (same measurement):
//   - a streaming decode that skips unwanted cards still pays the gunzip
//     (155 ms) and the full gob decode (228 ms) and its ~100 MB of garbage,
//     and needs a format change anyway (one gob value per card);
//   - lazy compilation from the .txt scripts (24 ms for the 172 FDN cards)
//     needs a name->path index all the same, and would build cards by the
//     parse route rather than the cache route every golden test runs on;
//   - segments: 11 ms to decode the 172 FDN cards, and the values are
//     bit-for-bit the ones the cache holds.

// SegmentPath is the segment file that sits beside the gob cache at cache.
func SegmentPath(cache string) string {
	return strings.TrimSuffix(cache, ".gob.gz") + ".seg"
}

const (
	segMagic   = "gorgeseg"
	segVersion = 1
	// segHeaderLen is magic(8) | cacheVersion u32 | segVersion u32 |
	// indexOff u64 | indexLen u64.
	segHeaderLen = 32
)

type segSpan struct{ Off, Len int64 }

// segIndex is the gob-encoded table at the end of a segment file.
type segIndex struct {
	Prelude segSpan   // raw (uncompressed) type prelude
	Cards   []segSpan // flate-compressed value message per card, corpus order
	Tokens  segSpan   // flate-compressed independent gob stream of segTokens
	// Names are the normalised names of the whole corpus's tiered name index
	// (the index LoadRegistry builds), sorted; Ords[i] is the ordinal in
	// Cards of the card Names[i] resolves to.
	Names []string
	Ords  []int32
}

type segTokens struct {
	Keys  []string
	Cards []*Card
}

// writeSegments writes the segment file for cs and tokens to path,
// atomically (a unique temp file renamed into place, as Save does).
func writeSegments(path string, cs []*Card, tokens map[string]*Card) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	fail := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		return fail(err)
	}
	var idx segIndex
	off := int64(segHeaderLen)
	if _, err := f.Write(make([]byte, segHeaderLen)); err != nil {
		return fail(err)
	}
	put := func(b []byte) (segSpan, error) {
		sp := segSpan{Off: off, Len: int64(len(b))}
		_, err := f.Write(b)
		off += int64(len(b))
		return sp, err
	}
	var vb, zb bytes.Buffer
	zw, err := flate.NewWriter(&zb, flate.BestSpeed)
	if err != nil {
		return fail(err)
	}
	compress := func(b []byte) ([]byte, error) {
		zb.Reset()
		zw.Reset(&zb)
		if _, err := zw.Write(b); err != nil {
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		return zb.Bytes(), nil
	}
	enc := gob.NewEncoder(&vb)
	if err := enc.Encode(&Card{}); err != nil {
		return fail(err)
	}
	if idx.Prelude, err = put(vb.Bytes()); err != nil {
		return fail(err)
	}
	ords := make(map[*Card]int32, len(cs))
	idx.Cards = make([]segSpan, len(cs))
	for i, c := range cs {
		if c == nil {
			return fail(fmt.Errorf("cards: segment file: nil card at %d", i))
		}
		ords[c] = int32(i)
		vb.Reset()
		if err := enc.Encode(c); err != nil {
			return fail(err)
		}
		z, err := compress(vb.Bytes())
		if err != nil {
			return fail(err)
		}
		if idx.Cards[i], err = put(z); err != nil {
			return fail(err)
		}
	}
	var st segTokens
	for k := range tokens {
		st.Keys = append(st.Keys, k)
	}
	sort.Strings(st.Keys)
	for _, k := range st.Keys {
		st.Cards = append(st.Cards, tokens[k])
	}
	vb.Reset()
	if err := gob.NewEncoder(&vb).Encode(st); err != nil {
		return fail(err)
	}
	z, err := compress(vb.Bytes())
	if err != nil {
		return fail(err)
	}
	if idx.Tokens, err = put(z); err != nil {
		return fail(err)
	}
	byName := nameIndexOf(cs)
	for k := range byName {
		idx.Names = append(idx.Names, k)
	}
	sort.Strings(idx.Names)
	idx.Ords = make([]int32, len(idx.Names))
	for i, k := range idx.Names {
		idx.Ords[i] = ords[byName[k]]
	}
	vb.Reset()
	if err := gob.NewEncoder(&vb).Encode(idx); err != nil {
		return fail(err)
	}
	indexOff := off
	if _, err := put(vb.Bytes()); err != nil {
		return fail(err)
	}
	hdr := make([]byte, segHeaderLen)
	copy(hdr, segMagic)
	binary.LittleEndian.PutUint32(hdr[8:], cacheVersion)
	binary.LittleEndian.PutUint32(hdr[12:], segVersion)
	binary.LittleEndian.PutUint64(hdr[16:], uint64(indexOff))
	binary.LittleEndian.PutUint64(hdr[24:], uint64(off-indexOff))
	if _, err := f.WriteAt(hdr, 0); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

var errSegFormat = errors.New("cards: segment file has a different format or cache version")

func readSegIndex(f *os.File) (*segIndex, []byte, error) {
	hdr := make([]byte, segHeaderLen)
	if _, err := f.ReadAt(hdr, 0); err != nil {
		return nil, nil, err
	}
	if string(hdr[:8]) != segMagic || binary.LittleEndian.Uint32(hdr[8:]) != cacheVersion ||
		binary.LittleEndian.Uint32(hdr[12:]) != segVersion {
		return nil, nil, errSegFormat
	}
	ioff, il := int64(binary.LittleEndian.Uint64(hdr[16:])), int64(binary.LittleEndian.Uint64(hdr[24:]))
	raw := make([]byte, il)
	if _, err := f.ReadAt(raw, ioff); err != nil {
		return nil, nil, err
	}
	var idx segIndex
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&idx); err != nil {
		return nil, nil, err
	}
	if len(idx.Names) != len(idx.Ords) {
		return nil, nil, errSegFormat
	}
	prelude := make([]byte, idx.Prelude.Len)
	if _, err := f.ReadAt(prelude, idx.Prelude.Off); err != nil {
		return nil, nil, err
	}
	return &idx, prelude, nil
}

// inflateSpan reads span sp of f and appends its decompressed bytes to dst.
func inflateSpan(f *os.File, sp segSpan, zr io.ReadCloser, dst *bytes.Buffer) error {
	z := make([]byte, sp.Len)
	if _, err := f.ReadAt(z, sp.Off); err != nil {
		return err
	}
	if err := zr.(flate.Resetter).Reset(bytes.NewReader(z), nil); err != nil {
		return err
	}
	_, err := io.Copy(dst, zr)
	return err
}

// decodeCards decodes the raw (cache-shaped) cards at ords, in that order.
func decodeCards(f *os.File, idx *segIndex, prelude []byte, ords []int32) ([]*Card, error) {
	var stream bytes.Buffer
	stream.Write(prelude)
	zr := flate.NewReader(bytes.NewReader(nil))
	for _, o := range ords {
		if o < 0 || int(o) >= len(idx.Cards) {
			return nil, errSegFormat
		}
		if err := inflateSpan(f, idx.Cards[o], zr, &stream); err != nil {
			return nil, err
		}
	}
	dec := gob.NewDecoder(bytes.NewReader(stream.Bytes()))
	var empty Card
	if err := dec.Decode(&empty); err != nil {
		return nil, err
	}
	out := make([]*Card, len(ords))
	for i := range ords {
		c := new(Card)
		if err := dec.Decode(c); err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

func decodeTokens(f *os.File, idx *segIndex) (map[string]*Card, error) {
	var stream bytes.Buffer
	if err := inflateSpan(f, idx.Tokens, flate.NewReader(bytes.NewReader(nil)), &stream); err != nil {
		return nil, err
	}
	var st segTokens
	if err := gob.NewDecoder(&stream).Decode(&st); err != nil {
		return nil, err
	}
	if len(st.Keys) != len(st.Cards) {
		return nil, errSegFormat
	}
	out := make(map[string]*Card, len(st.Keys))
	for i, k := range st.Keys {
		out[k] = st.Cards[i]
	}
	return out, nil
}

// subsetSource backs a subset registry's Lookup with the whole corpus: the
// segment file stays open, and a name outside the subset decodes its card on
// first use, so Lookup answers every name exactly as the full registry does.
type subsetSource struct {
	f       *os.File
	idx     *segIndex
	prelude []byte
	dir     string
	loaded  []atomic.Pointer[Card] // by corpus ordinal
	mu      sync.Mutex             // serialises fault-ins
}

func (s *subsetSource) ordinal(key string) (int32, bool) {
	i := sort.SearchStrings(s.idx.Names, key)
	if i >= len(s.idx.Names) || s.idx.Names[i] != key {
		return 0, false
	}
	return s.idx.Ords[i], true
}

func (s *subsetSource) lookup(key string) (*Card, bool) {
	ord, ok := s.ordinal(key)
	if !ok {
		return nil, false
	}
	if c := s.loaded[ord].Load(); c != nil {
		return c, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.loaded[ord].Load(); c != nil {
		return c, true
	}
	raw, err := decodeCards(s.f, s.idx, s.prelude, []int32{ord})
	if err != nil {
		// The file was readable at open; failing now is an I/O fault, and
		// answering "no such card" would silently diverge from the full
		// registry.
		panic(fmt.Sprintf("cards: subset registry: decoding %q from %s: %v", key, s.f.Name(), err))
	}
	// Its own one-card registry gives it a catalog binding; every compiled
	// code and mask is a function of the face alone, so it is the value the
	// full registry holds.
	mini, err := finishDecoded(raw, nil)
	if err != nil {
		panic(fmt.Sprintf("cards: subset registry: compiling %q: %v", key, err))
	}
	rerootPaths(mini, s.dir)
	c := mini.Cards[0]
	s.loaded[ord].Store(c)
	return c, true
}

// IsSubset reports whether r was built by OpenCorpusSubset: Cards holds only
// the requested cards (Lookup still answers every corpus name).
func (r *Registry) IsSubset() bool { return r != nil && r.sub != nil }

// segFresh applies OpenCorpus's staleness rule to the segment file: it must
// exist and be no older than the cache it was written beside or cards.lock.
func segFresh(dir, cache, seg string) bool {
	si, err := os.Stat(seg)
	if err != nil {
		return false
	}
	if ci, err := os.Stat(cache); err == nil && ci.ModTime().After(si.ModTime()) {
		return false
	}
	if li, err := os.Stat(filepath.Join(dir, "cards.lock")); err == nil && li.ModTime().After(si.ModTime()) {
		return false
	}
	return true
}

// refreshSegments rebuilds seg: from the gob cache when that is fresh (a raw
// decode, no compile), or by recompiling the corpus and saving both files.
func refreshSegments(dir, cache, seg string) error {
	ci, cerr := os.Stat(cache)
	li, lerr := os.Stat(filepath.Join(dir, "cards.lock"))
	if cerr == nil && (lerr != nil || !li.ModTime().After(ci.ModTime())) {
		if cf, err := decodeCacheFile(cache); err == nil && cf.Version == cacheVersion {
			return writeSegments(seg, cf.Cards, cf.Tokens)
		}
	}
	r, _, err := CompileDir(CorpusDir(dir))
	if err != nil {
		return err
	}
	if err := r.Save(cache); err != nil {
		return err
	}
	PruneCaches(dir, cache)
	return nil
}

func decodeCacheFile(path string) (*cacheFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var cf cacheFile
	if err := gob.NewDecoder(zr).Decode(&cf); err != nil {
		return nil, err
	}
	return &cf, nil
}

// OpenCorpusSubset opens dir's corpus holding only the cards names resolve
// to (by Lookup's rules against the WHOLE corpus), plus every token script:
// tokens are small (839 scripts) and the engine names several token keys in
// Go code (Clue, Treasure, Amass's Army, ...), so no closure over TokenScript$
// is needed or attempted. A card whose face is an unresolved CopyFaceFrom
// stub pulls in the card it names. A name that resolves to nothing is
// skipped: Lookup reports it missing exactly as the full registry would.
//
// The returned registry's Cards is the subset, in corpus order, so a caller
// that feeds Cards to rules.Config.NameUniverse gets a smaller universe --
// see OpenCorpusFor, which falls back to the full corpus when that would
// matter. Lookup answers every corpus name (decoding a card outside the
// subset on first use). A subset registry cannot be Saved.
//
// The segment file is rebuilt when missing or stale (from the gob cache, or
// by recompiling), so the first subset open after a compile costs one full
// raw decode.
func OpenCorpusSubset(dir string, names []string) (*Registry, error) {
	cache := CachePath(dir)
	seg := SegmentPath(cache)
	if !segFresh(dir, cache, seg) {
		if err := refreshSegments(dir, cache, seg); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(seg)
	if err != nil {
		return nil, err
	}
	idx, prelude, err := readSegIndex(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	s := &subsetSource{f: f, idx: idx, prelude: prelude, dir: abs, loaded: make([]atomic.Pointer[Card], len(idx.Cards))}
	want := map[int32]bool{}
	for _, n := range names {
		if o, ok := s.ordinal(NormalizeName(n)); ok {
			want[o] = true
		}
	}
	var ords []int32
	var raw []*Card
	for {
		ords = ords[:0]
		for o := range want {
			ords = append(ords, o)
		}
		sort.Slice(ords, func(i, j int) bool { return ords[i] < ords[j] })
		raw, err = decodeCards(f, idx, prelude, ords)
		if err != nil {
			f.Close()
			return nil, err
		}
		grew := false
		for _, c := range raw {
			for _, face := range c.Faces {
				if face == nil || face.CopyFaceFrom == "" || face.Name != "" {
					continue
				}
				if o, ok := s.ordinal(NormalizeName(face.CopyFaceFrom)); ok && !want[o] {
					want[o] = true
					grew = true
				}
			}
		}
		if !grew {
			break
		}
	}
	tokens, err := decodeTokens(f, idx)
	if err != nil {
		f.Close()
		return nil, err
	}
	r, err := finishDecoded(raw, tokens)
	if err != nil {
		f.Close()
		return nil, err
	}
	rerootPaths(r, dir)
	for i, o := range ords {
		s.loaded[o].Store(r.Cards[i])
	}
	r.sub = s
	return r, nil
}

// OpenCorpusFor is the registry for a process whose games seat only the
// cards names lists (every main deck, sideboard and commander card): the
// OpenCorpusSubset registry, unless a game fed the subset as its
// rules.Config.NameUniverse could play differently from one fed the whole
// corpus (NeedsFullNameUniverse), in which case the full SharedCorpus. It
// also falls back to SharedCorpus when the subset cannot be opened. Either
// way games are byte-identical to the full registry's; IsSubset says which
// one the caller got. Like SharedCorpus it opens once per process per
// (directory, distinct card names) and hands the same registry back, since
// the rules layer's pointer-keyed memos keep every opened copy live.
func OpenCorpusFor(dir string, names []string) (*Registry, error) {
	keys := make([]string, 0, len(names)+1)
	if abs, err := filepath.Abs(dir); err == nil {
		keys = append(keys, abs)
	} else {
		keys = append(keys, dir)
	}
	distinct := map[string]bool{}
	for _, n := range names {
		distinct[NormalizeName(n)] = true
	}
	for k := range distinct {
		keys = append(keys, k)
	}
	sort.Strings(keys[1:])
	key := strings.Join(keys, "\x00")
	subsetCorpora.Lock()
	defer subsetCorpora.Unlock()
	if r, ok := subsetCorpora.m[key]; ok {
		return r, nil
	}
	r, err := OpenCorpusSubset(dir, names)
	if err != nil || NeedsFullNameUniverse(r.Cards, r.Tokens) {
		if err == nil {
			r.sub.f.Close()
		}
		if r, err = SharedCorpus(dir); err != nil {
			return nil, err
		}
	}
	if subsetCorpora.m == nil {
		subsetCorpora.m = map[string]*Registry{}
	}
	subsetCorpora.m[key] = r
	return r, nil
}

var subsetCorpora struct {
	sync.Mutex
	m map[string]*Registry
}

// nameUniverseMarkers are the script words that reach a reader of
// rules.Config.NameUniverse (state.Game.NameUniverse): NameCard (the
// mid-resolution and as-enters name choice), ChosenName (Clone's
// CopyFromChosenName$ and the chosen-name copy that follow one) and
// AllNonBasicLandType (rules' land-type vocabulary, derived from the
// universe). rules' TestNameUniverseReadersAreKnown pins the Go files that
// read the universe, so a new reader must extend this list or
// NeedsFullNameUniverse.
var nameUniverseMarkers = []string{"NameCard", "ChosenName", "AllNonBasicLandType"}

// NeedsFullNameUniverse reports whether a game whose NameUniverse is cs could
// differ from one whose NameUniverse is the whole corpus, given that only cs
// and tokens are ever seated. It is true when any face of cs or tokens
// mentions a nameUniverseMarkers word anywhere in its compiled text (ability
// lines and parameters, sub-abilities, triggers, statics, replacements,
// keywords and SVars; deliberately textual, so it over-approximates), and
// when no land face in cs carries a basic land type: rules gates the CR 305.6
// granted-intrinsic mana path on its land-type vocabulary being non-empty,
// and a basic land type in the universe is what keeps it non-empty (the
// whole corpus's always is).
func NeedsFullNameUniverse(cs []*Card, tokens map[string]*Card) bool {
	basic := false
	for _, c := range cs {
		if cardMentions(c, nameUniverseMarkers) {
			return true
		}
		basic = basic || hasBasicLandTypedLand(c)
	}
	for _, c := range tokens {
		if cardMentions(c, nameUniverseMarkers) {
			return true
		}
	}
	return !basic
}

func hasBasicLandTypedLand(c *Card) bool {
	if c == nil {
		return false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		land, basicType := false, false
		for _, t := range f.Types {
			switch t {
			case "Land":
				land = true
			case "Plains", "Island", "Swamp", "Mountain", "Forest":
				basicType = true
			}
		}
		if land && basicType {
			return true
		}
	}
	return false
}

func cardMentions(c *Card, words []string) bool {
	if c == nil {
		return false
	}
	has := func(s string) bool {
		for _, w := range words {
			if strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	params := func(m map[string]string) bool {
		for k, v := range m {
			if has(k) || has(v) {
				return true
			}
		}
		return false
	}
	var sa func(a *SA, depth int) bool
	sa = func(a *SA, depth int) bool {
		for ; a != nil && depth < 64; a, depth = a.Sub, depth+1 {
			if has(a.API) || has(a.Line) || params(a.Params) {
				return true
			}
		}
		return false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, k := range f.Keywords {
			if has(k) {
				return true
			}
		}
		for _, a := range f.Abilities {
			if sa(a, 0) {
				return true
			}
		}
		for _, t := range f.Triggers {
			if has(t.Mode) || params(t.Params) || sa(t.Effect, 0) {
				return true
			}
		}
		for _, st := range f.Statics {
			if has(st.Mode) || params(st.Params) {
				return true
			}
		}
		for _, rp := range f.Repls {
			if has(rp.Event) || params(rp.Params) || sa(rp.With, 0) {
				return true
			}
		}
		for _, v := range f.SVars {
			if has(v) {
				return true
			}
		}
	}
	return false
}
