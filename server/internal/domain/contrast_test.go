package domain

import (
	"fmt"
	"testing"
)

func mustColor(t *testing.T, hex string) RGB {
	t.Helper()
	color, ok := ParseHexColor(hex)
	if !ok {
		t.Fatalf("ParseHexColor(%q) refused a valid color", hex)
	}
	return color
}

// The expected ratios are the published WCAG 2 values (WebAIM contrast checker), to two decimals.
func TestContrastRatioMatchesPublishedWCAGValues(t *testing.T) {
	for _, item := range []struct {
		a, b string
		want string
	}{
		{"#767676", "#ffffff", "4.54"},
		{"#777777", "#ffffff", "4.48"},
		{"#000000", "#ffffff", "21.00"},
		{"#ffffff", "#000000", "21.00"},
		{"#ffffff", "#ffffff", "1.00"},
		{"#0000ff", "#ffffff", "8.59"},
		{"#ff0000", "#ffffff", "4.00"},
	} {
		got := fmt.Sprintf("%.2f", ContrastRatio(mustColor(t, item.a), mustColor(t, item.b)))
		if got != item.want {
			t.Errorf("ContrastRatio(%s, %s) = %s, want %s", item.a, item.b, got, item.want)
		}
	}
}

func TestContrastPassesOnlyAtOrAboveTheRequiredRatio(t *testing.T) {
	white := mustColor(t, "#ffffff")
	if !ContrastPasses(mustColor(t, "#767676"), white, 4.5) {
		t.Error("#767676 on white (4.54:1) failed AA")
	}
	if ContrastPasses(mustColor(t, "#777777"), white, 4.5) {
		t.Error("#777777 on white (4.48:1) passed AA")
	}
}

func TestParseHexColorTakesOnlySixDigitHex(t *testing.T) {
	for _, bad := range []string{"", "#fff", "767676", "#76767g", "#7676767", "rgb(1,2,3)", " #767676"} {
		if _, ok := ParseHexColor(bad); ok {
			t.Errorf("ParseHexColor(%q) accepted it", bad)
		}
	}
	if color := mustColor(t, "#A1B2C3"); color.Hex() != "#a1b2c3" {
		t.Errorf("Hex() = %s, want lowercase #a1b2c3", color.Hex())
	}
}

func TestNearestPassingColorKeepsTheHueAndPasses(t *testing.T) {
	white := mustColor(t, "#ffffff")
	start := mustColor(t, "#999999")
	got, ok := NearestPassingColor(start, func(candidate RGB) bool { return ContrastPasses(candidate, white, 4.5) })
	if !ok {
		t.Fatal("no passing color for gray on white")
	}
	if !ContrastPasses(got, white, 4.5) {
		t.Fatalf("suggestion %s does not pass on white", got.Hex())
	}
	// Gray stays gray, and the nearest one is just past the line (#767676 is 4.54:1), not black.
	if got.R != got.G || got.G != got.B || got.R < 0x70 || got.R > 0x76 {
		t.Fatalf("suggestion %s is not the nearest passing gray", got.Hex())
	}
	if _, ok := NearestPassingColor(start, func(RGB) bool { return false }); ok {
		t.Fatal("a rule nothing meets still gave a color")
	}
}
