package tsgen

import "encoding/json"

// The fixture types are type-checked from THIS file's source by
// fixtureRoot, so it must import nothing but encoding/json.

type inner struct {
	N    int32            `json:"n"`
	Tags map[string]int32 `json:"tags,omitempty"`
	Pair [2]uint32        `json:"pair"`
	Raw  json.RawMessage  `json:"raw"`
	Skip string           `json:"-"`
}

type outer struct {
	ID       uint64  `json:"id"`
	Name     string  `json:"name,omitempty"`
	Flag     bool    `json:"flag"`
	Inner    inner   `json:"inner"`
	Inners   []inner `json:"inners"`
	MaybeN   *int32  `json:"maybe_n"`
	Kind     kindT   `json:"kind"`
	Bytes    []byte  `json:"bytes"`
	Any      any     `json:"any"`
	Optional *inner  `json:"optional,omitempty"`
}

type kindT string
