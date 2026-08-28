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
	"testing"

	"seehuhn.de/go/membudget"
)

// makeMft1 builds a minimal but valid mft1 tag with the given channel
// and grid-point counts, zero-filled tables and CLUT.
func makeMft1(inputCh, outputCh, gridPoints int) []byte {
	inputTable := 256 * inputCh
	outputTable := 256 * outputCh
	clutSize := 1
	for range inputCh {
		clutSize *= gridPoints
	}
	clutSize *= outputCh
	data := make([]byte, 48+inputTable+clutSize+outputTable)
	copy(data[0:4], "mft1")
	data[8] = byte(inputCh)
	data[9] = byte(outputCh)
	data[10] = byte(gridPoints)
	identity := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	for i, v := range identity {
		putS15Fixed16(data, 12+i*4, v)
	}
	return data
}

// makeMft2 builds a minimal but valid mft2 tag.
func makeMft2(inputCh, outputCh, gridPoints, inputEntries, outputEntries int) []byte {
	inputTable := 2 * inputCh * inputEntries
	outputTable := 2 * outputCh * outputEntries
	clutSize := 1
	for range inputCh {
		clutSize *= gridPoints
	}
	clutSize *= outputCh
	data := make([]byte, 52+inputTable+2*clutSize+outputTable)
	copy(data[0:4], "mft2")
	data[8] = byte(inputCh)
	data[9] = byte(outputCh)
	data[10] = byte(gridPoints)
	identity := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	for i, v := range identity {
		putS15Fixed16(data, 12+i*4, v)
	}
	putUint16(data, 48, uint16(inputEntries))
	putUint16(data, 50, uint16(outputEntries))
	return data
}

// makeCurv builds a minimal curveType tag with a sampled table of n
// entries.
func makeCurv(n int) []byte {
	data := make([]byte, 12+2*n)
	copy(data[0:4], "curv")
	putUint32(data, 8, uint32(n))
	return data
}

// makeMABCLUT builds the bytes of an mAB CLUT block: 16-byte
// gridPoints array, 1-byte precision, 3-byte padding, then the CLUT
// payload sized for precision == 1.
func makeMABCLUT(inputCh, outputCh, gridPoints int) []byte {
	clutSize := 1
	for range inputCh {
		clutSize *= gridPoints
	}
	clutSize *= outputCh
	data := make([]byte, 20+clutSize)
	for i := range inputCh {
		data[i] = byte(gridPoints)
	}
	data[16] = 1 // precision = 1 byte per sample
	return data
}

// makeMLUC builds a minimal mluc tag with n records.  The records have
// zero-length payloads so the only meaningful allocation is the record
// header slice itself.
func makeMLUC(n int) []byte {
	// header: 4 type + 4 reserved + 4 n + 4 recordSize = 16 bytes
	// per record: 12 bytes (lang + country + length + offset)
	data := make([]byte, 16+12*n)
	copy(data[0:4], "mluc")
	putUint32(data, 8, uint32(n))
	putUint32(data, 12, 12)
	// each record's length is 0 by default; offset can be anywhere
	// within the data since length is 0
	return data
}

func wantBudgetExceeded(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, membudget.ErrExceeded) {
		t.Fatalf("err = %v, want membudget.ErrExceeded", err)
	}
}

func TestBudgetMft1CLUTBomb(t *testing.T) {
	// 3 input channels, 15 output channels, gridPoints=26 →
	// 26^3 × 15 = 263,640 float32 entries = 1,054,560 bytes.
	// newBudget(0) = 1 MiB → CLUT alloc exceeds the budget.
	data := makeMft1(3, 15, 26)
	_, err := decodeLUT8(newBudget(0), data)
	wantBudgetExceeded(t, err)
}

func TestBudgetMft1Fits(t *testing.T) {
	// modest gridPoints leaves plenty of headroom in the 1 MiB base
	// budget.
	data := makeMft1(3, 1, 10)
	if _, err := decodeLUT8(newBudget(0), data); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestBudgetMft2CLUTBomb(t *testing.T) {
	// 3 input channels, 15 output channels, gridPoints=26 →
	// CLUT entries 263,640 × 4 bytes = 1.01 MiB exceeds 1 MiB base.
	data := makeMft2(3, 15, 26, 2, 2)
	_, err := decodeLUT16(newBudget(0), data)
	wantBudgetExceeded(t, err)
}

func TestBudgetMft2Fits(t *testing.T) {
	data := makeMft2(3, 1, 10, 2, 2)
	if _, err := decodeLUT16(newBudget(0), data); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestBudgetMABCLUTBomb(t *testing.T) {
	// 3 input channels, 15 output channels, gridPoints=26 →
	// CLUT entries 263,640 × 4 bytes = 1.01 MiB exceeds 1 MiB base.
	data := makeMABCLUT(3, 15, 26)
	_, _, _, err := decodeCLUT(newBudget(0), data, 0, 3, 15)
	wantBudgetExceeded(t, err)
}

func TestBudgetMABCLUTFits(t *testing.T) {
	data := makeMABCLUT(3, 1, 10)
	if _, _, _, err := decodeCLUT(newBudget(0), data, 0, 3, 1); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestBudgetCurvBomb(t *testing.T) {
	// curveType with 600_000 sampled entries → 1.2 MiB uint16 table.
	const n = 600_000
	data := makeCurv(n)
	_, err := decodeCurve(newBudget(0), data)
	wantBudgetExceeded(t, err)
}

func TestBudgetCurvFits(t *testing.T) {
	data := makeCurv(100)
	if _, err := decodeCurve(newBudget(0), data); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestBudgetMLUCAliasingBomb(t *testing.T) {
	// 60_000 records × sizeof(LocalizedUnicode) (48 bytes on 64-bit)
	// = 2.88 MiB exceeds the 1 MiB base budget.
	const n = 60_000
	data := makeMLUC(n)
	_, err := decodeMLUC(newBudget(0), data)
	wantBudgetExceeded(t, err)
}

func TestBudgetMLUCFits(t *testing.T) {
	data := makeMLUC(2)
	if _, err := decodeMLUC(newBudget(0), data); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}
