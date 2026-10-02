// Package mzbridge is the bridge between gorge and the upstream MageZero /
// DraftZero PyTorch training stack: the feature-id hash and namespace tree
// MageZero's Java encoder uses, the action/target vocabulary indexing, the
// training-shard writer and the inference client.
//
// Everything here is a port of a contract owned by upstream code, pinned at
// MageZero 521a8bd, the XMage fork 48e4918413 and draft-zero b1e0ba6. The
// golden vectors in testdata/ were produced by running the unmodified upstream
// Features.java under a real JVM, so a failing vector test means this package
// has drifted from upstream, never the other way round.
//
// Standard library only, no cgo.
package mzbridge
