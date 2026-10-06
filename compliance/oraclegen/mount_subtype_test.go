package oraclegen

import "testing"

// TestRegistryServesMountSubtype pins that registrySubtype serves Mount like
// any other subtype: it returns a real corpus card whose face carries Mount,
// so Mount target slots are filled rather than refused. The corpus has no
// quiet Mount (every Mount creature carries a trigger or static), so
// registryQuietSubtype finds none and subtypeBattlefield falls back here.
func TestRegistryServesMountSubtype(t *testing.T) {
	reg := censusRegistry(t)
	if q, ok := registryQuietSubtype(reg, "Mount"); ok {
		t.Logf("registryQuietSubtype(Mount) = %s", q)
	}
	name, ok := registrySubtype(reg, "Mount")
	if !ok {
		t.Fatal("registrySubtype(Mount) found no card")
	}
	card, found := reg.Lookup(name)
	if !found || card == nil {
		t.Fatalf("registrySubtype(Mount) = %q, which does not resolve in the registry", name)
	}
	hasMount := false
	for _, f := range card.Faces {
		hasMount = hasMount || faceHasSubtype(f, "Mount")
	}
	if !hasMount {
		t.Errorf("registrySubtype(Mount) = %q, which has no Mount face", name)
	}
	t.Logf("registrySubtype(Mount) = %s", name)
}
