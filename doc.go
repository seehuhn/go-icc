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

// Package icc reads and writes ICC colour profiles.
//
// ICC profiles describe how to convert colours between device colour spaces
// (such as RGB or CMYK) and a device-independent Profile Connection Space (PCS).
// The PCS is either CIEXYZ or CIELAB, both based on the D50 illuminant.
//
// # Reading and Writing Profiles
//
// Use [Decode] to read an ICC profile from binary data, and [Profile.Encode]
// to convert a profile back to binary form:
//
//	p, err := icc.Decode(data)
//	if err != nil {
//	    // handle error
//	}
//	// inspect p.Class, p.ColorSpace, p.Version, p.TagData, etc.
//
//	encoded, err := p.Encode()
//
// # Colour Transformations
//
// To convert colours using a profile, create a [Transform] with [NewTransform].
// A single Transform converts in both directions:
//
//	t, err := icc.NewTransform(p, icc.Perceptual)
//	if err != nil {
//	    // handle error
//	}
//	ws := &icc.Workspace{}
//	X, Y, Z := t.ToXYZ([]float64{r, g, b}, ws)  // device to PCS XYZ
//
//	rgb := make([]float64, t.Channels())
//	t.FromXYZ(X, Y, Z, rgb, ws)  // PCS XYZ to device
package icc
