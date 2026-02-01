// seehuhn.de/go/icc - read and write ICC profiles
// Copyright (C) 2026  Jochen Voss <voss@seehuhn.de>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package icc

import (
	"math"
	"testing"
)

func TestCurveGamma(t *testing.T) {
	tests := []struct {
		gamma float64
		input float64
		want  float64
	}{
		{1.0, 0.5, 0.5},
		{2.0, 0.5, 0.25},
		{2.2, 0.5, 0.2176},
		{2.2, 0.0, 0.0},
		{2.2, 1.0, 1.0},
	}

	for _, tt := range tests {
		c := &Curve{Gamma: tt.gamma}
		got := c.Evaluate(tt.input)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("Gamma %.1f: Evaluate(%.2f) = %.4f, want %.4f",
				tt.gamma, tt.input, got, tt.want)
		}
	}
}

func TestCurveGammaInvert(t *testing.T) {
	gammas := []float64{1.0, 1.8, 2.2, 2.4}
	inputs := []float64{0.0, 0.1, 0.25, 0.5, 0.75, 0.9, 1.0}

	for _, gamma := range gammas {
		c := &Curve{Gamma: gamma}
		for _, x := range inputs {
			y := c.Evaluate(x)
			xBack := c.Invert(y)
			if math.Abs(xBack-x) > 1e-6 {
				t.Errorf("Gamma %.1f: round-trip failed: %f -> %f -> %f",
					gamma, x, y, xBack)
			}
		}
	}
}

func TestCurveParametricType0(t *testing.T) {
	// type 0: y = x^g (same as gamma curve)
	c := &Curve{
		FuncType: 0,
		Params:   []float64{2.2},
	}

	got := c.Evaluate(0.5)
	want := math.Pow(0.5, 2.2)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("type 0: Evaluate(0.5) = %f, want %f", got, want)
	}

	// round-trip
	xBack := c.Invert(got)
	if math.Abs(xBack-0.5) > 1e-6 {
		t.Errorf("type 0: round-trip failed: 0.5 -> %f -> %f", got, xBack)
	}
}

func TestCurveParametricType3(t *testing.T) {
	// type 3: sRGB-like curve
	// y = (ax+b)^g for x >= d, else y = cx
	// sRGB: g=2.4, a=1/1.055, b=0.055/1.055, c=1/12.92, d=0.04045
	g := 2.4
	a := 1.0 / 1.055
	b := 0.055 / 1.055
	cc := 1.0 / 12.92
	d := 0.04045

	c := &Curve{
		FuncType: 3,
		Params:   []float64{g, a, b, cc, d},
	}

	tests := []float64{0.0, 0.01, 0.04, 0.04045, 0.05, 0.1, 0.5, 1.0}
	for _, x := range tests {
		y := c.Evaluate(x)
		xBack := c.Invert(y)
		if math.Abs(xBack-x) > 1e-5 {
			t.Errorf("type 3 sRGB: round-trip failed: %f -> %f -> %f", x, y, xBack)
		}
	}
}

func TestCurveSampled(t *testing.T) {
	// linear curve with 256 entries
	table := make([]uint16, 256)
	for i := range table {
		table[i] = uint16(i) << 8
	}
	c := &Curve{Table: table}

	tests := []float64{0.0, 0.25, 0.5, 0.75, 1.0}
	for _, x := range tests {
		y := c.Evaluate(x)
		if math.Abs(y-x) > 0.01 {
			t.Errorf("sampled linear: Evaluate(%f) = %f, want %f", x, y, x)
		}
	}
}

func TestCurveSampledInvert(t *testing.T) {
	// gamma 2.2 curve with 256 entries
	table := make([]uint16, 256)
	for i := range table {
		x := float64(i) / 255.0
		y := math.Pow(x, 2.2)
		table[i] = uint16(y * 65535)
	}
	c := &Curve{Table: table}

	inputs := []float64{0.0, 0.1, 0.25, 0.5, 0.75, 0.9, 1.0}
	for _, x := range inputs {
		y := c.Evaluate(x)
		xBack := c.Invert(y)
		// sampled curve inversion is less precise
		if math.Abs(xBack-x) > 0.01 {
			t.Errorf("sampled gamma: round-trip failed: %f -> %f -> %f", x, y, xBack)
		}
	}
}

func TestCurveIdentity(t *testing.T) {
	tests := []struct {
		name    string
		curve   *Curve
		isIdent bool
	}{
		{"gamma 1.0", &Curve{Gamma: 1.0}, true},
		{"gamma 2.2", &Curve{Gamma: 2.2}, false},
		{"type 0 gamma 1.0", &Curve{FuncType: 0, Params: []float64{1.0}}, true},
		{"type 0 gamma 2.2", &Curve{FuncType: 0, Params: []float64{2.2}}, false},
	}

	for _, tt := range tests {
		got := tt.curve.IsIdentity()
		if got != tt.isIdent {
			t.Errorf("%s: IsIdentity() = %v, want %v", tt.name, got, tt.isIdent)
		}
	}
}

func TestTetrahedralInterp3D(t *testing.T) {
	// simple 2x2x2 identity CLUT (output = input)
	gridSize := 2
	outChannels := 3
	clut := make([]float64, gridSize*gridSize*gridSize*outChannels)

	// fill CLUT with identity mapping
	for r := 0; r < gridSize; r++ {
		for g := 0; g < gridSize; g++ {
			for b := 0; b < gridSize; b++ {
				idx := (r*gridSize*gridSize + g*gridSize + b) * outChannels
				clut[idx+0] = float64(r) / float64(gridSize-1)
				clut[idx+1] = float64(g) / float64(gridSize-1)
				clut[idx+2] = float64(b) / float64(gridSize-1)
			}
		}
	}

	tests := [][3]float64{
		{0, 0, 0},
		{1, 1, 1},
		{0.5, 0.5, 0.5},
		{0.25, 0.75, 0.5},
	}

	for _, tt := range tests {
		result := tetrahedralInterp3D(clut, gridSize, outChannels, tt[0], tt[1], tt[2])
		for i := 0; i < 3; i++ {
			if math.Abs(result[i]-tt[i]) > 0.01 {
				t.Errorf("tetrahedral(%v) = %v, want %v", tt, result, tt)
				break
			}
		}
	}
}

func TestMultilinearInterp(t *testing.T) {
	// 3x3x3 identity CLUT
	gridPoints := []int{3, 3, 3}
	outChannels := 3
	size := 3 * 3 * 3 * outChannels
	clut := make([]float64, size)

	for r := 0; r < 3; r++ {
		for g := 0; g < 3; g++ {
			for b := 0; b < 3; b++ {
				idx := (r*9 + g*3 + b) * outChannels
				clut[idx+0] = float64(r) / 2.0
				clut[idx+1] = float64(g) / 2.0
				clut[idx+2] = float64(b) / 2.0
			}
		}
	}

	tests := [][]float64{
		{0, 0, 0},
		{1, 1, 1},
		{0.5, 0.5, 0.5},
		{0.25, 0.75, 0.5},
	}

	for _, tt := range tests {
		result := multilinearInterp(clut, gridPoints, outChannels, tt)
		for i := 0; i < 3; i++ {
			if math.Abs(result[i]-tt[i]) > 0.01 {
				t.Errorf("multilinear(%v) = %v, want %v", tt, result, tt)
				break
			}
		}
	}
}

func TestLabToXYZ(t *testing.T) {
	white := [3]float64{0.9642, 1.0, 0.8249} // D50

	tests := []struct {
		L, a, b             float64
		wantX, wantY, wantZ float64
	}{
		// white: L=100, a=0, b=0 should give white point
		{100, 0, 0, 0.9642, 1.0, 0.8249},
		// black: L=0 should give near zero
		{0, 0, 0, 0, 0, 0},
		// mid gray: L=50
		{50, 0, 0, 0.175, 0.1842, 0.1502},
	}

	for _, tt := range tests {
		x, y, z := labToXYZ([]float64{tt.L, tt.a, tt.b}, white)
		if math.Abs(x-tt.wantX) > 0.01 || math.Abs(y-tt.wantY) > 0.01 || math.Abs(z-tt.wantZ) > 0.01 {
			t.Errorf("labToXYZ(%v, %v, %v) = (%v, %v, %v), want (%v, %v, %v)",
				tt.L, tt.a, tt.b, x, y, z, tt.wantX, tt.wantY, tt.wantZ)
		}
	}
}

func TestXYZToLab(t *testing.T) {
	white := [3]float64{0.9642, 1.0, 0.8249} // D50

	tests := []struct {
		X, Y, Z             float64
		wantL, wantA, wantB float64
	}{
		// white point should give L=100, a=0, b=0
		{0.9642, 1.0, 0.8249, 100, 0, 0},
		// black should give L=0
		{0, 0, 0, 0, 0, 0},
	}

	for _, tt := range tests {
		L, a, b := xyzToLab(tt.X, tt.Y, tt.Z, white)
		if math.Abs(L-tt.wantL) > 0.1 || math.Abs(a-tt.wantA) > 0.1 || math.Abs(b-tt.wantB) > 0.1 {
			t.Errorf("xyzToLab(%v, %v, %v) = (%v, %v, %v), want (%v, %v, %v)",
				tt.X, tt.Y, tt.Z, L, a, b, tt.wantL, tt.wantA, tt.wantB)
		}
	}
}

func TestLabXYZRoundTrip(t *testing.T) {
	white := [3]float64{0.9642, 1.0, 0.8249}

	tests := [][]float64{
		{0, 0, 0},
		{50, 0, 0},
		{100, 0, 0},
		{50, 50, 0},
		{50, 0, 50},
		{50, -50, -50},
		{75, 25, -30},
	}

	for _, lab := range tests {
		x, y, z := labToXYZ(lab, white)
		L, a, b := xyzToLab(x, y, z, white)
		if math.Abs(L-lab[0]) > 0.01 || math.Abs(a-lab[1]) > 0.01 || math.Abs(b-lab[2]) > 0.01 {
			t.Errorf("Lab round-trip failed: %v -> XYZ(%v,%v,%v) -> Lab(%v,%v,%v)",
				lab, x, y, z, L, a, b)
		}
	}
}

func TestInvertMatrix3x3(t *testing.T) {
	// identity matrix
	identity := []float64{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
	}
	inv := invertMatrix3x3(identity)
	for i := range identity {
		if math.Abs(inv[i]-identity[i]) > 1e-10 {
			t.Errorf("inverse of identity differs at %d: %f vs %f", i, inv[i], identity[i])
		}
	}

	// sRGB to XYZ matrix (approximate)
	srgbToXYZ := []float64{
		0.4124564, 0.3575761, 0.1804375,
		0.2126729, 0.7151522, 0.0721750,
		0.0193339, 0.1191920, 0.9503041,
	}
	inv = invertMatrix3x3(srgbToXYZ)

	// multiply should give identity
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sum := 0.0
			for k := 0; k < 3; k++ {
				sum += srgbToXYZ[i*3+k] * inv[k*3+j]
			}
			expected := 0.0
			if i == j {
				expected = 1.0
			}
			if math.Abs(sum-expected) > 1e-6 {
				t.Errorf("matrix * inverse[%d][%d] = %f, want %f", i, j, sum, expected)
			}
		}
	}
}

func TestDecodeCurveType(t *testing.T) {
	// curveType with n=0 (identity)
	data := []byte{'c', 'u', 'r', 'v', 0, 0, 0, 0, 0, 0, 0, 0}
	c, err := DecodeCurve(data)
	if err != nil {
		t.Fatalf("decode identity curve: %v", err)
	}
	if c.Gamma != 1.0 {
		t.Errorf("identity curve gamma = %f, want 1.0", c.Gamma)
	}

	// curveType with n=1 (gamma)
	// gamma 2.2 as u8Fixed8Number = 2*256 + 0.2*256 = 563
	data = []byte{'c', 'u', 'r', 'v', 0, 0, 0, 0, 0, 0, 0, 1, 0x02, 0x33}
	c, err = DecodeCurve(data)
	if err != nil {
		t.Fatalf("decode gamma curve: %v", err)
	}
	expected := float64(0x0233) / 256.0
	if math.Abs(c.Gamma-expected) > 0.01 {
		t.Errorf("gamma curve = %f, want %f", c.Gamma, expected)
	}
}

func TestDecodeParametricCurve(t *testing.T) {
	// parametricCurveType type 0 with gamma 2.2
	// s15Fixed16: 2.2 = 0x00023333
	data := []byte{
		'p', 'a', 'r', 'a',
		0, 0, 0, 0, // reserved
		0, 0, // function type 0
		0, 0, // reserved
		0x00, 0x02, 0x33, 0x33, // gamma = 2.2
	}
	c, err := DecodeCurve(data)
	if err != nil {
		t.Fatalf("decode parametric curve: %v", err)
	}
	if c.FuncType != 0 {
		t.Errorf("funcType = %d, want 0", c.FuncType)
	}
	if len(c.Params) != 1 {
		t.Errorf("params len = %d, want 1", len(c.Params))
	}
	expected := float64(0x00023333) / 65536.0
	if math.Abs(c.Params[0]-expected) > 0.001 {
		t.Errorf("gamma param = %f, want %f", c.Params[0], expected)
	}
}
