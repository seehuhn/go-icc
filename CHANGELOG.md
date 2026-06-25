# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.7.4] (2026-06-25)

### Added
- `Workspace` holds reusable scratch buffers so colour conversions run
  without per-call heap allocation; use one per goroutine for
  concurrent, allocation-free transforms.

### Changed
- `Transform` is now bidirectional: a single transform converts both
  device→PCS (`ToXYZ`) and PCS→device (`FromXYZ`), and the public API
  was tidied accordingly.
- N-dimensional CLUTs are interpolated with simplex interpolation.

## [0.7.3] (2026-05-19)

### Changed
- Decoding untrusted ICC profiles now allocates against a cumulative
  memory budget sized from the input length, so adversarial profiles
  cannot amplify a small input into a large allocation.
- Minimum Go version raised to 1.25.

## [0.7.2] (2026-05-11)

### Changed
- Matrix-TRC and gray-TRC transforms no longer allocate per pixel,
  reducing total bytes allocated by 25–42 % on tile-rendering workloads.

### Fixed
- `Decode` no longer mutates or retains its input slice.  Previously the
  caller had to surrender ownership of the buffer, which silently corrupted
  shared inputs such as the embedded `SRGBv2Profile`, `SRGBv4Profile`, and
  `CMYKProfile` byte slices on every call.
- LUT profiles that omit `AToB0` (or `BToA0`) but include `AToB1`/`AToB2`
  are no longer rejected with "missing LUT tag"; the requested intent's
  tag is tried first, then the remaining intent tags in preferred order.

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
