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
	"math"
)

// Direction specifies the direction of a colour transformation.
type Direction int

const (
	// DeviceToPCS converts from device colour space to Profile Connection Space.
	DeviceToPCS Direction = iota
	// PCSToDevice converts from Profile Connection Space to device colour space.
	PCSToDevice
)

// Transform performs colour conversions using an ICC profile.
//
// Create a Transform using [NewTransform], then use [Transform.ToXYZ] or
// [Transform.FromXYZ] to convert colours. The Transform supports matrix/TRC
// profiles (common for displays), grayscale profiles, and LUT-based profiles
// (common for printers).
//
// A Transform is read-only once created: [Transform.ToXYZ], [Transform.FromXYZ],
// [Transform.Apply], and [Transform.FromXYZInto] may be called concurrently from
// multiple goroutines, provided each goroutine uses its own [Workspace] (or nil).
type Transform struct {
	profile   *Profile
	direction Direction
	intent    RenderingIntent

	// profile type determines which fields are used
	profileType profileType

	// for matrix/TRC profiles (RGB)
	matrix    []float64 // 3x3 matrix: device RGB to XYZ
	matrixInv []float64 // inverse matrix: XYZ to device RGB
	trc       [3]*Curve // R, G, B TRCs
	trcInv    [3]*Curve // inverted TRCs (only for PCSToDevice)

	// for gray TRC profiles
	grayTRC    *Curve
	grayTRCInv *Curve

	// for LUT-based profiles
	lut Lut

	// white point for chromatic adaptation
	whitePoint [3]float64 // XYZ of media white point
}

type profileType int

const (
	profileTypeUnknown profileType = iota
	profileTypeMatrixTRC
	profileTypeGrayTRC
	profileTypeLut
)

// NewTransform creates a colour transform from an ICC profile.
//
// The direction specifies whether to convert from device colours to PCS
// ([DeviceToPCS]) or from PCS to device colours ([PCSToDevice]).
// The intent selects which rendering intent to use for LUT-based profiles.
//
// After creating the transform, use [Transform.ToXYZ] or [Transform.FromXYZ]
// to convert colours.
func NewTransform(p *Profile, dir Direction, intent RenderingIntent) (*Transform, error) {
	t := &Transform{
		profile:   p,
		direction: dir,
		intent:    intent,
	}

	// detect profile type
	t.profileType = detectProfileType(p)

	switch t.profileType {
	case profileTypeMatrixTRC:
		if err := t.initMatrixTRC(); err != nil {
			return nil, err
		}
	case profileTypeGrayTRC:
		if err := t.initGrayTRC(); err != nil {
			return nil, err
		}
	case profileTypeLut:
		if err := t.initLut(); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("icc: unsupported profile type")
	}

	// parse white point if available
	if data, ok := p.TagData[MediaWhitePoint]; ok {
		t.parseWhitePoint(data)
	} else {
		// default D50 white point
		t.whitePoint = d50WhitePoint
	}

	return t, nil
}

func detectProfileType(p *Profile) profileType {
	// check for LUT-based profile (takes precedence)
	if _, ok := p.TagData[AToB0]; ok {
		return profileTypeLut
	}
	if _, ok := p.TagData[AToB1]; ok {
		return profileTypeLut
	}
	if _, ok := p.TagData[AToB2]; ok {
		return profileTypeLut
	}
	if _, ok := p.TagData[BToA0]; ok {
		return profileTypeLut
	}
	if _, ok := p.TagData[BToA1]; ok {
		return profileTypeLut
	}
	if _, ok := p.TagData[BToA2]; ok {
		return profileTypeLut
	}

	// check for matrix/TRC profile
	_, hasRXYZ := p.TagData[RedMatrixColumn]
	_, hasGXYZ := p.TagData[GreenMatrixColumn]
	_, hasBXYZ := p.TagData[BlueMatrixColumn]
	_, hasRTRC := p.TagData[RedTRC]
	_, hasGTRC := p.TagData[GreenTRC]
	_, hasBTRC := p.TagData[BlueTRC]
	if hasRXYZ && hasGXYZ && hasBXYZ && hasRTRC && hasGTRC && hasBTRC {
		return profileTypeMatrixTRC
	}

	// check for gray TRC profile
	if _, ok := p.TagData[GrayTRC]; ok {
		return profileTypeGrayTRC
	}

	return profileTypeUnknown
}

func (t *Transform) initMatrixTRC() error {
	p := t.profile

	// parse matrix columns
	rXYZ, err := parseXYZ(p.TagData[RedMatrixColumn])
	if err != nil {
		return err
	}
	gXYZ, err := parseXYZ(p.TagData[GreenMatrixColumn])
	if err != nil {
		return err
	}
	bXYZ, err := parseXYZ(p.TagData[BlueMatrixColumn])
	if err != nil {
		return err
	}

	// build 3x3 matrix (columns are the XYZ values)
	t.matrix = []float64{
		rXYZ[0], gXYZ[0], bXYZ[0],
		rXYZ[1], gXYZ[1], bXYZ[1],
		rXYZ[2], gXYZ[2], bXYZ[2],
	}

	// compute inverse matrix only when needed
	if t.direction == PCSToDevice {
		t.matrixInv = invertMatrix3x3(t.matrix)
		if t.matrixInv == nil {
			return errors.New("icc: singular colour matrix")
		}
	}

	// parse TRCs
	rTRC, err := DecodeCurve(p.TagData[RedTRC])
	if err != nil {
		return err
	}
	gTRC, err := DecodeCurve(p.TagData[GreenTRC])
	if err != nil {
		return err
	}
	bTRC, err := DecodeCurve(p.TagData[BlueTRC])
	if err != nil {
		return err
	}

	t.trc = [3]*Curve{rTRC, gTRC, bTRC}
	t.trcInv = [3]*Curve{rTRC, gTRC, bTRC} // same curves used for inversion

	// pre-build the inverse tables so that a Transform shared across goroutines
	// can run FromXYZ/FromXYZInto concurrently without lazily mutating Curve state
	if t.direction == PCSToDevice {
		for _, c := range t.trcInv {
			c.prepareInverse()
		}
	}

	return nil
}

func (t *Transform) initGrayTRC() error {
	p := t.profile

	grayTRC, err := DecodeCurve(p.TagData[GrayTRC])
	if err != nil {
		return err
	}

	t.grayTRC = grayTRC
	t.grayTRCInv = grayTRC

	// pre-build the inverse table so that a Transform shared across goroutines
	// can run FromXYZ/FromXYZInto concurrently without lazily mutating Curve state
	if t.direction == PCSToDevice {
		t.grayTRCInv.prepareInverse()
	}

	return nil
}

func (t *Transform) initLut() error {
	p := t.profile

	// preferred-first ordering: try the tag for the requested intent, then
	// the perceptual tag (ICC default fallback), then the remaining one.
	chain := lutFallbackChain(t.direction, t.intent)

	var data []byte
	for _, tagType := range chain {
		if d, ok := p.TagData[tagType]; ok {
			data = d
			break
		}
	}
	if data == nil {
		return errors.New("icc: missing LUT tag")
	}

	lut, err := DecodeLut(data)
	if err != nil {
		return err
	}

	t.lut = lut
	return nil
}

func lutFallbackChain(dir Direction, intent RenderingIntent) []TagType {
	var b0, b1, b2 TagType
	if dir == DeviceToPCS {
		b0, b1, b2 = AToB0, AToB1, AToB2
	} else {
		b0, b1, b2 = BToA0, BToA1, BToA2
	}
	switch intent {
	case RelativeColorimetric, AbsoluteColorimetric:
		return []TagType{b1, b0, b2}
	case Saturation:
		return []TagType{b2, b0, b1}
	default:
		return []TagType{b0, b1, b2}
	}
}

func (t *Transform) parseWhitePoint(data []byte) {
	xyz, err := parseXYZ(data)
	if err == nil {
		t.whitePoint = xyz
	}
}

func parseXYZ(data []byte) ([3]float64, error) {
	if len(data) < 20 {
		return [3]float64{}, errInvalidTagData
	}
	if string(data[0:4]) != "XYZ " {
		return [3]float64{}, errUnexpectedType
	}

	x := getS15Fixed16(data, 8)
	y := getS15Fixed16(data, 12)
	z := getS15Fixed16(data, 16)

	return [3]float64{x, y, z}, nil
}

// Apply transforms a colour. Input/output are normalised [0,1] slices.
// For DeviceToPCS direction, input is device colour, output is PCS XYZ or Lab.
// For PCSToDevice direction, input is PCS XYZ or Lab, output is device colour.
func (t *Transform) Apply(input []float64) []float64 {
	switch t.profileType {
	case profileTypeMatrixTRC:
		if len(input) != 3 {
			return make([]float64, 3)
		}
		x, y, z := t.applyMatrixTRC3(input[0], input[1], input[2])
		return []float64{x, y, z}
	case profileTypeGrayTRC:
		if t.direction == DeviceToPCS {
			if len(input) != 1 {
				return make([]float64, 3)
			}
			x, y, z := t.grayTRCToPCS(input[0])
			return []float64{x, y, z}
		}
		// PCSToDevice: extract Y from D50-adapted XYZ
		y := input[0]
		if len(input) >= 2 {
			y = input[1]
		}
		return []float64{t.grayTRCFromPCS(y)}
	case profileTypeLut:
		return t.applyLut(input)
	}
	return input
}

// applyMatrixTRC3 applies a 3-channel matrix-TRC transform to a colour
// triple, returning the transformed triple by value to avoid per-call
// allocation.  For DeviceToPCS the input is device colour in [0,1] and
// the output is PCS XYZ; for PCSToDevice the input is PCS XYZ and the
// output is device colour in [0,1].
func (t *Transform) applyMatrixTRC3(a, b, c float64) (float64, float64, float64) {
	if t.direction == DeviceToPCS {
		// linearise via TRCs, then matrix to XYZ
		r := t.trc[0].Evaluate(a)
		g := t.trc[1].Evaluate(b)
		bl := t.trc[2].Evaluate(c)
		return t.matrix[0]*r + t.matrix[1]*g + t.matrix[2]*bl,
			t.matrix[3]*r + t.matrix[4]*g + t.matrix[5]*bl,
			t.matrix[6]*r + t.matrix[7]*g + t.matrix[8]*bl
	}

	// PCSToDevice: inverse matrix, then inverse TRCs
	r := t.matrixInv[0]*a + t.matrixInv[1]*b + t.matrixInv[2]*c
	g := t.matrixInv[3]*a + t.matrixInv[4]*b + t.matrixInv[5]*c
	bl := t.matrixInv[6]*a + t.matrixInv[7]*b + t.matrixInv[8]*c
	return clamp(t.trcInv[0].Invert(clamp(r, 0, 1)), 0, 1),
		clamp(t.trcInv[1].Invert(clamp(g, 0, 1)), 0, 1),
		clamp(t.trcInv[2].Invert(clamp(bl, 0, 1)), 0, 1)
}

// grayTRCToPCS maps a single grey channel to PCS XYZ via the gray TRC.
// PCS is always D50-adapted XYZ; scaling by D50 covers profiles whose
// media white point differs from D50 (non-conforming but common),
// because Bradford adaptation is linear in y (WP*y → D50*y).
func (t *Transform) grayTRCToPCS(in float64) (X, Y, Z float64) {
	y := t.grayTRC.Evaluate(in)
	return d50WhitePoint[0] * y, d50WhitePoint[1] * y, d50WhitePoint[2] * y
}

// grayTRCFromPCS maps PCS Y to a single grey channel via the inverse
// gray TRC.
func (t *Transform) grayTRCFromPCS(Y float64) float64 {
	return t.grayTRCInv.Invert(clamp(Y, 0, 1))
}

func (t *Transform) applyLut(input []float64) []float64 {
	if t.lut == nil {
		return input
	}
	return t.lut.Apply(input)
}

// ToXYZ converts device colour to PCS XYZ (D50).
// Input is a normalised [0,1] slice with the device colour values.
//
// The optional ws supplies reusable scratch buffers; pass a non-nil [Workspace]
// to avoid per-call allocation in a hot loop, or nil to allocate internally.
// The same Workspace must not be used from more than one goroutine at a time.
func (t *Transform) ToXYZ(device []float64, ws *Workspace) (X, Y, Z float64) {
	if t.direction != DeviceToPCS {
		return 0, 0, 0
	}

	switch t.profileType {
	case profileTypeMatrixTRC:
		// matrix-TRC profiles use XYZ PCS and never need a Lab decode.
		if len(device) != 3 {
			return 0, 0, 0
		}
		return t.applyMatrixTRC3(device[0], device[1], device[2])

	case profileTypeGrayTRC:
		if len(device) != 1 {
			return 0, 0, 0
		}
		return t.grayTRCToPCS(device[0])
	}

	// LUT-based
	var result []float64
	if t.lut != nil {
		result = ws.result(t.lut.OutputChannels())
		t.lut.ApplyInto(device, result, ws)
	} else {
		// degenerate: treat device as PCS, mirroring applyLut returning input
		result = ws.result(len(device))
		copy(result, device)
	}
	if t.profile.PCS == PCSLabSpace {
		if len(result) >= 3 {
			denormaliseLabInPlace(result)
		}
		return labToXYZ(result, t.whitePoint)
	}
	if len(result) >= 3 {
		return result[0], result[1], result[2]
	}
	return 0, 0, 0
}

// FromXYZ converts PCS XYZ (D50) to device colour.
// Returns a normalised [0,1] slice with the device colour values.
func (t *Transform) FromXYZ(X, Y, Z float64) []float64 {
	if t.direction != PCSToDevice {
		return nil
	}
	dst := make([]float64, t.Channels())
	t.FromXYZInto(X, Y, Z, dst, nil)
	return dst
}

// FromXYZInto converts PCS XYZ (D50) to device colour, writing into dst.
// The dst slice must have length >= Channels().
// This avoids allocations in per-pixel loops.
//
// The optional ws supplies reusable scratch buffers for the LUT-based path;
// pass a non-nil [Workspace] to avoid per-call allocation in a hot loop, or nil
// to allocate internally. The same Workspace must not be used from more than
// one goroutine at a time.
func (t *Transform) FromXYZInto(X, Y, Z float64, dst []float64, ws *Workspace) {
	if t.direction != PCSToDevice {
		return
	}

	switch t.profileType {
	case profileTypeMatrixTRC:
		dst[0], dst[1], dst[2] = t.applyMatrixTRC3(X, Y, Z)

	case profileTypeGrayTRC:
		dst[0] = t.grayTRCFromPCS(Y)

	default:
		// LUT-based
		in := ws.result(3)
		if t.profile.PCS == PCSLabSpace {
			in[0], in[1], in[2] = xyzToLab(X, Y, Z, t.whitePoint)
			if t.profileType == profileTypeLut {
				normaliseLabInPlace(in)
			}
		} else {
			in[0], in[1], in[2] = X, Y, Z
		}
		if t.lut == nil {
			// mirror Apply returning input unchanged
			copy(dst, in[:3])
			return
		}
		t.lut.ApplyInto(in, dst, ws)
	}
}

// Channels returns the number of device colour channels for this transform.
func (t *Transform) Channels() int {
	return t.profile.ColorSpace.NumComponents()
}

// labToXYZ converts Lab to XYZ using the given white point.
// Lab values: L in [0, 100], a and b in [-128, 127].
func labToXYZ(lab []float64, white [3]float64) (X, Y, Z float64) {
	if len(lab) < 3 {
		return 0, 0, 0
	}

	L, a, b := lab[0], lab[1], lab[2]

	// normalise L from [0,100] to f(Y/Yn)
	fy := (L + 16) / 116
	fx := a/500 + fy
	fz := fy - b/200

	// inverse f function threshold: 6/29
	threshold := 6.0 / 29.0
	// scale factor: 108/841 = 3 * (6/29)^2
	scale := 108.0 / 841.0
	offset := 16.0 / 116.0

	var xr, yr, zr float64
	if fy > threshold {
		yr = fy * fy * fy
	} else {
		yr = (fy - offset) * scale
	}
	if fx > threshold {
		xr = fx * fx * fx
	} else {
		xr = (fx - offset) * scale
	}
	if fz > threshold {
		zr = fz * fz * fz
	} else {
		zr = (fz - offset) * scale
	}

	return xr * white[0], yr * white[1], zr * white[2]
}

// xyzToLab converts XYZ to Lab using the given white point.
func xyzToLab(X, Y, Z float64, white [3]float64) (L, a, b float64) {
	// use D50 as fallback if white point component is zero
	wx, wy, wz := white[0], white[1], white[2]
	if wx == 0 {
		wx = d50WhitePoint[0]
	}
	if wy == 0 {
		wy = d50WhitePoint[1]
	}
	if wz == 0 {
		wz = d50WhitePoint[2]
	}

	// normalise by white point
	xr := X / wx
	yr := Y / wy
	zr := Z / wz

	// f function threshold (6/29)^3
	threshold := 216.0 / 24389.0

	var fx, fy, fz float64
	// scale factor for linear part: 841/108 = (29/6)^2 / 3
	scale := 841.0 / 108.0
	offset := 16.0 / 116.0

	if xr > threshold {
		fx = math.Pow(xr, 1.0/3.0)
	} else {
		fx = xr*scale + offset
	}
	if yr > threshold {
		fy = math.Pow(yr, 1.0/3.0)
	} else {
		fy = yr*scale + offset
	}
	if zr > threshold {
		fz = math.Pow(zr, 1.0/3.0)
	} else {
		fz = zr*scale + offset
	}

	L = 116*fy - 16
	a = 500 * (fx - fy)
	b = 200 * (fy - fz)

	return L, a, b
}

// normaliseLabInPlace converts Lab values to their normalised [0,1] encoding
// for LUT processing, overwriting lab[:3]. Input: L in [0, 100], a and b in
// [-128, 127]; output: normalised values in [0, 1].
func normaliseLabInPlace(lab []float64) {
	if len(lab) < 3 {
		return
	}
	lab[0] = lab[0] / 100.0
	lab[1] = (lab[1] + 128.0) / 255.0
	lab[2] = (lab[2] + 128.0) / 255.0
}

// denormaliseLabInPlace converts normalised [0,1] Lab encoding back to actual
// Lab values, overwriting lab[:3]. It is the inverse of [normaliseLabInPlace].
func denormaliseLabInPlace(lab []float64) {
	if len(lab) < 3 {
		return
	}
	lab[0] = lab[0] * 100.0
	lab[1] = lab[1]*255.0 - 128.0
	lab[2] = lab[2]*255.0 - 128.0
}

// invertMatrix3x3 returns the inverse of a 3x3 matrix.
func invertMatrix3x3(m []float64) []float64 {
	if len(m) != 9 {
		return nil
	}

	a, b, c := m[0], m[1], m[2]
	d, e, f := m[3], m[4], m[5]
	g, h, i := m[6], m[7], m[8]

	det := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	if det == 0 {
		return nil
	}

	invDet := 1.0 / det

	return []float64{
		(e*i - f*h) * invDet, (c*h - b*i) * invDet, (b*f - c*e) * invDet,
		(f*g - d*i) * invDet, (a*i - c*g) * invDet, (c*d - a*f) * invDet,
		(d*h - e*g) * invDet, (b*g - a*h) * invDet, (a*e - b*d) * invDet,
	}
}

// ProfileType returns the detected type of the profile.
func (t *Transform) ProfileType() string {
	switch t.profileType {
	case profileTypeMatrixTRC:
		return "Matrix/TRC"
	case profileTypeGrayTRC:
		return "Gray TRC"
	case profileTypeLut:
		return "LUT"
	default:
		return "Unknown"
	}
}
