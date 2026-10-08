package cards

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"sort"
)

const imageSchema uint32 = 1

type imageBuilder struct {
	im  *Image
	ids map[string]StringID
}

func (b *imageBuilder) str(s string) StringID {
	if s == "" {
		return 0
	}
	if id := b.ids[s]; id != 0 {
		return id
	}
	id := StringID(len(b.im.Strs) + 1)
	off := uint32(len(b.im.Blob))
	b.im.Blob = append(b.im.Blob, s...)
	b.im.Strs = append(b.im.Strs, StringRef{off, uint32(len(s))})
	b.ids[s] = id
	return id
}
func (b *imageBuilder) params(m map[string]string) Span {
	if m == nil {
		return Span{}
	}
	if len(m) == 0 {
		return Span{Start: ^uint32(0)}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := Span{uint32(len(b.im.Params)), uint32(len(keys))}
	for _, k := range keys {
		b.im.Params = append(b.im.Params, ParamRow{b.str(k), b.str(m[k])})
	}
	return s
}
func (b *imageBuilder) words(v []string) Span {
	if len(v) == 0 {
		return Span{}
	}
	s := Span{uint32(len(b.im.Words)), uint32(len(v))}
	for _, x := range v {
		b.im.Words = append(b.im.Words, b.str(x))
	}
	return s
}
func (b *imageBuilder) node(a *SA) NodeID {
	if a == nil {
		return 0
	}
	id := NodeID(len(b.im.Nodes) + 1)
	b.im.Nodes = append(b.im.Nodes, SARec{})
	sub := b.node(a.Sub)
	b.im.Nodes[id-1] = SARec{b.str(a.Kind), b.str(a.API), b.str(a.Line), b.params(a.Params), sub}
	return id
}
func (b *imageBuilder) card(c *Card) CardRec {
	if c == nil {
		return CardRec{}
	}
	r := CardRec{Path: b.str(c.Path), AlternateMode: b.str(c.AlternateMode)}
	if len(c.Faces) > 0 {
		r.Faces = Span{uint32(len(b.im.Faces)), uint32(len(c.Faces))}
	}
	if cardMentions(c, []string{"NameCard"}) {
		r.Flags |= imageNamesACard
	}
	if c.ChangesTypes() {
		r.Flags |= imageChangesTypes
	}
	if c.SetsName() {
		r.Flags |= imageSetsName
	}
	if c.MayCarryControlStatic() {
		r.Flags |= imageMayCarryControlStatic
	}
	for _, f := range c.Faces {
		fr := FaceRec{}
		if f == nil {
			fr.Nil = 1
			b.im.Faces = append(b.im.Faces, fr)
			continue
		}
		fr.Name = b.str(f.Name)
		fr.ManaCost = b.str(f.ManaCost)
		fr.PT = b.str(f.PT)
		fr.Loyalty = b.str(f.Loyalty)
		fr.Defense = b.str(f.Defense)
		fr.Colors = b.str(f.Colors)
		fr.Oracle = b.str(f.Oracle)
		fr.SpecializeColor = b.str(f.SpecializeColor)
		fr.CopyFaceFrom = b.str(f.CopyFaceFrom)
		for _, typ := range f.Types {
			fr.TypeMask |= typeMaskFor(typ)
		}
		fr.Types = b.words(f.Types)
		fr.Keywords = b.words(f.Keywords)
		fr.Aliases = b.words(f.Aliases)
		if len(f.Abilities) > 0 {
			fr.Abilities = Span{uint32(len(b.im.FaceSAs)), uint32(len(f.Abilities))}
			for _, a := range f.Abilities {
				b.im.FaceSAs = append(b.im.FaceSAs, b.node(a))
			}
		}
		if len(f.Triggers) > 0 {
			fr.Triggers = Span{uint32(len(b.im.Trigs)), uint32(len(f.Triggers))}
			for _, t := range f.Triggers {
				b.im.Trigs = append(b.im.Trigs, TrigRec{b.str(t.Mode), b.params(t.Params), b.node(t.Effect)})
			}
		}
		if len(f.Statics) > 0 {
			fr.Statics = Span{uint32(len(b.im.Stats)), uint32(len(f.Statics))}
			for _, s := range f.Statics {
				b.im.Stats = append(b.im.Stats, StaticRec{b.str(s.Mode), b.params(s.Params)})
			}
		}
		if len(f.Repls) > 0 {
			fr.Repls = Span{uint32(len(b.im.Repls)), uint32(len(f.Repls))}
			for _, r := range f.Repls {
				b.im.Repls = append(b.im.Repls, ReplRec{b.str(r.Event), b.params(r.Params), b.node(r.With)})
			}
		}
		if f.SVars != nil {
			keys := make([]string, 0, len(f.SVars))
			for k := range f.SVars {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if len(keys) == 0 {
				fr.SVars = Span{Start: ^uint32(0)}
			} else {
				fr.SVars = Span{uint32(len(b.im.SVars)), uint32(len(keys))}
			}
			for _, k := range keys {
				b.im.SVars = append(b.im.SVars, SVarRow{b.str(k), b.str(f.SVars[k])})
			}
		}
		b.im.Faces = append(b.im.Faces, fr)
	}
	return r
}

func buildImage(cards []*Card, tokens map[string]*Card) (*Image, error) {
	im := &Image{Hdr: ImageHeader{Schema: imageSchema, CacheVersion: cacheVersion}}
	copy(im.Hdr.Magic[:], "gorgeimg")
	fingerprint, _ := hex.DecodeString(CompilerFingerprint)
	copy(im.Hdr.Fingerprint[:], fingerprint)
	b := imageBuilder{im: im, ids: map[string]StringID{}}
	im.Cards = make([]CardRec, len(cards))
	for i, c := range cards {
		im.Cards[i] = b.card(c)
	}
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	im.Tokens = make([]TokenRec, 0, len(keys))
	for _, k := range keys {
		im.Tokens = append(im.Tokens, TokenRec{b.str(k), b.card(tokens[k])})
	}
	// The name index is Lookup's: nameIndexOf's tiered first-wins index
	// (native fronts before every other face), recorded as ordinals.
	ordOf := make(map[*Card]uint32, len(cards))
	for i, c := range cards {
		if _, dup := ordOf[c]; !dup {
			ordOf[c] = uint32(i)
		}
	}
	first := map[string]uint32{}
	for k, c := range nameIndexOf(cards) {
		first[k] = ordOf[c]
	}
	nk := make([]string, 0, len(first))
	for k := range first {
		nk = append(nk, k)
	}
	sort.Strings(nk)
	for _, k := range nk {
		id := b.str(k)
		ord := first[k]
		im.NameKeys = append(im.NameKeys, id)
		im.NameOrds = append(im.NameOrds, ord)
	}
	fronts := map[string]uint32{}
	for i, c := range cards {
		if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
			continue
		}
		name := c.Faces[0].Name
		if _, ok := fronts[name]; !ok {
			fronts[name] = uint32(i)
		}
	}
	frontNames := make([]string, 0, len(fronts))
	for name := range fronts {
		frontNames = append(frontNames, name)
	}
	sort.Strings(frontNames)
	for _, name := range frontNames {
		im.FirstOrd = append(im.FirstOrd, NameOrd{b.str(name), fronts[name]})
	}
	// CorpusHash is the corpus's canonical identity: the SHA-256 of the
	// canonical gob bytes the persisted cache is written from, NOT a second
	// gob walk over the pointer-free image rows. cache_canon.go's
	// GobEncode/GobDecode pairs are the one encoder for the IR cache, so the
	// image cannot drift from it: any field or map-order rule that changes the
	// persisted bytes changes this digest too. im.CanonicalBytes stays the
	// pointer-free oracle (image_test.go compares it across builds).
	canon, err := canonicalCorpusGob(cards, tokens)
	if err != nil {
		return nil, err
	}
	im.Hdr.CorpusHash = sha256.Sum256(canon)
	return im, nil
}

// canonicalCorpusGob returns the deterministic gob encoding of the corpus in
// its persisted cache shape (cacheFile). It is the ONE canonical corpus byte
// form: saveGob writes exactly these bytes (gzipped) and buildImage derives
// Image.Hdr.CorpusHash from them, so the cache and the image share one
// encoder. cacheFile.GobEncode sorts its Tokens map and each Card's
// Params/SVars maps (cache_canon.go).
func canonicalCorpusGob(cards []*Card, tokens map[string]*Card) ([]byte, error) {
	cf := cacheFile{Version: cacheVersion, Cards: cards, Tokens: tokens}
	var buf bytes.Buffer
	// &cf: cacheFile's GobEncode is on the pointer receiver, and gob refuses
	// an unaddressable top-level value that only *T encodes (see saveGob).
	if err := gob.NewEncoder(&buf).Encode(&cf); err != nil {
		return nil, fmt.Errorf("encode corpus: %w", err)
	}
	return buf.Bytes(), nil
}

// CanonicalBytes encodes the ordered, pointer-free row representation.
func (im *Image) CanonicalBytes() ([]byte, error) {
	var out bytes.Buffer
	if err := gob.NewEncoder(&out).Encode(struct {
		Hdr      ImageHeader
		Blob     []byte
		Strs     []StringRef
		Cards    []CardRec
		Tokens   []TokenRec
		Faces    []FaceRec
		FaceSAs  []NodeID
		Nodes    []SARec
		Trigs    []TrigRec
		Stats    []StaticRec
		Repls    []ReplRec
		Params   []ParamRow
		SVars    []SVarRow
		Words    []StringID
		NameKeys []StringID
		NameOrds []uint32
		FirstOrd []NameOrd
	}{im.Hdr, im.Blob, im.Strs, im.Cards, im.Tokens, im.Faces, im.FaceSAs, im.Nodes, im.Trigs, im.Stats, im.Repls, im.Params, im.SVars, im.Words, im.NameKeys, im.NameOrds, im.FirstOrd}); err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}
	return out.Bytes(), nil
}
