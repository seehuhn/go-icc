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

import "math"

// Lut represents a colour lookup table from an ICC profile.
// The four concrete implementations are [Lut8], [Lut16], [LutAToB], and [LutBToA].
type Lut interface {
	// Apply transforms input values through the LUT.
	// Input values should be normalised to [0, 1].
	Apply(input []float64) []float64

	// Encode converts the LUT to ICC tag data in its native format.
	Encode() ([]byte, error)

	// InputChannels returns the number of input channels.
	InputChannels() int

	// OutputChannels returns the number of output channels.
	OutputChannels() int
}

// DecodeLut decodes a Lut from ICC tag data.
// This is used for AToB0, AToB1, AToB2, BToA0, BToA1, and BToA2 tags.
// Supported types: [Lut8] (mft1), [Lut16] (mft2), [LutAToB] (mAB), [LutBToA] (mBA).
func DecodeLut(data []byte) (Lut, error) {
	if len(data) < 8 {
		return nil, errInvalidTagData
	}

	typeID := string(data[0:4])
	switch typeID {
	case "mft1":
		return decodeLut8(data)
	case "mft2":
		return decodeLut16(data)
	case "mAB ":
		return decodeLutAToB(data)
	case "mBA ":
		return decodeLutBToA(data)
	default:
		return nil, errUnexpectedType
	}
}

// ----------------------------------------------------------------------------
// Lut8 - lut8Type (mft1)
// ----------------------------------------------------------------------------

// Lut8 represents an 8-bit LUT (lut8Type, tag signature "mft1").
// Processing order: Matrix → InputCurves → CLUT → OutputCurves
type Lut8 struct {
	inputChannels  int
	outputChannels int
	gridPoints     int       // same for all dimensions
	matrix         []float64 // 3×3, nil if identity
	inputCurves    []*Curve  // one per input channel
	clut           []float64 // flattened n-dimensional table, normalised [0,1]
	outputCurves   []*Curve  // one per output channel
}

func (l *Lut8) InputChannels() int  { return l.inputChannels }
func (l *Lut8) OutputChannels() int { return l.outputChannels }

// Apply transforms input values through the LUT.
// Processing order: Matrix → InputCurves → CLUT → OutputCurves
func (l *Lut8) Apply(input []float64) []float64 {
	if len(input) != l.inputChannels {
		return make([]float64, l.outputChannels)
	}

	values := make([]float64, len(input))
	copy(values, input)

	// matrix (applied first for lut8/lut16)
	values = applyMatrix3x3(l.matrix, values)

	// input curves
	values = applyCurves(l.inputCurves, values)

	// CLUT
	values = l.applyCLUT(values)

	// output curves
	values = applyCurves(l.outputCurves, values)

	// clamp output
	for i := range values {
		values[i] = clamp(values[i], 0, 1)
	}

	return values
}

func (l *Lut8) applyCLUT(values []float64) []float64 {
	if l.clut == nil || l.gridPoints == 0 {
		return values
	}
	gridPoints := make([]int, l.inputChannels)
	for i := range gridPoints {
		gridPoints[i] = l.gridPoints
	}
	if len(values) == 3 {
		return tetrahedralInterp3D(l.clut, l.gridPoints, l.outputChannels, values[0], values[1], values[2])
	}
	return multilinearInterp(l.clut, gridPoints, l.outputChannels, values)
}

// Encode converts the LUT to lut8Type (mft1) format.
func (l *Lut8) Encode() ([]byte, error) {
	inputTableSize := 256 * l.inputChannels
	clutSize := computeCLUTSizeUniform(l.gridPoints, l.inputChannels, l.outputChannels)
	outputTableSize := 256 * l.outputChannels
	totalSize := 48 + inputTableSize + clutSize + outputTableSize

	buf := make([]byte, totalSize)
	copy(buf[0:4], "mft1")
	buf[8] = byte(l.inputChannels)
	buf[9] = byte(l.outputChannels)
	buf[10] = byte(l.gridPoints)

	// write matrix (identity if nil)
	matrix := l.matrix
	if matrix == nil {
		matrix = []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	}
	for i := range 9 {
		putS15Fixed16(buf, 12+i*4, matrix[i])
	}

	// write input tables (256 entries per channel, 8-bit)
	offset := 48
	for ch := range l.inputChannels {
		var curve *Curve
		if ch < len(l.inputCurves) {
			curve = l.inputCurves[ch]
		}
		for i := range 256 {
			val := float64(i) / 255.0
			if curve != nil {
				val = curve.Evaluate(val)
			}
			buf[offset+ch*256+i] = byte(clamp(val, 0, 1) * 255.0)
		}
	}
	offset += inputTableSize

	// write CLUT (8-bit values)
	for i, v := range l.clut {
		buf[offset+i] = byte(clamp(v, 0, 1) * 255.0)
	}
	offset += clutSize

	// write output tables (256 entries per channel, 8-bit)
	for ch := range l.outputChannels {
		var curve *Curve
		if ch < len(l.outputCurves) {
			curve = l.outputCurves[ch]
		}
		for i := range 256 {
			val := float64(i) / 255.0
			if curve != nil {
				val = curve.Evaluate(val)
			}
			buf[offset+ch*256+i] = byte(clamp(val, 0, 1) * 255.0)
		}
	}

	return buf, nil
}

func decodeLut8(data []byte) (*Lut8, error) {
	if len(data) < 48 {
		return nil, errInvalidTagData
	}

	inputChannels := int(data[8])
	outputChannels := int(data[9])
	clutPoints := int(data[10])

	if inputChannels == 0 || outputChannels == 0 || inputChannels > 15 || outputChannels > 15 {
		return nil, errInvalidTagData
	}

	// matrix at offset 12
	matrix := make([]float64, 9)
	for i := range 9 {
		matrix[i] = getS15Fixed16(data, 12+i*4)
	}
	if isIdentityMatrix3x3(matrix) {
		matrix = nil
	}

	// input tables: 256 entries per channel
	inputTableStart := 48
	inputTableSize := 256 * inputChannels
	if len(data) < inputTableStart+inputTableSize {
		return nil, errInvalidTagData
	}

	inputCurves := make([]*Curve, inputChannels)
	for ch := range inputChannels {
		table := make([]uint16, 256)
		for i := range 256 {
			// scale 8-bit to 16-bit: 0x00->0x0000, 0xFF->0xFFFF
			v := uint16(data[inputTableStart+ch*256+i])
			table[i] = v<<8 | v
		}
		inputCurves[ch] = &Curve{Table: table}
	}

	// CLUT size
	clutSize := computeCLUTSizeUniform(clutPoints, inputChannels, outputChannels)
	if clutSize == 0 {
		return nil, errInvalidTagData
	}

	clutStart := inputTableStart + inputTableSize
	if len(data) < clutStart+clutSize {
		return nil, errInvalidTagData
	}

	clut := make([]float64, clutSize)
	for i := range clutSize {
		clut[i] = float64(data[clutStart+i]) / 255.0
	}

	// output tables: 256 entries per channel
	outputTableStart := clutStart + clutSize
	outputTableSize := 256 * outputChannels
	if len(data) < outputTableStart+outputTableSize {
		return nil, errInvalidTagData
	}

	outputCurves := make([]*Curve, outputChannels)
	for ch := range outputChannels {
		table := make([]uint16, 256)
		for i := range 256 {
			// scale 8-bit to 16-bit: 0x00->0x0000, 0xFF->0xFFFF
			v := uint16(data[outputTableStart+ch*256+i])
			table[i] = v<<8 | v
		}
		outputCurves[ch] = &Curve{Table: table}
	}

	return &Lut8{
		inputChannels:  inputChannels,
		outputChannels: outputChannels,
		gridPoints:     clutPoints,
		matrix:         matrix,
		inputCurves:    inputCurves,
		clut:           clut,
		outputCurves:   outputCurves,
	}, nil
}

// ----------------------------------------------------------------------------
// Lut16 - lut16Type (mft2)
// ----------------------------------------------------------------------------

// Lut16 represents a 16-bit LUT (lut16Type, tag signature "mft2").
// Processing order: Matrix → InputCurves → CLUT → OutputCurves
type Lut16 struct {
	inputChannels   int
	outputChannels  int
	gridPoints      int       // same for all dimensions
	matrix          []float64 // 3×3, nil if identity
	inputTableSize  int       // entries per input curve
	outputTableSize int       // entries per output curve
	inputCurves     []*Curve  // one per input channel
	clut            []float64 // flattened n-dimensional table, normalised [0,1]
	outputCurves    []*Curve  // one per output channel
}

func (l *Lut16) InputChannels() int  { return l.inputChannels }
func (l *Lut16) OutputChannels() int { return l.outputChannels }

// Apply transforms input values through the LUT.
// Processing order: Matrix → InputCurves → CLUT → OutputCurves
func (l *Lut16) Apply(input []float64) []float64 {
	if len(input) != l.inputChannels {
		return make([]float64, l.outputChannels)
	}

	values := make([]float64, len(input))
	copy(values, input)

	// matrix (applied first for lut8/lut16)
	values = applyMatrix3x3(l.matrix, values)

	// input curves
	values = applyCurves(l.inputCurves, values)

	// CLUT
	values = l.applyCLUT(values)

	// output curves
	values = applyCurves(l.outputCurves, values)

	// clamp output
	for i := range values {
		values[i] = clamp(values[i], 0, 1)
	}

	return values
}

func (l *Lut16) applyCLUT(values []float64) []float64 {
	if l.clut == nil || l.gridPoints == 0 {
		return values
	}
	gridPoints := make([]int, l.inputChannels)
	for i := range gridPoints {
		gridPoints[i] = l.gridPoints
	}
	if len(values) == 3 {
		return tetrahedralInterp3D(l.clut, l.gridPoints, l.outputChannels, values[0], values[1], values[2])
	}
	return multilinearInterp(l.clut, gridPoints, l.outputChannels, values)
}

// Encode converts the LUT to lut16Type (mft2) format.
func (l *Lut16) Encode() ([]byte, error) {
	inputTableEntries := l.inputTableSize
	if inputTableEntries == 0 {
		inputTableEntries = 256
	}
	outputTableEntries := l.outputTableSize
	if outputTableEntries == 0 {
		outputTableEntries = 256
	}

	inputTableBytes := inputTableEntries * l.inputChannels * 2
	clutSize := computeCLUTSizeUniform(l.gridPoints, l.inputChannels, l.outputChannels)
	outputTableBytes := outputTableEntries * l.outputChannels * 2
	totalSize := 52 + inputTableBytes + clutSize*2 + outputTableBytes

	buf := make([]byte, totalSize)
	copy(buf[0:4], "mft2")
	buf[8] = byte(l.inputChannels)
	buf[9] = byte(l.outputChannels)
	buf[10] = byte(l.gridPoints)
	putUint16(buf, 48, uint16(inputTableEntries))
	putUint16(buf, 50, uint16(outputTableEntries))

	// write matrix (identity if nil)
	matrix := l.matrix
	if matrix == nil {
		matrix = []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	}
	for i := range 9 {
		putS15Fixed16(buf, 12+i*4, matrix[i])
	}

	// write input tables (16-bit)
	offset := 52
	for ch := range l.inputChannels {
		var curve *Curve
		if ch < len(l.inputCurves) {
			curve = l.inputCurves[ch]
		}
		for i := range inputTableEntries {
			val := float64(i) / float64(inputTableEntries-1)
			if curve != nil {
				val = curve.Evaluate(val)
			}
			putUint16(buf, offset+(ch*inputTableEntries+i)*2, uint16(clamp(val, 0, 1)*65535.0))
		}
	}
	offset += inputTableBytes

	// write CLUT (16-bit values)
	for i, v := range l.clut {
		putUint16(buf, offset+i*2, uint16(clamp(v, 0, 1)*65535.0))
	}
	offset += clutSize * 2

	// write output tables (16-bit)
	for ch := range l.outputChannels {
		var curve *Curve
		if ch < len(l.outputCurves) {
			curve = l.outputCurves[ch]
		}
		for i := range outputTableEntries {
			val := float64(i) / float64(outputTableEntries-1)
			if curve != nil {
				val = curve.Evaluate(val)
			}
			putUint16(buf, offset+(ch*outputTableEntries+i)*2, uint16(clamp(val, 0, 1)*65535.0))
		}
	}

	return buf, nil
}

func decodeLut16(data []byte) (*Lut16, error) {
	if len(data) < 52 {
		return nil, errInvalidTagData
	}

	inputChannels := int(data[8])
	outputChannels := int(data[9])
	clutPoints := int(data[10])

	if inputChannels == 0 || outputChannels == 0 || inputChannels > 15 || outputChannels > 15 {
		return nil, errInvalidTagData
	}

	// matrix at offset 12
	matrix := make([]float64, 9)
	for i := range 9 {
		matrix[i] = getS15Fixed16(data, 12+i*4)
	}
	if isIdentityMatrix3x3(matrix) {
		matrix = nil
	}

	inputTableEntries := int(getUint16(data, 48))
	outputTableEntries := int(getUint16(data, 50))

	// input tables
	inputTableStart := 52
	inputTableSize := inputTableEntries * inputChannels * 2
	if len(data) < inputTableStart+inputTableSize {
		return nil, errInvalidTagData
	}

	inputCurves := make([]*Curve, inputChannels)
	for ch := range inputChannels {
		table := make([]uint16, inputTableEntries)
		for i := range inputTableEntries {
			table[i] = getUint16(data, inputTableStart+(ch*inputTableEntries+i)*2)
		}
		inputCurves[ch] = &Curve{Table: table}
	}

	// CLUT size
	clutSize := computeCLUTSizeUniform(clutPoints, inputChannels, outputChannels)
	if clutSize == 0 {
		return nil, errInvalidTagData
	}

	clutStart := inputTableStart + inputTableSize
	if len(data) < clutStart+clutSize*2 {
		return nil, errInvalidTagData
	}

	clut := make([]float64, clutSize)
	for i := range clutSize {
		clut[i] = float64(getUint16(data, clutStart+i*2)) / 65535.0
	}

	// output tables
	outputTableStart := clutStart + clutSize*2
	outputTableBytes := outputTableEntries * outputChannels * 2
	if len(data) < outputTableStart+outputTableBytes {
		return nil, errInvalidTagData
	}

	outputCurves := make([]*Curve, outputChannels)
	for ch := range outputChannels {
		table := make([]uint16, outputTableEntries)
		for i := range outputTableEntries {
			table[i] = getUint16(data, outputTableStart+(ch*outputTableEntries+i)*2)
		}
		outputCurves[ch] = &Curve{Table: table}
	}

	return &Lut16{
		inputChannels:   inputChannels,
		outputChannels:  outputChannels,
		gridPoints:      clutPoints,
		matrix:          matrix,
		inputTableSize:  inputTableEntries,
		outputTableSize: outputTableEntries,
		inputCurves:     inputCurves,
		clut:            clut,
		outputCurves:    outputCurves,
	}, nil
}

// ----------------------------------------------------------------------------
// LutAToB - lutAtoBType (mAB)
// ----------------------------------------------------------------------------

// LutAToB represents an A-to-B LUT (lutAtoBType, tag signature "mAB ").
// Processing order: ACurves → CLUT → MCurves → Matrix → BCurves
type LutAToB struct {
	inputChannels  int
	outputChannels int
	aCurves        []*Curve  // input curves (one per input channel)
	gridPoints     []int     // grid size per dimension
	clut           []float64 // flattened n-dimensional table, normalised [0,1]
	clutPrecision  int       // 1 for 8-bit, 2 for 16-bit (default 2)
	mCurves        []*Curve  // curves between CLUT and matrix
	matrix         []float64 // 3×4, nil if identity
	bCurves        []*Curve  // output curves (one per output channel)
}

func (l *LutAToB) InputChannels() int  { return l.inputChannels }
func (l *LutAToB) OutputChannels() int { return l.outputChannels }

// Apply transforms input values through the LUT.
// Processing order: ACurves → CLUT → MCurves → Matrix → BCurves
func (l *LutAToB) Apply(input []float64) []float64 {
	if len(input) != l.inputChannels {
		return make([]float64, l.outputChannels)
	}

	values := make([]float64, len(input))
	copy(values, input)

	// A curves (input)
	values = applyCurves(l.aCurves, values)

	// CLUT
	values = l.applyCLUT(values)

	// M curves
	values = applyCurves(l.mCurves, values)

	// matrix
	values = applyMatrix3x4(l.matrix, values)

	// B curves (output)
	values = applyCurves(l.bCurves, values)

	// clamp output
	for i := range values {
		values[i] = clamp(values[i], 0, 1)
	}

	return values
}

func (l *LutAToB) applyCLUT(values []float64) []float64 {
	if l.clut == nil || len(l.gridPoints) != len(values) {
		return values
	}
	if len(values) == 3 && l.gridPoints[0] == l.gridPoints[1] && l.gridPoints[1] == l.gridPoints[2] {
		return tetrahedralInterp3D(l.clut, l.gridPoints[0], l.outputChannels, values[0], values[1], values[2])
	}
	return multilinearInterp(l.clut, l.gridPoints, l.outputChannels, values)
}

// Encode converts the LUT to lutAtoBType (mAB) format.
func (l *LutAToB) Encode() ([]byte, error) {
	return encodeLutAB(l.inputChannels, l.outputChannels, l.aCurves, l.gridPoints, l.clut, l.clutPrecision, l.mCurves, l.matrix, l.bCurves, false)
}

func decodeLutAToB(data []byte) (*LutAToB, error) {
	if len(data) < 32 {
		return nil, errInvalidTagData
	}

	inputChannels := int(data[8])
	outputChannels := int(data[9])

	if inputChannels == 0 || outputChannels == 0 || inputChannels > 15 || outputChannels > 15 {
		return nil, errInvalidTagData
	}

	bCurveOffset := getUint32(data, 12)
	matrixOffset := getUint32(data, 16)
	mCurveOffset := getUint32(data, 20)
	clutOffset := getUint32(data, 24)
	aCurveOffset := getUint32(data, 28)

	lut := &LutAToB{
		inputChannels:  inputChannels,
		outputChannels: outputChannels,
	}

	// decode B curves (output curves for mAB)
	if bCurveOffset != 0 {
		curves, err := decodeCurvesAtOffset(data, int(bCurveOffset), outputChannels)
		if err != nil {
			return nil, err
		}
		lut.bCurves = curves
	}

	// decode A curves (input curves for mAB)
	if aCurveOffset != 0 {
		curves, err := decodeCurvesAtOffset(data, int(aCurveOffset), inputChannels)
		if err != nil {
			return nil, err
		}
		lut.aCurves = curves
	}

	// decode matrix (3x4) - must decode before M curves to know if matrix is present
	if matrixOffset != 0 {
		matrix, err := decodeMatrix3x4(data, int(matrixOffset))
		if err != nil {
			return nil, err
		}
		lut.matrix = matrix
	}

	// decode M curves (3 channels when matrix is present, as M curves operate
	// on 3 channels between CLUT and matrix)
	if mCurveOffset != 0 {
		mCurveCount := 3 // M curves always operate on 3 channels (matrix input)
		curves, err := decodeCurvesAtOffset(data, int(mCurveOffset), mCurveCount)
		if err != nil {
			return nil, err
		}
		lut.mCurves = curves
	}

	// decode CLUT
	if clutOffset != 0 {
		gridPoints, clut, precision, err := decodeCLUT(data, int(clutOffset), inputChannels, outputChannels)
		if err != nil {
			return nil, err
		}
		lut.gridPoints = gridPoints
		lut.clut = clut
		lut.clutPrecision = precision
	}

	return lut, nil
}

// ----------------------------------------------------------------------------
// LutBToA - lutBtoAType (mBA)
// ----------------------------------------------------------------------------

// LutBToA represents a B-to-A LUT (lutBtoAType, tag signature "mBA ").
// Processing order: BCurves → Matrix → MCurves → CLUT → ACurves
type LutBToA struct {
	inputChannels  int
	outputChannels int
	bCurves        []*Curve  // input curves (one per input channel)
	matrix         []float64 // 3×4, nil if identity
	mCurves        []*Curve  // curves between matrix and CLUT
	gridPoints     []int     // grid size per dimension
	clut           []float64 // flattened n-dimensional table, normalised [0,1]
	clutPrecision  int       // 1 for 8-bit, 2 for 16-bit (default 2)
	aCurves        []*Curve  // output curves (one per output channel)
}

func (l *LutBToA) InputChannels() int  { return l.inputChannels }
func (l *LutBToA) OutputChannels() int { return l.outputChannels }

// Apply transforms input values through the LUT.
// Processing order: BCurves → Matrix → MCurves → CLUT → ACurves
func (l *LutBToA) Apply(input []float64) []float64 {
	if len(input) != l.inputChannels {
		return make([]float64, l.outputChannels)
	}

	values := make([]float64, len(input))
	copy(values, input)

	// B curves (input)
	values = applyCurves(l.bCurves, values)

	// matrix
	values = applyMatrix3x4(l.matrix, values)

	// M curves
	values = applyCurves(l.mCurves, values)

	// CLUT
	values = l.applyCLUT(values)

	// A curves (output)
	values = applyCurves(l.aCurves, values)

	// clamp output
	for i := range values {
		values[i] = clamp(values[i], 0, 1)
	}

	return values
}

func (l *LutBToA) applyCLUT(values []float64) []float64 {
	if l.clut == nil || len(l.gridPoints) != len(values) {
		return values
	}
	if len(values) == 3 && l.gridPoints[0] == l.gridPoints[1] && l.gridPoints[1] == l.gridPoints[2] {
		return tetrahedralInterp3D(l.clut, l.gridPoints[0], l.outputChannels, values[0], values[1], values[2])
	}
	return multilinearInterp(l.clut, l.gridPoints, l.outputChannels, values)
}

// Encode converts the LUT to lutBtoAType (mBA) format.
func (l *LutBToA) Encode() ([]byte, error) {
	return encodeLutAB(l.inputChannels, l.outputChannels, l.aCurves, l.gridPoints, l.clut, l.clutPrecision, l.mCurves, l.matrix, l.bCurves, true)
}

func decodeLutBToA(data []byte) (*LutBToA, error) {
	if len(data) < 32 {
		return nil, errInvalidTagData
	}

	inputChannels := int(data[8])
	outputChannels := int(data[9])

	if inputChannels == 0 || outputChannels == 0 || inputChannels > 15 || outputChannels > 15 {
		return nil, errInvalidTagData
	}

	bCurveOffset := getUint32(data, 12)
	matrixOffset := getUint32(data, 16)
	mCurveOffset := getUint32(data, 20)
	clutOffset := getUint32(data, 24)
	aCurveOffset := getUint32(data, 28)

	lut := &LutBToA{
		inputChannels:  inputChannels,
		outputChannels: outputChannels,
	}

	// decode B curves (input curves for mBA)
	if bCurveOffset != 0 {
		curves, err := decodeCurvesAtOffset(data, int(bCurveOffset), inputChannels)
		if err != nil {
			return nil, err
		}
		lut.bCurves = curves
	}

	// decode A curves (output curves for mBA)
	if aCurveOffset != 0 {
		curves, err := decodeCurvesAtOffset(data, int(aCurveOffset), outputChannels)
		if err != nil {
			return nil, err
		}
		lut.aCurves = curves
	}

	// decode matrix (3x4) - must decode before M curves to know if matrix is present
	if matrixOffset != 0 {
		matrix, err := decodeMatrix3x4(data, int(matrixOffset))
		if err != nil {
			return nil, err
		}
		lut.matrix = matrix
	}

	// decode M curves (3 channels when matrix is present, as M curves operate
	// between matrix output and CLUT input)
	if mCurveOffset != 0 {
		mCurveCount := 3 // M curves always operate on 3 channels (matrix output)
		curves, err := decodeCurvesAtOffset(data, int(mCurveOffset), mCurveCount)
		if err != nil {
			return nil, err
		}
		lut.mCurves = curves
	}

	// decode CLUT
	if clutOffset != 0 {
		gridPoints, clut, precision, err := decodeCLUT(data, int(clutOffset), inputChannels, outputChannels)
		if err != nil {
			return nil, err
		}
		lut.gridPoints = gridPoints
		lut.clut = clut
		lut.clutPrecision = precision
	}

	return lut, nil
}

// ----------------------------------------------------------------------------
// Helper functions
// ----------------------------------------------------------------------------

// computeCLUTSize calculates the total CLUT size with overflow checking.
func computeCLUTSize(gridPoints []int, outputChannels int) int {
	const maxSize = 1 << 30
	size := uint64(1)
	for _, g := range gridPoints {
		size *= uint64(g)
		if size > maxSize {
			return 0
		}
	}
	size *= uint64(outputChannels)
	if size > maxSize {
		return 0
	}
	return int(size)
}

// computeCLUTSizeUniform calculates CLUT size for uniform grid.
func computeCLUTSizeUniform(gridPoints, inputChannels, outputChannels int) int {
	const maxSize = 1 << 30
	size := uint64(1)
	for range inputChannels {
		size *= uint64(gridPoints)
		if size > maxSize {
			return 0
		}
	}
	size *= uint64(outputChannels)
	if size > maxSize {
		return 0
	}
	return int(size)
}

func isIdentityMatrix3x3(m []float64) bool {
	if len(m) != 9 {
		return false
	}
	identity := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	for i := range 9 {
		if math.Abs(m[i]-identity[i]) > 1e-6 {
			return false
		}
	}
	return true
}

func isIdentityMatrix3x4(m []float64) bool {
	if len(m) != 12 {
		return false
	}
	identity := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}
	for i := range 12 {
		if math.Abs(m[i]-identity[i]) > 1e-6 {
			return false
		}
	}
	return true
}

func applyCurves(curves []*Curve, values []float64) []float64 {
	if curves == nil {
		return values
	}
	for i, c := range curves {
		if c != nil && i < len(values) {
			values[i] = c.Evaluate(values[i])
		}
	}
	return values
}

func applyMatrix3x3(m []float64, values []float64) []float64 {
	if m == nil || len(values) != 3 {
		return values
	}
	x, y, z := values[0], values[1], values[2]
	return []float64{
		m[0]*x + m[1]*y + m[2]*z,
		m[3]*x + m[4]*y + m[5]*z,
		m[6]*x + m[7]*y + m[8]*z,
	}
}

func applyMatrix3x4(m []float64, values []float64) []float64 {
	if m == nil || len(values) != 3 {
		return values
	}
	x, y, z := values[0], values[1], values[2]
	return []float64{
		m[0]*x + m[1]*y + m[2]*z + m[9],
		m[3]*x + m[4]*y + m[5]*z + m[10],
		m[6]*x + m[7]*y + m[8]*z + m[11],
	}
}

func decodeCurvesAtOffset(data []byte, offset int, numCurves int) ([]*Curve, error) {
	curves := make([]*Curve, numCurves)
	pos := offset
	for i := range numCurves {
		if pos+8 > len(data) {
			return nil, errInvalidTagData
		}

		typeID := string(data[pos : pos+4])
		var size int
		switch typeID {
		case "curv":
			if pos+12 > len(data) {
				return nil, errInvalidTagData
			}
			n := getUint32(data, pos+8)
			size = 12 + int(n)*2
		case "para":
			if pos+12 > len(data) {
				return nil, errInvalidTagData
			}
			funcType := int(getUint16(data, pos+8))
			numParams := []int{1, 3, 4, 5, 7}[min(funcType, 4)]
			size = 12 + numParams*4
		default:
			return nil, errUnexpectedType
		}

		size = (size + 3) &^ 3

		if pos+size > len(data) {
			return nil, errInvalidTagData
		}

		curve, err := DecodeCurve(data[pos : pos+size])
		if err != nil {
			return nil, err
		}
		curves[i] = curve
		pos += size
	}
	return curves, nil
}

func decodeMatrix3x4(data []byte, offset int) ([]float64, error) {
	if offset+48 > len(data) {
		return nil, errInvalidTagData
	}
	matrix := make([]float64, 12)
	for i := range 12 {
		matrix[i] = getS15Fixed16(data, offset+i*4)
	}
	if isIdentityMatrix3x4(matrix) {
		return nil, nil
	}
	return matrix, nil
}

func decodeCLUT(data []byte, offset int, inputChannels, outputChannels int) ([]int, []float64, int, error) {
	if offset+20 > len(data) {
		return nil, nil, 0, errInvalidTagData
	}

	gridPoints := make([]int, inputChannels)
	for i := range inputChannels {
		gridPoints[i] = int(data[offset+i])
		if gridPoints[i] == 0 {
			gridPoints[i] = 1
		}
	}

	precision := int(data[offset+16])

	size := computeCLUTSize(gridPoints, outputChannels)
	if size == 0 {
		return nil, nil, 0, errInvalidTagData
	}

	clutDataStart := offset + 20
	var clut []float64
	switch precision {
	case 1:
		if len(data) < clutDataStart+size {
			return nil, nil, 0, errInvalidTagData
		}
		clut = make([]float64, size)
		for i := range size {
			clut[i] = float64(data[clutDataStart+i]) / 255.0
		}
	case 2:
		if len(data) < clutDataStart+size*2 {
			return nil, nil, 0, errInvalidTagData
		}
		clut = make([]float64, size)
		for i := range size {
			clut[i] = float64(getUint16(data, clutDataStart+i*2)) / 65535.0
		}
	default:
		return nil, nil, 0, errInvalidTagData
	}

	return gridPoints, clut, precision, nil
}

func encodeLutAB(inputChannels, outputChannels int, aCurves []*Curve, gridPoints []int, clut []float64, clutPrecision int, mCurves []*Curve, matrix []float64, bCurves []*Curve, isBToA bool) ([]byte, error) {
	offset := uint32(32)

	// determine curve counts based on format
	var aCurveCount, bCurveCount int
	if isBToA {
		bCurveCount = inputChannels
		aCurveCount = outputChannels
	} else {
		aCurveCount = inputChannels
		bCurveCount = outputChannels
	}
	// M curves always operate on 3 channels (matrix input/output)
	mCurveCount := 3

	// calculate B curves offset
	var bCurveOffset uint32
	var bCurveData []byte
	if len(bCurves) > 0 {
		bCurveOffset = offset
		bCurveData = encodeCurves(bCurves, bCurveCount)
		offset += uint32(len(bCurveData))
	}

	// calculate matrix offset
	var matrixOffset uint32
	if len(matrix) >= 9 {
		offset = align4(offset)
		matrixOffset = offset
		offset += 48
	}

	// calculate M curves offset
	var mCurveOffset uint32
	var mCurveData []byte
	if len(mCurves) > 0 {
		offset = align4(offset)
		mCurveOffset = offset
		mCurveData = encodeCurves(mCurves, mCurveCount)
		offset += uint32(len(mCurveData))
	}

	// calculate CLUT offset
	var clutOffset uint32
	var clutData []byte
	if clut != nil && len(gridPoints) > 0 {
		offset = align4(offset)
		clutOffset = offset
		clutData = encodeCLUT(gridPoints, outputChannels, clut, clutPrecision)
		offset += uint32(len(clutData))
	}

	// calculate A curves offset
	var aCurveOffset uint32
	var aCurveData []byte
	if len(aCurves) > 0 {
		offset = align4(offset)
		aCurveOffset = offset
		aCurveData = encodeCurves(aCurves, aCurveCount)
		offset += uint32(len(aCurveData))
	}

	buf := make([]byte, align4(offset))

	if isBToA {
		copy(buf[0:4], "mBA ")
	} else {
		copy(buf[0:4], "mAB ")
	}
	buf[8] = byte(inputChannels)
	buf[9] = byte(outputChannels)
	putUint32(buf, 12, bCurveOffset)
	putUint32(buf, 16, matrixOffset)
	putUint32(buf, 20, mCurveOffset)
	putUint32(buf, 24, clutOffset)
	putUint32(buf, 28, aCurveOffset)

	if bCurveOffset != 0 {
		copy(buf[bCurveOffset:], bCurveData)
	}

	if matrixOffset != 0 {
		matrix12 := make([]float64, 12)
		copy(matrix12, matrix)
		for i := range 12 {
			putS15Fixed16(buf, int(matrixOffset)+i*4, matrix12[i])
		}
	}

	if mCurveOffset != 0 {
		copy(buf[mCurveOffset:], mCurveData)
	}

	if clutOffset != 0 {
		copy(buf[clutOffset:], clutData)
	}

	if aCurveOffset != 0 {
		copy(buf[aCurveOffset:], aCurveData)
	}

	return buf, nil
}

func encodeCLUT(gridPoints []int, outputChannels int, clut []float64, precision int) []byte {
	size := computeCLUTSize(gridPoints, outputChannels)

	// default to 16-bit precision if not specified
	if precision != 1 {
		precision = 2
	}

	var buf []byte
	if precision == 1 {
		buf = make([]byte, 20+size)
	} else {
		buf = make([]byte, 20+size*2)
	}

	for i, g := range gridPoints {
		if i < 16 {
			buf[i] = byte(g)
		}
	}

	buf[16] = byte(precision)

	if precision == 1 {
		for i, v := range clut {
			buf[20+i] = byte(clamp(v, 0, 1) * 255.0)
		}
	} else {
		for i, v := range clut {
			putUint16(buf, 20+i*2, uint16(clamp(v, 0, 1)*65535.0))
		}
	}

	return buf
}

func encodeCurves(curves []*Curve, count int) []byte {
	var buf []byte
	for i := range count {
		var curveData []byte
		if i < len(curves) && curves[i] != nil {
			curveData = curves[i].Encode()
		} else {
			curveData = (&Curve{Gamma: 1.0}).Encode()
		}
		for len(curveData)%4 != 0 {
			curveData = append(curveData, 0)
		}
		buf = append(buf, curveData...)
	}
	return buf
}

func align4(n uint32) uint32 {
	return (n + 3) &^ 3
}
