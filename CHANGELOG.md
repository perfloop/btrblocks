# Changelog

## Unreleased

- Split physical arrays, codec nodes, and compression selection into public
  `array`, `codec`, and `compress` packages while retaining the root facade.
- Added first-class null-mask constructors and bounded validity materialization.
- Added build and decode memory limits, recursive-depth limits, hostile-input
  tests, version 1 framing fixtures, and production CI gates.
- Documented the draft wire format and the compatibility boundary established
  by the first `v1.0.0` release.
