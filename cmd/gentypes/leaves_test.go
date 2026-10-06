package main

// leaves_test.go is type-checked from source by leafRoot, so it must stay
// import-free.

// The leaf synthetic structs deliberately carry NO struct-level doc comment -
// such a comment would become its own emitted block and shift the comment-block
// counts the leaves assert on. All explanatory text lives in the test bodies.

type gt1LeafOne struct {
	// Alpha is the first documented field; it must appear.
	Alpha int `json:"alpha"`
	// Beta is documented too, and carries omitempty.
	Beta  string `json:"beta,omitempty"`
	Gamma bool   `json:"gamma"` // a trailing comment is not a doc comment
}

type gt1LeafTwo struct {
	// Delta carries its own struct's field comment.
	Delta float64 `json:"delta"`
}

type gt1LeafPathological struct {
	// This doc contains a*/b terminator that must not end the block early.
	X int `json:"x"`
	Y int `json:"y"` // no doc comment: only X's block should be emitted
}
