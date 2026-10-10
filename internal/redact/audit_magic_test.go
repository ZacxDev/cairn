package redact

import (
	"bytes"
	"testing"
)

// TestAnASCIIMagicNeedsABinaryByte (review round 2, R4): `%PDF-`, `BZh`, `GIF8?a` and
// `RIFF…WEBP` are spellable, so text that merely STARTS with one is scanned (at 2ba3e5c it shipped
// unscanned); a real file of the format — which carries a non-text byte early — still ships
// byte-identical.
func TestAnASCIIMagicNeedsABinaryByte(t *testing.T) {
	r, _ := New(bytes.Repeat([]byte{7}, 32), nil)
	v := "Zq9" + rnd(201, alnum, 16)
	line := "\nDB_PASSWORD=" + v + "\n"
	for _, magic := range []string{"%PDF-1.7", "BZh91AY", "GIF89a", "RIFF0000WEBPVP8 "} {
		text := []byte(magic + " text" + line)
		if out, _ := r.Blob("x.txt", text); bytes.Contains(out, []byte(v)) {
			t.Errorf("%q followed by text skipped scanning", magic)
		}
		real := append([]byte(magic+"\x00\x01\xe2\xe3"), []byte(line)...)
		if out, _ := r.Blob("x.bin", real); !bytes.Equal(out, real) {
			t.Errorf("a real %q file (a binary byte early) was altered", magic)
		}
	}
}
