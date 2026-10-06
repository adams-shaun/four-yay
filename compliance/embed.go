package compliance

import (
	"embed"
	"encoding/json"
	"path"
)

// The declared-set and printed-card manifests are build inputs of this
// package. Embedding them makes that explicit: a test that reads them through
// these accessors is cache-correct under Go's test cache (its result depends
// on this package's compiled output, which changes when the manifests do),
// unlike one that opens the files off disk. The accessors return the same
// values LoadDeclared/LoadPrinted do, so a census keeps its exhaustive
// coverage and a manifest edit still moves its pins.
//
//go:embed declared.json
var embeddedDeclared []byte

//go:embed printed
var embeddedPrinted embed.FS

// EmbeddedDeclared parses the embedded compliance/declared.json.
func EmbeddedDeclared() (Declared, error) {
	var d Declared
	err := json.Unmarshal(embeddedDeclared, &d)
	return d, err
}

// EmbeddedPrinted parses the embedded compliance/printed/<code>.json for a
// declared set, or a zero Printed when the set has no printed manifest (the
// same shape LoadPrinted produces for a missing file).
func EmbeddedPrinted(code string) (Printed, error) {
	raw, err := embeddedPrinted.ReadFile(path.Join("printed", ManifestFileName(code)))
	if err != nil {
		return Printed{}, err
	}
	var p Printed
	err = json.Unmarshal(raw, &p)
	return p, err
}
