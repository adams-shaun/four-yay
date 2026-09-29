package cards

import "testing"

func TestSaddleExpansion(t *testing.T) {
	c, diags := ParseBytes("test.txt", []byte("Name:Test Mount\nManaCost:2\nTypes:Creature Mount\nPT:2/2\nK:Saddle:3\n"))
	if len(diags) > 0 {
		t.Fatalf("parse diagnostics: %+v", diags)
	}
	c.Faces[0].expandKeywords()
	if len(c.Faces[0].Abilities) != 1 {
		t.Fatalf("Saddle expansion produced %d abilities, want 1", len(c.Faces[0].Abilities))
	}
	sa := c.Faces[0].Abilities[0]
	want := map[string]string{
		"Cost":    "tapXType<Any/Creature.Other+withTotalPowerGE3>",
		"Defined": "Self", "Attributes": "Saddled", "SorcerySpeed": "True", "Keyword": "Saddle",
	}
	if sa.API != "AlterAttribute" {
		t.Errorf("Saddle API = %q, want AlterAttribute", sa.API)
	}
	for k, v := range want {
		if got := sa.Params[k]; got != v {
			t.Errorf("Saddle ability %s = %q, want %q", k, got, v)
		}
	}
	if sa.Params["KeywordLine"] != "Saddle:3" {
		t.Errorf("KeywordLine = %q, want Saddle:3", sa.Params["KeywordLine"])
	}
}
