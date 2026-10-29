package qrsvg

import (
	"bytes"
	"strings"
	"testing"
)

// The error correction of the worked example of version 1-M ("HELLO WORLD" in alphanumeric mode): its
// data codewords give these ten error correction codewords.
func TestReedSolomonMatchesTheStandardsWorkedExample(t *testing.T) {
	data := []byte{32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17}
	want := []byte{196, 35, 39, 119, 235, 215, 231, 226, 93, 23}
	if got := rsRemainder(data, rsDivisor(10)); !bytes.Equal(got, want) {
		t.Fatalf("error correction = %v, want %v", got, want)
	}
}

// The format information of level M and mask 0 is 101010000010010 (the standard's table).
func TestFormatBitsOfLevelMMaskZero(t *testing.T) {
	q := newCode(1)
	q.drawFormat(0)
	var got strings.Builder
	for i := 0; i <= 5; i++ {
		got.WriteString(map[bool]string{true: "1", false: "0"}[q.modules[i][8]])
	}
	// bits 0..5 sit in column 8, rows 0..5, least significant first: 0x5412 reads 010010 there.
	if got.String() != "010010" {
		t.Fatalf("format bits 0-5 = %s", got.String())
	}
}

func TestEncodeSizesAndFinders(t *testing.T) {
	url := "otpauth://totp/Colliedesk:operator%40example.com?issuer=Colliedesk&secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	modules, err := Encode(url)
	if err != nil {
		t.Fatal(err)
	}
	size := len(modules)
	if (size-17)%4 != 0 || size < 21 || size > 57 {
		t.Fatalf("size %d", size)
	}
	for _, corner := range [][2]int{{0, 0}, {size - 7, 0}, {0, size - 7}} {
		for i := 0; i < 7; i++ {
			if !modules[corner[1]][corner[0]+i] || !modules[corner[1]+6][corner[0]+i] {
				t.Fatalf("finder at %v is not drawn", corner)
			}
		}
	}
	if _, err := Encode(strings.Repeat("x", 214)); err != ErrTooLong {
		t.Fatalf("214 bytes: %v", err)
	}
	svg, err := SVG(url, `Authenticator key for "ops"`)
	if err != nil || !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, `aria-label="Authenticator key for &quot;ops&quot;"`) {
		t.Fatalf("svg = %.120s, %v", svg, err)
	}
}
