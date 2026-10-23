package domain

import (
	"fmt"
	"math"
)

// RGB is an sRGB color with eight bits per channel.
type RGB struct{ R, G, B uint8 }

// ParseHexColor reads #rrggbb, in either case; nothing else is a color here.
func ParseHexColor(value string) (RGB, bool) {
	if len(value) != 7 || value[0] != '#' {
		return RGB{}, false
	}
	var channels [3]uint8
	for i := range channels {
		high, okHigh := hexDigit(value[1+2*i])
		low, okLow := hexDigit(value[2+2*i])
		if !okHigh || !okLow {
			return RGB{}, false
		}
		channels[i] = high<<4 | low
	}
	return RGB{channels[0], channels[1], channels[2]}, true
}

func hexDigit(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// Hex writes the color as lowercase #rrggbb.
func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// srgbToLinear is the WCAG 2 channel transfer (the 0.04045 threshold of IEC 61966-2-1).
func srgbToLinear(channel uint8) float64 {
	v := float64(channel) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func linearToSRGB(v float64) uint8 {
	v = math.Max(0, math.Min(1, v))
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
}

// RelativeLuminance is the WCAG 2 relative luminance of a color, from 0 (black) to 1 (white).
func RelativeLuminance(c RGB) float64 {
	return 0.2126*srgbToLinear(c.R) + 0.7152*srgbToLinear(c.G) + 0.0722*srgbToLinear(c.B)
}

// ContrastRatio is the WCAG 2 contrast ratio of two colors, from 1 to 21, in either order.
func ContrastRatio(a, b RGB) float64 {
	la, lb := RelativeLuminance(a), RelativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// ContrastPasses compares the exact ratio, never a rounded one: 4.497:1 fails 4.5:1.
func ContrastPasses(a, b RGB, required float64) bool { return ContrastRatio(a, b) >= required }

// oklab is a color in Björn Ottosson's OKLab space: perceptual lightness L and two opponent axes.
type oklab struct{ L, A, B float64 }

func toOKLab(c RGB) oklab {
	r, g, b := srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return oklab{
		L: 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		A: 1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		B: 0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

// toRGB converts back, clipping each channel into sRGB.
func (c oklab) toRGB() RGB {
	l := c.L + 0.3963377774*c.A + 0.2158037573*c.B
	m := c.L - 0.1055613458*c.A - 0.0638541728*c.B
	s := c.L - 0.0894841775*c.A - 1.2914855480*c.B
	l, m, s = l*l*l, m*m*m, s*s*s
	return RGB{
		R: linearToSRGB(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		G: linearToSRGB(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		B: linearToSRGB(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s),
	}
}

// lightnessStep is how far apart the candidates of NearestPassingColor are in OKLab lightness.
const lightnessStep = 0.002

// NearestPassingColor keeps the hue and chroma of c and moves its OKLab lightness, a step at a time in
// both directions, to the first color passes accepts. It reports false when no lightness does.
func NearestPassingColor(c RGB, passes func(RGB) bool) (RGB, bool) {
	if passes(c) {
		return c, true
	}
	start := toOKLab(c)
	for distance := lightnessStep; distance <= 1+lightnessStep; distance += lightnessStep {
		for _, lightness := range []float64{start.L - distance, start.L + distance} {
			if lightness < 0 || lightness > 1 {
				continue
			}
			candidate := oklab{L: lightness, A: start.A, B: start.B}.toRGB()
			if passes(candidate) {
				return candidate, true
			}
		}
	}
	// Strong chroma can clip before the ends; black and white are the last resort.
	for _, candidate := range []RGB{{}, {255, 255, 255}} {
		if passes(candidate) {
			return candidate, true
		}
	}
	return RGB{}, false
}
