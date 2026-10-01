package state

import (
	"os"
	"testing"
)

// faceVerify makes every cached Object.Face() read re-derive the face and
// panic on a mismatch. It is on in every test binary (testing.Testing) so a
// Card/CopyFace/FaceIdx write that bypasses the setters fails loudly there,
// and off in production binaries, where the check is one predictable branch.
// GORGE_FACE_VERIFY=0 turns it off in a test binary, for a benchmark that
// must time the production read.
var faceVerify = testing.Testing() && os.Getenv("GORGE_FACE_VERIFY") != "0"
