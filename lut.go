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

	"seehuhn.de/go/membudget"
)

// LUT represents a colour lookup table from an ICC profile.
// The four concrete implementations are [LUT8], [LUT16], [LUTAToB], and
// [LUTBToA].
type LUT interface {
	// Apply transforms input through the LUT, writing OutputChannels()
	// values into dst. Input values should be normalised to [0, 1] and the
	// dst slice must have length >= OutputChannels(). The Workspace argument
	// is used as scratch space.
	Apply(input, dst []float64, ws *Workspace)

	// Encode converts the LUT to binary ICC tag data.
	Encode() ([]byte, error)

	// InputChannels returns the number of input channels.
	InputChannels() int

	// OutputChannels returns the number of output channels.
	OutputChannels() int
}

// DecodeLUT decodes a LUT from ICC tag data.
// This is used for AToB0, AToB1, AToB2, BToA0, BToA1, and BToA2 tags.
func DecodeLUT(data []byte) (LUT, error) {
	if len(data) < 8 {
		return nil, errInvalidTagData
	}

	b := newBudget(int64(len(data)))
	typeID := string(data[0:4])
	switch typeID {
	case "mft1":
		return decodeLUT8(b, data)
	case "mft2":
		return decodeLUT16(b, data)
	case "mAB ":
		return decodeLUTAToB(b, data)
	case "mBA ":
		return decodeLUTBToA(b, data)
	default:
		return nil, errUnexpectedType
	}
}

// ----------------------------------------------------------------------------
// LUT8 - lut8Type (mft1)
// ----------------------------------------------------------------------------

// LUT8 represents an 8-bit LUT (lut8Type, tag signature "mft1").
// Processing order: Matrix → InputCurves → CLUT → OutputCurves
type LUT8 struct {
	nInput     int
	nOutput    int
	gridPoints int // same for all dimensions

	matrix       []float64 // 3×3, nil if identity
	inputCurves  []*Curve  // one per input channel
	clut         []float32 // flattened n-dimensional table, normalised [0,1]
	outputCurves []*Curve  // one per output channel
}

func (l *LUT8) InputChannels() int  { return l.nInput }
func (l *LUT8) OutputChannels() int { return l.nOutput }

// Apply implements the [LUT] interface.
func (l *LUT8) Apply(input, dst []float64, ws *Workspace) {
	if len(input) != l.nInput {
		zeroFloats(dst[:l.nOutput])
		return
	}
	v := ws.values(l.nInput)
	copy(v, input)
	applyMatrix3x3InPlace(l.matrix, v)
	applyCurves(l.inputCurves, v)
	clutScalarGridInto(l.clut, l.gridPoints, l.nInput, l.nOutput, v, dst, ws)
	applyCurves(l.outputCurves, dst[:l.nOutput])
	clampInto(dst[:l.nOutput])
}

// Encode converts the LUT to lut8Type (mft1) format.
func (l *LUT8) Encode() ([]byte, error) {
	inputTableSize := 256 * l.nInput
	clutSize := computeCLUTSizeUniform(l.gridPoints, l.nInput, l.nOutput)
	outputTableSize := 256 * l.nOutput
	totalSize := 48 + inputTableSize + clutSize + outputTableSize

	buf := make([]byte, totalSize)
	copy(buf[0:4], "mft1")
	buf[8] = byte(l.nInput)
	buf[9] = byte(l.nOutput)
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
	for ch := range l.nInput {
		var curve *Curve
		if ch < len(l.inputCurves) {
			curve = l.inputCurves[ch]
		}
		for i := range 256 {
			val := float64(i) / 255.0
			if curve != nil {
				val = curve.Evaluate(val)
			}
			buf[offset+ch*256+i] = quantise8(val)
		}
	}
	offset += inputTableSize

	// write CLUT (8-bit values)
	for i, v := range l.clut {
		buf[offset+i] = quantise8(float64(v))
	}
	offset += clutSize

	// write output tables (256 entries per channel, 8-bit)
	for ch := range l.nOutput {
		var curve *Curve
		if ch < len(l.outputCurves) {
			curve = l.outputCurves[ch]
		}
		for i := range 256 {
			val := float64(i) / 255.0
			if curve != nil {
				val = curve.Evaluate(val)
			}
			buf[offset+ch*256+i] = quantise8(val)
		}
	}

	return buf, nil
}

func decodeLUT8(b *membudget.Budget, data []byte) (*LUT8, error) {
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
	matrix, err := membudget.AllocSlice[float64](b, 9)
	if err != nil {
		return nil, err
	}
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

	inputCurves, err := membudget.AllocSlice[*Curve](b, inputChannels)
	if err != nil {
		return nil, err
	}
	for ch := range inputChannels {
		table, err := membudget.AllocSlice[uint16](b, 256)
		if err != nil {
			return nil, err
		}
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

	clut, err := membudget.AllocSlice[float32](b, clutSize)
	if err != nil {
		return nil, err
	}
	for i := range clutSize {
		clut[i] = float32(data[clutStart+i]) / 255
	}

	// output tables: 256 entries per channel
	outputTableStart := clutStart + clutSize
	outputTableSize := 256 * outputChannels
	if len(data) < outputTableStart+outputTableSize {
		return nil, errInvalidTagData
	}

	outputCurves, err := membudget.AllocSlice[*Curve](b, outputChannels)
	if err != nil {
		return nil, err
	}
	for ch := range outputChannels {
		table, err := membudget.AllocSlice[uint16](b, 256)
		if err != nil {
			return nil, err
		}
		for i := range 256 {
			// scale 8-bit to 16-bit: 0x00->0x0000, 0xFF->0xFFFF
			v := uint16(data[outputTableStart+ch*256+i])
			table[i] = v<<8 | v
		}
		outputCurves[ch] = &Curve{Table: table}
	}

	return &LUT8{
		nInput:       inputChannels,
		nOutput:      outputChannels,
		gridPoints:   clutPoints,
		matrix:       matrix,
		inputCurves:  inputCurves,
		clut:         clut,
		outputCurves: outputCurves,
	}, nil
}

// ----------------------------------------------------------------------------
// LUT16 - lut16Type (mft2)
// ----------------------------------------------------------------------------

// LUT16 represents a 16-bit LUT (lut16Type, tag signature "mft2").
// Processing order: Matrix → InputCurves → CLUT → OutputCurves
//
// The values this type reads and writes are the tag's own encoding, not
// interpreted colour.  Where a lut16Type exchanges PCS Lab, that encoding is
// the legacy 16-bit one, which puts L* = 100 and a* = b* = +127 at 0xFF00
// rather than at 0xFFFF.  Converting to and from Lab therefore needs the
// profile header, which this type does not see; [Transform] handles it.
// A caller applying the LUT directly must scale the normalised values by
// 65535/65280 after Apply, or by the reciprocal before it.
type LUT16 struct {
	nInput     int
	nOutput    int
	gridPoints int // same for all dimensions

	matrix          []float64 // 3×3, nil if identity
	inputTableSize  int       // entries per input curve
	inputCurves     []*Curve  // one per input channel
	clut            []float32 // flattened n-dimensional table, normalised [0,1]
	outputTableSize int       // entries per output curve
	outputCurves    []*Curve  // one per output channel
}

func (l *LUT16) InputChannels() int  { return l.nInput }
func (l *LUT16) OutputChannels() int { return l.nOutput }

// Apply implements the [LUT] interface.
func (l *LUT16) Apply(input, dst []float64, ws *Workspace) {
	if len(input) != l.nInput {
		zeroFloats(dst[:l.nOutput])
		return
	}
	v := ws.values(l.nInput)
	copy(v, input)
	applyMatrix3x3InPlace(l.matrix, v)
	applyCurves(l.inputCurves, v)
	clutScalarGridInto(l.clut, l.gridPoints, l.nInput, l.nOutput, v, dst, ws)
	applyCurves(l.outputCurves, dst[:l.nOutput])
	clampInto(dst[:l.nOutput])
}

// Encode converts the LUT to lut16Type (mft2) format.
func (l *LUT16) Encode() ([]byte, error) {
	inputTableEntries := l.inputTableSize
	if inputTableEntries == 0 {
		inputTableEntries = 256
	}
	outputTableEntries := l.outputTableSize
	if outputTableEntries == 0 {
		outputTableEntries = 256
	}

	inputTableBytes := inputTableEntries * l.nInput * 2
	clutSize := computeCLUTSizeUniform(l.gridPoints, l.nInput, l.nOutput)
	outputTableBytes := outputTableEntries * l.nOutput * 2
	totalSize := 52 + inputTableBytes + clutSize*2 + outputTableBytes

	buf := make([]byte, totalSize)
	copy(buf[0:4], "mft2")
	buf[8] = byte(l.nInput)
	buf[9] = byte(l.nOutput)
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
	for ch := range l.nInput {
		var curve *Curve
		if ch < len(l.inputCurves) {
			curve = l.inputCurves[ch]
		}
		for i := range inputTableEntries {
			val := float64(i) / float64(inputTableEntries-1)
			if curve != nil {
				val = curve.Evaluate(val)
			}
			putUint16(buf, offset+(ch*inputTableEntries+i)*2, quantise16(val))
		}
	}
	offset += inputTableBytes

	// write CLUT (16-bit values)
	for i, v := range l.clut {
		putUint16(buf, offset+i*2, quantise16(float64(v)))
	}
	offset += clutSize * 2

	// write output tables (16-bit)
	for ch := range l.nOutput {
		var curve *Curve
		if ch < len(l.outputCurves) {
			curve = l.outputCurves[ch]
		}
		for i := range outputTableEntries {
			val := float64(i) / float64(outputTableEntries-1)
			if curve != nil {
				val = curve.Evaluate(val)
			}
			putUint16(buf, offset+(ch*outputTableEntries+i)*2, quantise16(val))
		}
	}

	return buf, nil
}

func decodeLUT16(b *membudget.Budget, data []byte) (*LUT16, error) {
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
	matrix, err := membudget.AllocSlice[float64](b, 9)
	if err != nil {
		return nil, err
	}
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

	inputCurves, err := membudget.AllocSlice[*Curve](b, inputChannels)
	if err != nil {
		return nil, err
	}
	for ch := range inputChannels {
		table, err := membudget.AllocSlice[uint16](b, inputTableEntries)
		if err != nil {
			return nil, err
		}
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

	clut, err := membudget.AllocSlice[float32](b, clutSize)
	if err != nil {
		return nil, err
	}
	for i := range clutSize {
		clut[i] = float32(getUint16(data, clutStart+i*2)) / 65535
	}

	// output tables
	outputTableStart := clutStart + clutSize*2
	outputTableBytes := outputTableEntries * outputChannels * 2
	if len(data) < outputTableStart+outputTableBytes {
		return nil, errInvalidTagData
	}

	outputCurves, err := membudget.AllocSlice[*Curve](b, outputChannels)
	if err != nil {
		return nil, err
	}
	for ch := range outputChannels {
		table, err := membudget.AllocSlice[uint16](b, outputTableEntries)
		if err != nil {
			return nil, err
		}
		for i := range outputTableEntries {
			table[i] = getUint16(data, outputTableStart+(ch*outputTableEntries+i)*2)
		}
		outputCurves[ch] = &Curve{Table: table}
	}

	return &LUT16{
		nInput:          inputChannels,
		nOutput:         outputChannels,
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
// LUTAToB - lutAtoBType (mAB)
// ----------------------------------------------------------------------------

// LUTAToB represents an A-to-B LUT (lutAtoBType, tag signature "mAB ").
// Processing order: ACurves → CLUT → MCurves → Matrix → BCurves
type LUTAToB struct {
	nInput     int
	nOutput    int
	gridPoints []int // grid size per dimension

	aCurves       []*Curve  // input curves (one per input channel)
	clutPrecision int       // 1 for 8-bit, 2 for 16-bit (default 2)
	clut          []float32 // flattened n-dimensional table, normalised [0,1]
	mCurves       []*Curve  // curves between CLUT and matrix
	matrix        []float64 // 3×4, nil if identity
	bCurves       []*Curve  // output curves (one per output channel)
}

func (l *LUTAToB) InputChannels() int  { return l.nInput }
func (l *LUTAToB) OutputChannels() int { return l.nOutput }

// Apply implements the [LUT] interface.
func (l *LUTAToB) Apply(input, dst []float64, ws *Workspace) {
	if len(input) != l.nInput {
		zeroFloats(dst[:l.nOutput])
		return
	}
	v := ws.values(l.nInput)
	copy(v, input)
	applyCurves(l.aCurves, v)
	clutGridInto(l.clut, l.gridPoints, l.nOutput, v, dst, ws)
	applyCurves(l.mCurves, dst[:l.nOutput])
	applyMatrix3x4InPlace(l.matrix, dst[:l.nOutput])
	applyCurves(l.bCurves, dst[:l.nOutput])
	clampInto(dst[:l.nOutput])
}

// Encode converts the LUT to lutAtoBType (mAB) format.
func (l *LUTAToB) Encode() ([]byte, error) {
	return encodeLUTAB(l.nInput, l.nOutput, l.aCurves, l.gridPoints, l.clut, l.clutPrecision, l.mCurves, l.matrix, l.bCurves, false)
}

func decodeLUTAToB(b *membudget.Budget, data []byte) (*LUTAToB, error) {
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

	lut := &LUTAToB{
		nInput:  inputChannels,
		nOutput: outputChannels,
	}

	// decode B curves (output curves for mAB)
	if bCurveOffset != 0 {
		curves, err := decodeCurvesAtOffset(b, data, int(bCurveOffset), outputChannels)
		if err != nil {
			return nil, err
		}
		lut.bCurves = curves
	}

	// decode A curves (input curves for mAB)
	if aCurveOffset != 0 {
		curves, err := decodeCurvesAtOffset(b, data, int(aCurveOffset), inputChannels)
		if err != nil {
			return nil, err
		}
		lut.aCurves = curves
	}

	// decode matrix (3x4) - must decode before M curves to know if matrix is present
	if matrixOffset != 0 {
		matrix, err := decodeMatrix3x4(b, data, int(matrixOffset))
		if err != nil {
			return nil, err
		}
		lut.matrix = matrix
	}

	// decode M curves (3 channels when matrix is present, as M curves operate
	// on 3 channels between CLUT and matrix)
	if mCurveOffset != 0 {
		mCurveCount := 3 // M curves always operate on 3 channels (matrix input)
		curves, err := decodeCurvesAtOffset(b, data, int(mCurveOffset), mCurveCount)
		if err != nil {
			return nil, err
		}
		lut.mCurves = curves
	}

	// decode CLUT
	if clutOffset != 0 {
		gridPoints, clut, precision, err := decodeCLUT(b, data, int(clutOffset), inputChannels, outputChannels)
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
// LUTBToA - lutBtoAType (mBA)
// ----------------------------------------------------------------------------

// LUTBToA represents a B-to-A LUT (lutBtoAType, tag signature "mBA ").
// Processing order: BCurves → Matrix → MCurves → CLUT → ACurves
type LUTBToA struct {
	nInput     int
	nOutput    int
	gridPoints []int // grid size per dimension

	bCurves       []*Curve  // input curves (one per input channel)
	matrix        []float64 // 3×4, nil if identity
	mCurves       []*Curve  // curves between matrix and CLUT
	clutPrecision int       // 1 for 8-bit, 2 for 16-bit (default 2)
	clut          []float32 // flattened n-dimensional table, normalised [0,1]
	aCurves       []*Curve  // output curves (one per output channel)
}

func (l *LUTBToA) InputChannels() int  { return l.nInput }
func (l *LUTBToA) OutputChannels() int { return l.nOutput }

// Apply implements the [LUT] interface.
func (l *LUTBToA) Apply(input, dst []float64, ws *Workspace) {
	if len(input) != l.nInput {
		zeroFloats(dst[:l.nOutput])
		return
	}
	v := ws.values(l.nInput)
	copy(v, input)
	applyCurves(l.bCurves, v)
	applyMatrix3x4InPlace(l.matrix, v)
	applyCurves(l.mCurves, v)
	clutGridInto(l.clut, l.gridPoints, l.nOutput, v, dst, ws)
	applyCurves(l.aCurves, dst[:l.nOutput])
	clampInto(dst[:l.nOutput])
}

// Encode converts the LUT to lutBtoAType (mBA) format.
func (l *LUTBToA) Encode() ([]byte, error) {
	return encodeLUTAB(l.nInput, l.nOutput, l.aCurves, l.gridPoints, l.clut, l.clutPrecision, l.mCurves, l.matrix, l.bCurves, true)
}

func decodeLUTBToA(b *membudget.Budget, data []byte) (*LUTBToA, error) {
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

	lut := &LUTBToA{
		nInput:  inputChannels,
		nOutput: outputChannels,
	}

	// decode B curves (input curves for mBA)
	if bCurveOffset != 0 {
		curves, err := decodeCurvesAtOffset(b, data, int(bCurveOffset), inputChannels)
		if err != nil {
			return nil, err
		}
		lut.bCurves = curves
	}

	// decode A curves (output curves for mBA)
	if aCurveOffset != 0 {
		curves, err := decodeCurvesAtOffset(b, data, int(aCurveOffset), outputChannels)
		if err != nil {
			return nil, err
		}
		lut.aCurves = curves
	}

	// decode matrix (3x4) - must decode before M curves to know if matrix is present
	if matrixOffset != 0 {
		matrix, err := decodeMatrix3x4(b, data, int(matrixOffset))
		if err != nil {
			return nil, err
		}
		lut.matrix = matrix
	}

	// decode M curves (3 channels when matrix is present, as M curves operate
	// between matrix output and CLUT input)
	if mCurveOffset != 0 {
		mCurveCount := 3 // M curves always operate on 3 channels (matrix output)
		curves, err := decodeCurvesAtOffset(b, data, int(mCurveOffset), mCurveCount)
		if err != nil {
			return nil, err
		}
		lut.mCurves = curves
	}

	// decode CLUT
	if clutOffset != 0 {
		gridPoints, clut, precision, err := decodeCLUT(b, data, int(clutOffset), inputChannels, outputChannels)
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

// applyMatrix3x3InPlace multiplies the 3x3 matrix m by v[:3], overwriting v
// with the result. It is a no-op if m is nil or v is not of length 3.
func applyMatrix3x3InPlace(m, v []float64) {
	if m == nil || len(v) != 3 {
		return
	}
	x, y, z := v[0], v[1], v[2]
	v[0] = m[0]*x + m[1]*y + m[2]*z
	v[1] = m[3]*x + m[4]*y + m[5]*z
	v[2] = m[6]*x + m[7]*y + m[8]*z
}

// applyMatrix3x4InPlace multiplies the 3x4 affine matrix m by v[:3], overwriting
// v with the result. It is a no-op if m is nil or v is not of length 3.
func applyMatrix3x4InPlace(m, v []float64) {
	if m == nil || len(v) != 3 {
		return
	}
	x, y, z := v[0], v[1], v[2]
	v[0] = m[0]*x + m[1]*y + m[2]*z + m[9]
	v[1] = m[3]*x + m[4]*y + m[5]*z + m[10]
	v[2] = m[6]*x + m[7]*y + m[8]*z + m[11]
}

// clutGridInto applies a CLUT to values, writing outChannels results into dst.
// gridPoints holds the grid size of each input dimension. Scratch is taken from
// ws. It is the allocation-free counterpart of the per-LUT applyCLUT methods.
func clutGridInto(clut []float32, gridPoints []int, outChannels int, values, dst []float64, ws *Workspace) {
	if clut == nil || len(gridPoints) != len(values) {
		// degenerate: mirror applyCLUT returning values unchanged
		n := min(len(values), outChannels)
		zeroFloats(dst[:outChannels])
		copy(dst[:n], values[:n])
		return
	}
	n := len(gridPoints)
	if n == 3 && gridPoints[0] == gridPoints[1] && gridPoints[1] == gridPoints[2] {
		tetrahedralInterp3DInto(clut, gridPoints[0], outChannels, values[0], values[1], values[2], dst)
		return
	}
	strides := ws.strides(n)
	computeStrides(strides, gridPoints, outChannels)
	indices := ws.indices(n)
	fracs := ws.fracs(n)
	order := ws.order(n)
	simplexInterpInto(clut, gridPoints, strides, outChannels, values, dst, indices, order, fracs)
}

// clutScalarGridInto is [clutGridInto] for LUTs (LUT8/LUT16) whose grid size is
// the same scalar gridSize for every one of the inputChannels dimensions.
func clutScalarGridInto(clut []float32, gridSize, inputChannels, outChannels int, values, dst []float64, ws *Workspace) {
	if clut == nil || gridSize == 0 {
		n := min(len(values), outChannels)
		zeroFloats(dst[:outChannels])
		copy(dst[:n], values[:n])
		return
	}
	if len(values) == 3 {
		tetrahedralInterp3DInto(clut, gridSize, outChannels, values[0], values[1], values[2], dst)
		return
	}
	grid := ws.grid(inputChannels)
	for i := range grid {
		grid[i] = gridSize
	}
	clutGridInto(clut, grid, outChannels, values, dst, ws)
}

func zeroFloats(v []float64) {
	for i := range v {
		v[i] = 0
	}
}

func clampInto(v []float64) {
	for i := range v {
		v[i] = clamp(v[i], 0, 1)
	}
}

func decodeCurvesAtOffset(b *membudget.Budget, data []byte, offset int, numCurves int) ([]*Curve, error) {
	curves, err := membudget.AllocSlice[*Curve](b, numCurves)
	if err != nil {
		return nil, err
	}
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

		curve, err := decodeCurve(b, data[pos:pos+size])
		if err != nil {
			return nil, err
		}
		curves[i] = curve
		pos += size
	}
	return curves, nil
}

func decodeMatrix3x4(b *membudget.Budget, data []byte, offset int) ([]float64, error) {
	if offset+48 > len(data) {
		return nil, errInvalidTagData
	}
	matrix, err := membudget.AllocSlice[float64](b, 12)
	if err != nil {
		return nil, err
	}
	for i := range 12 {
		matrix[i] = getS15Fixed16(data, offset+i*4)
	}
	if isIdentityMatrix3x4(matrix) {
		return nil, nil
	}
	return matrix, nil
}

func decodeCLUT(b *membudget.Budget, data []byte, offset int, inputChannels, outputChannels int) ([]int, []float32, int, error) {
	if offset+20 > len(data) {
		return nil, nil, 0, errInvalidTagData
	}

	gridPoints, err := membudget.AllocSlice[int](b, inputChannels)
	if err != nil {
		return nil, nil, 0, err
	}
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
	var clut []float32
	switch precision {
	case 1:
		if len(data) < clutDataStart+size {
			return nil, nil, 0, errInvalidTagData
		}
		clut, err = membudget.AllocSlice[float32](b, size)
		if err != nil {
			return nil, nil, 0, err
		}
		for i := range size {
			clut[i] = float32(data[clutDataStart+i]) / 255
		}
	case 2:
		if len(data) < clutDataStart+size*2 {
			return nil, nil, 0, errInvalidTagData
		}
		clut, err = membudget.AllocSlice[float32](b, size)
		if err != nil {
			return nil, nil, 0, err
		}
		for i := range size {
			clut[i] = float32(getUint16(data, clutDataStart+i*2)) / 65535
		}
	default:
		return nil, nil, 0, errInvalidTagData
	}

	return gridPoints, clut, precision, nil
}

func encodeLUTAB(inputChannels, outputChannels int, aCurves []*Curve, gridPoints []int, clut []float32, clutPrecision int, mCurves []*Curve, matrix []float64, bCurves []*Curve, isBToA bool) ([]byte, error) {
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
		var err error
		bCurveData, err = encodeCurves(bCurves, bCurveCount)
		if err != nil {
			return nil, err
		}
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
		var err error
		mCurveData, err = encodeCurves(mCurves, mCurveCount)
		if err != nil {
			return nil, err
		}
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
		var err error
		aCurveData, err = encodeCurves(aCurves, aCurveCount)
		if err != nil {
			return nil, err
		}
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

func encodeCLUT(gridPoints []int, outputChannels int, clut []float32, precision int) []byte {
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
			buf[20+i] = quantise8(float64(v))
		}
	} else {
		for i, v := range clut {
			putUint16(buf, 20+i*2, quantise16(float64(v)))
		}
	}

	return buf
}

func encodeCurves(curves []*Curve, count int) ([]byte, error) {
	var buf []byte
	for i := range count {
		curve := &Curve{Gamma: 1.0}
		if i < len(curves) && curves[i] != nil {
			curve = curves[i]
		}
		curveData, err := curve.Encode()
		if err != nil {
			return nil, err
		}
		for len(curveData)%4 != 0 {
			curveData = append(curveData, 0)
		}
		buf = append(buf, curveData...)
	}
	return buf, nil
}

func align4(n uint32) uint32 {
	return (n + 3) &^ 3
}
