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
	case ChromaticAdaptation:
		return "Chromatic Adaptation"
	case RedMatrixColumn:
		return "Red Matrix Column"
	case GreenMatrixColumn:
		return "Green Matrix Column"
	case BlueMatrixColumn:
		return "Blue Matrix Column"
	case RedTRC:
		return "Red TRC"
	case GreenTRC:
		return "Green TRC"
	case BlueTRC:
		return "Blue TRC"
	case GrayTRC:
		return "Gray TRC"
	case MediaWhitePoint:
		return "Media White Point"
	case AToB0:
		return "A to B0"
	case AToB1:
		return "A to B1"
	case AToB2:
		return "A to B2"
	case BToA0:
		return "B to A0"
	case BToA1:
		return "B to A1"
	case BToA2:
		return "B to A2"
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

// Some tag types defined in the ICC specification.
const (
	ProfileDescription  TagType = 0x64657363 // "desc"
	Copyright           TagType = 0x63707274 // "cprt"
	ChromaticAdaptation TagType = 0x63686164 // "chad"

	// Matrix/TRC profile tags
	RedMatrixColumn   TagType = 0x7258595A // "rXYZ"
	GreenMatrixColumn TagType = 0x6758595A // "gXYZ"
	BlueMatrixColumn  TagType = 0x6258595A // "bXYZ"
	RedTRC            TagType = 0x72545243 // "rTRC"
	GreenTRC          TagType = 0x67545243 // "gTRC"
	BlueTRC           TagType = 0x62545243 // "bTRC"
	GrayTRC           TagType = 0x6B545243 // "kTRC"
	MediaWhitePoint   TagType = 0x77747074 // "wtpt"

	// LUT-based profile tags
	AToB0 TagType = 0x41324230 // "A2B0" - Perceptual
	AToB1 TagType = 0x41324231 // "A2B1" - Relative Colorimetric
	AToB2 TagType = 0x41324232 // "A2B2" - Saturation
	BToA0 TagType = 0x42324130 // "B2A0" - Perceptual
	BToA1 TagType = 0x42324131 // "B2A1" - Relative Colorimetric
	BToA2 TagType = 0x42324132 // "B2A2" - Saturation
)

// Copyright returns the contents of the copyright tag.
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
