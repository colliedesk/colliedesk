// Package qrsvg draws a QR code (ISO/IEC 18004, byte mode, error correction level M, versions 1 to 10) as
// SVG, for the authenticator key of the operator console: the console's first bundle carries no QR library,
// and the key is always shown as text beside it.
package qrsvg

import (
	"errors"
	"fmt"
	"strings"
)

// ErrTooLong means the text does not fit version 10 at level M (213 bytes).
var ErrTooLong = errors.New("text too long for a QR code")

// blocks per version at level M: error correction codewords per block, then (count, data codewords) groups.
var levelM = [11]struct {
	ec     int
	groups [][2]int
}{
	{}, {10, [][2]int{{1, 16}}}, {16, [][2]int{{1, 28}}}, {26, [][2]int{{1, 44}}}, {18, [][2]int{{2, 32}}}, {24, [][2]int{{2, 43}}},
	{16, [][2]int{{4, 27}}}, {18, [][2]int{{4, 31}}}, {22, [][2]int{{2, 38}, {2, 39}}}, {22, [][2]int{{3, 36}, {2, 37}}}, {26, [][2]int{{4, 43}, {1, 44}}},
}

var alignment = [11][]int{nil, nil, {6, 18}, {6, 22}, {6, 26}, {6, 30}, {6, 34}, {6, 22, 38}, {6, 24, 42}, {6, 26, 46}, {6, 28, 50}}

type code struct {
	size     int
	modules  [][]bool
	function [][]bool
}

func dataCapacity(version int) int {
	total := 0
	for _, group := range levelM[version].groups {
		total += group[0] * group[1]
	}
	return total
}

// Encode returns the module grid of text, dark modules true.
func Encode(text string) ([][]bool, error) {
	data := []byte(text)
	version := 0
	for v := 1; v <= 10; v++ {
		countBits := 8
		if v >= 10 {
			countBits = 16
		}
		if 4+countBits+8*len(data) <= dataCapacity(v)*8 {
			version = v
			break
		}
	}
	if version == 0 {
		return nil, ErrTooLong
	}
	countBits := 8
	if version >= 10 {
		countBits = 16
	}
	var bits bitBuffer
	bits.append(0b0100, 4)
	bits.append(len(data), countBits)
	for _, b := range data {
		bits.append(int(b), 8)
	}
	capacity := dataCapacity(version) * 8
	terminator := min(4, capacity-len(bits))
	bits.append(0, terminator)
	bits.append(0, (8-len(bits)%8)%8)
	for pad := 0xEC; len(bits) < capacity; pad ^= 0xEC ^ 0x11 {
		bits.append(pad, 8)
	}
	codewords := make([]byte, len(bits)/8)
	for i, bit := range bits {
		if bit {
			codewords[i>>3] |= 1 << (7 - i&7)
		}
	}
	q := newCode(version)
	q.drawCodewords(interleave(version, codewords))
	best, bestPenalty := -1, 0
	for mask := 0; mask < 8; mask++ {
		q.applyMask(mask)
		q.drawFormat(mask)
		if penalty := q.penalty(); best < 0 || penalty < bestPenalty {
			best, bestPenalty = mask, penalty
		}
		q.applyMask(mask)
	}
	q.applyMask(best)
	q.drawFormat(best)
	return q.modules, nil
}

// SVG draws text as an SVG image with a quiet zone of four modules; accessibleName labels it.
func SVG(text, accessibleName string) (string, error) {
	modules, err := Encode(text)
	if err != nil {
		return "", err
	}
	size := len(modules) + 8
	var path strings.Builder
	for y, row := range modules {
		for x, dark := range row {
			if dark {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x+4, y+4)
			}
		}
	}
	name := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(accessibleName)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" role="img" aria-label="%s" shape-rendering="crispEdges">`+
		`<rect width="%d" height="%d" fill="#fff"/><path d="%s" fill="#000"/></svg>`, size, size, name, size, size, path.String()), nil
}

type bitBuffer []bool

func (b *bitBuffer) append(value, length int) {
	for i := length - 1; i >= 0; i-- {
		*b = append(*b, (value>>i)&1 == 1)
	}
}

func interleave(version int, data []byte) []byte {
	ec := levelM[version].ec
	divisor := rsDivisor(ec)
	var dataBlocks, ecBlocks [][]byte
	offset := 0
	for _, group := range levelM[version].groups {
		for i := 0; i < group[0]; i++ {
			block := data[offset : offset+group[1]]
			offset += group[1]
			dataBlocks = append(dataBlocks, block)
			ecBlocks = append(ecBlocks, rsRemainder(block, divisor))
		}
	}
	var out []byte
	for i := 0; ; i++ {
		added := false
		for _, block := range dataBlocks {
			if i < len(block) {
				out = append(out, block[i])
				added = true
			}
		}
		if !added {
			break
		}
	}
	for i := 0; i < ec; i++ {
		for _, block := range ecBlocks {
			out = append(out, block[i])
		}
	}
	return out
}

func gfMultiply(x, y byte) byte {
	var z int
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x11D)
		z ^= int((y>>i)&1) * int(x)
	}
	return byte(z)
}

func rsDivisor(degree int) []byte {
	result := make([]byte, degree)
	result[degree-1] = 1
	root := byte(1)
	for i := 0; i < degree; i++ {
		for j := range result {
			result[j] = gfMultiply(result[j], root)
			if j+1 < len(result) {
				result[j] ^= result[j+1]
			}
		}
		root = gfMultiply(root, 0x02)
	}
	return result
}

func rsRemainder(data, divisor []byte) []byte {
	result := make([]byte, len(divisor))
	for _, b := range data {
		factor := b ^ result[0]
		copy(result, result[1:])
		result[len(result)-1] = 0
		for i := range result {
			result[i] ^= gfMultiply(divisor[i], factor)
		}
	}
	return result
}

func newCode(version int) *code {
	size := 17 + 4*version
	q := &code{size: size, modules: make([][]bool, size), function: make([][]bool, size)}
	for i := range q.modules {
		q.modules[i] = make([]bool, size)
		q.function[i] = make([]bool, size)
	}
	for i := 0; i < size; i++ {
		q.set(6, i, i%2 == 0)
		q.set(i, 6, i%2 == 0)
	}
	for _, corner := range [][2]int{{3, 3}, {size - 4, 3}, {3, size - 4}} {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := corner[0]+dx, corner[1]+dy
				if x >= 0 && x < size && y >= 0 && y < size {
					distance := max(abs(dx), abs(dy))
					q.set(x, y, distance != 2 && distance != 4)
				}
			}
		}
	}
	positions := alignment[version]
	last := len(positions) - 1
	for i, cx := range positions {
		for j, cy := range positions {
			if (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					q.set(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	q.drawFormat(0) // reserves the format modules
	if version >= 7 {
		rem := version
		for i := 0; i < 12; i++ {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
		}
		bits := version<<12 | rem
		for i := 0; i < 18; i++ {
			bit := (bits>>i)&1 == 1
			a, b := size-11+i%3, i/3
			q.set(a, b, bit)
			q.set(b, a, bit)
		}
	}
	return q
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// set marks a function module (not data) at column x, row y.
func (q *code) set(x, y int, dark bool) {
	q.modules[y][x] = dark
	q.function[y][x] = true
}

func (q *code) drawFormat(mask int) {
	data := 0<<3 | mask // level M is 00
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return (bits>>i)&1 == 1 }
	for i := 0; i <= 5; i++ {
		q.set(8, i, bit(i))
	}
	q.set(8, 7, bit(6))
	q.set(8, 8, bit(7))
	q.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		q.set(14-i, 8, bit(i))
	}
	for i := 0; i < 8; i++ {
		q.set(q.size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		q.set(8, q.size-15+i, bit(i))
	}
	q.set(8, q.size-8, true)
}

func (q *code) drawCodewords(data []byte) {
	i := 0
	for right := q.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < q.size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				y := vert
				if (right+1)&2 == 0 {
					y = q.size - 1 - vert
				}
				if !q.function[y][x] && i < len(data)*8 {
					q.modules[y][x] = (data[i>>3]>>(7-i&7))&1 == 1
					i++
				}
			}
		}
	}
}

func (q *code) applyMask(mask int) {
	for y := 0; y < q.size; y++ {
		for x := 0; x < q.size; x++ {
			var invert bool
			switch mask {
			case 0:
				invert = (x+y)%2 == 0
			case 1:
				invert = y%2 == 0
			case 2:
				invert = x%3 == 0
			case 3:
				invert = (x+y)%3 == 0
			case 4:
				invert = (x/3+y/2)%2 == 0
			case 5:
				invert = x*y%2+x*y%3 == 0
			case 6:
				invert = (x*y%2+x*y%3)%2 == 0
			case 7:
				invert = ((x+y)%2+x*y%3)%2 == 0
			}
			if invert && !q.function[y][x] {
				q.modules[y][x] = !q.modules[y][x]
			}
		}
	}
}

// penalty scores runs, 2x2 blocks and the balance of dark modules (ISO/IEC 18004 rules 1, 2 and 4); any
// mask makes a valid code, the score only picks an easy one to read.
func (q *code) penalty() int {
	score := 0
	for i := 0; i < q.size; i++ {
		for _, horizontal := range []bool{true, false} {
			run, last := 0, false
			for j := 0; j < q.size; j++ {
				dark := q.modules[i][j]
				if !horizontal {
					dark = q.modules[j][i]
				}
				if j > 0 && dark == last {
					run++
				} else {
					if run >= 5 {
						score += run - 2
					}
					run, last = 1, dark
				}
			}
			if run >= 5 {
				score += run - 2
			}
		}
	}
	dark := 0
	for y := 0; y < q.size; y++ {
		for x := 0; x < q.size; x++ {
			if q.modules[y][x] {
				dark++
			}
			if x+1 < q.size && y+1 < q.size {
				c := q.modules[y][x]
				if c == q.modules[y][x+1] && c == q.modules[y+1][x] && c == q.modules[y+1][x+1] {
					score += 3
				}
			}
		}
	}
	total := q.size * q.size
	deviation := abs(dark*20-total*10) / total
	return score + deviation*10
}
