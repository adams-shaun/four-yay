//go:build gamepool && !gamepool_gpu

package main

import (
	"fmt"

	"github.com/adams-shaun/gorge/internal/gamepool"
)

// newScorer resolves -scorer for a build without the GPU tag.
func newScorer(name, _ string) (gamepool.Scorer, error) {
	switch name {
	case "", "local":
		return gamepool.LocalScorer{}, nil
	case "gpu":
		return nil, fmt.Errorf("gamepool: scorer %q needs a build with -tags gamepool_gpu", name)
	default:
		return nil, fmt.Errorf("gamepool: unknown scorer %q (want local or gpu)", name)
	}
}
