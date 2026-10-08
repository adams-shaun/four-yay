//go:build !broker

// Package broker's default-build stub. The real scheduler is behind -tags
// broker; this file keeps the package importable (and the experimental flag
// checkable) in a default build without pulling the scheduler, the seat
// wrapper, or their dependencies into the 32-bit and vet paths.
package broker

import (
	"errors"
	"time"
)

// ErrDisabled is returned by New in a build without -tags broker.
var ErrDisabled = errors.New("broker: disabled (build with -tags broker)")

// Enabled reports whether this build carries the real scheduler. A driver
// checks it once and falls back to running without batching.
const Enabled = false

// Request, Scorer and Options mirror the tagged build's types so a caller can
// compile against either build. They carry no behaviour here.
type Request struct{}

type Scorer interface {
	Serve(batch []*Request) error
	Name() string
}

type Options struct {
	MaxBatch int
	Wait     time.Duration
	OnServe  func(width int)
}

type Stats struct {
	Served, States, WidthSum, WidthMax, MaxBatch, ServeNS, QueueNS int64
	Buckets                                                        [9]int64
}

func (Stats) MeanWidth() float64       { return 0 }
func (Stats) MeanServeMicros() float64 { return 0 }
func (Stats) String() string           { return "broker: disabled" }

type Broker struct{}

// New always reports ErrDisabled in this build.
func New(Options, Scorer) (*Broker, error) { return nil, ErrDisabled }

func (*Broker) Stop() Stats { return Stats{} }
