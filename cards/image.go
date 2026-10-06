package cards

// Image is an immutable, pointer-free representation of the gob-shaped corpus.
type Image struct {
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
}
type ImageHeader struct {
	Magic                [8]byte
	Schema, CacheVersion uint32
	Fingerprint          [16]byte
	CorpusHash           [32]byte
}
type NodeID uint32
type CardRec struct {
	Path, AlternateMode StringID
	Faces               Span
	Flags               CardFlags
}
type TokenRec struct {
	Key  StringID
	Card CardRec
}
type FaceRec struct {
	Name, ManaCost, PT, Loyalty, Defense, Colors, Oracle StringID
	SpecializeColor, CopyFaceFrom                        StringID
	Types, Keywords, Aliases                             Span
	Abilities                                            Span
	Triggers, Statics, Repls, SVars                      Span
	TypeMask                                             TypeMask
	Nil                                                  uint32
}
type SARec struct {
	Kind, API, Line StringID
	Params          Span
	Sub             NodeID
}
type TrigRec struct {
	Mode   StringID
	Params Span
	Effect NodeID
}
type StaticRec struct {
	Mode   StringID
	Params Span
}
type ReplRec struct {
	Event  StringID
	Params Span
	With   NodeID
}
type NameOrd struct {
	Name StringID
	Ord  uint32
}

type CardFlags uint32

const (
	imageNamesACard CardFlags = 1 << iota
	imageChangesTypes
	imageSetsName
	imageMayCarryControlStatic
)
