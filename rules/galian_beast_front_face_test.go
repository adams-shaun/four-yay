package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestGalianBeastReturnsFrontFace pins CR 712.8a/712.14: Vincent Valentine
// placed transformed and destroyed dies as Galian Beast, whose death trigger
// returns the card tapped "front face up" -- as Vincent Valentine 2/2, not as
// Galian Beast 3/2 (the back face a graveyard exit used to keep).
func TestGalianBeastReturnsFrontFace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Vincent Valentine")
	if !ok || len(c.Faces) < 2 || c.Faces[0].Name == c.Faces[1].Name {
		t.Fatalf("Vincent Valentine is not a two-named-face card in the corpus")
	}
	front, back := c.Faces[0], c.Faces[1]
	if front.PT == back.PT {
		t.Fatalf("both faces are %s; the test cannot tell them apart", front.PT)
	}

	sc := `{"name":"galian-front","setup":{"p0":{"battlefield":["Vincent Valentine"],"back_face":["Vincent Valentine"]},"p1":{"hand":["Murder"]}},` +
		`"steps":[{"op":"pass","seat":0},{"op":"cast","seat":1,"card":"p1:Murder","mana":"CBB","targets":["p0:` + back.Name + `"]},{"op":"resolve","seat":1},{"op":"resolve","seat":0}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Precondition: the permanent was on the back face before Murder.
	sawBack := false
	for _, s := range res.Snapshots {
		for _, p := range s.Permanents {
			if p.Controller == 0 && p.Name == back.Name {
				sawBack = true
			}
		}
	}
	if !sawBack {
		t.Fatalf("no snapshot shows %s on p0's battlefield; the setup never put it on the back face", back.Name)
	}

	last := res.Snapshots[len(res.Snapshots)-1]
	var got *OracleSnapPerm
	for i := range last.Permanents {
		p := &last.Permanents[i]
		if p.Controller == 0 && (p.Name == front.Name || p.Name == back.Name) {
			got = p
		}
	}
	if got == nil {
		t.Fatalf("the card is not on p0's battlefield after Murder; the death trigger did not return it: %+v", last.Permanents)
	}
	if got.Name != front.Name || got.PT != front.PT {
		t.Errorf("returned as %s %s, want %s %s (front face up)", got.Name, got.PT, front.Name, front.PT)
	}
	if !got.Tapped {
		t.Errorf("returned untapped, want tapped")
	}
}
