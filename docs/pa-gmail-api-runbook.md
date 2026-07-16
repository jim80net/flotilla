# PA Gmail API OAuth runbook

> **Interim operator runbook.** This provisions the first Authorization Domain
> grant: `principal=pa`, `capability=gmail.api`, OAuth scope
> `https://www.googleapis.com/auth/gmail.readonly`. It does not enable drafts,
> sends, modifications, or deletion.

The canonical product design is [Authorization Domains](./authorization-domains.md).
This runbook uses Google's installed desktop application flow because the PA
acts while the operator is offline and therefore needs a refresh token.

## Before you begin

You need:

- a Google Cloud project controlled by the operator;
- a Gmail-enabled Google account;
- permission to enable Gmail API and create an OAuth Desktop client;
- a browser on the host for the one-time consent;
- a decision on OAuth audience: Internal for an eligible Workspace organization,
  or External for a consumer/cross-organization account.

Google classifies `gmail.readonly` as restricted. Public or multi-user External
apps may require OAuth verification, and storing or transmitting restricted-scope
data on servers can trigger additional assessment requirements. Review Google's
[scope classification](https://developers.google.com/workspace/gmail/api/auth/scopes)
before expanding beyond a private operator deployment.

An External app left in **Testing** is suitable for a short trial, not a durable
PA: Google documents that test-user authorizations, including offline refresh
tokens for these scopes, expire after seven days. See
[Manage App Audience](https://support.google.com/cloud/answer/15549945).

## 1. Create the Google credential

Follow Google's current
[Gmail Python quickstart setup](https://developers.google.com/workspace/gmail/api/quickstart/python):

1. Create or select the operator-controlled Google Cloud project.
2. Enable **Gmail API**.
3. Configure Google Auth Platform Branding, Audience, and Data Access.
4. If the audience is External and still Testing, add the operator Gmail account
   as a test user and accept the seven-day token lifetime for the trial.
5. Create an OAuth client with application type **Desktop app**.
6. Download its JSON directly to a private local directory. Do not paste its
   contents into chat, an issue, a PR, or a shell command line.

Do not use a service account for a personal Gmail mailbox. Domain-wide delegation
is a separate Workspace-administrator design and is outside this grant.

## 2. Prepare the PA-only host directory

Run as the OS identity that will own the PA credential:

```sh
install -d -m 0700 "$HOME/.flotilla/pa"
install -m 0600 /private/path/to/downloaded-client.json \
  "$HOME/.flotilla/pa/gmail-client.json"
```

The canonical authorized-user token path is:

```text
~/.flotilla/pa/gmail-oauth.json
```

The directory must be `0700` and both JSON files `0600`. Prefer this host-local
directory over a worktree, even a gitignored one, because worktrees are routinely
inspected, archived, and replaced.

## 3. Perform one-time consent

Use an installed-app OAuth flow that requests exactly:

```text
https://www.googleapis.com/auth/gmail.readonly
```

Google's quickstart uses `InstalledAppFlow.from_client_secrets_file(...)` and
`run_local_server(port=0)`, opens the browser for consent, and serializes the
authorized-user credentials with `Credentials.to_json()`. Run that quickstart
from a temporary private directory, with its `SCOPES` list containing only the
scope above. After successful consent:

```sh
install -m 0600 /private/quickstart/token.json \
  "$HOME/.flotilla/pa/gmail-oauth.json"
stat -c '%a %n' "$HOME/.flotilla/pa/gmail-client.json" \
  "$HOME/.flotilla/pa/gmail-oauth.json"
```

Expected modes are `600`. Remove the temporary quickstart directory after the
canonical files are installed.

The PA connector must make one harmless identity/read check (for example,
retrieve the authenticated profile and list labels), print no token or message
content, and fail closed if the returned account is not the operator-approved
account.

## 4. Wire only the PA launch

Create a private schema-v1 grant file and an empty audit file outside the
worktree. The grant follows the exact `pa-gmail-readonly` example in
`authorization-domains.md`. Both files are host configuration; the OAuth and
audit files must be owned by the PA process identity and mode `0600`.

The host-local launch recipe for `pa` receives only these references and
non-secret selectors:

```sh
FLOTILLA_PA_GMAIL_OAUTH_FILE="$HOME/.flotilla/pa/gmail-oauth.json"
FLOTILLA_PA_GMAIL_APPROVED_ACCOUNT="<operator-approved Gmail address>"
FLOTILLA_PA_GMAIL_ACCOUNT_RESOURCE="operator-primary"
FLOTILLA_PA_GMAIL_AUDIT_FILE="$HOME/.flotilla/pa/gmail-audit.jsonl"
FLOTILLA_PA_GMAIL_LABEL_BINDINGS='{"inbox-label":"INBOX"}'
FLOTILLA_SELF="pa"
```

`FLOTILLA_PA_GMAIL_LABEL_BINDINGS` maps portable logical labels in the grant to
Gmail provider IDs. Omit it when the grant has no label restriction. Do not put
these values in the shared secrets file or another seat's recipe. The approved
address is private host configuration and must never appear in audit output.

The PA connector must require all of the following before enabling Gmail:

- caller principal resolves exactly to `pa`;
- the grant is `capability=gmail.api` with `gmail.readonly` only;
- the configured file is regular, owned by the expected OS identity, and mode
  `0600` (reject symlinks and broader permissions);
- the token's granted scopes contain no write-capable Gmail scope;
- refresh and a harmless read check succeed.

Do not add the variable or credential to the shared secrets file, coordinator
environment, generic shell profile, or another seat's launch recipe. Other
principals must receive “grant not found” before any secret resolution attempt.

Mode `0600` does not isolate processes that share the same Unix user. For strict
pre-broker isolation, run PA/the Gmail broker as a dedicated OS identity and own
the directory with that identity. This interim path otherwise protects against
accidental launch/env fan-out, not a malicious same-UID process.

## 5. Gate and verify

Cos gates placement without reading secret values into a message or review.
From the PA launch environment, run the harmless executable smoke:

```sh
flotilla gmail --roster /path/to/flotilla.json \
  --grant "$HOME/.flotilla/pa/pa-gmail-readonly.yaml" smoke
flotilla gmail --roster /path/to/flotilla.json \
  --grant "$HOME/.flotilla/pa/pa-gmail-readonly.yaml" labels-list
```

Success prints nothing for `smoke`; `labels-list` prints only the provider JSON
allowed by the grant. Never redirect debug HTTP/token data to the terminal.
Then prove denial from a non-PA seat before secret access:

```sh
FLOTILLA_SELF=other flotilla gmail --roster /path/to/flotilla.json \
  --grant "$HOME/.flotilla/pa/pa-gmail-readonly.yaml" smoke
```

This must return `effective roster principal is not pa` without consulting the
OAuth path. Finally:

1. Verify directory/file ownership and modes locally.
2. Verify only the PA launch contains `FLOTILLA_PA_GMAIL_OAUTH_FILE`.
3. Verify the shared secrets file and other launch recipes do not contain Gmail
   client IDs, client secrets, refresh tokens, or the PA path variable.
4. Launch PA and perform the harmless read check.
5. From a different seat, verify the authorization resolver returns “grant not
   found” and does not disclose whether the secret path exists.
6. Record only grant ID, principal, scope, timestamp, and pass/fail in the audit.

Revoke the grant immediately if the token appears in output, source control,
chat, or another process environment. Revoke access in the Google Account,
remove the local files, and create a new Desktop client/token before retrying.

## What the operator must provide

If the setup cannot be completed directly on the host, hand Cos **through a
private local channel only**:

- Google Cloud project ID;
- the downloaded Desktop OAuth client JSON, or its client ID and client secret;
- the intended Gmail account address;
- whether the account is consumer Gmail or Google Workspace;
- consent-screen audience and publishing status;
- any Workspace administrator restriction that blocks the restricted scope.

Never hand over an access token or refresh token through Discord, GitHub, a
prompt, or a PR. Prefer that the operator performs browser consent personally so
the refresh token is created directly on the PA host.

## Scope expansion

Drafting, sending, modifying labels/messages, deletion, and settings each require
a separate operator decision, grant update, consent review, and regression gate.
Do not silently replace `gmail.readonly` with `gmail.modify`, `gmail.compose`,
`gmail.send`, or the full-mail scope.
