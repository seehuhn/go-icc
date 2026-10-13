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

// Package icctest provides ICC profiles for use in tests.
package icctest

import (
	"encoding/binary"
	"math"
	"time"

	"seehuhn.de/go/icc"
)

// IntentProfile returns a gray-device, Lab-PCS LUT profile whose A2B0/A2B1/A2B2
// and B2A0/B2A1/B2A2 tables differ, for testing rendering-intent selection.
//
// For a device gray value g in [0, 1], the device-to-PCS tables give
//
//   - A2B0 (perceptual): L* = 20 + 80·g, a* = b* = 0, so that black is raised;
//   - A2B1 (relative colorimetric): L* = 100·g, a* = b* = 0;
//   - A2B2 (saturation): L* = 100·g, a* = 100·g·(1 − g), b* = 0, so that a*
//     reaches +25 in the mid-tones.
//
// The PCS-to-device tables invert these, ignoring a* and b*: B2A0 gives
// g = (L* − 20)/80 clipped to [0, 1], and B2A1 and B2A2 give g = L*/100.
//
// The tables use lut16Type, which is valid in all profile versions.
func IntentProfile(v icc.Version, class icc.ProfileClass) *icc.Profile {
	lPerc := func(g float64) float64 { return 20 + 80*g }
	lRel := func(g float64) float64 { return 100 * g }
	zero := func(float64) float64 { return 0 }
	aSat := func(g float64) float64 { return 100 * g * (1 - g) }

	return &icc.Profile{
		Version:         v,
		Class:           class,
		ColorSpace:      icc.GraySpace,
		PCS:             icc.PCSLabSpace,
		CreationDate:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RenderingIntent: icc.Perceptual,
		TagData: map[icc.TagType][]byte{
			icc.MediaWhitePoint: encodeXYZ(icc.PCSWhitePoint),
			icc.AToB0:           grayToLab(lPerc, zero),
			icc.AToB1:           grayToLab(lRel, zero),
			icc.AToB2:           grayToLab(lRel, aSat),
			icc.BToA0: labToGray(func(L float64) float64 {
				return min(max((L-20)/80, 0), 1)
			}),
			icc.BToA1: labToGray(func(L float64) float64 { return L / 100 }),
			icc.BToA2: labToGray(func(L float64) float64 { return L / 100 }),
		},
	}
}

// entries per input and output table
const tableSize = 256

// grayToLab encodes a lut16Type table mapping gray to L*(g), a*(g), b* = 0.
//
// The CLUT copies g into the L* and a* channels and holds b* = 0; the output
// tables then apply the two functions.
func grayToLab(L, a func(g float64) float64) []byte {
	clut := []uint16{
		0, 0, legacyA(0), // g = 0
		0xFFFF, 0xFFFF, legacyA(0), // g = 1
	}
	in := [][]uint16{identity(2)}
	out := [][]uint16{
		sample(tableSize, func(g float64) uint16 { return legacyL(L(g)) }),
		sample(tableSize, func(g float64) uint16 { return legacyA(a(g)) }),
		identity(tableSize),
	}
	return encodeMft2(1, 3, 2, in, clut, out)
}

// labToGray encodes a lut16Type table mapping L*, a*, b* to the gray value
// g(L*).
//
// The L* input table applies the function, and the CLUT and output table
// pass the result through.
func labToGray(g func(L float64) float64) []byte {
	// the eight CLUT corners, with L* the most significant input
	clut := []uint16{0, 0, 0, 0, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF}
	in := [][]uint16{
		sample(tableSize, func(u float64) uint16 {
			// u is L* in the legacy encoding, where 0xFF00 is L* = 100
			return quantise(g(u * 65535 / 65280 * 100))
		}),
		identity(tableSize),
		identity(tableSize),
	}
	out := [][]uint16{identity(2)}
	return encodeMft2(3, 1, 2, in, clut, out)
}

// encodeMft2 encodes a lut16Type tag with an identity matrix.  All input
// tables must have the same length, and so must all output tables.
func encodeMft2(nIn, nOut, grid int, in [][]uint16, clut []uint16, out [][]uint16) []byte {
	buf := make([]byte, 52)
	copy(buf, "mft2")
	buf[8] = byte(nIn)
	buf[9] = byte(nOut)
	buf[10] = byte(grid)
	for i := range 3 {
		// identity matrix, as s15Fixed16
		binary.BigEndian.PutUint32(buf[12+16*i:], 0x00010000)
	}
	binary.BigEndian.PutUint16(buf[48:], uint16(len(in[0])))
	binary.BigEndian.PutUint16(buf[50:], uint16(len(out[0])))
	for _, table := range in {
		buf = appendUint16s(buf, table)
	}
	buf = appendUint16s(buf, clut)
	for _, table := range out {
		buf = appendUint16s(buf, table)
	}
	return buf
}

func appendUint16s(buf []byte, values []uint16) []byte {
	for _, v := range values {
		buf = binary.BigEndian.AppendUint16(buf, v)
	}
	return buf
}

// sample returns a table of n entries, holding f at equally spaced points
// from 0 to 1.
func sample(n int, f func(x float64) uint16) []uint16 {
	table := make([]uint16, n)
	for i := range table {
		table[i] = f(float64(i) / float64(n-1))
	}
	return table
}

// identity returns an identity table with n entries.
func identity(n int) []uint16 {
	return sample(n, quantise)
}

// legacyL encodes L* in the legacy 16-bit PCS Lab encoding.
func legacyL(L float64) uint16 {
	return uint16(math.Round(min(max(L, 0), 100) / 100 * 0xFF00))
}

// legacyA encodes a* or b* in the legacy 16-bit PCS Lab encoding.
func legacyA(a float64) uint16 {
	return uint16(math.Round((min(max(a, -128), 127) + 128) * 256))
}

// quantise maps [0, 1] to a 16-bit value.
func quantise(x float64) uint16 {
	return uint16(math.Round(min(max(x, 0), 1) * 0xFFFF))
}

func encodeXYZ(xyz [3]float64) []byte {
	buf := make([]byte, 8, 20)
	copy(buf, "XYZ ")
	for _, v := range xyz {
		buf = binary.BigEndian.AppendUint32(buf, uint32(int32(math.Round(v*65536))))
	}
	return buf
}
