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
persistent counter and exact command. The five shortcuts remain `EM=ON`,
`EM=OFF`, `TX=ON`, `TX=OFF` and `COMMIT`; a free-form authenticated-command
field uses the same endpoint, key, signed identity and persistent counter.
There is no command allowlist in Graywolf: firmware decides which commands it
supports. Commands are trimmed and uppercased, start with an ASCII letter,
and contain printable ASCII without spaces, colon, braces, pipe or tilde.
The maximum is 41 command bytes, reduced by the actual counter length so the
complete `!RC1:A:<base36-counter>:<command>:<tag>` body stays within 67 bytes.
Envelope validation runs inside the reservation transaction; rejected input
rolls back the counter, while an ambiguous transport failure still consumes it.
Never prepend `@@#`: that belongs to the separate Actions mechanism.
OTA examples are `OTA=?`, `OTA=PROFILE,434550000,250000,8,6,3`,
`OTA=FAST,30` and `OTA=NORMAL`. The iGate OTA firmware on `experiment/lora-ota`
at `ca82256` supports these signed commands. On 31 October 2026, the operator
verified signed `RXT=?` and `OTA=?` queries over RF to `GENATX`, with replies
`RXT=ON` and `OTA NORMAL ARMED=0 PROFILE=0`. OTA setting changes
(`PROFILE`, `FAST`, `NORMAL`) still require radio validation.

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
| POST | `/api/remote-actions/commands/{target}` | `sendRemoteCommand` | Reserves a counter, signs a format- and length-validated command and queues it through Messages. |

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

## LoRa firmware updates in Messages

The authenticated-command panel opens `RadioOtaModal.svelte` with **Update
firmware over LoRa**. The browser uploads an application image to Graywolf;
it never opens USB. Routes under `/api/radio-ota/{target}` handle station
settings, upload/start, progress and pause. `pkg/webapi/radio_ota.go` resolves
the RF recipient, controller and stored signed identity. GET reports
`engine: native-go`; it performs no hardware access or external runtime probe.

`pkg/radioota/` now implements the transfer natively in Go:

- `service.go`: private settings, immutable attempt files, persistent counters,
  job lifecycle, boot-proof validation and exclusive job reservation.
- `protocol.go`: end-to-end HMAC domain `LORA-APRS-OTA2`, routed HMAC domain
  `LORA-APRS-OTA-HOP1`, strict identities/lengths/nonces and boot replies.
- `bridge.go`: `go.bug.st/serial`, RTS/DTR initially low, Unix exclusive open,
  bounded console reads, `OTABRIDGE 921600` handshake from 115200 and cleanup.
- `native.go`: profiles, HELLO, 128-byte uncompressed page bursts, bitmap
  repair, durable recovery, activation, confirmation and fresh-session readback.

No Python, PySerial, interpreter discovery or subprocess is required or used.
The archived `reference-python/` and `reference-python-tests/` files are retained only as development references;
they are neither embedded in the binary nor extracted into new attempts.
`testdata/reference-vectors.json` contains frozen vectors from the independent
client, with its commit and source hashes. Go tests compare both codecs and
actual pre-activation engine emissions, direct/routed and for all board IDs.
Tests also run complete simulated cycles with an empty executable search path.
This is software verification, not an RF flash validation of the Go engine.

The operator must explicitly select TTGO LoRa32 V2.1, Heltec V3 or QRP Labs
LightGateway Plus. No board is silently selected for old settings. The board ID
is retained in job.json and the image event and sent in the manifest. Header
validation distinguishes ESP32 from ESP32-S3, not Heltec from QRP Labs. The
receiver checks its board ID too. Only the separate TTGO CLI cycle has physical
validation; Heltec/QRP bench tests and the Go 128-byte path remain to validate.
FORMAT is explained according to its operation, never automatically blamed on
the model; logs retain op/status/offset/payload length.

Station settings, counters and attempt files live in `radio-ota/` alongside
the config DB. Secrets are private, never returned to the browser, and temporary
key files are removed at attempt completion. The APRS command secret remains
in `remote_command_credentials`; binary HELLO counters are separate and persist
`max(Unix time, saved counter + 1)` before transmission. Corrupt/exhausted counters
fail closed. Never reset radio anti-replay state to reconcile clients.

One job owns the bridge. Configured TNC2 (including flags), KISS, GPS and weather
serial ports are rejected. Radio-setting mutations are locked during a job;
a second signed command to its target is rejected. Automatic preparation runs on the configured common normal profile before
firmware transfer: signed role commands use the reserved USB bridge and the
existing credential store/counters, bypassing the generic APRS retry queue.
Keys must already be provisioned; no key is generated or distributed. The
firmware suppresses ordinary APRS forwarding while its OTA session is active. The local USB bridge is the controller,
never the remote target. TCP TNC2 does not carry firmware blocks.

Options `replace_existing`, `require_offline`, `exercise_confirm_timeout` are
per-attempt and default false; `expected_version` is required. Replacement
explicitly destroys the inactive candidate checkpoint. Normal mode confirms
immediately. Advanced validation waits 190 seconds for rollback, verifies the
old boot, reactivates and confirms. Success requires the expected version/SHA,
matching active/boot partitions, pending=false, a fresh authenticated HELLO,
engine completion and controller bridge-off acknowledgement. A full byte count
is not proof of installation. Pause is cooperative before activation. Cleanup
failures are not a clean pause; the public job reports unconfirmed restoration.
Closing the browser leaves the native Go job running; it cannot leave a detached
Python process after a Graywolf crash. On restart an unfinished job is marked
interrupted; no blind activation/retry is performed.

The image descriptor may name ESP-IDF, not the radio version; never infer the
expected version from it or a filename. Browser SHA is available on localhost
or HTTPS; plain HTTP LAN clients still upload and use the authoritative server
SHA. Draft settings stay in memory per target, not browser storage; polling
never replaces edits, and outside clicks do not dismiss the OTA modal.

**Generate route key (initial setup)** uses browser crypto.getRandomValues for
32 bytes encoded as 43 Base64URL characters. It does not provision devices.
Normal updates reuse the existing shared route key; blank preserves the saved
key for the same relay. A replacement requires explicit browser confirmation.
Saved secrets remain write-only; copy failures select the visible draft for
manual Ctrl+C. Initial keys are provisioned before deployment, not by visiting
remote relays during each update.

Reference duration: about 63 minutes to write/verify 1.78 MB at 160 bytes;
whole-image timing at 128 bytes is not yet measured. The roughly 71-minute
laboratory cycle included deliberate rollback. The relay is unavailable for
APRS traffic during the operation. These are not guaranteed update durations.

The full preparation plan and audit remain
[`../FUOTA_ORCHESTRATION_PLAN_FR.md`](../FUOTA_ORCHESTRATION_PLAN_FR.md) and
[`../FUOTA_ORCHESTRATION_AUDIT_FR.md`](../FUOTA_ORCHESTRATION_AUDIT_FR.md).
Operator instructions: [English](../handbook/radio-ota-en.html) /
[français](../handbook/radio-ota-fr.html), also available as PDFs.
Validation: `go test -race ./pkg/radioota ./pkg/webapi`, plus the simulated browser
flow `scripts/screenshots/radio-ota-test.mjs`. Agree hardware availability and
actual profiles/power/versions before any RF test; software checks do not
validate the new Go implementation on cards.


### Route preparation and non-destructive preflight

Native OTA diagnostics include `usb_tx`, `usb_tx_complete`, `usb_read`,
`usb_rx`, `usb_rx_partial`, `usb_read_error` and `usb_write_error` in each
attempt's existing `transfer.jsonl`. The optional bridge trace callback is
installed before the USB handshake. `serial_trace.go` summarizes binary RF
frames without payloads or HMACs, redacts long encoded tokens in console text,
and bounds retained diagnostic text. Partial reads record lengths, not raw
fragments. `bridge_tx_timeout` identifies a missing local controller `TXDONE`
with the block offset and wait duration. Driver write completion is not an RF
receipt. These events do not change retries, profile leases or recovery.
Tests cover fragmented replies, absent TXDONE, I/O errors, secret redaction
and a full simulated cycle with trace persistence. Hardware diagnosis remains
to be performed. Operator paths and sharing precautions are in the OTA guides.

`preparation.go` sends signed `!RC1` APRS frames through `TXRAW` on the reserved
controller USB bridge. It uses `CommandCredentialStore.ReserveEnvelope` through
a server-provided signer, with the RF recipient and signed identity checked
separately. `relay_recipient` optionally names the relay tactical identity;
`via` remains its signed Callsign-SSID for the binary route. The relay needs
its own existing authenticated-control credential in Graywolf. Route members'
commands and credential mutations are blocked during checks and updates.

Before binary HELLO, the engine reads `OTA=?`, requires NORMAL/ARMED=0,
reads `OTA=ROUTE?`, changes RELAY/VIA or direct OFF only if needed, and reads
back the role. These persistent role changes are explicitly announced before
launch and recorded in `roles_changed`; they are not silently rolled back.
Application text replies are source/addressee checked but **not authenticated**.
APRS ACKs and replies for other command categories are ignored; buffered older
replies are cleared. Retries are bounded at three and reserve fresh counters.
The firmware has no request identifier in application text replies, so exact
cryptographic correlation is not claimed. Readiness requires the subsequent
fresh binary HMAC roundtrip, including the shared route key when relayed.

`POST /api/radio-ota/{target}/preflight` accepts the chosen image for local
format/size checks, expected version and the same saved settings. It does not
send START/DATA/ACTIVATE, does not discard a candidate, and ignores destructive
and rollback flags. It can configure the selected persistent roles. After HELLO,
it sends nothing for 65 seconds to let both firmware 60-second sessions expire,
then reads NORMAL/ARMED=0 again from relay and target. The elapsed timer alone
is not a readiness proof. The relay cannot carry APRS traffic during this wait.
Only successful proof, observed idle state and controller bridge cleanup produce
job state `ready`, distinct from firmware state `succeeded`. The result persists;
settings changes mark it stale. Every actual update prepares/rechecks the route
again, rather than trusting an old check. Check/progress/role changes and each
committed firmware page are persisted for interruption diagnosis.

The normal profile must be compatible on the whole route. Lack of response is
not diagnosed uniquely as frequency mismatch: credential, path, RF format and
radio conditions can also cause it. Such uncertainty prevents firmware transfer.
The bridge cannot independently report the local card identity, so the stable
USB link must be associated with the controller during initial setup. Dynamic
Linux ttyACM/ttyUSB names are rejected in favour of stable device links.
Control replies currently support the legacy text radio envelope; exclusively
native/compressed repeats can cause preparation to be refused. The binary OTA
path is independent of this text-envelope limitation. No firmware is modified
by implementing these controls; their complete hardware sequence remains to test.

### Moving authenticated control between computers

There is currently no supported export/import or backup API for command keys,
identities and anti-replay counters. Credential reads expose `last_counter` but
never the secret; create resets the sender counter to zero, and its DTO has no
counter input. Recreating credentials on another PC therefore does not preserve
an installed station's replay state. Binary counters and route settings also
live outside SQLite in the config-adjacent `radio-ota/` directory.

A single active sender is necessary but insufficient: the next PC must resume
above all previously consumed counters, and the returning PC must receive the
new state too. A manual cold copy requires both instances stopped, a consistent
SQLite copy (including proper WAL handling) and the associated radio-ota state;
it can also overwrite machine-specific port settings. This is not a reliable
operator handoff feature to recommend as if already supported.

A future export/import must include both counter domains and identities/keys,
refuse live OTA/signing races and never lower a matching installed counter.
Simply using the clock is not a complete multi-controller solution: firmware
accepts one configured controller identity, and clock skew, same-tick collisions
and radio reordering still matter. Native FUOTA clock-based HELLO counters do
not remove the sequential APRS counters consumed during route preparation.


### Keep the OTA form across incoming-message refreshes

`RadioOtaModal.svelte` derives primitive station/open values before its
initialization effect. A parent `conversation.key` getter can track the whole
conversation object; replacing that object during APRS refresh used to rerun
the effect even for the same station, clearing the selected image, version and
per-attempt checkboxes. Same-station refresh now only updates server progress.
The browser fixture `web/tests/RadioOtaPreview.svelte` replaces a conversation
object while keeping its key and asserts preservation of File/version/options.
The test failed with the original code and passes with the primitive boundary.
Actual station changes or reopening still initialize a new attempt.

The selected filename is also exposed in a read-only text field for copying;
clicking a native file-input filename opens its picker instead of selecting
text. Empty picker changes/cancellation keep the already selected File;
**Remove selected firmware** explicitly clears it. None of these UI actions
transmit firmware or operate a radio.


The operator's initial card preparation order and the bench configuration
example are in [the French handbook](../handbook/radio-ota-fr.html#ordre-preparation)
and [the English handbook](../handbook/radio-ota-en.html#preparation-order).
Prepare the remote relay then target; the local controller is ready before
radio operations. Updating the relay's own firmware is not a prerequisite
for configuring its route role. These instructions describe required settings,
not a live readout or proof of the bench cards' current state.


### Different service profiles (rendezvous)

When the target's service profile differs from the controller and relay
profile (`Settings.TargetNormal`), `pkg/radioota/rendezvous.go` moves the relay
to the target's profile for the operation, then returns it to its own service
and verifies it. It needs coordination v1 in the unified firmware
(`4.0.2RXT-OTA2` and later): operations 32 state/challenge, 33 rendezvous,
34 monotonic keepalive, 35 non-destructive release. The relay key is resolved
through the existing credential store. USB `RADIOSERVICE` and `RADIOPROFILE`
distinguish the controller's configured service from its applied profile.

The relay holds the target's profile under a 60-second lease renewed every
25 s (each keepalive may be asked three times), and returns to its own service
by itself when the lease lapses. The candidate is confirmed right after its
boot proof; the target's service profile is verified afterwards. After a lost
link the rendezvous is established again from the service profiles before the
transfer resumes. A rendezvous uses a single relay; the common-profile path is
unchanged when `TargetNormal` is absent.

Validated on the bench on 14-15 November 2026 (three TTGO LoRa32 V2.1,
433.775 / 434.100 MHz): full image transferred in two sessions with resume,
five link losses recovered, confirmed and read back, relay and controller read
back on their own service. No field validation yet.

#### Relay chains

OTA profiles accept SF5-SF12. The target-specific UI and backend keep SF7 as
the minimum for TTGO/SX1278 target service and transfer profiles; QRP Labs
(SX1268) and Heltec V3 (SX1262) can select SF5/SF6. A target model says nothing
about the USB controller or relays. The controller validates PROFILEDEF in its
own firmware. For SF5/SF6 transfer, preparation also sends OTA=PROFILE to every
relay and the target, requiring exactly OTA PROFILE READY (RAM) before binary
negotiation. This validates support without switching profiles or writing Config;
APRS replies remain observations, not authenticated proof. Actual switching
still uses the existing HMAC PREPARE_FAST exchange and firmware chip limits.
No settings migration or firmware change is needed. SF5/SF6 paths have software
tests only, not a physical transfer validation.

"Further relays towards the target" adds relays after the first one, nearest
first, as `CALL-SSID` or `CALL-SSID:NAME` when a relay transmits under a
tactical name (at most three). Route preparation checks and configures each
relay in turn, reaching it through the relays already configured with an
explicit APRS path, with `OTA=RELAY,<target>,<previous>,<next>`; the target is
told to expect the last relay. The transfer itself is unchanged for the
controller, which only exchanges frames with the first relay; response delays
grow with each relay. Every relay needs firmware `4.0.2RXT-OTA2.2` or later,
the shared route key, digipeating enabled and an authenticated-control key in
Graywolf. A chain uses one common service profile: the rendezvous between
different service profiles is limited to a single relay.
