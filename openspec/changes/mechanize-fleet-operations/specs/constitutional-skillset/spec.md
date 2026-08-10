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

### Requirement: Legacy doctrine provenance migrates conservatively
For an existing whole-file doctrine installation with no recorded packaged version/digest, migration SHALL preserve local bytes unless they exactly match a known historical packaged digest. A known match MAY be bound to that package version as unmodified. Unknown provenance SHALL stage the current packaged candidate and require explicit `keep_local`, `accept_packaged`, or `merge` resolution without overwriting local bytes.

#### Scenario: Legacy bytes match a historical package
- **WHEN** an installation lacks provenance metadata but its bytes match a known historical packaged digest
- **THEN** migration records the matching package version/digest
- **AND** the asset may follow the unmodified refresh path

#### Scenario: Legacy bytes have unknown provenance
- **WHEN** an installation lacks provenance metadata and its bytes match no known historical packaged digest
- **THEN** migration preserves the local file unchanged
- **AND** stages the current packaged candidate for explicit keep-local, accept-packaged, or merge resolution
- **AND** does not infer that the local bytes are unedited
