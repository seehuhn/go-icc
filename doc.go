// Copyright 2026 Jochen Voss <voss@seehuhn.de>
// SPDX-License-Identifier: BSD-3-Clause

// Package icc reads and writes ICC colour profiles.
//
// ICC profiles describe how to convert colours between device colour spaces
// (such as RGB or CMYK) and a device-independent Profile Connection Space (PCS).
// The PCS is either CIEXYZ or CIELAB, both relative to [PCSWhitePoint].
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
