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
// The checks that need upstream's Python (MageZero's shard reader, its
// trainer and its inference server) are env-gated tests, skipped by default:
//
//	MZBRIDGE_PYTHON     <venv>/bin/python with torch, h5py, msgpack
//	MZBRIDGE_MAGEZERO   <MageZero>/src/magezero
//	MZBRIDGE_SYNTH_OUT  directory to write a synthetic training/testing shard pair into
//	MZBRIDGE_SERVER_URL, MZBRIDGE_RUN_ROOT, MZBRIDGE_DECK, MZ_ACTION_VOCAB
//	                    a running server.py and the run directory it serves (mzclient)
//
// The HTTP client is the sub-package mzclient, the only part that reads a
// clock. Standard library only, no cgo.
package mzbridge
