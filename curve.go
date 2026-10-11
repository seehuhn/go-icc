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
	"errors"
	"fmt"
	"math"
	"sort"

	"seehuhn.de/go/membudget"
)

// Curve represents a 1D transfer function (TRC) used in ICC profiles.
//
// A Curve is safe for concurrent use.
type Curve struct {
	// Gamma specifies the exponent for a simple gamma curve, y = x^Gamma.
	// The zero value gives the constant curve y = 1.
	//
	// This value is ignored if either Params or Table is set.
	Gamma float64

	// Params defines an ICC parametricCurveType.  The number of coefficients
	// determines the ICC function type and the meaning of the coefficients
	// [g, a, b, c, d, e, f]:
	//
	//   - 1 param  (type 0): y = x^g
	//   - 3 params (type 1): y = (ax+b)^g for x >= -b/a, else y = 0
	//   - 4 params (type 2): y = (ax+b)^g + c for x >= -b/a, else y = c
	//   - 5 params (type 3): y = (ax+b)^g for x >= d, else y = cx
	//   - 7 params (type 4): y = (ax+b)^g + e for x >= d, else y = cx + f
	//
	// As for Gamma, 0^0 is taken to be 1, so g = 0 makes the power term
	// equal to 1 wherever it applies.
	//
	// This field is ignored if Table is set.
	Params []float64

	// Table specifies a sampled curve.  Values are evenly spaced from input 0
	// to 1, with linear interpolation between samples.
	// A non-nil Table must have at least two entries.
	Table []uint16
}

// curveKind identifies which field of a [Curve] defines the curve.
type curveKind int

const (
	curveGamma curveKind = iota
	curveParametric
	curveSampled
)

// kind returns the field which defines the curve.
// Table takes precedence over Params, and Params over Gamma.
func (c *Curve) kind() curveKind {
	switch {
	case c.Table != nil:
		return curveSampled
	case c.Params != nil:
		return curveParametric
	default:
		return curveGamma
	}
}

// DecodeCurve decodes a curve from ICC tag data.
// This is used for the RedTRC, GreenTRC, BlueTRC, and GrayTRC tags.
// The data must be a curveType or parametricCurveType element.
func DecodeCurve(data []byte) (*Curve, error) {
	return decodeCurve(newBudget(int64(len(data))), data)
}

func decodeCurve(b *membudget.Budget, data []byte) (*Curve, error) {
	if len(data) < 8 {
		return nil, errInvalidTagData
	}

	typeID := string(data[0:4])
	switch typeID {
	case "curv":
		return decodeCurveType(b, data)
	case "para":
		return decodeParametricCurve(b, data)
	default:
		return nil, errUnexpectedType
	}
}

func decodeCurveType(b *membudget.Budget, data []byte) (*Curve, error) {
	if len(data) < 12 {
		return nil, errInvalidTagData
	}

	n := getUint32(data, 8)
	if n == 0 {
		// identity curve: y = x (gamma = 1.0)
		return &Curve{Gamma: 1.0}, nil
	}
	if n == 1 {
		if len(data) < 14 {
			return nil, errInvalidTagData
		}
		// gamma encoded as u8Fixed8Number
		gamma := float64(getUint16(data, 12)) / 256.0
		return &Curve{Gamma: gamma}, nil
	}

	// sampled curve
	if uint64(len(data)) < 12+2*uint64(n) {
		return nil, errInvalidTagData
	}
	table, err := b.AllocSlice[uint16](int(n))
	if err != nil {
		return nil, err
	}
	for i := range table {
		table[i] = getUint16(data, 12+i*2)
	}
	return &Curve{Table: table}, nil
}

func decodeParametricCurve(b *membudget.Budget, data []byte) (*Curve, error) {
	if len(data) < 12 {
		return nil, errInvalidTagData
	}

	funcType := int(getUint16(data, 8))
	// reserved bytes at offset 10-11

	var numParams int
	switch funcType {
	case 0:
		numParams = 1 // g
	case 1:
		numParams = 3 // g, a, b
	case 2:
		numParams = 4 // g, a, b, c
	case 3:
		numParams = 5 // g, a, b, c, d
	case 4:
		numParams = 7 // g, a, b, c, d, e, f
	default:
		return nil, errInvalidTagData
	}

	if len(data) < 12+numParams*4 {
		return nil, errInvalidTagData
	}

	params, err := b.AllocSlice[float64](numParams)
	if err != nil {
		return nil, err
	}
	for i := range params {
		params[i] = getS15Fixed16(data, 12+i*4)
	}

	return &Curve{
		Params: params,
	}, nil
}

// Evaluate computes the output value for an input value x in [0, 1].
// The output is clamped to [0, 1].
func (c *Curve) Evaluate(x float64) float64 {
	x = clamp(x, 0, 1)

	var y float64
	switch c.kind() {
	case curveSampled:
		y = c.evaluateSampled(x)
	case curveParametric:
		y = c.evaluateParametric(x)
	default:
		y = math.Pow(x, c.Gamma)
	}

	return clamp(y, 0, 1)
}

func (c *Curve) evaluateSampled(x float64) float64 {
	n := len(c.Table)

	// linear interpolation
	pos := x * float64(n-1)
	idx := int(pos)
	if idx >= n-1 {
		return float64(c.Table[n-1]) / 65535.0
	}

	frac := pos - float64(idx)
	v0 := float64(c.Table[idx]) / 65535.0
	v1 := float64(c.Table[idx+1]) / 65535.0
	return v0 + frac*(v1-v0)
}

func (c *Curve) evaluateParametric(x float64) float64 {
	g := c.Params[0]

	switch len(c.Params) {
	case 1:
		// type 0: y = x^g
		return math.Pow(x, g)

	case 3:
		// type 1: y = (ax+b)^g for x >= -b/a, else y = 0
		a, b := c.Params[1], c.Params[2]
		threshold := -b / a
		if x >= threshold {
			v := a*x + b
			if v < 0 {
				return 0
			}
			return math.Pow(v, g)
		}
		return 0

	case 4:
		// type 2: y = (ax+b)^g + c for x >= -b/a, else y = c
		a, b, cc := c.Params[1], c.Params[2], c.Params[3]
		threshold := -b / a
		if x >= threshold {
			v := a*x + b
			if v < 0 {
				return cc
			}
			return math.Pow(v, g) + cc
		}
		return cc

	case 5:
		// type 3: y = (ax+b)^g for x >= d, else y = cx
		a, b, cc, d := c.Params[1], c.Params[2], c.Params[3], c.Params[4]
		if x >= d {
			v := a*x + b
			if v < 0 {
				return 0
			}
			return math.Pow(v, g)
		}
		return cc * x

	case 7:
		// type 4: y = (ax+b)^g + e for x >= d, else y = cx + f
		a, b, cc, d, e, f := c.Params[1], c.Params[2], c.Params[3], c.Params[4], c.Params[5], c.Params[6]
		if x >= d {
			v := a*x + b
			if v < 0 {
				return e
			}
			return math.Pow(v, g) + e
		}
		return cc*x + f
	}

	return x
}

// Invert returns an input value x in [0, 1] for an output value y.
// For an increasing curve, x is the smallest input with Evaluate(x) >= y,
// or 1 if there is none.  For a decreasing sampled curve, x is the smallest
// input with Evaluate(x) <= y.  Where Evaluate is one-to-one, Invert is its
// inverse.
func (c *Curve) Invert(y float64) float64 {
	y = clamp(y, 0, 1)

	var x float64
	switch c.kind() {
	case curveSampled:
		x = c.invertSampled(y)
	case curveParametric:
		x = c.invertParametric(y)
	default:
		x = root(y, c.Gamma)
	}

	return clamp(x, 0, 1)
}

func (c *Curve) invertSampled(y float64) float64 {
	tab := c.Table
	n := len(tab)
	target := y * 65535

	// sort.Search returns an index where the predicate is false just before
	// and true at, even for a table which is not monotone, so the
	// interpolations below never divide by zero.
	if tab[n-1] >= tab[0] {
		idx := sort.Search(n, func(j int) bool { return float64(tab[j]) >= target })
		if idx == 0 {
			return 0
		}
		if idx == n {
			return 1
		}
		v0, v1 := float64(tab[idx-1]), float64(tab[idx])
		return (float64(idx-1) + (target-v0)/(v1-v0)) / float64(n-1)
	}

	idx := sort.Search(n, func(j int) bool { return float64(tab[j]) <= target })
	if idx == 0 {
		return 0
	}
	if idx == n {
		return 1
	}
	v0, v1 := float64(tab[idx-1]), float64(tab[idx])
	return (float64(idx-1) + (v0-target)/(v0-v1)) / float64(n-1)
}

func (c *Curve) invertParametric(y float64) float64 {
	p := c.Params
	g := p[0]

	switch len(p) {
	case 1:
		// type 0: y = x^g
		return root(y, g)

	case 3, 4:
		// types 1 and 2: y = (ax+b)^g + c for x >= -b/a, else y = c
		a, b := p[1], p[2]
		var cc float64
		if len(p) == 4 {
			cc = p[3]
		}
		yc := y - cc
		if yc <= 0 {
			return 0
		}
		if a == 0 {
			// the curve is constant
			if c.evaluateParametric(0) >= y {
				return 0
			}
			return 1
		}
		return (root(yc, g) - b) / a

	case 5, 7:
		// types 3 and 4: y = (ax+b)^g + e for x >= d, else y = cx + f
		a, b, cc, d := p[1], p[2], p[3], p[4]
		var e, f float64
		if len(p) == 7 {
			e, f = p[5], p[6]
		}
		if d > 0 && y <= cc*d+f {
			// the linear segment reaches y
			if cc <= 0 {
				return 0
			}
			return (y - f) / cc
		}
		// The power segment only applies from d onwards; this also covers
		// a jump at d, where the curve passes y without taking the value.
		ye := y - e
		if ye <= 0 {
			return d
		}
		if a == 0 {
			// the power segment is constant
			if c.evaluateParametric(max(d, 0)) >= y {
				return d
			}
			return 1
		}
		return max(d, (root(ye, g)-b)/a)
	}

	return y
}

// root inverts the power function v -> v^g, returning t^(1/g).
// For g = 0, where v^g = 1 for every v, it returns the smallest v with
// v^g >= t: 0 if t <= 1, and +Inf otherwise.
func root(t, g float64) float64 {
	if g == 0 {
		if t <= 1 {
			return 0
		}
		return math.Inf(1)
	}
	return math.Pow(t, 1/g)
}

// IsIdentity reports whether the curve is the identity function.
// Parametric curves are recognised only in their type 0 form.
func (c *Curve) IsIdentity() bool {
	switch c.kind() {
	case curveSampled:
		n := len(c.Table)
		for i, v := range c.Table {
			if uint64(v)*uint64(n-1) != uint64(i)*65535 {
				return false
			}
		}
		return true
	case curveParametric:
		return len(c.Params) == 1 && c.Params[0] == 1.0
	default:
		return c.Gamma == 1.0
	}
}

// Encode converts the curve to ICC tag data.
// The result is either a curveType element or parametricCurveType element.
// An error is returned if Table is non-nil with fewer than two entries, if
// Params has a length other than 1, 3, 4, 5 or 7, or if a value cannot be
// represented in the ICC encoding: Gamma must lie in [0, 256) and the
// coefficients in Params in [-32768, 32768), after rounding to the precision
// of the encoding.
func (c *Curve) Encode() ([]byte, error) {
	switch c.kind() {
	case curveSampled:
		return c.encodeSampled()
	case curveParametric:
		return c.encodeParametric()
	default:
		return c.encodeGamma()
	}
}

func (c *Curve) encodeSampled() ([]byte, error) {
	n := len(c.Table)
	if n < 2 {
		return nil, errors.New("sampled curve needs at least two table entries")
	}
	buf := make([]byte, 12+n*2)
	copy(buf[0:4], "curv")
	putUint32(buf, 8, uint32(n))
	for i, v := range c.Table {
		putUint16(buf, 12+i*2, v)
	}
	return buf, nil
}

func (c *Curve) encodeGamma() ([]byte, error) {
	if c.Gamma == 1.0 {
		// identity curve (n=0)
		buf := make([]byte, 12)
		copy(buf[0:4], "curv")
		return buf, nil
	}

	// gamma curve (n=1), as a u8Fixed8Number
	gamma := math.Round(c.Gamma * 256.0)
	if !(gamma >= 0 && gamma <= 0xFFFF) {
		return nil, fmt.Errorf("gamma %g outside the range [0, 256)", c.Gamma)
	}
	buf := make([]byte, 14)
	copy(buf[0:4], "curv")
	putUint32(buf, 8, 1)
	putUint16(buf, 12, uint16(gamma))
	return buf, nil
}

func (c *Curve) encodeParametric() ([]byte, error) {
	numParams := len(c.Params)
	var funcType uint16
	switch numParams {
	case 1:
		funcType = 0
	case 3:
		funcType = 1
	case 4:
		funcType = 2
	case 5:
		funcType = 3
	case 7:
		funcType = 4
	default:
		return nil, fmt.Errorf("invalid number of parametric curve coefficients: %d", numParams)
	}

	for _, v := range c.Params {
		if !s15Fixed16InRange(v) {
			return nil, fmt.Errorf("parametric curve coefficient %g out of range", v)
		}
	}

	buf := make([]byte, 12+numParams*4)
	copy(buf[0:4], "para")
	putUint16(buf, 8, funcType)
	for i, v := range c.Params {
		putS15Fixed16(buf, 12+i*4, v)
	}
	return buf, nil
}

func putUint16(data []byte, offset int, value uint16) {
	data[offset] = byte(value >> 8)
	data[offset+1] = byte(value)
}

// putS15Fixed16 writes value, rounded to the nearest multiple of 1/65536.
// The caller must ensure that [s15Fixed16InRange] holds.
func putS15Fixed16(data []byte, offset int, value float64) {
	raw := int32(math.Round(value * 65536.0))
	putUint32(data, offset, uint32(raw))
}

// s15Fixed16InRange reports whether value can be written by [putS15Fixed16].
func s15Fixed16InRange(value float64) bool {
	raw := math.Round(value * 65536.0)
	return raw >= math.MinInt32 && raw <= math.MaxInt32
}

func getUint16(data []byte, offset int) uint16 {
	return uint16(data[offset])<<8 | uint16(data[offset+1])
}

func getS15Fixed16(data []byte, offset int) float64 {
	raw := int32(getUint32(data, offset))
	return float64(raw) / 65536.0
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// quantise8 converts a normalised value to an 8-bit sample, clamping to
// [0, 1] and rounding to the nearest representable value.
func quantise8(v float64) byte {
	return byte(clamp(v, 0, 1)*255.0 + 0.5)
}

// quantise16 converts a normalised value to a 16-bit sample, clamping to
// [0, 1] and rounding to the nearest representable value.
func quantise16(v float64) uint16 {
	return uint16(clamp(v, 0, 1)*65535.0 + 0.5)
}
