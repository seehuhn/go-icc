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

import "fmt"

// The TagType identifies a tag in an ICC profile.
type TagType uint32

func (t TagType) String() string {
	switch t {
	case ProfileDescription:
		return "Profile Description"
	case Copyright:
		return "Copyright"
	case ChromaticAdaption:
		return "Chromatic Adaption"
	default:
		bb := []byte{
			byte(t >> 24),
			byte(t >> 16),
			byte(t >> 8),
			byte(t),
		}
		isASCII := true
		for _, c := range bb {
			if c < 0x20 || c > 0x7E {
				isASCII = false
				break
			}
		}
		if isASCII {
			return fmt.Sprintf("%q", string(bb))
		}
		return fmt.Sprintf("0x%08X", uint32(t))
	}
}

// These are the tag types defined in the ICC specification.
// (The list is currently incomplete.)
const (
	ProfileDescription TagType = 0x64657363 // "desc"
	Copyright          TagType = 0x63707274 // "cprt"
	ChromaticAdaption  TagType = 0x63686164 // "chad"
)

func (p *Profile) Copyright() (MultiLocalizedUnicode, error) {
	tag, ok := p.TagData[Copyright]
	if !ok {
		return nil, errMissingTag
	}
	val, err := decodeMLUC(tag)
	if err != errUnexpectedType {
		return val, err
	}

	s, err := decodeText(tag)
	if err != nil {
		return nil, err
	}
	val = MultiLocalizedUnicode{
		{
			Language: "en",
			Country:  "US",
			Value:    s,
		},
	}
	return val, nil
}
