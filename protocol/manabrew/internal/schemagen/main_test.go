package main

import (
	"os"
	"testing"
)

// TestSchemaGenIsFresh fails when protocol/manabrew/schema_gen.go no longer
// matches the package source: run `go generate ./protocol/manabrew`.
func TestSchemaGenIsFresh(t *testing.T) {
	want, err := generate("../..")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../" + genFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("protocol/manabrew/schema_gen.go is stale: run go generate ./protocol/manabrew")
	}
}
