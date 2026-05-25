# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial plugin scaffold with `/crossguard init` slash command
- Admin console custom section
- Docker development environment
- CI/CD workflows for PR validation and releases
- SBOM generation and vulnerability scanning
- Restored JSON wire format as an optional outbound encoding alongside
  XML. Each outbound connection carries a `message_format` setting
  (`xml` default, `json` opt-in). Inbound connections auto-detect from
  the first non-whitespace byte (`<` selects XML, `{` selects JSON).
  See `schema/crossguard.schema.json` for the JSON contract, which is
  mechanically generated from `schema/crossguard.xsd` via
  `make generate-json-schema`.

### Changed
- Webapp connection-form default for `message_format` flipped from
  `json` to `xml`, matching the backend default. **Upgrade note for
  operators:** outbound connections saved through the admin UI before
  this release carry `message_format: "json"` (the field was orphaned;
  the backend silently sent XML). After this release the value is
  honored, so those connections will start emitting JSON on the wire.
  Review every outbound connection and explicitly set `xml` if XML is
  required (for Cross Domain Solutions, for example).
