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
	"sort"
)

// Curve represents a 1D transfer function (TRC) used in ICC profiles.
// It can represent either an ICC curveType or parametricCurveType.
//
// A Curve is not safe for concurrent use. If the same Curve needs to be
// used from multiple goroutines, callers must provide their own synchronisation.
//
// Precedence when evaluating: Table > Params > Gamma.
//
// To create a curve:
//   - Gamma curve (curveType): set Gamma only (e.g. &Curve{Gamma: 2.2})
//   - Sampled curve (curveType): set Table only
//   - Parametric curve (parametricCurveType): set FuncType and Params
type Curve struct {
	// Gamma specifies the exponent for a simple gamma curve (curveType with
	// n=1). The curve computes y = x^Gamma. Set to 1.0 for an identity curve
	// (encoded as curveType with n=0). Ignored if Params or Table is set.
	Gamma float64

	// FuncType and Params define an ICC parametricCurveType. FuncType selects
	// the ICC function type (0-4) and Params provides the coefficients
	// [g, a, b, c, d, e, f]:
	//   - type 0: y = x^g
	//   - type 1: y = (ax+b)^g for x >= -b/a, else y = 0
	//   - type 2: y = (ax+b)^g + c for x >= -b/a, else y = c
	//   - type 3: y = (ax+b)^g for x >= d, else y = cx
	//   - type 4: y = (ax+b)^g + e for x >= d, else y = cx + f
	FuncType int
	Params   []float64 // [g], [g,a,b], [g,a,b,c], [g,a,b,c,d], or [g,a,b,c,d,e,f]

	// Table specifies a sampled curve (curveType with n>1). Values are evenly
	// spaced from input 0 to 1, with linear interpolation between samples.
	Table []uint16

	// cached inverse table for sampled curves
	inverseTable []float64
}

// DecodeCurve decodes a curve from ICC tag data.
// The data must be a curveType or parametricCurveType element.
func DecodeCurve(data []byte) (*Curve, error) {
	if len(data) < 8 {
		return nil, errInvalidTagData
	}

	typeID := string(data[0:4])
	switch typeID {
	case "curv":
		return decodeCurveType(data)
	case "para":
		return decodeParametricCurve(data)
	default:
		return nil, errUnexpectedType
	}
}

func decodeCurveType(data []byte) (*Curve, error) {
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
	table := make([]uint16, n)
	for i := range table {
		table[i] = getUint16(data, 12+i*2)
	}
	return &Curve{Table: table}, nil
}

func decodeParametricCurve(data []byte) (*Curve, error) {
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

	params := make([]float64, numParams)
	for i := range params {
		params[i] = getS15Fixed16(data, 12+i*4)
	}

	return &Curve{
		FuncType: funcType,
		Params:   params,
	}, nil
}

// Evaluate computes the output value for an input value x in [0, 1].
// The output is clamped to [0, 1] as required by the ICC specification.
func (c *Curve) Evaluate(x float64) float64 {
	x = clamp(x, 0, 1)

	var y float64

	// gamma-only curve
	if c.Gamma != 0 && c.Params == nil && c.Table == nil {
		if x <= 0 {
			y = 0
		} else {
			y = math.Pow(x, c.Gamma)
		}
	} else if c.Params != nil {
		// parametric curve
		y = c.evaluateParametric(x)
	} else if c.Table != nil {
		// sampled curve
		y = c.evaluateSampled(x)
	} else {
		// identity
		y = x
	}

	return clamp(y, 0, 1)
}

func (c *Curve) evaluateParametric(x float64) float64 {
	g := c.Params[0]

	switch c.FuncType {
	case 0:
		// y = x^g
		if x <= 0 {
			return 0
		}
		return math.Pow(x, g)

	case 1:
		// y = (ax+b)^g for x >= -b/a, else y = 0
		a, b := c.Params[1], c.Params[2]
		threshold := -b / a
		if x >= threshold {
			v := a*x + b
			if v <= 0 {
				return 0
			}
			return math.Pow(v, g)
		}
		return 0

	case 2:
		// y = (ax+b)^g + c for x >= -b/a, else y = c
		a, b, cc := c.Params[1], c.Params[2], c.Params[3]
		threshold := -b / a
		if x >= threshold {
			v := a*x + b
			if v <= 0 {
				return cc
			}
			return math.Pow(v, g) + cc
		}
		return cc

	case 3:
		// y = (ax+b)^g for x >= d, else y = cx
		a, b, cc, d := c.Params[1], c.Params[2], c.Params[3], c.Params[4]
		if x >= d {
			v := a*x + b
			if v <= 0 {
				return 0
			}
			return math.Pow(v, g)
		}
		return cc * x

	case 4:
		// y = (ax+b)^g + e for x >= d, else y = cx + f
		a, b, cc, d, e, f := c.Params[1], c.Params[2], c.Params[3], c.Params[4], c.Params[5], c.Params[6]
		if x >= d {
			v := a*x + b
			if v <= 0 {
				return e
			}
			return math.Pow(v, g) + e
		}
		return cc*x + f
	}

	return x
}

func (c *Curve) evaluateSampled(x float64) float64 {
	n := len(c.Table)
	if n == 0 {
		return x
	}
	if n == 1 {
		return float64(c.Table[0]) / 65535.0
	}

	// linear interpolation
	pos := x * float64(n-1)
	idx := int(pos)
	if idx < 0 {
		return float64(c.Table[0]) / 65535.0
	}
	if idx >= n-1 {
		return float64(c.Table[n-1]) / 65535.0
	}

	frac := pos - float64(idx)
	v0 := float64(c.Table[idx]) / 65535.0
	v1 := float64(c.Table[idx+1]) / 65535.0
	return v0 + frac*(v1-v0)
}

// Invert computes the input value for an output value y in [0, 1].
// This is the inverse of Evaluate.
func (c *Curve) Invert(y float64) float64 {
	y = clamp(y, 0, 1)

	// gamma-only curve
	if c.Gamma != 0 && c.Params == nil && c.Table == nil {
		if y <= 0 {
			return 0
		}
		return math.Pow(y, 1.0/c.Gamma)
	}

	// parametric curve
	if c.Params != nil {
		return c.invertParametric(y)
	}

	// sampled curve
	if c.Table != nil {
		return c.invertSampled(y)
	}

	// identity
	return y
}

func (c *Curve) invertParametric(y float64) float64 {
	g := c.Params[0]
	if g == 0 {
		return 0
	}
	invG := 1.0 / g

	switch c.FuncType {
	case 0:
		// y = x^g => x = y^(1/g)
		if y <= 0 {
			return 0
		}
		return math.Pow(y, invG)

	case 1:
		// y = (ax+b)^g => x = (y^(1/g) - b) / a
		a, b := c.Params[1], c.Params[2]
		if a == 0 {
			return 0
		}
		if y <= 0 {
			return -b / a
		}
		return (math.Pow(y, invG) - b) / a

	case 2:
		// y = (ax+b)^g + c => x = ((y-c)^(1/g) - b) / a
		a, b, cc := c.Params[1], c.Params[2], c.Params[3]
		if a == 0 {
			return 0
		}
		yc := y - cc
		if yc <= 0 {
			return -b / a
		}
		return (math.Pow(yc, invG) - b) / a

	case 3:
		// y = (ax+b)^g for x >= d, else y = cx
		a, b, cc, d := c.Params[1], c.Params[2], c.Params[3], c.Params[4]
		// threshold output is at cc*d
		yThreshold := cc * d
		if y < yThreshold {
			if cc == 0 {
				return 0
			}
			return y / cc
		}
		if a == 0 {
			return d
		}
		if y <= 0 {
			return d
		}
		return (math.Pow(y, invG) - b) / a

	case 4:
		// y = (ax+b)^g + e for x >= d, else y = cx + f
		a, b, cc, d, e, f := c.Params[1], c.Params[2], c.Params[3], c.Params[4], c.Params[5], c.Params[6]
		// threshold output is at cc*d + f
		yThreshold := cc*d + f
		if y < yThreshold {
			if cc == 0 {
				return 0
			}
			return (y - f) / cc
		}
		if a == 0 {
			return d
		}
		ye := y - e
		if ye <= 0 {
			return d
		}
		return (math.Pow(ye, invG) - b) / a
	}

	return y
}

func (c *Curve) invertSampled(y float64) float64 {
	// build inverse LUT on first use
	if c.inverseTable == nil {
		c.buildInverseTable()
	}

	n := len(c.inverseTable)
	if n == 0 {
		return y
	}

	// linear interpolation in inverse table
	pos := y * float64(n-1)
	idx := int(pos)
	if idx < 0 {
		return c.inverseTable[0]
	}
	if idx >= n-1 {
		return c.inverseTable[n-1]
	}

	frac := pos - float64(idx)
	return c.inverseTable[idx] + frac*(c.inverseTable[idx+1]-c.inverseTable[idx])
}

func (c *Curve) buildInverseTable() {
	const invSize = 4096
	c.inverseTable = make([]float64, invSize)

	n := len(c.Table)
	if n == 0 {
		for i := range c.inverseTable {
			c.inverseTable[i] = float64(i) / float64(invSize-1)
		}
		return
	}

	// for each output value, find the corresponding input using binary search
	for i := range c.inverseTable {
		target := uint16(float64(i) / float64(invSize-1) * 65535.0)

		// find smallest index where Table[idx] >= target
		idx := sort.Search(n, func(j int) bool {
			return c.Table[j] >= target
		})

		if idx == 0 {
			// target is at or below the minimum output
			c.inverseTable[i] = 0
		} else if idx >= n {
			// target is above the maximum output
			c.inverseTable[i] = 1
		} else {
			// linear interpolation between idx-1 and idx
			v0 := float64(c.Table[idx-1])
			v1 := float64(c.Table[idx])
			if v1 == v0 {
				c.inverseTable[i] = float64(idx) / float64(n-1)
			} else {
				frac := (float64(target) - v0) / (v1 - v0)
				c.inverseTable[i] = (float64(idx-1) + frac) / float64(n-1)
			}
		}
	}
}

// IsIdentity returns true if the curve represents an identity function.
func (c *Curve) IsIdentity() bool {
	if c.Gamma == 1.0 && c.Params == nil && c.Table == nil {
		return true
	}
	if c.Params != nil && c.FuncType == 0 && c.Params[0] == 1.0 {
		return true
	}
	return false
}

// Encode converts the curve to ICC tag data.
// The result is either a curveType or parametricCurveType element.
func (c *Curve) Encode() []byte {
	if c.Params != nil {
		return c.encodeParametric()
	}
	return c.encodeCurveType()
}

func (c *Curve) encodeCurveType() []byte {
	if c.Table != nil {
		// sampled curve
		n := len(c.Table)
		buf := make([]byte, 12+n*2)
		copy(buf[0:4], "curv")
		putUint32(buf, 8, uint32(n))
		for i, v := range c.Table {
			putUint16(buf, 12+i*2, v)
		}
		return buf
	}

	if c.Gamma == 1.0 {
		// identity curve (n=0)
		buf := make([]byte, 12)
		copy(buf[0:4], "curv")
		return buf
	}

	// gamma curve (n=1)
	buf := make([]byte, 14)
	copy(buf[0:4], "curv")
	putUint32(buf, 8, 1)
	// encode gamma as u8Fixed8Number
	gamma := uint16(c.Gamma * 256.0)
	putUint16(buf, 12, gamma)
	return buf
}

func (c *Curve) encodeParametric() []byte {
	var numParams int
	switch c.FuncType {
	case 0:
		numParams = 1
	case 1:
		numParams = 3
	case 2:
		numParams = 4
	case 3:
		numParams = 5
	case 4:
		numParams = 7
	default:
		numParams = len(c.Params)
	}

	buf := make([]byte, 12+numParams*4)
	copy(buf[0:4], "para")
	putUint16(buf, 8, uint16(c.FuncType))
	for i := 0; i < numParams && i < len(c.Params); i++ {
		putS15Fixed16(buf, 12+i*4, c.Params[i])
	}
	return buf
}

func putUint16(data []byte, offset int, value uint16) {
	data[offset] = byte(value >> 8)
	data[offset+1] = byte(value)
}

func putS15Fixed16(data []byte, offset int, value float64) {
	raw := int32(value * 65536.0)
	putUint32(data, offset, uint32(raw))
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
