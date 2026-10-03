package cards

import (
	"bufio"
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
	"slices"
	"sort"
	"strings"
	"sync"
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
// one segment and the whole corpus's tiered name index (nameIndexOf). Decoded
// segments go through the same finishDecoded pipeline LoadRegistry runs, so
// a subset card is the value the full registry would hold.
//
// Why segments and not the alternatives (same measurement):
//   - a streaming decode that skips unwanted cards still pays the gunzip
//     (155 ms) and the full gob decode (228 ms) and its ~100 MB of garbage,
//     and needs a format change anyway (one gob value per card);
//   - lazy compilation from the .txt scripts (24 ms for the 172 FDN cards)
//     needs a name->path index all the same, and would build cards by the
//     parse route rather than the cache route every golden test runs on;
//   - segments: the 172 FDN cards decode in 2 ms / 1.0 MB, and the values
//     are bit-for-bit the ones the cache holds. A whole FDN subset open
//     (index, cards, all tokens, finishDecoded) is ~9 ms / 7 MB allocated.

// SegmentPath is the segment file that sits beside the gob cache at cache.
func SegmentPath(cache string) string {
	return strings.TrimSuffix(cache, ".gob.gz") + ".seg"
}

// Segment file layout (all integers little-endian):
//
//	header  magic "gorgeseg" | cacheVersion u32 | segVersion u32 |
//	        nCards u32 | nNames u32 | preludeLen u32 | tokensLen u32 |
//	        tokensOff u64 | cardBase u64 | tailOff u64        (segHeaderLen)
//	prelude raw gob type prelude (the encoding of an empty Card)
//	cards   card i's flate-compressed gob value message at
//	        [cardBase+end(i-1), cardBase+end(i))
//	tokens  one flate-compressed gob stream of segTokens
//	tail    cardEnds [nCards]u32 | nameEnds [nNames]u32 | ords [nNames]u32 |
//	        names blob
//
// The tail is read with ONE ReadAt into one buffer and used in place: the
// name index (the whole corpus's tiered index, nameIndexOf, as sorted
// normalised names concatenated in the blob, nameEnds[i] the end of name i
// and ords[i] its card's ordinal) is searched without decoding it, so opening
// a subset allocates the tail once instead of a string per corpus name.
const (
	segMagic     = "gorgeseg"
	segVersion   = 2
	segHeaderLen = 56
)

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
	w := bufio.NewWriterSize(f, 1<<16)
	off := int64(segHeaderLen)
	if _, err := w.Write(make([]byte, segHeaderLen)); err != nil {
		return fail(err)
	}
	put := func(b []byte) error {
		_, err := w.Write(b)
		off += int64(len(b))
		return err
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
	preludeLen := vb.Len()
	if err := put(vb.Bytes()); err != nil {
		return fail(err)
	}
	cardBase := off
	ords := make(map[*Card]uint32, len(cs))
	cardEnds := make([]byte, 0, 4*len(cs))
	for i, c := range cs {
		if c == nil {
			return fail(fmt.Errorf("cards: segment file: nil card at %d", i))
		}
		ords[c] = uint32(i)
		vb.Reset()
		if err := enc.Encode(c); err != nil {
			return fail(err)
		}
		z, err := compress(vb.Bytes())
		if err != nil {
			return fail(err)
		}
		if err := put(z); err != nil {
			return fail(err)
		}
		cardEnds = binary.LittleEndian.AppendUint32(cardEnds, uint32(off-cardBase))
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
	tokensOff, tokensLen := off, len(z)
	if err := put(z); err != nil {
		return fail(err)
	}
	byName := nameIndexOf(cs)
	names := make([]string, 0, len(byName))
	for k := range byName {
		names = append(names, k)
	}
	sort.Strings(names)
	tailOff := off
	var nameEnds, nameOrds, blob []byte
	for _, k := range names {
		blob = append(blob, k...)
		nameEnds = binary.LittleEndian.AppendUint32(nameEnds, uint32(len(blob)))
		nameOrds = binary.LittleEndian.AppendUint32(nameOrds, ords[byName[k]])
	}
	for _, b := range [][]byte{cardEnds, nameEnds, nameOrds, blob} {
		if err := put(b); err != nil {
			return fail(err)
		}
	}
	if err := w.Flush(); err != nil {
		return fail(err)
	}
	hdr := make([]byte, 0, segHeaderLen)
	hdr = append(hdr, segMagic...)
	for _, v := range []uint32{cacheVersion, segVersion, uint32(len(cs)), uint32(len(names)), uint32(preludeLen), uint32(tokensLen)} {
		hdr = binary.LittleEndian.AppendUint32(hdr, v)
	}
	for _, v := range []int64{tokensOff, cardBase, tailOff} {
		hdr = binary.LittleEndian.AppendUint64(hdr, uint64(v))
	}
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

// segFile is an open segment file: its header fields, the prelude and the
// tail (card ends, name index), each read once and used in place.
type segFile struct {
	f                  *os.File
	nCards, nNames     int
	prelude            []byte
	tokensOff          int64
	tokensLen          int64
	cardBase           int64
	cardEnds           []byte // [nCards]u32
	nameEnds, nameOrds []byte // [nNames]u32
	blob               []byte
}

func openSegFile(path string) (*segFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	sf, err := readSegFile(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return sf, nil
}

func readSegFile(f *os.File) (*segFile, error) {
	var hdr [segHeaderLen]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		return nil, err
	}
	le := binary.LittleEndian
	if string(hdr[:8]) != segMagic || le.Uint32(hdr[8:]) != cacheVersion || le.Uint32(hdr[12:]) != segVersion {
		return nil, errSegFormat
	}
	sf := &segFile{f: f, nCards: int(le.Uint32(hdr[16:])), nNames: int(le.Uint32(hdr[20:])),
		tokensLen: int64(le.Uint32(hdr[28:])), tokensOff: int64(le.Uint64(hdr[32:])),
		cardBase: int64(le.Uint64(hdr[40:]))}
	tailOff := int64(le.Uint64(hdr[48:]))
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fixed := int64(4*sf.nCards + 8*sf.nNames)
	if tailOff < segHeaderLen || info.Size()-tailOff < fixed {
		return nil, errSegFormat
	}
	// One read for the prelude (right after the header) and one for the
	// tail; every slice below aliases one of these two buffers.
	sf.prelude = make([]byte, le.Uint32(hdr[24:]))
	if _, err := f.ReadAt(sf.prelude, segHeaderLen); err != nil {
		return nil, err
	}
	tail := make([]byte, info.Size()-tailOff)
	if _, err := f.ReadAt(tail, tailOff); err != nil {
		return nil, err
	}
	sf.cardEnds, tail = tail[:4*sf.nCards], tail[4*sf.nCards:]
	sf.nameEnds, tail = tail[:4*sf.nNames], tail[4*sf.nNames:]
	sf.nameOrds, sf.blob = tail[:4*sf.nNames], tail[4*sf.nNames:]
	if sf.nNames > 0 && int(le.Uint32(sf.nameEnds[4*(sf.nNames-1):])) != len(sf.blob) {
		return nil, errSegFormat
	}
	return sf, nil
}

func (sf *segFile) name(i int) string {
	start := uint32(0)
	if i > 0 {
		start = binary.LittleEndian.Uint32(sf.nameEnds[4*(i-1):])
	}
	return string(sf.blob[start:binary.LittleEndian.Uint32(sf.nameEnds[4*i:])])
}

// ordinal is the corpus ordinal of the card normalised name key resolves to.
// The search compares blob bytes against key without allocating.
func (sf *segFile) ordinal(key string) (int32, bool) {
	le := binary.LittleEndian
	nameAt := func(i int) []byte {
		start := uint32(0)
		if i > 0 {
			start = le.Uint32(sf.nameEnds[4*(i-1):])
		}
		return sf.blob[start:le.Uint32(sf.nameEnds[4*i:])]
	}
	i := sort.Search(sf.nNames, func(i int) bool { return string(nameAt(i)) >= key })
	if i >= sf.nNames || string(nameAt(i)) != key {
		return 0, false
	}
	return int32(le.Uint32(sf.nameOrds[4*i:])), true
}

func (sf *segFile) cardSpan(ord int32) (off, n int64) {
	le := binary.LittleEndian
	start := int64(0)
	if ord > 0 {
		start = int64(le.Uint32(sf.cardEnds[4*(ord-1):]))
	}
	return sf.cardBase + start, int64(le.Uint32(sf.cardEnds[4*ord:])) - start
}

// spanStream is an io.Reader over a raw prefix followed by flate-compressed
// file spans inflated one after another through one reused decompressor and
// one reused compressed-bytes buffer, so a gob decoder reads the cards
// straight off the file with no whole-stream buffer in between.
type spanStream struct {
	f      *os.File
	spans  [][2]int64
	next   int
	z      []byte
	zbr    bytes.Reader
	zr     io.ReadCloser
	pre    bytes.Reader
	cur    io.Reader
	hasPre bool
}

func newSpanStream(f *os.File, prefix []byte, spans [][2]int64) *spanStream {
	s := &spanStream{f: f, spans: spans}
	if len(prefix) > 0 {
		s.pre.Reset(prefix)
		s.cur, s.hasPre = &s.pre, true
	}
	return s
}

func (s *spanStream) Read(p []byte) (int, error) {
	for {
		if s.cur == nil {
			if s.next >= len(s.spans) {
				return 0, io.EOF
			}
			sp := s.spans[s.next]
			s.next++
			if int64(cap(s.z)) < sp[1] {
				s.z = make([]byte, sp[1], 2*sp[1])
			}
			s.z = s.z[:sp[1]]
			if _, err := s.f.ReadAt(s.z, sp[0]); err != nil {
				return 0, err
			}
			s.zbr.Reset(s.z)
			if s.zr == nil {
				s.zr = flate.NewReader(&s.zbr)
			} else if err := s.zr.(flate.Resetter).Reset(&s.zbr, nil); err != nil {
				return 0, err
			}
			s.cur = s.zr
		}
		n, err := s.cur.Read(p)
		if err == io.EOF {
			s.cur = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

// decodeCards decodes the raw (cache-shaped) cards at ords, in that order,
// straight into one slab of Cards: the registry's final storage.
func (sf *segFile) decodeCards(ords []int32) ([]*Card, error) {
	spans := make([][2]int64, len(ords))
	for i, o := range ords {
		if o < 0 || int(o) >= sf.nCards {
			return nil, errSegFormat
		}
		spans[i][0], spans[i][1] = sf.cardSpan(o)
	}
	dec := gob.NewDecoder(newSpanStream(sf.f, sf.prelude, spans))
	var empty Card
	if err := dec.Decode(&empty); err != nil {
		return nil, err
	}
	slab := make([]Card, len(ords))
	out := make([]*Card, len(ords))
	for i := range slab {
		if err := dec.Decode(&slab[i]); err != nil {
			return nil, err
		}
		out[i] = &slab[i]
	}
	return out, nil
}

func (sf *segFile) decodeTokens() (map[string]*Card, error) {
	var st segTokens
	dec := gob.NewDecoder(newSpanStream(sf.f, nil, [][2]int64{{sf.tokensOff, sf.tokensLen}}))
	if err := dec.Decode(&st); err != nil {
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
// ords/cards (the subset, sorted by ordinal) are immutable after open, so a
// hit takes no lock; only a fault-in does.
type subsetSource struct {
	sf      *segFile
	dir     string
	ords    []int32
	cards   []*Card
	mu      sync.Mutex
	faulted map[int32]*Card
}

func (s *subsetSource) lookup(key string) (*Card, bool) {
	ord, ok := s.sf.ordinal(key)
	if !ok {
		return nil, false
	}
	if i, ok := slices.BinarySearch(s.ords, ord); ok {
		return s.cards[i], true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.faulted[ord]; ok {
		return c, true
	}
	raw, err := s.sf.decodeCards([]int32{ord})
	if err != nil {
		// The file was readable at open; failing now is an I/O fault, and
		// answering "no such card" would silently diverge from the full
		// registry.
		panic(fmt.Sprintf("cards: subset registry: decoding %q from %s: %v", key, s.sf.f.Name(), err))
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
	if s.faulted == nil {
		s.faulted = map[int32]*Card{}
	}
	s.faulted[ord] = c
	return c, true
}

// IsSubset reports whether r was built by OpenCorpusSubset: Cards holds only
// the requested cards (Lookup still answers every corpus name).
func (r *Registry) IsSubset() bool { return r != nil && r.sub != nil }

// segFresh applies OpenCorpus's staleness rule to the segment file: it must
// exist and be no older than the cache it was written beside, cards.lock, or
// the corpus folders -- the segment is a derived artifact of all of them, so
// it shares corpusInputNewerThan with cacheFresh rather than a second copy of
// the comparison.
func segFresh(dir, cache, seg string) bool {
	si, err := os.Stat(seg)
	if err != nil {
		return false
	}
	if ci, err := os.Stat(cache); err == nil && ci.ModTime().After(si.ModTime()) {
		return false
	}
	return !corpusInputNewerThan(dir, si)
}

// refreshSegments rebuilds seg: from the gob cache when that is fresh (a raw
// decode, no compile), or by recompiling the corpus and saving both files.
func refreshSegments(dir, cache, seg string) error {
	if cacheFresh(dir, cache) {
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
// to (by Lookup's rules against the WHOLE corpus), plus every token script.
// Every token, not a closure over the cards' TokenScript$ values: the engine
// also builds token keys in Go -- Investigate's Clue, Recruit's Soldier,
// Incubate, venture's dungeons, and Amass's "b_0_0_<type>_army" from the
// card's type word -- so a textual closure could miss one and change a game,
// and all 839 cost 4 MB allocated / 5 ms (measured 2026-09-30). A card whose
// face is an unresolved CopyFaceFrom stub pulls in the card it names. A name
// that resolves to nothing is skipped: Lookup reports it missing exactly as
// the full registry would.
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
	sf, err := openSegFile(seg)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Registry, error) {
		sf.f.Close()
		return nil, err
	}
	ords := make([]int32, 0, len(names))
	for _, n := range names {
		if o, ok := sf.ordinal(NormalizeName(n)); ok {
			ords = append(ords, o)
		}
	}
	var raw []*Card
	for {
		slices.Sort(ords)
		ords = slices.Compact(ords)
		raw, err = sf.decodeCards(ords)
		if err != nil {
			return fail(err)
		}
		n := len(ords)
		for _, c := range raw {
			for _, face := range c.Faces {
				if face == nil || face.CopyFaceFrom == "" || face.Name != "" {
					continue
				}
				if o, ok := sf.ordinal(NormalizeName(face.CopyFaceFrom)); ok {
					if _, have := slices.BinarySearch(ords[:n], o); !have {
						ords = append(ords, o)
					}
				}
			}
		}
		if len(ords) == n {
			break
		}
	}
	tokens, err := sf.decodeTokens()
	if err != nil {
		return fail(err)
	}
	r, err := finishDecoded(raw, tokens)
	if err != nil {
		return fail(err)
	}
	rerootPaths(r, dir)
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	r.sub = &subsetSource{sf: sf, dir: abs, ords: ords, cards: r.Cards[:len(r.Cards):len(r.Cards)]}
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
			r.sub.sf.f.Close()
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
