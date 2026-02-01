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
// A Transform is not safe for concurrent use. If the same Transform needs to be
// used from multiple goroutines, callers must provide their own synchronisation.
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

	return nil
}

func (t *Transform) initLut() error {
	p := t.profile

	// select appropriate LUT based on direction and intent
	var tagType TagType
	if t.direction == DeviceToPCS {
		switch t.intent {
		case Perceptual:
			tagType = AToB0
		case RelativeColorimetric, AbsoluteColorimetric:
			tagType = AToB1
		case Saturation:
			tagType = AToB2
		}
		// fall back to AToB0 if specific intent not available
		if _, ok := p.TagData[tagType]; !ok {
			tagType = AToB0
		}
	} else {
		switch t.intent {
		case Perceptual:
			tagType = BToA0
		case RelativeColorimetric, AbsoluteColorimetric:
			tagType = BToA1
		case Saturation:
			tagType = BToA2
		}
		// fall back to BToA0 if specific intent not available
		if _, ok := p.TagData[tagType]; !ok {
			tagType = BToA0
		}
	}

	data, ok := p.TagData[tagType]
	if !ok {
		return errors.New("icc: missing LUT tag")
	}

	lut, err := DecodeLut(data)
	if err != nil {
		return err
	}

	t.lut = lut
	return nil
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
		return t.applyMatrixTRC(input)
	case profileTypeGrayTRC:
		return t.applyGrayTRC(input)
	case profileTypeLut:
		return t.applyLut(input)
	}
	return input
}

func (t *Transform) applyMatrixTRC(input []float64) []float64 {
	if len(input) != 3 {
		return make([]float64, 3)
	}

	if t.direction == DeviceToPCS {
		// apply TRCs to linearise
		r := t.trc[0].Evaluate(input[0])
		g := t.trc[1].Evaluate(input[1])
		b := t.trc[2].Evaluate(input[2])

		// apply matrix to get XYZ
		x := t.matrix[0]*r + t.matrix[1]*g + t.matrix[2]*b
		y := t.matrix[3]*r + t.matrix[4]*g + t.matrix[5]*b
		z := t.matrix[6]*r + t.matrix[7]*g + t.matrix[8]*b

		return []float64{x, y, z}
	}

	// PCSToDevice
	x, y, z := input[0], input[1], input[2]

	// apply inverse matrix to get linear RGB
	r := t.matrixInv[0]*x + t.matrixInv[1]*y + t.matrixInv[2]*z
	g := t.matrixInv[3]*x + t.matrixInv[4]*y + t.matrixInv[5]*z
	b := t.matrixInv[6]*x + t.matrixInv[7]*y + t.matrixInv[8]*z

	// apply inverse TRCs
	r = t.trcInv[0].Invert(clamp(r, 0, 1))
	g = t.trcInv[1].Invert(clamp(g, 0, 1))
	b = t.trcInv[2].Invert(clamp(b, 0, 1))

	return []float64{clamp(r, 0, 1), clamp(g, 0, 1), clamp(b, 0, 1)}
}

func (t *Transform) applyGrayTRC(input []float64) []float64 {
	if len(input) != 1 {
		return make([]float64, 1)
	}

	if t.direction == DeviceToPCS {
		// apply TRC to get linear, then Y = linear value
		// XYZ for gray is (0.9642*Y, Y, 0.8249*Y) scaled by D50 white
		y := t.grayTRC.Evaluate(input[0])
		return []float64{
			t.whitePoint[0] * y,
			t.whitePoint[1] * y,
			t.whitePoint[2] * y,
		}
	}

	// PCSToDevice: extract Y and apply inverse TRC
	y := input[0]
	if len(input) >= 2 {
		y = input[1] // use Y from XYZ
	}
	// normalise by white point Y
	if t.whitePoint[1] != 0 {
		y /= t.whitePoint[1]
	}
	return []float64{t.grayTRCInv.Invert(clamp(y, 0, 1))}
}

func (t *Transform) applyLut(input []float64) []float64 {
	if t.lut == nil {
		return input
	}
	return t.lut.Apply(input)
}

// ToXYZ converts device colour to PCS XYZ (D50).
// Input is a normalised [0,1] slice with the device colour values.
func (t *Transform) ToXYZ(device []float64) (X, Y, Z float64) {
	if t.direction != DeviceToPCS {
		return 0, 0, 0
	}

	result := t.Apply(device)

	// handle Lab to XYZ conversion if needed
	if t.profile.PCS == PCSLabSpace {
		// LUT outputs are normalised [0,1]; convert to Lab ranges
		if t.profileType == profileTypeLut && len(result) >= 3 {
			result = denormaliseLab(result)
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

	var input []float64
	if t.profile.PCS == PCSLabSpace {
		L, a, b := xyzToLab(X, Y, Z, t.whitePoint)
		input = []float64{L, a, b}
		// LUT inputs are normalised [0,1]; convert from Lab ranges
		if t.profileType == profileTypeLut {
			input = normaliseLab(input)
		}
	} else {
		input = []float64{X, Y, Z}
	}

	return t.Apply(input)
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

// normaliseLab converts Lab values to normalised [0,1] encoding for LUT processing.
// Input: L in [0, 100], a and b in [-128, 127].
// Output: normalised values in [0, 1].
func normaliseLab(lab []float64) []float64 {
	if len(lab) < 3 {
		return lab
	}
	return []float64{
		lab[0] / 100.0,           // L: [0, 100] -> [0, 1]
		(lab[1] + 128.0) / 255.0, // a: [-128, 127] -> [0, 1]
		(lab[2] + 128.0) / 255.0, // b: [-128, 127] -> [0, 1]
	}
}

// denormaliseLab converts normalised [0,1] Lab encoding to actual Lab values.
// Input: normalised values in [0, 1].
// Output: L in [0, 100], a and b in [-128, 127].
func denormaliseLab(lab []float64) []float64 {
	if len(lab) < 3 {
		return lab
	}
	return []float64{
		lab[0] * 100.0,       // L: [0, 1] -> [0, 100]
		lab[1]*255.0 - 128.0, // a: [0, 1] -> [-128, 127]
		lab[2]*255.0 - 128.0, // b: [0, 1] -> [-128, 127]
	}
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
