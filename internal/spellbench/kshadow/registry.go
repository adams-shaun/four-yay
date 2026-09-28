package kshadow

import (
	"os"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// OpenRegistry opens a card registry: a saved registry file (a pool
// registry written by PoolRegistry + Save, *.gob.gz) or a corpus directory.
func OpenRegistry(path string) (*cards.Registry, error) {
	if st, err := os.Stat(path); err == nil && !st.IsDir() && strings.HasSuffix(path, ".gob.gz") {
		return cards.LoadRegistry(path)
	}
	return cards.SharedCorpus(path)
}
