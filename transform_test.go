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

	"github.com/google/go-cmp/cmp"
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
		{0.0, 0.0, 1.0},
		{0.0, 0.5, 1.0},
	}

	for _, tt := range tests {
		c := &Curve{Gamma: tt.gamma}
		got := c.Evaluate(tt.input)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("Gamma %.1f: Evaluate(%.2f) = %.4f, want %.4f",
				tt.gamma, tt.input, got, tt.want)
		}

		data, err := c.Encode()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeCurve(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := decoded.Evaluate(tt.input); math.Abs(got-tt.want) > 0.001 {
			t.Errorf("Gamma %.1f: decoded Evaluate(%.2f) = %.4f, want %.4f",
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
		Params: []float64{2.2},
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
		Params: []float64{g, a, b, cc, d},
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

// TestCurveInverse checks that Invert is a generalised inverse of Evaluate
// for monotone curves of every kind: for each y, the curve passes through y
// within distance dx of Invert(y).  Curves which map [0, 1] one-to-one onto
// [0, 1] must also satisfy Invert(Evaluate(x)) = x and Evaluate(Invert(y)) = y.
func TestCurveInverse(t *testing.T) {
	// rounding makes the first two entries 0, so this curve is not one-to-one
	gamma22 := make([]uint16, 256)
	for i := range gamma22 {
		// rounding makes the first two entries 0, so the curve is not one-to-one
		gamma22[i] = uint16(math.Round(math.Pow(float64(i)/255, 2.2) * 65535))
	}

	// sRGB parameters
	const (
		sg = 2.4
		sa = 1 / 1.055
		sb = 0.055 / 1.055
		sc = 1 / 12.92
		sd = 0.04045
	)

	tests := []struct {
		name       string
		curve      *Curve
		decreasing bool
		bijective  bool
		dx, dy     float64 // tolerances in input and output space
	}{
		{"gamma 1", &Curve{Gamma: 1}, false, true, 1e-9, 1e-9},
		{"gamma 2.2", &Curve{Gamma: 2.2}, false, true, 1e-9, 1e-9},
		{"gamma 0.45", &Curve{Gamma: 0.45}, false, true, 1e-9, 1e-9},
		{"gamma 0", &Curve{Gamma: 0}, false, false, 1e-9, 1e-9},
		{"type 0", &Curve{Params: []float64{2.4}}, false, true, 1e-9, 1e-9},
		{"type 1", &Curve{Params: []float64{2, 1.5, -0.5}}, false, false, 1e-9, 1e-9},
		{"type 2", &Curve{Params: []float64{1, 0.5, 0, 0.25}}, false, false, 1e-9, 1e-9},
		{"type 3 sRGB", &Curve{Params: []float64{sg, sa, sb, sc, sd}}, false, true, 1e-9, 1e-9},
		{"type 3 jump", &Curve{Params: []float64{1, 1, 0, 0.5, 0.5}}, false, false, 1e-9, 1e-9},
		{"type 4 offsets", &Curve{Params: []float64{2.2, 0.9, 0.1, 0.2, 0.1, 0, 0.0059}}, false, false, 1e-9, 1e-9},
		{"type 4 jump", &Curve{Params: []float64{1, 0.5, 0, 0.5, 0.5, 0.5, 0}}, false, false, 1e-9, 1e-9},
		{"type 0 g=0", &Curve{Params: []float64{0}}, false, false, 1e-9, 1e-9},
		{"type 2 g=0", &Curve{Params: []float64{0, 2, -0.5, -0.5}}, false, false, 1e-9, 1e-9},
		{"type 4 g=0", &Curve{Params: []float64{0, 1, -0.25, 0.5, 0.25, -0.2, 0}}, false, false, 1e-9, 1e-9},
		{"type 2 a=0", &Curve{Params: []float64{1, 0, 0.5, 0}}, false, false, 1e-9, 1e-9},
		{"type 4 a=0", &Curve{Params: []float64{2, 0, 0.5, 1, 0.25, 0.25, 0}}, false, false, 1e-9, 1e-9},
		{"sampled linear", &Curve{Table: []uint16{0, 65535}}, false, true, 1e-6, 1e-6},
		{"sampled gamma 2.2", &Curve{Table: gamma22}, false, false, 1e-6, 1e-6},
		{"sampled flat", &Curve{Table: []uint16{0, 0, 32768, 65535}}, false, false, 1e-6, 1e-6},
		{"sampled decreasing", &Curve{Table: []uint16{65535, 0}}, true, true, 1e-6, 1e-6},
	}

	const n = 1000
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.curve
			for i := range n + 1 {
				y := float64(i) / n
				x := c.Invert(y)
				if !(x >= 0 && x <= 1) {
					t.Fatalf("Invert(%g) = %g, outside [0, 1]", y, x)
				}

				// the curve must pass through y between x-dx and x+dx
				left, right := y+tt.dy, y-tt.dy
				if tt.decreasing {
					left, right = right, left
				}
				if x-tt.dx >= 0 {
					v := c.Evaluate(x - tt.dx)
					if tt.decreasing && v < left || !tt.decreasing && v > left {
						t.Fatalf("Invert(%g) = %g, but Evaluate(%g) = %g", y, x, x-tt.dx, v)
					}
				}
				if x+tt.dx <= 1 {
					v := c.Evaluate(x + tt.dx)
					if tt.decreasing && v > right || !tt.decreasing && v < right {
						t.Fatalf("Invert(%g) = %g, but Evaluate(%g) = %g", y, x, x+tt.dx, v)
					}
				}

				if !tt.bijective {
					continue
				}
				if got := c.Evaluate(x); math.Abs(got-y) > tt.dy {
					t.Fatalf("Evaluate(Invert(%g)) = %g", y, got)
				}
				x = y
				if got := c.Invert(c.Evaluate(x)); math.Abs(got-x) > tt.dx {
					t.Fatalf("Invert(Evaluate(%g)) = %g", x, got)
				}
			}
		})
	}
}

// TestCurveZeroExponent checks that a zero exponent makes the power term
// equal to 1 wherever it applies, including at base 0, and that Invert
// returns the smallest input which reaches the output.
func TestCurveZeroExponent(t *testing.T) {
	tests := []struct {
		name  string
		curve *Curve
		x, y  float64 // Evaluate(x) = y
		inv   float64 // Invert(y)
	}{
		{"gamma at 0", &Curve{Gamma: 0}, 0, 1, 0},
		{"gamma at 1", &Curve{Gamma: 0}, 1, 1, 0},
		{"type 0 at 0", &Curve{Params: []float64{0}}, 0, 1, 0},
		{"type 1 below step", &Curve{Params: []float64{0, 2, -0.5}}, 0.2, 0, 0},
		{"type 1 at step", &Curve{Params: []float64{0, 2, -0.5}}, 0.25, 1, 0.25},
		{"type 2 at step", &Curve{Params: []float64{0, 2, -0.5, -0.5}}, 0.25, 0.5, 0.25},
		{"type 3 at base 0", &Curve{Params: []float64{0, 1, -0.5, 0, 0.25}}, 0.5, 1, 0.5},
		{"type 4 at d", &Curve{Params: []float64{0, 1, 0, 0.5, 0.25, -0.2, 0}}, 0.25, 0.8, 0.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.curve.Evaluate(tt.x); math.Abs(got-tt.y) > 1e-12 {
				t.Errorf("Evaluate(%g) = %g, want %g", tt.x, got, tt.y)
			}
			if got := tt.curve.Invert(tt.y); math.Abs(got-tt.inv) > 1e-12 {
				t.Errorf("Invert(%g) = %g, want %g", tt.y, got, tt.inv)
			}
		})
	}
}

// TestCurveInvertFlat checks Invert for parametric curves with a = 0, where
// the power term is constant: an output above the constant is never reached,
// so Invert returns 1.
func TestCurveInvertFlat(t *testing.T) {
	// constant 0.5
	type2 := &Curve{Params: []float64{1, 0, 0.5, 0}}
	// constant 0 (b < 0 means the power segment never applies)
	type1 := &Curve{Params: []float64{1, 0, -0.5}}
	// y = x below 0.25, then 0.5^2 + 0.25 = 0.5
	type4 := &Curve{Params: []float64{2, 0, 0.5, 1, 0.25, 0.25, 0}}
	// y = 0.5x below 0.25, then 0.5^2 = 0.25
	type3 := &Curve{Params: []float64{2, 0, 0.5, 0.5, 0.25}}

	tests := []struct {
		name  string
		curve *Curve
		y     float64
		want  float64
	}{
		{"type 2 below", type2, 0.3, 0},
		{"type 2 at", type2, 0.5, 0},
		{"type 2 above", type2, 0.8, 1},
		{"type 1 zero", type1, 0, 0},
		{"type 1 above", type1, 0.1, 1},
		{"type 4 linear", type4, 0.2, 0.2},
		{"type 4 jump", type4, 0.4, 0.25},
		{"type 4 at", type4, 0.5, 0.25},
		{"type 4 above", type4, 0.6, 1},
		{"type 3 jump", type3, 0.2, 0.25},
		{"type 3 above", type3, 0.3, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.curve.Invert(tt.y); math.Abs(got-tt.want) > 1e-12 {
				t.Errorf("Invert(%g) = %g, want %g", tt.y, got, tt.want)
			}
		})
	}
}

// TestCurvePrecedence checks that Evaluate and Encode agree on which field
// defines a curve when several are set: Table, then Params, then Gamma.
func TestCurvePrecedence(t *testing.T) {
	tests := []struct {
		name  string
		curve *Curve
		want  *Curve
	}{
		{"table over params",
			&Curve{Gamma: 2, Params: []float64{1}, Table: []uint16{0, 0}},
			&Curve{Table: []uint16{0, 0}}},
		{"table over gamma",
			&Curve{Gamma: 2, Table: []uint16{0, 65535}},
			&Curve{Table: []uint16{0, 65535}}},
		{"params over gamma",
			&Curve{Gamma: 2, Params: []float64{1}},
			&Curve{Params: []float64{1}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.curve.Encode()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeCurve(data)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, decoded); diff != "" {
				t.Errorf("round trip failed (-want +got):\n%s", diff)
			}
			for i := range 11 {
				x := float64(i) / 10
				want := tt.want.Evaluate(x)
				if got := tt.curve.Evaluate(x); got != want {
					t.Errorf("Evaluate(%g) = %g, want %g", x, got, want)
				}
				if got := decoded.Evaluate(x); got != want {
					t.Errorf("decoded Evaluate(%g) = %g, want %g", x, got, want)
				}
			}
		})
	}
}

// TestEncodeParamsRound checks that parametric curve coefficients are rounded
// to the nearest multiple of 1/65536, and that the extremes of the range
// are accepted.
func TestEncodeParamsRound(t *testing.T) {
	params := []float64{100.7 / 65536, -100.7 / 65536, -32768, 32767.99999}
	want := []float64{101.0 / 65536, -101.0 / 65536, -32768, 32768 - 1.0/65536}
	for i, v := range params {
		data, err := (&Curve{Params: []float64{v}}).Encode()
		if err != nil {
			t.Fatalf("Encode(%g): %v", v, err)
		}
		decoded, err := DecodeCurve(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := decoded.Params[0]; got != want[i] {
			t.Errorf("coefficient %g encoded as %g, want %g", v, got, want[i])
		}
	}
}

// TestCurveEncodeInvalid checks that Encode rejects curves which have no
// ICC representation.
func TestCurveEncodeInvalid(t *testing.T) {
	tests := []struct {
		name  string
		curve *Curve
	}{
		{"empty table", &Curve{Table: []uint16{}}},
		{"one-entry table", &Curve{Table: []uint16{65535}}},
		{"two params", &Curve{Params: []float64{1, 2}}},
		{"six params", &Curve{Params: []float64{1, 2, 3, 4, 5, 6}}},
		{"negative gamma", &Curve{Gamma: -1}},
		{"large gamma", &Curve{Gamma: 65535.5 / 256}},
		{"NaN gamma", &Curve{Gamma: math.NaN()}},
		{"large coefficient", &Curve{Params: []float64{32768}}},
		{"small coefficient", &Curve{Params: []float64{-32768.00001}}},
		{"NaN coefficient", &Curve{Params: []float64{2.2, math.NaN(), 0}}},
		{"infinite coefficient", &Curve{Params: []float64{math.Inf(1)}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.curve.Encode(); err == nil {
				t.Error("expected an error")
			}
		})
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
		{"type 0 gamma 1.0", &Curve{Params: []float64{1.0}}, true},
		{"type 0 gamma 2.2", &Curve{Params: []float64{2.2}}, false},
		{"table linear", &Curve{Table: []uint16{0, 65535}}, true},
		{"table linear 4", &Curve{Table: []uint16{0, 21845, 43690, 65535}}, true},
		{"table rounded", &Curve{Table: []uint16{0, 32768, 65535}}, false},
		{"table over params", &Curve{Params: []float64{1}, Table: []uint16{0, 0}}, false},
		{"params over gamma", &Curve{Gamma: 1, Params: []float64{2.2}}, false},
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
	clut := make([]float32, gridSize*gridSize*gridSize*outChannels)

	// fill CLUT with identity mapping
	for r := range gridSize {
		for g := range gridSize {
			for b := range gridSize {
				idx := (r*gridSize*gridSize + g*gridSize + b) * outChannels
				clut[idx+0] = float32(r) / float32(gridSize-1)
				clut[idx+1] = float32(g) / float32(gridSize-1)
				clut[idx+2] = float32(b) / float32(gridSize-1)
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
		for i := range 3 {
			if math.Abs(result[i]-tt[i]) > 0.01 {
				t.Errorf("tetrahedral(%v) = %v, want %v", tt, result, tt)
				break
			}
		}
	}
}

func TestLabToXYZ(t *testing.T) {
	tests := []struct {
		L, a, b             float64
		wantX, wantY, wantZ float64
	}{
		// white: L=100, a=0, b=0 should give the PCS white point
		{100, 0, 0, PCSWhitePoint[0], PCSWhitePoint[1], PCSWhitePoint[2]},
		// black: L=0 should give near zero
		{0, 0, 0, 0, 0, 0},
		// mid gray: L=50
		{50, 0, 0, 0.175, 0.1842, 0.1502},
	}

	for _, tt := range tests {
		x, y, z := labToXYZ([]float64{tt.L, tt.a, tt.b})
		if math.Abs(x-tt.wantX) > 0.01 || math.Abs(y-tt.wantY) > 0.01 || math.Abs(z-tt.wantZ) > 0.01 {
			t.Errorf("labToXYZ(%v, %v, %v) = (%v, %v, %v), want (%v, %v, %v)",
				tt.L, tt.a, tt.b, x, y, z, tt.wantX, tt.wantY, tt.wantZ)
		}
	}

	// a LUT with fewer than three output channels yields zero output
	if x, y, z := labToXYZ([]float64{50, 0}); x != 0 || y != 0 || z != 0 {
		t.Errorf("labToXYZ(short) = (%v, %v, %v), want zero", x, y, z)
	}
}

func TestXYZToLab(t *testing.T) {
	tests := []struct {
		X, Y, Z             float64
		wantL, wantA, wantB float64
	}{
		// the PCS white point should give L=100, a=0, b=0
		{PCSWhitePoint[0], PCSWhitePoint[1], PCSWhitePoint[2], 100, 0, 0},
		// black should give L=0
		{0, 0, 0, 0, 0, 0},
	}

	for _, tt := range tests {
		L, a, b := xyzToLab(tt.X, tt.Y, tt.Z)
		if math.Abs(L-tt.wantL) > 0.1 || math.Abs(a-tt.wantA) > 0.1 || math.Abs(b-tt.wantB) > 0.1 {
			t.Errorf("xyzToLab(%v, %v, %v) = (%v, %v, %v), want (%v, %v, %v)",
				tt.X, tt.Y, tt.Z, L, a, b, tt.wantL, tt.wantA, tt.wantB)
		}
	}
}

func TestLabXYZRoundTrip(t *testing.T) {
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
		x, y, z := labToXYZ(lab)
		L, a, b := xyzToLab(x, y, z)
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
	for i := range 3 {
		for j := range 3 {
			sum := 0.0
			for k := range 3 {
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

// d65WhitePoint is the CIE standard illuminant D65 white point, used here to
// build profiles measured against something other than the PCS illuminant.
// The values are the ASTM E308 tristimulus values for the 2° observer.
var d65WhitePoint = [3]float64{0.95047, 1.0, 1.08883}

// encodeXYZTag encodes an XYZType tag holding a single XYZ triple.
func encodeXYZTag(xyz [3]float64) []byte {
	data := make([]byte, 20)
	copy(data[:4], "XYZ ")
	putS15Fixed16(data, 8, xyz[0])
	putS15Fixed16(data, 12, xyz[1])
	putS15Fixed16(data, 16, xyz[2])
	return data
}

// makeGrayProfile creates a gray TRC profile with the given gamma and white point.
func makeGrayProfile(gamma float64, whitePoint [3]float64) *Profile {
	return &Profile{
		ColorSpace: GraySpace,
		PCS:        PCSXYZSpace,
		TagData: map[TagType][]byte{
			GrayTRC:         mustEncodeCurve(&Curve{Gamma: gamma}),
			MediaWhitePoint: encodeXYZTag(whitePoint),
		},
	}
}

func TestGrayTRCNeutral(t *testing.T) {
	// A gray profile with a D65 white point must produce neutral (achromatic)
	// output when round-tripped through DeviceToPCS → PCSToDevice.
	// This is a regression test: previously the D65 white point was used
	// directly for XYZ scaling, producing a blue-tinted result.
	p := makeGrayProfile(2.2, d65WhitePoint)

	fwd, err := NewTransform(p, Perceptual)
	if err != nil {
		t.Fatal(err)
	}

	for _, gray := range []float64{0.0, 0.25, 0.5, 0.75, 0.992, 1.0} {
		X, Y, Z := fwd.ToXYZ([]float64{gray}, &Workspace{})

		// XYZ should be proportional to D50, not D65
		if Y > 1e-6 {
			ratioX := X / Y
			ratioZ := Z / Y
			wantRatioX := PCSWhitePoint[0] / PCSWhitePoint[1]
			wantRatioZ := PCSWhitePoint[2] / PCSWhitePoint[1]
			if math.Abs(ratioX-wantRatioX) > 1e-4 {
				t.Errorf("gray %.3f: X/Y = %.6f, want %.6f (D50 ratio)",
					gray, ratioX, wantRatioX)
			}
			if math.Abs(ratioZ-wantRatioZ) > 1e-4 {
				t.Errorf("gray %.3f: Z/Y = %.6f, want %.6f (D50 ratio)",
					gray, ratioZ, wantRatioZ)
			}
		}
	}
}

func TestGrayTRCRoundTrip(t *testing.T) {
	// round-trip DeviceToPCS → PCSToDevice must recover the original gray,
	// regardless of the profile's media white point
	for _, wp := range [][3]float64{
		PCSWhitePoint,
		d65WhitePoint,
		{0.9000, 1.0, 0.9000}, // arbitrary
	} {
		p := makeGrayProfile(2.2, wp)

		tr, err := NewTransform(p, Perceptual)
		if err != nil {
			t.Fatal(err)
		}

		for _, gray := range []float64{0.0, 0.1, 0.5, 0.9, 1.0} {
			X, Y, Z := tr.ToXYZ([]float64{gray}, &Workspace{})
			back := make([]float64, tr.Channels())
			tr.FromXYZ(X, Y, Z, back, &Workspace{})
			if len(back) != 1 || math.Abs(back[0]-gray) > 1e-4 {
				t.Errorf("wp=%v gray=%.1f: round-trip gave %.6f",
					wp, gray, back[0])
			}
		}
	}
}

// TestAbsoluteIntentGrayTRC checks the ICC-absolute scaling on a gray profile,
// where the PCS values never pass through a LUT.
func TestAbsoluteIntentGrayTRC(t *testing.T) {
	p := makeGrayProfile(2.2, d65WhitePoint)

	// the tag rounds to s15Fixed16, so read back what the profile stores
	wp, err := parseXYZ(p.TagData[MediaWhitePoint])
	if err != nil {
		t.Fatal(err)
	}

	rel, err := NewTransform(p, RelativeColorimetric)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := NewTransform(p, AbsoluteColorimetric)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Intent != RelativeColorimetric || abs.Intent != AbsoluteColorimetric {
		t.Errorf("intents %v, %v", rel.Intent, abs.Intent)
	}

	for _, gray := range []float64{0.25, 0.5, 1.0} {
		Xr, Yr, Zr := rel.ToXYZ([]float64{gray}, &Workspace{})
		Xa, Ya, Za := abs.ToXYZ([]float64{gray}, &Workspace{})

		wantX := Xr * wp[0] / PCSWhitePoint[0]
		wantY := Yr * wp[1] / PCSWhitePoint[1]
		wantZ := Zr * wp[2] / PCSWhitePoint[2]
		if math.Abs(Xa-wantX) > 1e-9 ||
			math.Abs(Ya-wantY) > 1e-9 ||
			math.Abs(Za-wantZ) > 1e-9 {
			t.Errorf("gray %.2f: absolute XYZ (%g, %g, %g), want (%g, %g, %g)",
				gray, Xa, Ya, Za, wantX, wantY, wantZ)
		}

		back := make([]float64, 1)
		abs.FromXYZ(Xa, Ya, Za, back, &Workspace{})
		if math.Abs(back[0]-gray) > 1e-6 {
			t.Errorf("gray %.2f: round trip gave %g", gray, back[0])
		}
	}
}

// TestMediaWhitePointV2Display checks that version 2 display profiles ignore
// the media white point tag, which often holds the unadapted display white.
func TestMediaWhitePointV2Display(t *testing.T) {
	for _, tc := range []struct {
		version Version
		class   ProfileClass
		ignored bool
	}{
		{Version2_1_0, DisplayDeviceProfile, true},
		{Version2_1_0, OutputDeviceProfile, false},
		{Version4_0_0, DisplayDeviceProfile, false},
	} {
		p := makeGrayProfile(1.0, d65WhitePoint)
		p.Version = tc.version
		p.Class = tc.class
		tr, err := NewTransform(p, AbsoluteColorimetric)
		if err != nil {
			t.Fatal(err)
		}

		want := PCSWhitePoint
		if !tc.ignored {
			want, err = parseXYZ(p.TagData[MediaWhitePoint])
			if err != nil {
				t.Fatal(err)
			}
		}
		X, Y, Z := tr.ToXYZ([]float64{1}, &Workspace{})
		if math.Abs(X-want[0]) > 1e-9 ||
			math.Abs(Y-want[1]) > 1e-9 ||
			math.Abs(Z-want[2]) > 1e-9 {
			t.Errorf("%v %v: white -> XYZ (%g, %g, %g), want %v",
				tc.version, tc.class, X, Y, Z, want)
		}
	}
}

// TestMediaWhitePointFallback checks that an unusable media white point tag
// falls back to the PCS white.  Without this, the ICC-absolute scaling would
// zero or invert every output value.
func TestMediaWhitePointFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"missing", nil},
		{"truncated", []byte("XYZ ")},
		{"wrong type", append([]byte("desc"), make([]byte, 16)...)},
		{"zero", encodeXYZTag([3]float64{0, 0, 0})},
		{"negative", encodeXYZTag([3]float64{-0.9642, -1.0, -0.8249})},
		{"percent scale", encodeXYZTag([3]float64{96.42, 100, 82.49})},
		{"huge", encodeXYZTag([3]float64{32767, 32767, 32767})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := makeGrayProfile(1.0, PCSWhitePoint)
			if tc.data == nil {
				delete(p.TagData, MediaWhitePoint)
			} else {
				p.TagData[MediaWhitePoint] = tc.data
			}

			tr, err := NewTransform(p, AbsoluteColorimetric)
			if err != nil {
				t.Fatal(err)
			}
			if tr.whitePoint != PCSWhitePoint {
				t.Errorf("white point = %v, want %v", tr.whitePoint, PCSWhitePoint)
			}

			X, Y, Z := tr.ToXYZ([]float64{1}, &Workspace{})
			if math.Abs(X-PCSWhitePoint[0]) > 1e-9 ||
				math.Abs(Y-PCSWhitePoint[1]) > 1e-9 ||
				math.Abs(Z-PCSWhitePoint[2]) > 1e-9 {
				t.Errorf("white -> XYZ (%g, %g, %g), want %v", X, Y, Z, PCSWhitePoint)
			}
		})
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
	if len(c.Params) != 1 {
		t.Errorf("params len = %d, want 1", len(c.Params))
	}
	expected := float64(0x00023333) / 65536.0
	if math.Abs(c.Params[0]-expected) > 0.001 {
		t.Errorf("gamma param = %f, want %f", c.Params[0], expected)
	}
}

func TestLUTFallbackChain(t *testing.T) {
	tests := []struct {
		name   string
		dir    direction
		intent RenderingIntent
		want   []TagType
	}{
		{"deviceToPCS/Perceptual", deviceToPCS, Perceptual, []TagType{AToB0, AToB1, AToB2}},
		{"deviceToPCS/RelativeColorimetric", deviceToPCS, RelativeColorimetric, []TagType{AToB1, AToB0, AToB2}},
		{"deviceToPCS/AbsoluteColorimetric", deviceToPCS, AbsoluteColorimetric, []TagType{AToB1, AToB0, AToB2}},
		{"deviceToPCS/Saturation", deviceToPCS, Saturation, []TagType{AToB2, AToB0, AToB1}},
		{"pcsToDevice/Perceptual", pcsToDevice, Perceptual, []TagType{BToA0, BToA1, BToA2}},
		{"pcsToDevice/RelativeColorimetric", pcsToDevice, RelativeColorimetric, []TagType{BToA1, BToA0, BToA2}},
		{"pcsToDevice/Saturation", pcsToDevice, Saturation, []TagType{BToA2, BToA0, BToA1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lutFallbackChain(tc.dir, tc.intent)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("chain[%d] = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestInitLUTCapability(t *testing.T) {
	// a LUT profile now constructs successfully regardless of which direction's
	// tags are present; the available directions are reported by CanToXYZ /
	// CanFromXYZ rather than surfaced as construction errors.
	tests := []struct {
		name        string
		tags        map[TagType][]byte
		wantToXYZ   bool
		wantFromXYZ bool
	}{
		{"AToB1 only", map[TagType][]byte{AToB1: mABTag()}, true, false},
		{"AToB2 only", map[TagType][]byte{AToB2: mABTag()}, true, false},
		{"BToA1 only", map[TagType][]byte{BToA1: mABTag()}, false, true},
		{"AToB0 only", map[TagType][]byte{AToB0: mABTag()}, true, false},
		{"both directions", map[TagType][]byte{AToB0: mABTag(), BToA0: mABTag()}, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &Profile{
				ColorSpace: RGBSpace,
				PCS:        PCSXYZSpace,
				TagData:    tc.tags,
			}
			tr, err := NewTransform(p, Perceptual)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := tr.CanToXYZ(); got != tc.wantToXYZ {
				t.Errorf("CanToXYZ = %v, want %v", got, tc.wantToXYZ)
			}
			if got := tr.CanFromXYZ(); got != tc.wantFromXYZ {
				t.Errorf("CanFromXYZ = %v, want %v", got, tc.wantFromXYZ)
			}
		})
	}
}

// mABTag builds a minimal mAB tag (LUTAToB), 32-byte header, no sub-tags.
func mABTag() []byte {
	b := make([]byte, 32)
	copy(b[0:4], "mAB ")
	b[8] = 3 // input channels
	b[9] = 3 // output channels
	return b
}

func TestOneDirectionalLUTDegrades(t *testing.T) {
	t.Run("forward only", func(t *testing.T) {
		p := &Profile{
			ColorSpace: RGBSpace,
			PCS:        PCSXYZSpace,
			TagData:    map[TagType][]byte{AToB0: mABTag()},
		}
		tr, err := NewTransform(p, Perceptual)
		if err != nil {
			t.Fatal(err)
		}
		if !tr.CanToXYZ() || tr.CanFromXYZ() {
			t.Fatalf("CanToXYZ=%v CanFromXYZ=%v, want true/false", tr.CanToXYZ(), tr.CanFromXYZ())
		}
		// unsupported reverse direction writes nothing
		dst := []float64{-1, -2, -3}
		tr.FromXYZ(0.3, 0.4, 0.5, dst, &Workspace{})
		if dst[0] != -1 || dst[1] != -2 || dst[2] != -3 {
			t.Errorf("FromXYZ wrote to dst on unsupported direction: %v", dst)
		}
	})

	t.Run("reverse only", func(t *testing.T) {
		p := &Profile{
			ColorSpace: RGBSpace,
			PCS:        PCSXYZSpace,
			TagData:    map[TagType][]byte{BToA0: mABTag()},
		}
		tr, err := NewTransform(p, Perceptual)
		if err != nil {
			t.Fatal(err)
		}
		if tr.CanToXYZ() || !tr.CanFromXYZ() {
			t.Fatalf("CanToXYZ=%v CanFromXYZ=%v, want false/true", tr.CanToXYZ(), tr.CanFromXYZ())
		}
		// unsupported forward direction returns zero
		if X, Y, Z := tr.ToXYZ([]float64{0.3, 0.4, 0.5}, &Workspace{}); X != 0 || Y != 0 || Z != 0 {
			t.Errorf("ToXYZ on unsupported direction = %v,%v,%v, want 0,0,0", X, Y, Z)
		}
	})
}

func TestSingularMatrixReverseUnavailable(t *testing.T) {
	xyzTag := func(x, y, z float64) []byte {
		b := make([]byte, 20)
		copy(b[:4], "XYZ ")
		putS15Fixed16(b, 8, x)
		putS15Fixed16(b, 12, y)
		putS15Fixed16(b, 16, z)
		return b
	}
	trc := mustEncodeCurve(&Curve{Gamma: 2.2})

	// three identical columns => singular matrix => reverse unavailable
	col := xyzTag(0.5, 0.5, 0.5)
	p := &Profile{
		ColorSpace: RGBSpace,
		PCS:        PCSXYZSpace,
		TagData: map[TagType][]byte{
			RedMatrixColumn:   col,
			GreenMatrixColumn: col,
			BlueMatrixColumn:  col,
			RedTRC:            trc,
			GreenTRC:          trc,
			BlueTRC:           trc,
		},
	}
	tr, err := NewTransform(p, Perceptual)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tr.CanToXYZ() {
		t.Errorf("CanToXYZ = false, want true")
	}
	if tr.CanFromXYZ() {
		t.Errorf("CanFromXYZ = true, want false (singular matrix)")
	}
	dst := []float64{-1, -2, -3}
	tr.FromXYZ(0.3, 0.4, 0.5, dst, &Workspace{})
	if dst[0] != -1 || dst[1] != -2 || dst[2] != -3 {
		t.Errorf("FromXYZ wrote to dst with singular matrix: %v", dst)
	}
}

// badLUTTag returns bytes with an unrecognised type signature, so DecodeLUT
// fails even though Profile.ProfileType keys on the tag's presence alone.
func badLUTTag() []byte {
	return []byte("junkjunk")
}

// A present-but-malformed preferred-intent tag must not mask a valid fallback
// further along the chain.
func TestDecodeLUTChainSkipsMalformedTag(t *testing.T) {
	p := &Profile{
		ColorSpace: RGBSpace,
		PCS:        PCSXYZSpace,
		TagData: map[TagType][]byte{
			AToB0: badLUTTag(), // preferred (Perceptual), but corrupt
			AToB1: mABTag(),    // valid fallback
		},
	}
	tr, err := NewTransform(p, Perceptual)
	if err != nil {
		t.Fatal(err)
	}
	// the empty fallback LUT passes the device values through, so a non-zero
	// result proves the fallback was used rather than abandoned.
	X, Y, Z := tr.ToXYZ([]float64{0.3, 0.4, 0.5}, &Workspace{})
	if X == 0 && Y == 0 && Z == 0 {
		t.Errorf("ToXYZ = 0,0,0; malformed AToB0 masked valid AToB1 fallback")
	}
}

// When every present tag for a direction fails to decode, the direction yields
// zero output rather than leaking PCS values into the device channels.
func TestMalformedLUTYieldsZeroOutput(t *testing.T) {
	t.Run("forward", func(t *testing.T) {
		p := &Profile{
			ColorSpace: RGBSpace,
			PCS:        PCSXYZSpace,
			TagData:    map[TagType][]byte{AToB0: badLUTTag()},
		}
		tr, err := NewTransform(p, Perceptual)
		if err != nil {
			t.Fatal(err)
		}
		if !tr.CanToXYZ() {
			t.Fatal("CanToXYZ = false, want true (tag present)")
		}
		if X, Y, Z := tr.ToXYZ([]float64{0.3, 0.4, 0.5}, &Workspace{}); X != 0 || Y != 0 || Z != 0 {
			t.Errorf("ToXYZ = %v,%v,%v, want 0,0,0", X, Y, Z)
		}
	})

	t.Run("reverse", func(t *testing.T) {
		p := &Profile{
			ColorSpace: CMYKSpace, // 4 channels, to catch a stale trailing channel
			PCS:        PCSXYZSpace,
			TagData:    map[TagType][]byte{BToA0: badLUTTag()},
		}
		tr, err := NewTransform(p, Perceptual)
		if err != nil {
			t.Fatal(err)
		}
		if !tr.CanFromXYZ() {
			t.Fatal("CanFromXYZ = false, want true (tag present)")
		}
		dst := []float64{1, 2, 3, 4}
		tr.FromXYZ(0.3, 0.4, 0.5, dst, &Workspace{})
		for i, v := range dst {
			if v != 0 {
				t.Errorf("dst[%d] = %v, want 0 (all device channels)", i, v)
			}
		}
	})
}

// identityLUT16Tag builds a lut16Type tag whose CLUT is the identity on the
// unit cube.  Encode fills the input and output tables with identity ramps, so
// the whole tag passes normalised values through unchanged.
func identityLUT16Tag() []byte {
	const grid = 2
	clut := make([]float32, grid*grid*grid*3)
	for r := range grid {
		for g := range grid {
			for b := range grid {
				// the first channel varies slowest
				base := ((r*grid+g)*grid + b) * 3
				clut[base+0] = float32(r)
				clut[base+1] = float32(g)
				clut[base+2] = float32(b)
			}
		}
	}
	lut := &LUT16{nInput: 3, nOutput: 3, gridPoints: grid, clut: clut}
	data, err := lut.Encode()
	if err != nil {
		panic(err)
	}
	return data
}

// TestPCSXYZEncoding checks that the PCS XYZ values a LUT tag exchanges are
// read in the 16-bit PCSXYZ encoding, a u1Fixed15Number in which 0x8000 stands
// for 1.0 and the top of the range for 1 + 32767/32768.
//
// The tag is an identity, so the normalised value encoding the PCS white point
// must come back out as the white point itself.  Read as a plain [0,1]
// fraction instead, the same table gives half of every value, and the reverse
// direction asks for twice the value it should and clips at the top of the
// input table.
func TestPCSXYZEncoding(t *testing.T) {
	p := &Profile{
		ColorSpace: RGBSpace,
		PCS:        PCSXYZSpace,
		TagData: map[TagType][]byte{
			AToB1: identityLUT16Tag(),
			BToA1: identityLUT16Tag(),
		},
	}
	tr, err := NewTransform(p, RelativeColorimetric)
	if err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{}

	// the identity CLUT and the 16-bit tables are all exact at these values,
	// so the tolerance covers float rounding only
	const tol = 1e-9
	encoded := []float64{
		PCSWhitePoint[0] / pcsXYZScale,
		PCSWhitePoint[1] / pcsXYZScale,
		PCSWhitePoint[2] / pcsXYZScale,
	}

	X, Y, Z := tr.ToXYZ(encoded, ws)
	if math.Abs(X-PCSWhitePoint[0]) > tol ||
		math.Abs(Y-PCSWhitePoint[1]) > tol ||
		math.Abs(Z-PCSWhitePoint[2]) > tol {
		t.Errorf("encoded white -> XYZ (%g, %g, %g), want %v", X, Y, Z, PCSWhitePoint)
	}

	dst := make([]float64, 3)
	tr.FromXYZ(PCSWhitePoint[0], PCSWhitePoint[1], PCSWhitePoint[2], dst, ws)
	for i, v := range dst {
		if math.Abs(v-encoded[i]) > tol {
			t.Errorf("PCS white -> channel %d = %g, want %g", i, v, encoded[i])
		}
	}
}

// TestLegacyLabClipping checks that values which the legacy 16-bit PCS Lab
// encoding can represent but the current one cannot are clipped per component,
// as ICC.1:2022 §10.8 requires.
//
// The identity tag turns device (1, 1, 1) into the top of the table, 0xFFFF in
// every channel.  Scaled by 65535/65280 and left unclipped that decodes to
// L* = 100.39 and a* = b* = 127.996, beyond the current encoding's range.
func TestLegacyLabClipping(t *testing.T) {
	p := &Profile{
		ColorSpace: RGBSpace,
		PCS:        PCSLabSpace,
		TagData:    map[TagType][]byte{AToB1: identityLUT16Tag()},
	}
	tr, err := NewTransform(p, RelativeColorimetric)
	if err != nil {
		t.Fatal(err)
	}

	X, Y, Z := tr.ToXYZ([]float64{1, 1, 1}, &Workspace{})
	L, a, b := xyzToLab(X, Y, Z)

	const tol = 1e-9
	if math.Abs(L-100) > tol || math.Abs(a-127) > tol || math.Abs(b-127) > tol {
		t.Errorf("Lab = (%g, %g, %g), want (100, 127, 127)", L, a, b)
	}
}

// Gamma is encoded as a u8Fixed8Number (ICC.1:2022 §4.9): round to the nearest
// 1/256 and clamp to the representable range [0, 255 + 255/256].
func TestEncodeGammaRound(t *testing.T) {
	tests := []struct {
		name  string
		gamma float64
		want  uint16
	}{
		{"rounds up", 100.7 / 256.0, 101}, // 100.7 -> 101, not 100
		{"common 2.2", 2.2, 563},          // 563.2 -> 563
		{"largest", 65535.4 / 256.0, 0xFFFF},
		{"zero", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, err := (&Curve{Gamma: tc.gamma}).Encode()
			if err != nil {
				t.Fatal(err)
			}
			if len(buf) != 14 {
				t.Fatalf("encoded length = %d, want 14 (gamma curveType)", len(buf))
			}
			if got := getUint16(buf, 12); got != tc.want {
				t.Errorf("encoded gamma = %d, want %d", got, tc.want)
			}
		})
	}
}
