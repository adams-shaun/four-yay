package cards

import "testing"

func TestGrantedSaddleAbilitySynthesis(t *testing.T) {
	c, diags := ParseBytes("test.txt", []byte("Name:Test Mount\nTypes:Creature Mount\nK:Saddle:2\n"))
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics: %+v", diags)
	}
	c.Faces[0].expandKeywords()
	got := GrantedSaddleAbility("Saddle:2")
	if len(c.Faces[0].Abilities) != 1 {
		t.Fatalf("printed abilities = %d, want 1", len(c.Faces[0].Abilities))
	}
	want := c.Faces[0].Abilities[0]
	if got == nil || got.API != want.API || !equalParams(got.Params, want.Params) {
		t.Fatalf("granted synthesis differs: got %#v, printed %#v", got, want)
	}
}

func TestGrantedCrewAbilitySynthesis(t *testing.T) {
	c, diags := ParseBytes("test.txt", []byte("Name:Test Vehicle\nTypes:Artifact Vehicle\nK:Crew:1\n"))
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics: %+v", diags)
	}
	c.Faces[0].expandKeywords()
	got := GrantedCrewAbility("Crew:1")
	if len(c.Faces[0].Abilities) != 1 {
		t.Fatalf("printed abilities = %d, want 1", len(c.Faces[0].Abilities))
	}
	want := c.Faces[0].Abilities[0]
	if got == nil || got.API != want.API || !equalParams(got.Params, want.Params) {
		t.Fatalf("granted synthesis differs: got %#v, printed %#v", got, want)
	}
}

func equalParams(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
