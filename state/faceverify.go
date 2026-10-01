package state

import "testing"

// faceVerify makes every cached Object.Face() read re-derive the face and
// panic on a mismatch. It is on in every test binary (testing.Testing) so a
// Card/CopyFace/FaceIdx write that bypasses the setters fails loudly there,
// and off in production binaries, where the check is one predictable branch.
var faceVerify = testing.Testing()
