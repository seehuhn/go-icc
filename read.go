// seehuhn.de/go/icc - read and write ICC profiles
// Copyright (C) 2024  Jochen Voss <voss@seehuhn.de>
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
	"bytes"
	"crypto/md5"
	"fmt"
	"time"
)

// Decode decodes an ICC profile from the given data.
// The function takes over ownership of the data.
func Decode(data []byte) (*Profile, error) {
	if len(data) < 128+4 {
		return nil, invalidProfile(0, "profile is too short")
	}
	if string(data[36:40]) != "acsp" {
		return nil, invalidProfile(36, "missing 'acsp' signature")
	}

	numTags := getUint32(data, 128)
	maxNumTags := uint((len(data) - 128 - 4) / 12)
	if uint(numTags) > maxNumTags {
		return nil, invalidProfile(128, "too many tags")
	}
	// since len(data) is an int, numTags can be represented as an int

	// if !bytes.Equal(data[68:80], d50) {
	// 	return nil, invalidProfile(68, "missing 'D50 ' signature")
	// }

	p := &Profile{
		PreferedCMMType:    getUint32(data, 4),
		Version:            Version(getUint32(data, 8)),
		Class:              ProfileClass(getUint32(data, 12)),
		ColorSpace:         getUint32(data, 16),
		PCS:                getUint32(data, 20),
		CreationDate:       getDateTime(data, 24),
		PrimaryPlatform:    getUint32(data, 40),
		Flags:              getUint32(data, 44),
		DeviceManufacturer: getUint32(data, 48),
		DeviceModel:        getUint32(data, 52),
		DeviceAttributes:   getUint64(data, 56),
		RenderingIntent:    RenderingIntent(getUint32(data, 64)),
		Creator:            getUint32(data, 80),

		TagData: make(map[TagType][]byte),
	}

	if !isZero(data[84:100]) {
		var givenHash [16]byte
		copy(givenHash[:], data[84:100])

		// The entire profile, whose length is given by the size field in the
		// header, with the profile flags field, rendering intent field, and
		// profile ID field in the profile header temporarily set to zeros
		// shall be used to calculate the ID.
		putUint32(data, 44, 0)
		putUint32(data, 64, 0)
		for i := 84; i < 100; i++ {
			data[i] = 0
		}

		computedHash := md5.Sum(data)
		if bytes.Equal(computedHash[:], givenHash[:]) {
			p.CheckSum = CheckSumValid
		} else {
			p.CheckSum = CheckSumInvalid
		}
	}

	minTagOffset := 128 + 4 + int64(numTags)*12
	for i := 0; i < int(numTags); i++ {
		offset := 128 + 4 + i*12
		tagType := TagType(getUint32(data, offset))
		tagOffset := getUint32(data, offset+4)
		tagSize := getUint32(data, offset+8)
		if tagSize < 4 {
			return nil, invalidProfile(offset+8, "tag is too small")
		} else if tagSize > 0xFFFFFFFC {
			return nil, invalidProfile(offset+8, "tag is too large")
		}

		start := int64(tagOffset)
		end := start + int64(tagSize)
		if start < minTagOffset || end > int64(len(data)) {
			return nil, invalidProfile(offset, "tag is out of bounds")
		}
		p.TagData[tagType] = data[start:end]
	}

	if p.Version == 0 {
		p.Version = currentVersion
	}

	return p, nil
}

func isZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

func getUint16(data []byte, offset int) uint16 {
	return uint16(data[offset])<<8 | uint16(data[offset+1])
}

func getUint32(data []byte, offset int) uint32 {
	return uint32(data[offset])<<24 | uint32(data[offset+1])<<16 | uint32(data[offset+2])<<8 | uint32(data[offset+3])
}

func getUint64(data []byte, offset int) uint64 {
	return uint64(data[offset])<<56 | uint64(data[offset+1])<<48 | uint64(data[offset+2])<<40 | uint64(data[offset+3])<<32 |
		uint64(data[offset+4])<<24 | uint64(data[offset+5])<<16 | uint64(data[offset+6])<<8 | uint64(data[offset+7])
}

func getDateTime(data []byte, offset int) time.Time {
	year := int(data[offset])<<8 | int(data[offset+1])       // e.g. 1994
	month := int(data[offset+2])<<8 | int(data[offset+3])    // 1 to 12
	day := int(data[offset+4])<<8 | int(data[offset+5])      // 1 to 31
	hour := int(data[offset+6])<<8 | int(data[offset+7])     // 0 to 23
	minute := int(data[offset+8])<<8 | int(data[offset+9])   // 0 to 59
	second := int(data[offset+10])<<8 | int(data[offset+11]) // 0 to 59
	if year < 1970 || year > 3000 ||
		month < 1 || month > 12 ||
		day < 1 || day > 31 ||
		hour > 23 || minute > 59 || second > 61 {
		return time.Time{}
	}
	return time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
}

// InvalidProfileError indicates that an ICC profile contains invalid binary
// data and cannot be decoded.
type InvalidProfileError struct {
	Offset int
	Reason string
}

func invalidProfile(offset int, reason string) error {
	return &InvalidProfileError{Offset: offset, Reason: reason}
}

func (e *InvalidProfileError) Error() string {
	return fmt.Sprintf("icc: invalid profile (byte %d): %s", e.Offset, e.Reason)
}
