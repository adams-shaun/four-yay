//go:build enginebench_subset

package main

import "github.com/adams-shaun/gorge/cards"

// corpus_subset.go is the shim for builds that have cards.OpenCorpusFor (the
// wt/bbmem-corpus subset loader): the run opens only the workloads' cards,
// falling back to the full corpus when OpenCorpusFor says the pool needs it.
func init() {
	corpusMode = "subset"
	openCorpus = func(dir string, names []string) (*cards.Registry, error) {
		reg, err := cards.OpenCorpusFor(dir, names)
		if err == nil && !reg.IsSubset() {
			corpusMode = "subset-shim-fell-back-to-full"
		}
		return reg, err
	}
}
