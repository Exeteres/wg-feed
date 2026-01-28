# wg-feed Subscription Protocol (Draft-00)

## Abstract

This document specifies **wg-feed**, a subscription mechanism for distributing WireGuard tunnel configurations to clients via an HTTPS URL that returns a JSON document. Clients fetch the document, reconcile it with locally-managed tunnels, and apply changes.

wg-feed distributes tunnel configuration as **raw `wg-quick` config text** to preserve client-specific extensions (e.g., Android app include/exclude lists, AmneziaWG parameters).

## Status of This Memo

This is an Internet-Draft style document for early review. It can change at any time without notice.

## 1. Terminology

- **Subscription URL**: An HTTPS URL returning a wg-feed response.
- **Setup URL**: A Subscription URL provided by the user to add a subscription entry.
- **Subscription entry**: A client-local record on a specific device that references a specific feed (by Feed ID) obtained via a Setup URL, along with any client-specific settings (e.g., enabled/disabled, refresh policy).
- **Feed document**: The JSON object describing a set of tunnels. In an unencrypted success response it is carried in the `data` field; in an encrypted success response it is the decrypted plaintext of `encrypted_data`.
- **Feed ID**: The top-level `id` field of the feed document (a UUID).
- **Tunnel**: A WireGuard configuration that can be applied as a connection profile.
- **Managed tunnel**: A tunnel created/owned by the wg-feed client.
- **Reconciliation**: Create/update/remove operations to align local managed tunnel state to the feed.

The key words **MUST**, **SHOULD**, and **MAY** are to be interpreted as described in RFC 2119.

## 2. Protocol Summary

### 2.1 Client Behavior

A wg-feed client:
1. Performs an HTTP `GET` to a Setup URL.
2. Validates the response (TLS, status code, content type, schema).
3. Parses the feed document (from `data`, or by decrypting `encrypted_data`) into a list of tunnels.
4. Syncs the latest feed state (polling or SSE) and reconciles local managed tunnel state (as separate processes or as a single combined process).
5. Applies changes using an OS backend (e.g., NetworkManager, wg-quick, WireGuard service APIs, mobile app import).

### 2.2 Sync vs Reconciliation

This specification defines **sync** (obtaining the latest Feed Document via polling or SSE) and **reconciliation** (creating/updating/removing locally-managed tunnels to match the Feed Document).

Clients MAY implement sync and reconciliation as separate processes, or as a single combined process for simplicity.

For the purposes of this specification, a **successful sync** is either:
- A `200 OK` response that contains a valid wg-feed JSON success response (Section 3.1), or
- A `304 Not Modified` response to a valid conditional request.

A `304 Not Modified` response indicates the server asserts that the feed document has not changed relative to the `If-None-Match` value presented. A `304 Not Modified` response does not provide a new `revision` value and does not, by itself, imply that any local managed tunnel state requires change.

After a successful sync, a client SHOULD perform reconciliation if and only if either:
- The `revision` has changed since the last successfully reconciled revision for that subscription entry, or
- A forced sync has been requested (by the user or by the client to repair local state).

Clients MAY reconcile multiple times for the same revision in reaction to local/external state changes (e.g., backend state drift, user modifications, failed previous apply).

## 3. HTTP Protocol

### 3.1 Method and Response Codes

- Clients MUST use `GET`.
- Servers SHOULD return:
	- `200 OK` with a wg-feed JSON success response envelope:
		- `Content-Type: application/json; charset=utf-8`
		- JSON body (unencrypted): `{ "version": "wg-feed-00", "success": true, "revision": "...", "ttl_seconds": 3600, "supports_sse": false, "data": <FeedDocument> }` (Sections 3.6, 4)
		- JSON body (encrypted): `{ "version": "wg-feed-00", "success": true, "revision": "...", "ttl_seconds": 3600, "supports_sse": false, "encrypted": true, "encrypted_data": "..." }` (Sections 3.5, 3.6)
	- `304 Not Modified` when the client presents a valid conditional request.
	- `401/403` for authentication/authorization failures.

### 3.2 Content Negotiation and Media Types

The base wg-feed response format is JSON.

Servers MUST support:
- `Content-Type: application/json; charset=utf-8`

Clients SHOULD send an `Accept` header indicating which representation is desired.

Servers MUST select the response representation as follows:
- If the request `Accept` includes `text/event-stream`, and the server supports SSE for this Subscription URL, the server MUST respond with an SSE stream (Section 3.2.1).
- Else if the request `Accept` includes `application/json`, the server MUST respond with the JSON representation (Section 3.1).
- Otherwise, the server MAY respond with an alternative representation (e.g., a human-readable HTML page) or MAY respond with `406 Not Acceptable`.

#### 3.2.1 Server-Sent Events (SSE) (optional)

Servers MAY additionally support streaming updates using Server-Sent Events (SSE).

If the request `Accept` includes `text/event-stream`, and the server supports SSE, the server MUST respond with an SSE stream:
- `Content-Type: text/event-stream; charset=utf-8`

If the server needs to return an error to an SSE request, it MAY respond with a non-200 status code and `Content-Type: application/json; charset=utf-8` (Section 3.4). In this case, no stream is started.

SSE event format:
- The server MUST send an `event: feed` event immediately when the request starts.
- The server MUST send a new `event: feed` event as soon as an updated feed is available.
- Each `event: feed` event MUST include exactly one `data:` field whose value is the full, serialized wg-feed JSON success response object (Section 3.1).

Keepalive:
- The server MAY send `event: ping` events to keep the connection alive.
- Each `event: ping` event MUST use `data: {}`.

This SSE mode does not require use of SSE `id` fields or the `Last-Event-ID` request header.

### 3.3 Caching and Conditional Requests

- A wg-feed JSON success response MUST include a `revision` field.
- Servers MUST set the HTTP `ETag` value such that the entity-tag payload equals the `revision` string.
	- Specifically, servers MUST set `ETag` to a strong entity-tag of the form `"<revision>"` (including the quotes required by HTTP).
- Clients SHOULD use `If-None-Match`.
- Clients SHOULD honor `Cache-Control` where present.

Clients MAY use `revision`/`ETag` to detect that the feed document has changed.

### 3.4 Error Responses

Servers MUST only send a wg-feed JSON error response (`success = false`) together with a non-200 HTTP status code.

Clients MUST NOT assume that a non-200 response includes a wg-feed JSON body. Proxies and intermediaries MAY replace the body with non-JSON content.

If a non-200 response does contain a JSON body, and that body is produced by a wg-feed server, it MUST follow the error response shape defined in this section.

A wg-feed JSON error response body MUST be a JSON object of the form:

`{ "version": "wg-feed-00", "success": false, "message": <string>, "retriable": <boolean> }`

SSE connections:
- A server MAY close an established SSE connection without an explicit error body (e.g., when it believes the client is no longer authenticated).
- Clients SHOULD attempt to reconnect according to local policy. A subsequent reconnection attempt MAY return an HTTP error response.

#### 3.4.1 Terminal Conditions

Clients MUST treat the following as **terminal conditions** for a subscription entry:
- Receiving wg-feed JSON error responses with `retriable = false` from all reachable Subscription URLs for that subscription entry.
- Any other condition explicitly described by this specification as a terminal condition.

Clients MAY treat a subscription entry as being in a terminal condition when, according to local policy, a reasonable number of consecutive attempts to successfully sync using any Subscription URL have failed (for example, repeated failures to poll and/or repeated failures to establish/maintain SSE across all Subscription URLs).

When a terminal condition occurs for a subscription entry, the client MUST stop automatic polling and automatic SSE reconnection attempts for that subscription entry until the user manually resumes.

### 3.5 Optional Encryption (age)

wg-feed MAY optionally encrypt the Feed Document using **age**.

Reference implementation and format: https://age-encryption.org/

When `success = true`, a server MAY return an encrypted success response instead of including `data`.

Unencrypted success response:

`{ "version": "wg-feed-00", "success": true, "revision": "...", "ttl_seconds": 3600, "supports_sse": false, "data": <FeedDocument> }`

Encrypted success response:

`{ "version": "wg-feed-00", "success": true, "revision": "...", "ttl_seconds": 3600, "supports_sse": false, "encrypted": true, "encrypted_data": <string> }`

Requirements:
- If `encrypted = true`, the response object MUST NOT include `data`.
- `encrypted_data` MUST be an ASCII-armored age payload whose decrypted plaintext is the UTF-8 JSON serialization of the Feed Document object (Section 4).
- Clients MUST treat Subscription URLs as secret regardless of encryption.

Key delivery via URL fragment:
- When encryption is used, the client-side private key MUST be provided out-of-band via the Setup URL fragment component (the portion after `#`). URL fragments are not transmitted in HTTP requests.
- The age secret key (identity) is in the form `AGE-SECRET-KEY-...`.
- The fragment MUST be the age secret key with the `AGE-SECRET-KEY-` prefix removed and the remainder lowercased.

Clients MUST NOT require (and MUST NOT expect) Subscription URLs (from `endpoints[]`) to contain a fragment.

Example:
- Secret key: `AGE-SECRET-KEY-EXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMPLE`
- URL fragment: `#exampleexampleexampleexampleexampleexampleexampleexampleexample`

Client behavior:
- If a client receives `encrypted = true` but has no usable key material, it MUST treat the subscription as not syncable.
- If a client cannot decrypt or parse the decrypted Feed Document, it MUST treat this as a terminal condition (Section 3.4.1).

### 3.6 Success Response Metadata

Every wg-feed JSON success response (`success = true`) MUST include:
- `revision`: opaque revision identifier for this response. The HTTP `ETag` mapping for `revision` is defined in Section 3.3.
- `ttl_seconds`: suggested refresh interval.

The success response MAY include:
- `supports_sse` (default: `false`): server capability declaration.

If `supports_sse = true`, the server MUST support SSE for this Subscription URL. In particular, if a client sends `Accept: text/event-stream`, the server MUST respond with an SSE stream.

If a server supports SSE for this Subscription URL, it MUST set `supports_sse = true` in wg-feed JSON success responses for that Subscription URL.

## 4. Feed Document (JSON Model)

The Feed Document is the JSON object describing tunnels for a feed. In an unencrypted success response it is carried in the `data` field; in an encrypted success response it is obtained by decrypting `encrypted_data` (Sections 3.1, 3.5).

A feed document contains:
- Feed identity (`id`)
- Feed endpoints (`endpoints[]`)
- Human display metadata (`display_info`)
- Optional warning metadata (`warning_message`)
- A list of tunnel definitions (`tunnels[]`)

Clients MUST use local device time (not a server-provided timestamp) for UI display of “last refreshed” / “last checked”.

Clients MUST ignore unknown fields in the feed document.

Clients MUST NOT assume that a non-200 response includes a wg-feed JSON body (Section 3.4).

### 4.1 Feed Identity

Each feed document MUST include a top-level `id` field which MUST be a UUID.

The feed `id` identifies the feed instance referenced by a subscription entry on a specific device. Clients MUST use this `id` to detect duplicate subscription entries.

If multiple Setup URLs resolve to the same feed `id`, clients MUST treat them as referring to the same feed and MUST NOT create multiple subscription entries for that feed on the same device.

If an existing subscription entry observes that fetching any of its Subscription URLs returns a feed document with a different `id` than the one previously recorded for that subscription entry, the client MUST treat this as a terminal condition for automatic sync (Section 3.4.1).

It is RECOMMENDED (but not required) that the operator issues a unique feed `id` per client/device/user.

### 4.2 Display Metadata

The feed document MUST include a top-level `display_info` object.
Each tunnel MUST include a `display_info` object.

`display_info` contains:
- `title` (required): human-friendly title.
- `description` (optional): longer human-friendly text.
- `icon_url` (optional): an icon reference.

Clients MAY ignore `description` and/or `icon_url` if they cannot be displayed in the target UI/backend (e.g., headless `wg-quick`, NetworkManager profiles).

`icon_url` MUST be a `data:` URL as defined in RFC 2397 whose media type is `image/svg+xml`.

### 4.3 Warning Message

The feed document MAY include `warning_message`.

`warning_message` is a human-oriented, user-defined string intended to communicate a logical warning while the subscription remains reachable (e.g., the subscription is expired and requires payment to renew).

If `warning_message` is present and non-empty, clients MUST surface it to the user for that subscription entry (e.g., in the subscription details UI and/or as a prominent banner).

### 4.4 Endpoints Array

The feed document MUST include `endpoints[]`, a non-empty list of Subscription URLs.

Requirements:
- `endpoints[]` MUST contain at least one item.
- Each item MUST be an HTTPS URL.
- Items in `endpoints[]` MUST be unique.
- Items in `endpoints[]` MUST NOT include a URL fragment (the portion after `#`). In particular, `endpoints[]` MUST NOT include an age encryption key fragment.

Client behavior:
- Clients MUST treat all Subscription URLs as equivalent inputs (i.e., servers MUST NOT assume clients will pick a specific URL).
- Clients MUST attempt endpoints one-by-one until a successful sync occurs (Section 2.2) or all endpoints have been tried.
- Clients SHOULD prefer Subscription URLs that have recently succeeded for this subscription entry on previous syncs.

When attempting to sync using endpoints, clients MUST use the first endpoint in their chosen order and fall back to subsequent endpoints when:
- The request fails without producing a valid wg-feed JSON error response (Section 3.4) (e.g., connection error, timeout, TLS failure, proxy/HTML error body), or
- A valid wg-feed JSON error response is received with `retriable = true`.

If a valid wg-feed JSON error response is received with `retriable = false` from a particular Subscription URL, the client SHOULD still attempt other Subscription URLs during the same poll attempt (or, for SSE, the next Subscription URL) unless local policy forbids it.

Clients MUST only enter the terminal condition state (Section 3.4.1) due to `retriable = false` when all reachable Subscription URLs for that subscription entry that can return a valid wg-feed JSON error response return `retriable = false`.

Clients MAY additionally enter the terminal condition state (Section 3.4.1) according to local policy after a reasonable number of consecutive unsuccessful attempts to successfully sync using any Subscription URL.

Polling:
- For each poll attempt, clients MUST attempt to sync using the endpoints one-by-one until a successful sync occurs (Section 2.2) or all endpoints have been tried.

SSE:
- When using SSE, clients MUST connect to one Subscription URL.
- If an established SSE connection fails for a retriable reason (e.g., network error, unexpected disconnect), clients SHOULD connect to the next Subscription URL.

Server behavior:
- Servers SHOULD include the current Subscription URL they are serving in `endpoints[]`.
- Servers MAY omit the current Subscription URL from `endpoints[]` when the operator intends to migrate clients to other endpoints.

### 4.5 Tunnels Array

The feed document MUST include `tunnels[]`, a list of tunnel definitions.

## 5. Tunnel Semantics

### 5.1 Tunnel Identity

Each tunnel MUST have a stable `id`.
- `id` MUST be unique within the document.
- `id` MUST be stable across updates for the same logical tunnel.

### 5.2 Tunnel Name Hint

Each tunnel MUST include a `name` field.

`name` is a suggested interface/profile name (backend-specific) and MUST be treated as a hint:
- If the backend requires unique names, the client MAY adjust `name` to ensure uniqueness (e.g., appending `-1`, `-2`).
- If the backend has name length limits, the client MAY truncate `name`.

`name` MUST be alphanumeric with dashes, starting with a letter.

Clients MUST NOT rely on `name` for identity. Clients MUST track the correspondence between `(feed.id, tunnel.id)` and the created system object (e.g., NetworkManager connection UUID, interface name, mobile tunnel identifier) in client state.

### 5.3 Tunnel Payload: Raw wg-quick config

Each tunnel MUST include `wg_quick_config` which is a string containing a `wg-quick` style configuration.

The server MUST provide a **complete, working configuration** suitable for direct import/apply by the target client(s). In particular, when the target requires it, this includes any required secrets (e.g., `PrivateKey`, `PresharedKey`) and all other necessary fields.

`wg_quick_config` is treated as **opaque text**: clients SHOULD avoid parsing and re-serializing it, and SHOULD prefer backend import mechanisms that preserve unknown keys.

Clients MAY parse `wg_quick_config` to enable safe in-place updates when their backend supports this; however, clients MUST remain correct if parsing is not implemented and SHOULD NOT lose unknown keys as a result of parsing/re-serialization.

Rationale: many clients extend the basic WireGuard config format with additional keys, and naive parsers/reserializers may drop or reorder them.

### 5.4 Desired State Resolution

Each tunnel MAY include the following fields:
- `enabled` (default: `false`): a suggested enabled/disabled value.
- `forced` (default: `false`): whether `enabled` is enforced by the server.

Clients MAY provide a user-facing option to ignore server state and let the user control tunnels manually. When enabled, the client MUST ignore both `enabled` and `forced`, and MUST allow the user to enable/disable tunnels.

If server state is not ignored:
- If `forced = true`: the client MUST ensure the tunnel's enabled/disabled state matches `enabled`, and MUST forbid user toggling of the tunnel's enabled/disabled state.
- If `forced = false`: `enabled` is only the default for newly created/imported tunnels. The client MUST allow the user to enable/disable the tunnel at any time, and MUST ignore any subsequent changes to `enabled` from the feed.

Backend limitations:
- If a backend cannot represent a disabled-but-present tunnel/profile, and `forced = true` with `enabled = false`, the client MUST NOT create the tunnel, and if a managed tunnel with the same `id` already exists, the client SHOULD remove it.

Single-active clients (mobile):
- If the client chooses to auto-activate a tunnel based on the feed, it MUST select the **first** tunnel in `tunnels[]` with `forced = true` and `enabled = true` and activate that tunnel.
- If no tunnel matches `forced = true` and `enabled = true`, the client MUST NOT activate any tunnel.
- If the user has chosen to ignore server state (manual mode), the client SHOULD keep the user’s current selection and MUST NOT auto-switch based on feed ordering.

### 5.5 Reconciliation Algorithm

Clients MUST track which tunnels were created/managed by each subscription entry.

This state MUST persist across client restarts.

Clients MUST handle external modifications to the device/backend (e.g., the user deletes or edits a tunnel outside of wg-feed) and MAY trigger reconciliation in response.

Clients reconcile based on the pair `(feed.id, tunnel.id)`:
- If a tunnel is new for that feed: create/import.
- If a tunnel exists for that feed: update.
- If a previously managed tunnel for that feed is missing from the latest fetched `tunnels[]`: remove it.

When reconciling a tunnel's enabled/disabled state:
- If `forced = true` (and server state is not ignored), the client MUST apply the feed's `enabled` value.
- If `forced = false` (or server state is ignored), the client MUST NOT change enabled/disabled state due to the feed. For newly created/imported tunnels, the client SHOULD use `enabled` as the initial default when the backend supports it.

### 5.6 Update vs Recreate

Clients MUST perform best-effort update.

If a client knows an in-place update is not possible for its backend, or if the client attempted an in-place update and it failed, the client MUST recreate/restart the tunnel to apply the change unless the user has forbidden automatic recreation/restart.

Clients MAY provide a user-facing option to forbid automatic tunnel recreation/restart (to avoid disruption). When this option is enabled and a change would require restart/recreate:
- The client MUST NOT automatically restart the tunnel.
- The client SHOULD surface a notification indicating that the tunnel configuration has changed and that the user should recreate it (e.g., turn it off and on again) to apply updates.
- The client SHOULD record that the tunnel has pending changes and apply them when the user next restarts/recreates it.

## 6. Subscription Management (Optional Feature)

Subscription management (i.e., persisting and managing subscription entries on a device) is an OPTIONAL client feature.

A client MAY implement wg-feed as a one-shot import/apply from a Setup URL without storing a subscription entry. If a client does support subscription entries, it SHOULD provide the capabilities in this section.

### 6.1 One-shot Mode

If a client does not store subscription entries, it still implements the protocol defined in Sections 2–5.

### 6.2 Adding a Subscription Entry

When a user adds a new Setup URL, the client MUST fetch it first and read the feed `id`.

Setup URL requirements:
- The Setup URL is a Subscription URL and MUST meet all Subscription URL requirements.
- The URL is opaque/arbitrary: it MUST be treated as a server-defined identifier and does not need to contain (or encode) the Feed ID.

After bootstrapping:
- Clients MUST NOT store the Setup URL itself.
- Clients MUST store and use the Feed Document `endpoints[]` list for ongoing sync.

If the fetched success response is encrypted (`encrypted = true`), the client MUST verify that an encryption key is provided via the Setup URL fragment (Section 3.5) and that the client can successfully decrypt and parse the Feed Document before creating the subscription entry.

If encryption is used, clients MUST extract the encryption key from the Setup URL fragment during bootstrapping and store that key material for subsequent use with all endpoints. Clients MUST NOT require (and MUST NOT expect) `endpoints[]` URLs to contain a fragment.

If a subscription entry with the same feed `id` has already been added on that device, the client MUST indicate that to the user and MUST NOT create a duplicate subscription entry.

### 6.3 Enabling/Disabling Subscription Entries (optional)

Clients MAY implement an enabled/disabled toggle per subscription entry.

When a subscription entry is disabled:
- The client MUST NOT automatically refresh that subscription entry.
- The client MUST NOT perform automatic reconciliation actions for that subscription entry.
- The client MUST leave any tunnels previously managed by that subscription entry as-is.

## 7. Security Considerations

### 7.1 Transport

- Subscription URLs MUST be HTTPS. (Setup URLs are Subscription URLs.)
- Clients MUST validate TLS normally using native platform trust and verification.
- No additional encryption layer or signature mechanism is defined or required by wg-feed-00.

### 7.2 Authentication

Draft-00 does not mandate a single auth mechanism.

Because clients are only required to let the user enter a Setup URL (and are not required to provide an interactive browser environment), embedding an access token in the Setup URL query is a valid and common approach.

Operators and clients MUST treat Subscription URLs as secrets and avoid logging or displaying them unnecessarily.

In particular, the URL path and query components MUST be treated as secret and SHOULD be redacted in logs and user-visible strings.

The URL host component is not considered secret (it is typically exposed via DNS and SNI).

### 7.3 Private Keys

wg-feed distributes complete, working tunnel configurations including any required secrets (commonly **client private keys** and **preshared keys**) inside `wg_quick_config`.

- The feed document and transport MUST be treated as **highly sensitive**.
- Clients MUST NOT write `wg_quick_config` (or derived secrets) to logs, crash reports, analytics, or telemetry.
- Clients SHOULD store imported configurations using OS facilities appropriate for secrets (e.g., keychain/keystore) when available.

If a client implementation must persist tunnel configurations locally (for example, when using a `wg-quick` backend that reads config files), it MUST store those configurations encrypted at rest using OS facilities appropriate for secrets when available.

### 7.4 Integrity

wg-feed-00 relies on HTTPS/TLS for transport integrity and server authenticity.

Optional encryption (Section 3.5) provides an additional confidentiality layer for the Feed Document payload (the `encrypted_data` ciphertext) when present, but it does not replace HTTPS/TLS requirements.

## 8. Additional Files

- [`wg-feed.schema.json`](wg-feed.schema.json): JSON Schema for wg-feed response bodies.
- [`examples/`](examples/): Example feed documents and configurations.