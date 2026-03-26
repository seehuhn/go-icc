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

import _ "embed"

// Built-in ICC profiles.
var (
	// SRGBv2Profile contains a compact sRGB ICC profile using ICC version 2.
	// From https://github.com/saucecontrol/Compact-ICC-Profiles (CC0 1.0).
	//
	//go:embed profiles/sRGB-v2-micro.icc
	SRGBv2Profile []byte

	// SRGBv4Profile contains a compact sRGB ICC profile using ICC version 4.
	// From https://github.com/saucecontrol/Compact-ICC-Profiles (CC0 1.0).
	//
	//go:embed profiles/sRGB-v4.icc
	SRGBv4Profile []byte

	// CMYKProfile contains a CMYK ICC profile for US web-offset printing
	// on coated paper (CGATS TR 005 / SWOP). The profile supports both
	// device-to-PCS and PCS-to-device transforms.
	// Generated from characterization data in the Debian icc-profiles-free
	// package (zlib/libpng license). See profiles/Licenses.txt for details.
	//
	//go:embed profiles/TR005.icc
	CMYKProfile []byte
)
