# Remote Actions and authenticated iGate control

Operator-curated affordance that turns the same `@@<otp>#<action>` wire
grammar into outbound traffic: the operator picks a remote station in
Messages, fires a saved macro (or types a free-form action), and the
Messages send path delivers `@@<otp>#<action> [k=v ...]` to that
station. The remote side runs the inbound [Actions
subsystem](actions.md); this side just composes and sends.

Lives in [`../../pkg/remoteactions/`](../../pkg/remoteactions/) with
persistence in [`../../pkg/configstore/`](../../pkg/configstore/) and
wiring in [`../../pkg/app/wiring.go`](../../pkg/app/wiring.go). REST
handlers under
[`../../pkg/webapi/remote_actions_*.go`](../../pkg/webapi/). UI lives
under `web/src/components/messages/remote_actions/`.

## Sibling, not fork

`pkg/remoteactions/` is a **sibling** of `pkg/actions/`, not a fork.
The two share **only the wire grammar** — `pkg/actions/parser.go`
exports `ValidActionName` and `MaxActionNameLen`, which
`pkg/remoteactions/validate.go` consumes so outbound macro creation
rejects names the receiver would refuse. There is no runtime coupling:
- Inbound classifier never reads remote credentials or macros.
- Outbound macro fires never enter the inbound classifier.

## Wire grammar (recap)

```
@@<otp>#<action> [k=v ...]
```

Same grammar as inbound. `<otp>` is six digits or empty when the
remote Action has `otp_required=false`. The outbound side does not
know the remote Action's `otp_required` value, so the operator picks
"manual OTP" (omit) vs. "use credential" (six digits stamped from a
TOTP secret) per macro.

## Hot path

```
operator clicks macro tile
  -> pkg/remoteactions store loads macro + (optional) credential
  -> POST /api/remote-actions/otp/{credId} -> RFC 6238 TOTP code
  -> frontend assembles "@@<digits>#<name> [args]"
  -> POST /api/messages (existing send path; identical to typed lines)
  -> pkg/messages.Service.SendMessage -> RF/IS as configured
```

The macro fire is **indistinguishable on the wire from a hand-typed
line**. There is no `Source: macro` flag in the messages table; the
auditor sees one outbound message of kind `text`.

## CA2RXU authenticated control

Operator setup and the distinction between ordinary queries and signed setting
changes are documented in [Send an authenticated iGate command](../handbook/remote-actions.html#ca2rxu-control).
For first-time setup, the illustrated walkthrough ([English](../handbook/authenticated-control-en.html),
[French](../handbook/authenticated-control-fr.html)) provides screenshots and a
printable PDF using one controller/target example.
The sender resolves its controller identity from Graywolf's Station Callsign
(`pkg/webapi/messages.go::resolveOurCall`); the iGate checks that exact source
against `remoteManagement.authController`. The published iGate firmware verifies
the signed target using `Config.callsign` (Station's Callsign-SSID), even when
the RF message is addressed to its tactical callsign. Graywolf keeps `TargetCall`
as the conversation/RF recipient and credential lookup key, while `SignedTarget`
(`signed_target` in REST/SQLite) is the iGate Station Callsign-SSID used in the
HMAC. Address the conversation to the name displayed on air and enter the
iGate Callsign-SSID as the signed identity in Install key or Key settings.
For example, address `BURNTX` while signing with `N7UV`. No firmware update is
needed: deployed firmware retains verification against `Config.callsign`.
The legacy managers list still gates
`?TX=?` and `?EM=?` queries, independently of the signed-command controller.

The same drawer also contains a separate controller for CA2RXU iGates using
the `!RC1` protocol. It is deliberately independent from the `@@`/TOTP macro
system: CA2RXU commands use HMAC-SHA-256 over the controller, target, key ID,
persistent counter and exact command. Supported v1 commands are `EM=ON`,
`EM=OFF`, `TX=ON`, `TX=OFF` and `COMMIT`.

The sender implementation is in `command_auth.go`. One 32-byte Base64URL key
and one monotonically increasing counter are stored per target in
`remote_command_credentials` (fork migration 30, consolidated by migration 31).
Upstream independently used version 30 for the serial KISS default repair.
Migration 31 runs both idempotent steps so databases from either history
converge without replacing stored keys or counters. Migration 32 adds
`signed_target` and backfills existing rows from `target_call`, preserving keys,
counters and timestamps. Create/update normalize the signed identity with
`NormalizeTargetCall` (maximum 9 characters). On create, empty or omitted
defaults to the RF target. On API update, omitted or null preserves the stored
signed identity; an explicit empty string defaults to the RF target.
Changing the identity does not reset the counter;
only secret replacement does.

The credential dialog exposes both the RF recipient and signed identity.
Changing the RF recipient updates the existing credential by ID, retains its
counter and key, and navigates to the destination DM. Conversation routing
preferences are not moved; operators must verify the destination's route and
channel before sending. A duplicate recipient is rejected without merging keys.
The REST read shape never contains the secret. `ReserveEnvelope` advances the counter transactionally
before returning the signed body; an ambiguous send failure consumes the
counter instead of risking reuse. The server then submits the body through
`messages.Service.SendMessage`, so the selected native TNC2, KISS or APRS-IS
message route remains authoritative. Selecting a channel does not override the
conversation send path: a LoRa-only target normally needs the conversation set
to `RF only` as well as the intended LoRa channel.

The UI components are `RemoteCommandPanel.svelte` and
`RemoteCommandCredentialModal.svelte`. The panel is target-scoped to the open
DM and passes its selected channel to `POST
/api/remote-actions/commands/{target}`. A queued response means only that
Graywolf accepted the APRS message; an ordinary APRS ACK is not treated as an
authenticated application result.

For receiver diagnostics, the RXT firmware's `src/query_utils.cpp` logs
`Remote command authentication rejected: <reason>` using
`REMOTE_AUTH::statusText`. The APRS `AUTH FAILED` reply does not distinguish
bad tags, malformed envelopes, disabled authentication or storage failures.
Replay counters are handled separately and silently ignored in the current
firmware; they must not be diagnosed from `AUTH FAILED` alone.

## Schema (migration 16)

`pkg/configstore/migrate_remote_actions.go`. Two tables, raw SQL so
the FK and the index can be expressed precisely:

| Table | Columns | Notes |
|---|---|---|
| `remote_otp_credentials` | `id`, `name` (UNIQUE), `secret_b32`, `algorithm`, `digits`, `period`, `created_at`, `last_used_at` | TOTP secret stored plaintext per the single-user-station design. `secret_b32` is never echoed back after Create. |
| `remote_action_macros` | `id`, `target_call`, `label`, `action_name`, `args_string`, `remote_otp_credential_id` (nullable FK), `position`, `created_at`, `updated_at` | `target_call` indexed for per-thread lookup. FK has `ON DELETE SET NULL` — deleting a credential demotes its macros to "manual OTP" instead of orphaning them. |
| `remote_command_credentials` | `id`, `name`, `target_call` (UNIQUE), `signed_target`, `secret_b64url`, `key_id`, `last_counter`, timestamps | CA2RXU HMAC key and sender anti-replay state. RF recipient and signed identity are distinct; secret is write-only through REST. |

The two matching Go models in
[`../../pkg/remoteactions/models.go`](../../pkg/remoteactions/models.go)
are intentionally **NOT** registered with AutoMigrate; migration 16 is
the single source of truth.

Migration version 16 follows version 15 (inbound `actions_tables`).
See [`code-map.md`](code-map.md#configstore) for the full migration
ledger.

## REST surface

All routes under `/api/remote-actions/`. Op IDs in
[`../../pkg/webapi/docs/op_ids.go`](../../pkg/webapi/docs/op_ids.go);
swagger tag `remote-actions`.

| Method | Path | Op ID | Notes |
|---|---|---|---|
| GET | `/api/remote-actions/credentials` | `listRemoteOTPCredentials` | Wire shape strips `secret_b32`. `used_by[]` carries distinct target callsigns of bound macros (single scan via `CredStore.UsedBy`). |
| POST | `/api/remote-actions/credentials` | `createRemoteOTPCredential` | Validates base32 (case-insensitive, whitespace tolerated); 409 on UNIQUE name collision. |
| PUT | `/api/remote-actions/credentials/{id}` | `updateRemoteOTPCredential` | Empty `secret_b32` leaves stored secret untouched. 404 when row gone. 409 on name collision. |
| DELETE | `/api/remote-actions/credentials/{id}` | `deleteRemoteOTPCredential` | 409 when `len(used_by) > 0`. |
| GET | `/api/remote-actions/macros?target=CALL` | `listRemoteActionMacros` | Target uppercased server-side. Sorted by `position ASC, id ASC`. |
| POST | `/api/remote-actions/macros` | `createRemoteActionMacro` | Validates target callsign + action name + args length. |
| PUT | `/api/remote-actions/macros/{id}` | `updateRemoteActionMacro` | Partial update with mixed semantics — see DTO doc on `RemoteActionMacroRequest`. **Position is ignored on PUT**; `/macros/reorder` is the sole owner of ordering. |
| DELETE | `/api/remote-actions/macros/{id}` | `deleteRemoteActionMacro` | 204 on success. |
| POST | `/api/remote-actions/macros/reorder` | `reorderRemoteActionMacros` | Body `{target_call, ids[]}`. Each id must resolve to a macro for that target; an unknown id rolls the transaction back and returns 400. |
| POST | `/api/remote-actions/otp/{credId}` | `generateRemoteOTPCode` | Returns `{code, expires_at}` (RFC3339 UTC, inclusive upper edge of current TOTP step). Bumps `last_used_at` (non-fatal on error). |
| GET/POST | `/api/remote-actions/command-credentials` | list/create command credential | Lists safe metadata or installs one key for a target. |
| PUT/DELETE | `/api/remote-actions/command-credentials/{id}` | update/delete command credential | Empty secret preserves the key; replacing it resets the sender counter. |
| POST | `/api/remote-actions/commands/{target}` | `sendRemoteCommand` | Reserves a counter, signs an allowed command and queues it through Messages. |

## Wiring

`pkg/app/wiring.go`:

1. `wireRemoteActions(ctx)` — runs after `wireActions`. Failure is
   non-fatal: logs and returns nil so the rest of the app boots. The
   webapi handlers fall back to 503 via `requireRemoteActions` when
   `App.remoteActions` is nil.
2. `apiSrv.SetRemoteActions(a.remoteActions)` — runs before
   `apiSrv.RegisterRoutes(apiMux)` so handlers see the service on
   the first request.

The composition root (`pkg/remoteactions/Service`) holds two stores
(`CredStore`, `MacroStore`) and a logger. There is no goroutine, no
scheduler, no inbound hook — outbound fires are synchronous and
piggy-back on `pkg/messages`.

## Cross-references

- Inbound counterpart: [`actions.md`](actions.md)
- Messages send path: see [`code-map.md`](code-map.md) row `messages`
- Single-user-station design rationale (plaintext secrets, no rate
  limit on OTP generate): consistent with inbound `otp_credentials`
  table.
- Original design intent: `docs/superpowers/specs/2026-05-03-messages-action-sender-design.md`,
  Phase A plan `docs/superpowers/plans/2026-05-03-messages-action-sender.md`.
