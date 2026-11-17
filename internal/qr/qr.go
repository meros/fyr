// Package qr makes QR codes (ISO/IEC 18004) for the sign-in and pairing
// links of decision 0030: byte mode, error correction level M, versions 1 to
// 6 (up to 106 bytes), the best of the eight masks. It exists because a link
// shown as text in a terminal, or as an image on a screen, is how a phone with
// no keyboard signs in. No dependency: the algorithm is Reed-Solomon over
// GF(256) and a fixed layout.
package qr

import (
	"errors"
	"strings"
)

// Matrix is a QR code: true is a dark module.
type Matrix [][]bool

// version holds the level M layout of one version: data codewords per block
// (group 1 then group 2), error-correction codewords per block, alignment
// pattern centres.
type version struct {
	ecPerBlock int
	blocks     []int // data codewords of each block
	align      []int
}

var versions = []version{
	{},
	{10, []int{16}, nil},
	{16, []int{28}, []int{6, 18}},
	{26, []int{44}, []int{6, 22}},
	{18, []int{32, 32}, []int{6, 26}},
	{24, []int{43, 43}, []int{6, 30}},
	{16, []int{27, 27, 27, 27}, []int{6, 34}},
}

// MaxBytes is the longest text Encode takes.
const MaxBytes = 106

// ErrTooLong: the text does not fit version 6.
var ErrTooLong = errors.New("qr: text too long")

func (v version) dataCodewords() int {
	n := 0
	for _, b := range v.blocks {
		n += b
	}
	return n
}

// Encode makes the QR code of text.
func Encode(text string) (Matrix, error) {
	data := []byte(text)
	ver := 0
	for i := 1; i < len(versions); i++ {
		// mode (4) + count (8) + data + terminator; all in bits
		if (4+8+8*len(data)+4+7)/8 <= versions[i].dataCodewords() {
			ver = i
			break
		}
	}
	if ver == 0 {
		return nil, ErrTooLong
	}
	v := versions[ver]
	cw := dataBits(data, v.dataCodewords())
	final := interleave(cw, v)
	size := 17 + 4*ver
	m := newBuilder(size, ver)
	m.function()
	m.place(final)
	best, bestScore := -1, 0
	for mask := 0; mask < 8; mask++ {
		t := m.clone()
		t.applyMask(mask)
		t.formatInfo(mask)
		if s := t.penalty(); best < 0 || s < bestScore {
			best, bestScore = mask, s
		}
	}
	m.applyMask(best)
	m.formatInfo(best)
	return m.cells, nil
}

// dataBits is the byte-mode bit stream, padded to n codewords.
func dataBits(data []byte, n int) []byte {
	var bits []byte
	put := func(v, w int) {
		for i := w - 1; i >= 0; i-- {
			bits = append(bits, byte(v>>i&1))
		}
	}
	put(0b0100, 4)
	put(len(data), 8)
	for _, b := range data {
		put(int(b), 8)
	}
	total := n * 8
	for t := 0; t < 4 && len(bits) < total; t++ {
		bits = append(bits, 0)
	}
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	out := make([]byte, 0, n)
	for i := 0; i < len(bits); i += 8 {
		var b byte
		for j := 0; j < 8; j++ {
			b = b<<1 | bits[i+j]
		}
		out = append(out, b)
	}
	for pad := []byte{0xEC, 0x11}; len(out) < n; {
		out = append(out, pad[0])
		pad[0], pad[1] = pad[1], pad[0]
	}
	return out
}

// interleave splits the data into blocks, adds each block's error-correction
// codewords and interleaves them.
func interleave(data []byte, v version) []byte {
	var blocks, ecs [][]byte
	pos := 0
	for _, n := range v.blocks {
		b := data[pos : pos+n]
		pos += n
		blocks = append(blocks, b)
		ecs = append(ecs, reedSolomon(b, v.ecPerBlock))
	}
	var out []byte
	maxLen := 0
	for _, b := range blocks {
		if len(b) > maxLen {
			maxLen = len(b)
		}
	}
	for i := 0; i < maxLen; i++ {
		for _, b := range blocks {
			if i < len(b) {
				out = append(out, b[i])
			}
		}
	}
	for i := 0; i < v.ecPerBlock; i++ {
		for _, e := range ecs {
			out = append(out, e[i])
		}
	}
	return out
}

var gfExp [512]byte
var gfLog [256]int

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

// reedSolomon is the n error-correction codewords of data.
func reedSolomon(data []byte, n int) []byte {
	gen := []byte{1}
	for i := 0; i < n; i++ { // gen *= (x - alpha^i)
		next := make([]byte, len(gen)+1)
		for j, c := range gen {
			next[j] ^= c
			next[j+1] ^= gfMul(c, gfExp[i])
		}
		gen = next
	}
	rem := make([]byte, n)
	for _, d := range data {
		f := d ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		for j := 0; j < n; j++ {
			rem[j] ^= gfMul(gen[j+1], f)
		}
	}
	return rem
}

type builder struct {
	n     int
	ver   int
	cells Matrix
	fn    [][]bool // function modules: not data, never masked
}

func newBuilder(n, ver int) *builder {
	b := &builder{n: n, ver: ver, cells: make(Matrix, n), fn: make([][]bool, n)}
	for i := range b.cells {
		b.cells[i] = make([]bool, n)
		b.fn[i] = make([]bool, n)
	}
	return b
}

func (b *builder) clone() *builder {
	c := newBuilder(b.n, b.ver)
	for i := range b.cells {
		copy(c.cells[i], b.cells[i])
		copy(c.fn[i], b.fn[i])
	}
	return c
}

func (b *builder) set(r, c int, dark, fn bool) {
	if r < 0 || c < 0 || r >= b.n || c >= b.n {
		return
	}
	b.cells[r][c] = dark
	if fn {
		b.fn[r][c] = true
	}
}

func (b *builder) function() {
	finder := func(r, c int) {
		for dr := -1; dr <= 7; dr++ {
			for dc := -1; dc <= 7; dc++ {
				dark := dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6 &&
					(dr == 0 || dr == 6 || dc == 0 || dc == 6 || (dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4))
				b.set(r+dr, c+dc, dark, true)
			}
		}
	}
	finder(0, 0)
	finder(0, b.n-7)
	finder(b.n-7, 0)
	for i := 8; i < b.n-8; i++ { // timing
		b.set(6, i, i%2 == 0, true)
		b.set(i, 6, i%2 == 0, true)
	}
	for _, r := range versions[b.ver].align {
		for _, c := range versions[b.ver].align {
			if b.fn[r][c] { // under a finder
				continue
			}
			for dr := -2; dr <= 2; dr++ {
				for dc := -2; dc <= 2; dc++ {
					b.set(r+dr, c+dc, max(abs(dr), abs(dc)) != 1, true)
				}
			}
		}
	}
	b.set(b.n-8, 8, true, true) // the dark module
	for i := 0; i < 9; i++ {    // reserve the format areas
		b.fn[8][i], b.fn[i][8] = true, true
	}
	for i := 0; i < 8; i++ {
		b.fn[8][b.n-1-i], b.fn[b.n-1-i][8] = true, true
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// place writes the codewords in the zigzag of the standard, skipping function
// modules.
func (b *builder) place(data []byte) {
	i := 0
	up := true
	for col := b.n - 1; col > 0; col -= 2 {
		if col == 6 {
			col--
		}
		for k := 0; k < b.n; k++ {
			r := k
			if up {
				r = b.n - 1 - k
			}
			for dc := 0; dc < 2; dc++ {
				c := col - dc
				if b.fn[r][c] {
					continue
				}
				dark := false
				if i < len(data)*8 {
					dark = data[i/8]>>(7-i%8)&1 == 1
				}
				b.cells[r][c] = dark
				i++
			}
		}
		up = !up
	}
}

func maskBit(mask, r, c int) bool {
	switch mask {
	case 0:
		return (r+c)%2 == 0
	case 1:
		return r%2 == 0
	case 2:
		return c%3 == 0
	case 3:
		return (r+c)%3 == 0
	case 4:
		return (r/2+c/3)%2 == 0
	case 5:
		return r*c%2+r*c%3 == 0
	case 6:
		return (r*c%2+r*c%3)%2 == 0
	}
	return ((r+c)%2+r*c%3)%2 == 0
}

func (b *builder) applyMask(mask int) {
	for r := 0; r < b.n; r++ {
		for c := 0; c < b.n; c++ {
			if !b.fn[r][c] && maskBit(mask, r, c) {
				b.cells[r][c] = !b.cells[r][c]
			}
		}
	}
}

// FormatBits is the 15-bit format information for level M and a mask: 5 data
// bits (level 00, mask) with 10 BCH bits, XORed with 0x5412.
func FormatBits(mask int) int {
	data := mask // level M is 00
	rem := data
	for i := 0; i < 10; i++ {
		rem = rem<<1 ^ (rem>>9)*0x537
	}
	return (data<<10 | rem) ^ 0x5412
}

func (b *builder) formatInfo(mask int) {
	f := FormatBits(mask)
	bit := func(i int) bool { return f>>i&1 == 1 }
	for i := 0; i <= 5; i++ {
		b.set(i, 8, bit(i), true)
	}
	b.set(7, 8, bit(6), true)
	b.set(8, 8, bit(7), true)
	b.set(8, 7, bit(8), true)
	for i := 9; i < 15; i++ {
		b.set(8, 14-i, bit(i), true)
	}
	for i := 0; i < 8; i++ {
		b.set(8, b.n-1-i, bit(i), true)
	}
	for i := 8; i < 15; i++ {
		b.set(b.n-15+i, 8, bit(i), true)
	}
	b.set(b.n-8, 8, true, true)
}

// penalty scores a masked matrix by the four rules of the standard.
func (b *builder) penalty() int {
	n := b.n
	score := 0
	for dir := 0; dir < 2; dir++ {
		at := func(i, j int) bool {
			if dir == 0 {
				return b.cells[i][j]
			}
			return b.cells[j][i]
		}
		for i := 0; i < n; i++ {
			run := 1
			for j := 1; j < n; j++ {
				if at(i, j) == at(i, j-1) {
					run++
					continue
				}
				if run >= 5 {
					score += run - 2
				}
				run = 1
			}
			if run >= 5 {
				score += run - 2
			}
			for j := 0; j+10 < n; j++ { // 1011101 with four light modules on a side
				p := [11]bool{true, false, true, true, true, false, true, false, false, false, false}
				q := [11]bool{false, false, false, false, true, false, true, true, true, false, true}
				mp, mq := true, true
				for k := 0; k < 11; k++ {
					mp = mp && at(i, j+k) == p[k]
					mq = mq && at(i, j+k) == q[k]
				}
				if mp {
					score += 40
				}
				if mq {
					score += 40
				}
			}
		}
	}
	for r := 0; r+1 < n; r++ {
		for c := 0; c+1 < n; c++ {
			v := b.cells[r][c]
			if v == b.cells[r][c+1] && v == b.cells[r+1][c] && v == b.cells[r+1][c+1] {
				score += 3
			}
		}
	}
	dark := 0
	for r := range b.cells {
		for _, v := range b.cells[r] {
			if v {
				dark++
			}
		}
	}
	pct := dark * 100 / (n * n)
	score += abs(pct-50) / 5 * 10
	return score
}

// Text draws the code for a terminal: two rows of modules per line with
// half-block characters, four light modules of margin, dark on light.
func (m Matrix) Text() string {
	const quiet = 4
	n := len(m)
	dark := func(r, c int) bool {
		r, c = r-quiet, c-quiet
		return r >= 0 && c >= 0 && r < n && c < n && m[r][c]
	}
	var sb strings.Builder
	for r := 0; r < n+2*quiet; r += 2 {
		for c := 0; c < n+2*quiet; c++ {
			top, bot := dark(r, c), dark(r+1, c)
			switch {
			case top && bot:
				sb.WriteString("█")
			case top:
				sb.WriteString("▀")
			case bot:
				sb.WriteString("▄")
			default:
				sb.WriteString(" ")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// SVG draws the code as an image, one path, dark on light with a margin.
func (m Matrix) SVG() string {
	const quiet = 4
	n := len(m) + 2*quiet
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	sb.WriteString(itoa(n) + " " + itoa(n))
	sb.WriteString(`" shape-rendering="crispEdges"><rect width="100%" height="100%" fill="#fff"/><path fill="#000" d="`)
	for r := range m {
		for c := range m[r] {
			if m[r][c] {
				sb.WriteString("M" + itoa(c+quiet) + " " + itoa(r+quiet) + "h1v1h-1z")
			}
		}
	}
	sb.WriteString(`"/></svg>`)
	return sb.String()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}
