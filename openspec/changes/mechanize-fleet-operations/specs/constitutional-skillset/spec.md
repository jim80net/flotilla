## ADDED Requirements

### Requirement: Whole-file doctrine assets remain product-managed and versioned
Each packaged whole-file doctrine asset SHALL carry a product version and digest, and each installation SHALL record the packaged version and digest from which it was installed. Installation SHALL NOT implicitly transfer permanent ownership of the asset to a local fork.

#### Scenario: Installed asset is unchanged when a new version ships
- **WHEN** the installed digest still matches its recorded packaged digest and a newer packaged version is available
- **THEN** refresh atomically installs the newer asset
- **AND** records its new version and digest

### Requirement: Doctrine refresh preserves local edits through explicit resolution
The system SHALL NOT overwrite a locally modified whole-file doctrine asset. It SHALL stage the newer packaged candidate, report the local drift and version conflict, and require an explicit `keep_local`, `accept_packaged`, or `merge` resolution. It SHALL record the selected resolution and resulting digest.

#### Scenario: Locally edited safety asset has an update
- **WHEN** the installed digest differs from its recorded packaged digest and a newer packaged version is available
- **THEN** automatic refresh leaves the local file unchanged
- **AND** exposes the staged packaged candidate and three explicit resolution choices

#### Scenario: Operator keeps the local version
- **WHEN** the operator selects `keep_local`
- **THEN** the product records the deliberate divergence and packaged version considered
- **AND** future status continues to expose the drift until it is resolved or superseded explicitly

#### Scenario: Operator accepts or merges the update
- **WHEN** the operator selects `accept_packaged` or supplies a merged result
- **THEN** the chosen content is installed atomically
- **AND** its provenance, source packaged version, and resulting digest are durable and queryable

### Requirement: Historical digest catalog is closed and product-owned
The authoritative historical digest catalog SHALL ship as a versioned product-release artifact owned by the product release maintainers and authenticated by the product release-signing identity. Each entry SHALL bind asset identity, packaged version, and content digest. The catalog SHALL update only through a newly authenticated product release. Runtime state, local configuration, installed files, caches, mirrors, and operator-supplied metadata SHALL NOT add, replace, or qualify catalog entries.

#### Scenario: Signed product release updates the catalog
- **WHEN** a new product release with a valid release signature carries a newer catalog version
- **THEN** that closed catalog becomes the authoritative set for the release
- **AND** its entries retain asset, version, and digest binding

#### Scenario: Local metadata claims a historical digest
- **WHEN** a local file, configuration value, cache record, mirror, or operator-supplied record claims a digest is historical
- **THEN** migration rejects that claim as a qualification source
- **AND** it does not enlarge or override the authenticated catalog

### Requirement: Legacy doctrine provenance migrates conservatively
For an existing whole-file doctrine installation with no recorded packaged version/digest, migration SHALL preserve local bytes unless they qualify through exactly one entry in the authoritative historical digest catalog. Unknown or ambiguous provenance SHALL stage the current packaged candidate and require explicit `keep_local`, `accept_packaged`, or `merge` resolution without overwriting local bytes.

#### Scenario: Legacy bytes match a historical package
- **WHEN** an installation lacks provenance metadata but its asset and bytes match exactly one entry in the authenticated product catalog
- **THEN** migration records the matching package version/digest
- **AND** the asset may follow the unmodified refresh path

#### Scenario: Legacy bytes have unknown provenance
- **WHEN** an installation lacks provenance metadata and its asset/bytes match no authoritative catalog entry
- **THEN** migration preserves the local file unchanged
- **AND** stages the current packaged candidate for explicit keep-local, accept-packaged, or merge resolution
- **AND** does not infer that the local bytes are unedited

#### Scenario: Catalog is unavailable or unauthenticated
- **WHEN** the authoritative catalog is absent or its product release signature cannot be authenticated
- **THEN** migration preserves the local file unchanged
- **AND** requires explicit resolution rather than qualifying any historical digest

#### Scenario: Multiple catalog entries qualify
- **WHEN** multiple authoritative entries qualify the same installed asset and bytes
- **THEN** migration treats the match as ambiguous
- **AND** preserves local bytes pending explicit resolution
