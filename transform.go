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
	"sync"
)

// direction selects which tag chain a LUT profile uses; it is an internal
// detail of LUT chain selection only.
type direction int

const (
	deviceToPCS direction = iota
	pcsToDevice
)

// Transform performs colour conversions using an ICC profile.
//
// Create a Transform using [NewTransform], then use [Transform.ToXYZ] (device
// to PCS) and [Transform.FromXYZ] (PCS to device) to convert colours. A single
// Transform converts in both directions. The Transform supports matrix/TRC
// profiles (common for displays), grayscale profiles, and LUT-based profiles
// (common for printers).
//
// The rendering intent fixed at construction determines what the PCS values
// mean: under [AbsoluteColorimetric] they are ICC-absolute, and under every
// other intent they are media-relative, referenced to [PCSWhitePoint]. Both
// directions use the same reference frame, so a Transform round-trips whatever
// its intent.
//
// The [Transform.Profile] and [Transform.Intent] fields record the arguments
// [NewTransform] was called with. They must not be modified afterwards.
//
// Not every profile supports both directions (e.g. a singular colour matrix, or
// a LUT profile carrying only AToB or only BToA tags). Query
// [Transform.CanToXYZ] and [Transform.CanFromXYZ] before relying on a direction;
// an unsupported direction yields zero output.
//
// A Transform is safe for concurrent use: [Transform.ToXYZ] and
// [Transform.FromXYZ] may be called from multiple goroutines, provided each
// goroutine uses its own [Workspace].
type Transform struct {
	// Profile is the profile the transform was created from.
	// It is read-only once the Transform has been created.
	Profile *Profile

	// Intent is the rendering intent the transform was created with.
	// It is read-only once the Transform has been created.
	Intent RenderingIntent

	// kind of profile, cached from Profile.ProfileType
	profileType ProfileType

	// for matrix/TRC profiles (RGB)
	matrix    []float64 // 3x3 matrix: device RGB to XYZ
	matrixInv []float64 // inverse matrix: XYZ to device RGB; nil if singular
	trc       [3]*Curve // R, G, B TRCs

	// for gray TRC profiles
	grayTRC *Curve

	// for LUT-based profiles: presence recorded eagerly, decoded lazily
	hasAToB bool
	hasBToA bool
	fwdLUT  LUT // built under fwdOnce
	revLUT  LUT // built under revOnce

	// XYZ of the media white point, used to convert between media-relative
	// and ICC-absolute colorimetry.  The PCS itself is always relative to
	// [PCSWhitePoint], whatever the media white point is.  All components
	// are positive and bounded, see [mediaWhitePoint].
	whitePoint [3]float64

	// lazy per-direction construction keeps a shared Transform concurrency-safe
	fwdOnce sync.Once
	revOnce sync.Once
}

// ProfileType describes the machinery a profile uses to convert colours.
type ProfileType int

// String returns a human-readable name for the profile type.
func (pt ProfileType) String() string {
	switch pt {
	case ProfileTypeUnknown:
		return "Unknown"
	case ProfileTypeMatrixTRC:
		return "Matrix/TRC"
	case ProfileTypeGrayTRC:
		return "Gray TRC"
	case ProfileTypeLUT:
		return "LUT"
	default:
		return fmt.Sprintf("ProfileType(%d)", int(pt))
	}
}

const (
	// ProfileTypeUnknown indicates a profile which cannot be used for
	// colour conversion.
	ProfileTypeUnknown ProfileType = iota

	// ProfileTypeMatrixTRC indicates an RGB profile which converts using
	// three tone reproduction curves followed by a 3x3 matrix.
	ProfileTypeMatrixTRC

	// ProfileTypeGrayTRC indicates a grayscale profile which converts
	// using a single tone reproduction curve.
	ProfileTypeGrayTRC

	// ProfileTypeLUT indicates a profile which converts using lookup
	// tables, as used for printers and other non-linear devices.
	ProfileTypeLUT
)

// NewTransform creates a bidirectional colour transform from an ICC profile.
//
// The returned Transform converts device colours to PCS XYZ ([Transform.ToXYZ])
// and PCS XYZ back to device colours ([Transform.FromXYZ]). The intent selects
// which table a LUT-based profile uses, and whether the PCS values are
// media-relative or ICC-absolute; it applies to both directions.
//
// NewTransform fails only for profiles that are fundamentally unusable (an
// unknown profile type, or undecodable matrix/gray data). A profile that can
// convert in only one direction is not an error; query [Transform.CanToXYZ] and
// [Transform.CanFromXYZ] before relying on a direction.
func NewTransform(p *Profile, intent RenderingIntent) (*Transform, error) {
	t := &Transform{
		Profile: p,
		Intent:  intent,
	}

	t.profileType = p.ProfileType()

	switch t.profileType {
	case ProfileTypeMatrixTRC:
		if err := t.initMatrixTRC(); err != nil {
			return nil, err
		}
	case ProfileTypeGrayTRC:
		if err := t.initGrayTRC(); err != nil {
			return nil, err
		}
	case ProfileTypeLUT:
		t.initLUTPresence()
	default:
		return nil, errors.New("icc: unsupported profile type")
	}

	t.whitePoint = mediaWhitePoint(p)

	return t, nil
}

// ProfileType reports which conversion machinery the profile provides.
// Profiles of type [ProfileTypeUnknown] cannot be used for colour conversion.
func (p *Profile) ProfileType() ProfileType {
	// check for LUT-based profile (takes precedence)
	if _, ok := p.TagData[AToB0]; ok {
		return ProfileTypeLUT
	}
	if _, ok := p.TagData[AToB1]; ok {
		return ProfileTypeLUT
	}
	if _, ok := p.TagData[AToB2]; ok {
		return ProfileTypeLUT
	}
	if _, ok := p.TagData[BToA0]; ok {
		return ProfileTypeLUT
	}
	if _, ok := p.TagData[BToA1]; ok {
		return ProfileTypeLUT
	}
	if _, ok := p.TagData[BToA2]; ok {
		return ProfileTypeLUT
	}

	// check for matrix/TRC profile
	_, hasRXYZ := p.TagData[RedMatrixColumn]
	_, hasGXYZ := p.TagData[GreenMatrixColumn]
	_, hasBXYZ := p.TagData[BlueMatrixColumn]
	_, hasRTRC := p.TagData[RedTRC]
	_, hasGTRC := p.TagData[GreenTRC]
	_, hasBTRC := p.TagData[BlueTRC]
	if hasRXYZ && hasGXYZ && hasBXYZ && hasRTRC && hasGTRC && hasBTRC {
		return ProfileTypeMatrixTRC
	}

	// check for gray TRC profile
	if _, ok := p.TagData[GrayTRC]; ok {
		return ProfileTypeGrayTRC
	}

	return ProfileTypeUnknown
}

func (t *Transform) initMatrixTRC() error {
	p := t.Profile

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

	// inverse used by FromXYZ; nil (singular) just means reverse unavailable
	t.matrixInv = invertMatrix3x3(t.matrix)

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

	return nil
}

func (t *Transform) initGrayTRC() error {
	p := t.Profile

	grayTRC, err := DecodeCurve(p.TagData[GrayTRC])
	if err != nil {
		return err
	}

	t.grayTRC = grayTRC

	return nil
}

// initLUTPresence records which AToB*/BToA* tags exist (cheap map lookups);
// the LUTs themselves are decoded lazily per direction.
func (t *Transform) initLUTPresence() {
	p := t.Profile
	for _, tag := range []TagType{AToB0, AToB1, AToB2} {
		if _, ok := p.TagData[tag]; ok {
			t.hasAToB = true
			break
		}
	}
	for _, tag := range []TagType{BToA0, BToA1, BToA2} {
		if _, ok := p.TagData[tag]; ok {
			t.hasBToA = true
			break
		}
	}
}

// buildForward decodes the forward (device->PCS) machinery on first ToXYZ.
func (t *Transform) buildForward() {
	t.fwdOnce.Do(func() {
		if t.profileType == ProfileTypeLUT {
			t.fwdLUT = t.decodeLUTChain(deviceToPCS)
		}
	})
}

// buildReverse decodes the reverse (PCS->device) LUT chain on first FromXYZ.
// A Transform shared across goroutines stays safe because the build runs once.
// Matrix/TRC and gray profiles need no preparation.
func (t *Transform) buildReverse() {
	t.revOnce.Do(func() {
		if t.profileType == ProfileTypeLUT {
			t.revLUT = t.decodeLUTChain(pcsToDevice)
		}
	})
}

// decodeLUTChain decodes the preferred LUT tag for the given direction and the
// transform's intent. It returns nil if no tag is present or every present tag
// fails to decode; the caller then degrades to zero output.
func (t *Transform) decodeLUTChain(dir direction) LUT {
	// preferred-first ordering: try the tag for the requested intent, then
	// the perceptual tag (ICC default fallback), then the remaining one.
	// A present-but-malformed tag must not mask a valid fallback, so skip
	// over decode failures rather than giving up.
	for _, tagType := range lutFallbackChain(dir, t.Intent) {
		if d, ok := t.Profile.TagData[tagType]; ok {
			lut, err := DecodeLUT(d)
			if err != nil {
				continue
			}
			return lut
		}
	}
	return nil
}

// CanToXYZ reports whether forward (device to PCS) conversion is supported.
func (t *Transform) CanToXYZ() bool {
	switch t.profileType {
	case ProfileTypeMatrixTRC, ProfileTypeGrayTRC:
		return true
	case ProfileTypeLUT:
		return t.hasAToB
	}
	return false
}

// CanFromXYZ reports whether reverse (PCS to device) conversion is supported.
func (t *Transform) CanFromXYZ() bool {
	switch t.profileType {
	case ProfileTypeMatrixTRC:
		return t.matrixInv != nil
	case ProfileTypeGrayTRC:
		return true
	case ProfileTypeLUT:
		return t.hasBToA
	}
	return false
}

func lutFallbackChain(dir direction, intent RenderingIntent) []TagType {
	var b0, b1, b2 TagType
	if dir == deviceToPCS {
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

// mediaWhitePoint returns the media white point of p, falling back to the PCS
// white point if the tag is missing, undecodable, or outside the range a
// tristimulus white point can take.  The encoded values are signed and reach
// ±32768, so an unchecked tag could flip the sign of the ICC-absolute scaling,
// divide by zero in it, or inflate it without bound.
func mediaWhitePoint(p *Profile) [3]float64 {
	data, ok := p.TagData[MediaWhitePoint]
	if !ok {
		return PCSWhitePoint
	}
	xyz, err := parseXYZ(data)
	if err != nil {
		return PCSWhitePoint
	}
	// White points are normalised so that Y is 1 for an ideal medium; real
	// media stay below this, and no illuminant has a component anywhere near
	// twice the corresponding PCS white component.
	for i, v := range xyz {
		if v <= 0 || v > 2*PCSWhitePoint[i] {
			return PCSWhitePoint
		}
	}
	return xyz
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

// matrixTRCToPCS maps a device colour triple in [0,1] to PCS XYZ: linearise via
// the TRCs, then apply the matrix. Returned by value to avoid per-call allocation.
func (t *Transform) matrixTRCToPCS(a, b, c float64) (float64, float64, float64) {
	r := t.trc[0].Evaluate(a)
	g := t.trc[1].Evaluate(b)
	bl := t.trc[2].Evaluate(c)
	return t.matrix[0]*r + t.matrix[1]*g + t.matrix[2]*bl,
		t.matrix[3]*r + t.matrix[4]*g + t.matrix[5]*bl,
		t.matrix[6]*r + t.matrix[7]*g + t.matrix[8]*bl
}

// matrixTRCFromPCS maps PCS XYZ to a device colour triple in [0,1]: apply the
// inverse matrix, then the inverse TRCs. The caller must ensure the reverse
// machinery is available (matrixInv != nil).
func (t *Transform) matrixTRCFromPCS(a, b, c float64) (float64, float64, float64) {
	r := t.matrixInv[0]*a + t.matrixInv[1]*b + t.matrixInv[2]*c
	g := t.matrixInv[3]*a + t.matrixInv[4]*b + t.matrixInv[5]*c
	bl := t.matrixInv[6]*a + t.matrixInv[7]*b + t.matrixInv[8]*c
	return t.trc[0].Invert(r), t.trc[1].Invert(g), t.trc[2].Invert(bl)
}

// grayTRCToPCS maps a single grey channel to PCS XYZ via the gray TRC.
// PCS values are always relative to the PCS white point; scaling by it covers
// profiles whose media white point differs (non-conforming but common),
// because Bradford adaptation is linear in y (WP*y → PCS*y).
func (t *Transform) grayTRCToPCS(in float64) (X, Y, Z float64) {
	y := t.grayTRC.Evaluate(in)
	return PCSWhitePoint[0] * y, PCSWhitePoint[1] * y, PCSWhitePoint[2] * y
}

// grayTRCFromPCS maps PCS Y to a single grey channel via the inverse
// gray TRC.
func (t *Transform) grayTRCFromPCS(Y float64) float64 {
	return t.grayTRC.Invert(Y)
}

// ToXYZ converts device colour to PCS XYZ. Input is a normalised [0,1]
// slice with the device colour values.
//
// For every intent but [AbsoluteColorimetric] the result is media-relative and
// referenced to [PCSWhitePoint].  Under [AbsoluteColorimetric] it is scaled by
// the media white point stored in the profile, and so is referenced to the
// illuminant that tag was measured against.  ICC version 4 requires the tag to
// be adapted to the PCS white point, while version 2 profiles commonly store
// the unadapted measurement; the two built-in sRGB profiles differ in exactly
// this way.
//
// The Workspace argument is used as scratch space.
//
// If the profile does not support this direction (see [Transform.CanToXYZ]),
// ToXYZ returns 0, 0, 0.
func (t *Transform) ToXYZ(device []float64, ws *Workspace) (X, Y, Z float64) {
	X, Y, Z = t.toPCS(device, ws)
	if t.Intent == AbsoluteColorimetric {
		X, Y, Z = t.mediaToAbsolute(X, Y, Z)
	}
	return X, Y, Z
}

func (t *Transform) toPCS(device []float64, ws *Workspace) (X, Y, Z float64) {
	if !t.CanToXYZ() {
		return 0, 0, 0
	}
	t.buildForward()

	switch t.profileType {
	case ProfileTypeMatrixTRC:
		// matrix-TRC profiles use XYZ PCS and never need a Lab decode.
		if len(device) != 3 {
			return 0, 0, 0
		}
		return t.matrixTRCToPCS(device[0], device[1], device[2])

	case ProfileTypeGrayTRC:
		if len(device) != 1 {
			return 0, 0, 0
		}
		return t.grayTRCToPCS(device[0])
	}

	// LUT-based. CanToXYZ already confirmed an AToB tag is present, so a nil
	// fwdLUT here means every present tag failed to decode; honour the
	// documented contract and yield zero output rather than leaking PCS values.
	if t.fwdLUT == nil {
		return 0, 0, 0
	}
	result := ws.result(t.fwdLUT.OutputChannels())
	t.fwdLUT.Apply(device, result, ws)
	if t.Profile.PCS == PCSLabSpace {
		// all three calls tolerate a LUT with fewer than three output channels
		if usesLegacyLab(t.fwdLUT) {
			legacyLabToCurrent(result)
		}
		denormaliseLabInPlace(result)
		return labToXYZ(result)
	}
	if len(result) >= 3 {
		return result[0] * pcsXYZScale, result[1] * pcsXYZScale, result[2] * pcsXYZScale
	}
	return 0, 0, 0
}

// FromXYZ converts PCS XYZ to device colour, writing the normalised [0,1]
// device values into dst. The destination slice must have length at least Channels().
//
// The input is interpreted in the same reference frame in which
// [Transform.ToXYZ] returns its result.
//
// The Workspace argument is used as scratch space.
//
// If the profile does not support this direction (see [Transform.CanFromXYZ]),
// FromXYZ writes nothing.
func (t *Transform) FromXYZ(X, Y, Z float64, dst []float64, ws *Workspace) {
	if t.Intent == AbsoluteColorimetric {
		X, Y, Z = t.absoluteToMedia(X, Y, Z)
	}
	t.fromPCS(X, Y, Z, dst, ws)
}

// mediaToAbsolute converts media-relative PCS values, which are normalised so
// that the media white point is the PCS white, to ICC-absolute values.
func (t *Transform) mediaToAbsolute(X, Y, Z float64) (float64, float64, float64) {
	return X * t.whitePoint[0] / PCSWhitePoint[0],
		Y * t.whitePoint[1] / PCSWhitePoint[1],
		Z * t.whitePoint[2] / PCSWhitePoint[2]
}

// absoluteToMedia is the inverse of [Transform.mediaToAbsolute].
func (t *Transform) absoluteToMedia(X, Y, Z float64) (float64, float64, float64) {
	return X * PCSWhitePoint[0] / t.whitePoint[0],
		Y * PCSWhitePoint[1] / t.whitePoint[1],
		Z * PCSWhitePoint[2] / t.whitePoint[2]
}

func (t *Transform) fromPCS(X, Y, Z float64, dst []float64, ws *Workspace) {
	if !t.CanFromXYZ() {
		return
	}
	t.buildReverse()

	switch t.profileType {
	case ProfileTypeMatrixTRC:
		dst[0], dst[1], dst[2] = t.matrixTRCFromPCS(X, Y, Z)

	case ProfileTypeGrayTRC:
		dst[0] = t.grayTRCFromPCS(Y)

	default:
		// LUT-based. CanFromXYZ already confirmed a BToA tag is present, so a
		// nil revLUT here means every present tag failed to decode; honour the
		// documented contract and yield zero output rather than leaking PCS
		// values into the device channels.
		if t.revLUT == nil {
			zeroFloats(dst[:t.Channels()])
			return
		}
		in := ws.result(3)
		if t.Profile.PCS == PCSLabSpace {
			in[0], in[1], in[2] = xyzToLab(X, Y, Z)
			normaliseLabInPlace(in)
			if usesLegacyLab(t.revLUT) {
				currentLabToLegacy(in)
			}
		} else {
			in[0], in[1], in[2] = X/pcsXYZScale, Y/pcsXYZScale, Z/pcsXYZScale
		}
		t.revLUT.Apply(in, dst, ws)
	}
}

// Channels returns the number of device colour channels for this transform.
func (t *Transform) Channels() int {
	return t.Profile.ColorSpace.NumComponents()
}

// labToXYZ converts PCS Lab to PCS XYZ.
// Lab values: L in [0, 100], a and b in [-128, 127].
func labToXYZ(lab []float64) (X, Y, Z float64) {
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

	return xr * PCSWhitePoint[0], yr * PCSWhitePoint[1], zr * PCSWhitePoint[2]
}

// xyzToLab converts PCS XYZ to PCS Lab.
func xyzToLab(X, Y, Z float64) (L, a, b float64) {
	// normalise by white point
	xr := X / PCSWhitePoint[0]
	yr := Y / PCSWhitePoint[1]
	zr := Z / PCSWhitePoint[2]

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

// pcsXYZScale converts a normalised [0,1] LUT value to PCS XYZ.
//
// The 16-bit PCSXYZ encoding is a u1Fixed15Number, so 0x8000 stands for 1.0
// and the top of the range, 0xFFFF, for 1 + 32767/32768.  The extra headroom
// exists because PCS X and Z routinely exceed 1.0; the PCS white point alone
// has X = 0.9642 and Z = 0.8249, and saturated colours go higher.  Reading a
// table as though 0xFFFF meant 1.0 halves every value.
//
// The 8-bit case is not covered by the standard, which defines no 8-bit
// PCSXYZ encoding and advises against lut8Type in PCSXYZ profiles for that
// reason.  Such tables get the same treatment, as littleCMS does.
const pcsXYZScale = 65535.0 / 32768.0

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

// legacyLabScale converts a normalised PCS Lab value from the legacy 16-bit
// encoding to the one [normaliseLabInPlace] and [denormaliseLabInPlace] use.
//
// The two differ in where the top of the range sits: the legacy encoding puts
// L* = 100 and a* = b* = +127 at 0xFF00, the current one at 0xFFFF. Reading a
// legacy table as though it were a current one shrinks every value by 0.39%,
// which is enough to move the media white off the PCS white point.
const legacyLabScale = 65535.0 / 65280.0

// usesLegacyLab reports whether lut exchanges PCS Lab values in the legacy
// 16-bit encoding. Of the four tag types that can appear in an AToB* or BToA*
// tag, only lut16Type does: the 8-bit encoding of lut8Type puts L* = 100 at
// 0xFF, the top of its range, so it needs no adjustment, and lutAToBType and
// lutBToAType use the current 16-bit encoding.
func usesLegacyLab(lut LUT) bool {
	_, ok := lut.(*LUT16)
	return ok
}

// legacyLabToCurrent converts the normalised Lab values in lab[:3] from the
// legacy 16-bit encoding to the current one.
//
// The legacy encoding reaches slightly further than the current one, to
// L* = 100 + 25500/65280 and a* = b* = 127 + 255/256.  ICC.1:2022 §10.8
// requires values beyond the current range to be clipped per component: an L*
// above 100 is not a valid value at all, while the extra a* and b* range holds
// valid PCS values that the current encoding simply cannot represent.
//
// It tolerates a slice shorter than three elements.
func legacyLabToCurrent(lab []float64) {
	for i := range min(3, len(lab)) {
		lab[i] = clamp(lab[i]*legacyLabScale, 0, 1)
	}
}

// currentLabToLegacy is the inverse of [legacyLabToCurrent], less the
// clipping: every value the current encoding can represent is representable in
// the legacy one too.
func currentLabToLegacy(lab []float64) {
	for i := range min(3, len(lab)) {
		lab[i] /= legacyLabScale
	}
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
