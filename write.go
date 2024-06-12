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
	"sort"
	"time"
)

// Encode converts the profile to binary form.
func (p *Profile) Encode() []byte {
	version := p.Version
	if version == 0 {
		version = currentVersion
	}

	// arrange tags in order of increasing length and merge duplicates
	type tagInfo struct {
		tagType   TagType
		data      []byte
		start     uint32
		duplicate bool
	}
	var tags []tagInfo
	for tagType, data := range p.TagData {
		tags = append(tags, tagInfo{
			tagType: tagType,
			data:    data,
		})
	}
	sort.Slice(tags, func(i, j int) bool {
		if len(tags[i].data) != len(tags[j].data) {
			return len(tags[i].data) < len(tags[j].data)
		}
		return bytes.Compare(tags[i].data, tags[j].data) < 0
	})
	pos := 128 + 4 + len(tags)*12
	for i := range tags {
		if i > 0 && bytes.Equal(tags[i].data, tags[i-1].data) {
			tags[i].start = tags[i-1].start
			tags[i].duplicate = true
		} else {
			tags[i].start = uint32(pos)
			pos += (len(tags[i].data) + 3) &^ 3
		}
	}

	buf := make([]byte, pos)
	putUint32(buf, 0, uint32(pos))
	putUint32(buf, 4, p.PreferedCMMType)
	putUint32(buf, 8, uint32(version))
	putUint32(buf, 12, uint32(p.Class))
	putUint32(buf, 16, p.ColorSpace)
	putUint32(buf, 20, p.PCS)
	putDateTime(buf, 24, p.CreationDate)
	putUint32(buf, 36, 0x61637370) // "acsp"
	putUint32(buf, 40, p.PrimaryPlatform)
	putUint32(buf, 48, p.DeviceManufacturer)
	putUint32(buf, 52, p.DeviceModel)
	putUint64(buf, 56, p.DeviceAttributes)
	copy(buf[68:], d50)
	putUint32(buf, 80, p.Creator)

	putUint32(buf, 128, uint32(len(tags)))
	tagTable := 128 + 4
	for i, tag := range tags {
		putUint32(buf, tagTable+i*12, uint32(tag.tagType))
		putUint32(buf, tagTable+i*12+4, tag.start)
		putUint32(buf, tagTable+i*12+8, uint32(len(tag.data)))
		if !tag.duplicate {
			copy(buf[tag.start:], tag.data)
		}
	}

	if version >= Version4_0_0 {
		// The entire profile, whose length is given by the size field in the
		// header, with the profile flags field, rendering intent field, and
		// profile ID field in the profile header temporarily set to zeros shall be
		// used to calculate the ID.
		h := md5.Sum(buf)
		copy(buf[84:], h[:])
	}

	putUint32(buf, 44, p.Flags)
	putUint32(buf, 64, uint32(p.RenderingIntent))

	return buf
}

// This is the value for the "PCS illuminant" header field (Bytes 68 to 79).
var d50 = []byte{
	0x00, 0x00, 0xf6, 0xd6, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0xd3, 0x2d,
}

func putUint32(data []byte, offset int, value uint32) {
	data[offset] = byte(value >> 24)
	data[offset+1] = byte(value >> 16)
	data[offset+2] = byte(value >> 8)
	data[offset+3] = byte(value)
}

func putUint64(data []byte, offset int, value uint64) {
	data[offset] = byte(value >> 56)
	data[offset+1] = byte(value >> 48)
	data[offset+2] = byte(value >> 40)
	data[offset+3] = byte(value >> 32)
	data[offset+4] = byte(value >> 24)
	data[offset+5] = byte(value >> 16)
	data[offset+6] = byte(value >> 8)
	data[offset+7] = byte(value)
}

func putDateTime(data []byte, offset int, t time.Time) {
	year := t.Year()
	data[offset] = byte(year >> 8)
	data[offset+1] = byte(year)
	data[offset+3] = byte(t.Month())
	data[offset+5] = byte(t.Day())
	data[offset+7] = byte(t.Hour())
	data[offset+9] = byte(t.Minute())
	data[offset+11] = byte(t.Second())
}
