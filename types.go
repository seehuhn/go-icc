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
	"errors"
	"unicode/utf16"
)

func decodeText(data []byte) (string, error) {
	err := checkType("text", data)
	if err != nil {
		return "", err
	}

	if len(data) < 8 {
		return "", errInvalidTagData
	}
	start := 8
	end := len(data)
	for end-1 > start && data[end-1] == 0 {
		end--
	}
	return string(data[start:end]), nil
}

// MultiLocalizedUnicode represents a localized Unicode string.
type MultiLocalizedUnicode []LocalizedUnicode

// LocalizedUnicode represents a language-country pair.
type LocalizedUnicode struct {
	Language string
	Country  string
	Value    string
}

func decodeMLUC(data []byte) (MultiLocalizedUnicode, error) {
	err := checkType("mluc", data)
	if err != nil {
		return nil, err
	}

	if len(data) < 12 {
		return nil, errInvalidTagData
	}
	n := getUint32(data, 8)

	if n == 0 || uint64(len(data)) < 16+12*uint64(n) {
		return nil, errInvalidTagData
	}
	res := make(MultiLocalizedUnicode, n)
	for i := range res {
		language := string(data[16+12*i : 16+12*i+2])
		country := string(data[16+12*i+2 : 16+12*i+4])
		length := getUint32(data, 16+12*i+4)
		offset := getUint32(data, 16+12*i+8)

		start := uint64(offset)
		end := start + uint64(length)
		if end > uint64(len(data)) || length&1 != 0 {
			return nil, errInvalidTagData
		}

		d16 := make([]uint16, length/2)
		for j := range d16 {
			d16[j] = uint16(data[start+2*uint64(j)])<<8 | uint16(data[start+2*uint64(j)+1])
		}
		res[i] = LocalizedUnicode{
			Language: language,
			Country:  country,
			Value:    string(utf16.Decode(d16)),
		}
	}
	return res, nil
}

func checkType(typeID string, data []byte) error {
	bb := []byte(typeID)
	for i, b := range bb {
		if i >= len(data) || data[i] != b {
			return errUnexpectedType
		}
	}
	return nil
}

var (
	errMissingTag     = errors.New("missing tag")
	errUnexpectedType = errors.New("unexpected tag data type")
	errInvalidTagData = errors.New("invalid tag data")
)
