## ADDED Requirements

### Requirement: doctrine install supports refresh of drifted identity-append blocks

The doctrine-install command SHALL accept a `--refresh` flag. When `--refresh` is set and an
identity-append member's opening marker is already present, the installer SHALL replace the fenced
region from the opening marker through the closing marker inclusive with the current embedded asset
content when the installed block differs from the asset (trailing-newline differences SHALL NOT
count as drift). When the installed block matches the asset, the install SHALL report a no-op. When
the opening marker is present but the closing marker is absent, the install SHALL fail closed with
an error and SHALL NOT partially rewrite the identity file. When the opening marker is absent,
`--refresh` SHALL append the block (same as a plain install). The command SHALL also accept `--all`
to run install/refresh against every agent in the roster.

#### Scenario: Refresh replaces a drifted fenced block

- **WHEN** `flotilla doctrine install --refresh <agent>` runs against an identity file whose fenced block content differs from the embedded asset
- **THEN** the fenced region is replaced with the current asset and reported as refreshed

#### Scenario: Refresh is a no-op when content is current

- **WHEN** `flotilla doctrine install --refresh <agent>` runs against an identity file whose fenced block already matches the embedded asset
- **THEN** the identity file is unchanged and the member is reported as already installed with reason `content current`

#### Scenario: Missing close marker fails closed

- **WHEN** `flotilla doctrine install --refresh <agent>` runs against an identity file with the opening marker present but the closing marker absent
- **THEN** the command errors without partially rewriting the file