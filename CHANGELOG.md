# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- `Decode` no longer mutates or retains its input slice.  Previously the
  caller had to surrender ownership of the buffer, which silently corrupted
  shared inputs such as the embedded `SRGBv2Profile`, `SRGBv4Profile`, and
  `CMYKProfile` byte slices on every call.

## [0.7.1] (2026-03-31)

### Added
- Color transformation and LUT support for ICC profiles.
- Embedded sRGB ICC profiles (v2 and v4).
- CMYK profile (SWOP/TR 005).
- `Profile.Equal` method and profile ID computation for all versions.
- Allocation-free `FromXYZInto` and `Channels` methods on `Transform`.

### Changed
- ICC profile data moved into `profiles/` subdirectory.

### Fixed
- Gray TRC transform now uses D50 white point for PCS XYZ.

## [0.7.0] (2025-01-25)

Initial tagged release.
