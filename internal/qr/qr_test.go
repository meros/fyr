package qr

import (
	"strings"
	"testing"
)

// The Reed-Solomon codewords of the standard's worked example: HELLO WORLD,
// version 1, level M (16 data codewords, 10 error-correction codewords).
func TestReedSolomonMatchesTheWorkedExample(t *testing.T) {
	data := []byte{32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17}
	want := []byte{196, 35, 39, 119, 235, 215, 231, 226, 93, 23}
	if got := reedSolomon(data, 10); string(got) != string(want) {
		t.Fatalf("EC codewords %v (want %v)", got, want)
	}
}

// Format information: level M with mask 0 is 101010000010010 in the standard.
func TestFormatBits(t *testing.T) {
	if got := FormatBits(0); got != 0b101010000010010 {
		t.Errorf("mask 0: %015b", got)
	}
	if got := FormatBits(5); got != 0b100000011001110 {
		t.Errorf("mask 5: %015b", got)
	}
}

func TestEncodeStructure(t *testing.T) {
	for _, s := range []string{"a", "http://fyr.localhost:7777/auth/once#abcdefghijklmnopqrstuv", strings.Repeat("x", MaxBytes)} {
		m, err := Encode(s)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		n := len(m)
		if (n-17)%4 != 0 {
			t.Fatalf("size %d", n)
		}
		// The three finder patterns and the timing pattern.
		for _, o := range [][2]int{{0, 0}, {0, n - 7}, {n - 7, 0}} {
			for i := 0; i < 7; i++ {
				if !m[o[0]][o[1]+i] || !m[o[0]+6][o[1]+i] || !m[o[0]+i][o[1]] || !m[o[0]+i][o[1]+6] {
					t.Fatalf("finder at %v is not drawn", o)
				}
			}
			if m[o[0]+1][o[1]+1] || !m[o[0]+3][o[1]+3] {
				t.Fatalf("finder at %v has the wrong core", o)
			}
		}
		for i := 8; i < n-8; i++ {
			if m[6][i] != (i%2 == 0) || m[i][6] != (i%2 == 0) {
				t.Fatalf("timing at %d", i)
			}
		}
		if !m[n-8][8] {
			t.Error("the dark module")
		}
	}
	if _, err := Encode(strings.Repeat("x", MaxBytes+1)); err != ErrTooLong {
		t.Errorf("too long: %v", err)
	}
	if m, _ := Encode("hello"); !strings.Contains(m.Text(), "█") || !strings.HasPrefix(m.SVG(), "<svg") {
		t.Error("renderers")
	}
}
