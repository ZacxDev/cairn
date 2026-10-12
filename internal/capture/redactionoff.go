package capture

// redactionOff is true ONLY in a binary built with the `cairn_capture_noredact` tag
// (`noredact.go`): the positive control of `tests/plugins/e2e.sh` clause (b), which must see every
// planted secret leave the host to prove its relay can see one at all. No release build sets the
// tag — `flake.nix` and the CI `go` job build without it — and such a build sends ONLY to a
// loopback address ([ValidateEndpoint]) and says so on every run.
var redactionOff = false

// RedactionOff reports whether this is the redaction-off TEST build.
func RedactionOff() bool { return redactionOff }
