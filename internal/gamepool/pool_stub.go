//go:build !gamepool

// Package gamepool's default-build stub. The real runner is behind -tags
// gamepool; this file keeps the package importable (and the experimental flag
// checkable) in a default build without pulling the scheduler, the seat
// wrapper, or their dependencies into the 32-bit and vet paths.
package gamepool

import (
	"errors"
	"time"
)

// ErrDisabled is returned by New in a build without -tags gamepool.
var ErrDisabled = errors.New("gamepool: disabled (build with -tags gamepool)")

// Enabled reports whether this build carries the real runner.
const Enabled = false

// Request, Scorer and Options mirror the tagged build's types so a caller can
// compile against either build. They carry no behaviour here.
type Request struct{}

type Scorer interface {
	Serve(batch []*Request) error
	Name() string
}

type Options struct {
	Workers    int
	MaxBatch   int
	FlushEvery time.Duration
	OnServe    func(width int)
}

type Stats struct{}

func (Stats) String() string { return "gamepool: disabled" }

type Pool struct{}

// New always reports ErrDisabled in this build.
func New(int, Scorer, Options) (*Pool, error) { return nil, ErrDisabled }
