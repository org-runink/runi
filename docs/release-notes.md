# Release notes

## 20261114-132942 (b350063)

## Release Notes

This release introduces a TOML v1.0.0 reader, updates `toon` serialization behavior for tags and indentation, fixes `barn` head detection, and restricts CI jobs to project-owned runners.

### Added
* **TOML**: Implemented a TOML v1.0.0 reader.
* **TOML**: Added support for writing local date and time kinds as text.
* **Toon**: Added support for honoring a `toon` tag when a `json` tag is absent.
* **Toon**: Expanded indentation acceptance to allow any consistent indent unit, not just two spaces.
* **Runi**: Adapted `runi` to fit two real consumers prior to API freeze.

### Changed
* **CI**: Configured all jobs to run exclusively on the project's own runners with owner-only access.
* **Barn**: Changed head detection to read the log forward instead of listing the directory.
* **Toon**: Removed "odd indent" from the second refusal list.
* **Docs**: Recorded two `toon` behavior changes in the v1.0.0 changelog.

### Fixed
* **Barn**: Fixed an issue where the head was found by listing the directory, which could be unreliable.
* **Toon**: Fixed a test that incorrectly read its own earlier result and restored a missing coverage case.
* **Toon**: Fixed the root-array assertion in the indent test.
* **Toon**: Ensured level skips are still refused once a unit is established (added coverage).
* **Barn**: Added coverage for `_checkpoint/` listing to ensure foreign names are skipped.

