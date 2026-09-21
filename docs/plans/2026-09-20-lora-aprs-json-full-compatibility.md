# LoRa APRS JSON full compatibility TODO

Status: implemented on 2026-09-21. The architecture review is incorporated,
the focused compatibility suite passes, and the operator/wiki documentation
describes the implemented behavior.

## Objective

Make Graywolf a fully compatible LoRa APRS JSON consumer while keeping its
AX.25 implementation strict. Every schema-valid `rx` event must be accepted
and preserved, including a reception whose `parse_status` is `malformed` and
which cannot produce a parsed TNC2 packet.

When an event does contain a valid TNC2-compatible envelope, its textual
addresses must not be rejected or altered merely because they cannot be
encoded as classic AX.25 addresses. This includes identities such as:

- `F4JJE-16`
- `NN7LE-S`
- `NN7LE-GS`

The complete textual address and its opaque suffix remain authoritative. A
numeric suffix greater than 15 is not an AX.25 SSID, and no integer type may
impose an artificial limit on the suffix namespace.

## Historical intent

Jon Adams, N7UV, asked that non-legacy LoRa TNCs not remain constrained by
KISS and classic AX.25 addressing. He specifically cited high and
alphanumeric suffixes, graphical application integration, and possible use
with TNC2-compatible non-APRS packet-radio applications. JSON is the modern
application interface; it is not the current on-air RF encoding.

## Defect addressed

Before this implementation, the JSON service decoded
`packet.raw_tnc2_base64`, but
`app.aprsJSONProduce` immediately calls `aprs.ParseTNC2`. That parser converts
every address through `ax25.ParseAddress`, which requires a 1-6 character
callsign and a numeric SSID in the range 0-15. The frame is then encoded back
to binary AX.25 before entering the receive fanout.

Consequences:

- opaque or out-of-range suffixes are rejected;
- the reception does not enter Graywolf's normal packet log or station cache;
- RXT links may still be updated before packet delivery fails;
- the JSON cursor is nevertheless advanced, so the rejected reception is not
  replayed;
- the wiki currently describes conversion to AX.25 more broadly than the code
  can safely provide.

## Layered receive model

A schema-valid JSON reception and a parsable TNC2 envelope are different
levels. The lossless model must make that distinction explicit:

```text
JSON rx event
    |
    +-- RawReception
            authoritative raw bytes
            event/boot/sequence identity
            receiver and timestamps
            local radio metrics
            RXT metadata
            parse_status
              |
              +-- TNC2Packet, only when parsable
                      |
                      +-- APRS semantics, when applicable
                      |
                      +-- AX.25 adapter, when representable
```

`RawReception` is the universal protocol-level container. It exists for every
accepted `rx`, including `parse_status:"malformed"`. `TNC2Packet` is an
optional derived representation and must never be required to preserve the
reception itself.

Candidate address model:

```go
type PacketAddress struct {
    Text     string
    Call     string
    Suffix   string
    Repeated bool
}
```

`Text` preserves the exact received representation. For a path element this
includes its received trailing `*`; `Repeated` exposes the same fact without
discarding it from `Text`. `Suffix` is always an opaque string. The logical
model deliberately contains no generic numeric-suffix field.

AX.25 representability is calculated only at the adapter boundary:

```go
func (a PacketAddress) AX25SSID() (uint8, bool)
```

It succeeds only when the address meets all classic AX.25 constraints and the
suffix is absent or strictly ASCII-decimal in the range 0-15. An arbitrarily
long numeric suffix remains valid and exactly preserved even though it cannot
be converted to an integer or encoded as AX.25.

Candidate parsed-packet model:

```go
type TNC2Packet struct {
    Raw         []byte
    Source      PacketAddress
    Destination PacketAddress
    Path        []PacketAddress
    Information []byte
}
```

## Architectural invariants

- `RawReception` owns protocol-level acceptance and exact preservation.
- `TNC2Packet` is derived only when the envelope is parsable.
- The extended TNC2 model and parser must not call or depend on
  `ax25.ParseAddress`.
- Dependency is one-way: a separate adapter may convert `TNC2Packet` to
  `ax25.Frame`; `ax25.Frame` must not define the extended model.
- Failed AX.25 conversion must not mutate or invalidate the reception.
- AX.25 representability and output authorization are independent gates.
- JSON ingress remains receive-only by default, including when its packet is
  AX.25-representable.
- Emission requires both representability and an explicitly enabled,
  policy-authorized output path. Representability alone never enables KISS,
  digipeating, RF or APRS-IS output.
- Extending transport identities does not relax constraints inside historical
  APRS application fields. Each APRS format retains its own validation rules.
- Cursor advancement means the protocol event was accepted and preserved; it
  must not depend on successful APRS decoding or AX.25 conversion.

## Ordered implementation checklist

1. [x] Introduce the lossless `RawReception` model and preserve every
       schema-valid `rx`, including `parse_status:"malformed"`.
2. [x] Introduce `PacketAddress` and optional `TNC2Packet` without numeric or
       AX.25-derived limits.
3. [x] Add a lossless extended-TNC2 parser that preserves raw bytes, binary
       information fields, opaque suffixes and exact path `*` markers.
4. [x] Add a separate, explicit and checked `TNC2Packet` to `ax25.Frame`
       adapter. Keep existing `ax25.Address` and encoder semantics strict.
5. [x] Move JSON ingress onto `RawReception` and the optional extended TNC2
       model instead of immediately forcing `aprs.ParseTNC2` and AX.25
       re-encoding.
6. [x] Decouple APRS semantic decoding from the requirement that the complete
       transport envelope first become an `ax25.Frame`. Preserve the existing
       constraints of individual APRS fields.
7. [x] Feed accepted receptions and extended identities to the packet log,
       station cache and map without truncation or identity rewriting. Retain
       valid non-APRS application data as well.
8. [x] Audit every output boundary. Require explicit policy authorization in
       addition to representability, and keep JSON ingress receive-only by
       default.
9. [x] Add the complete protocol compatibility and regression suite listed
       below, using official schema/example vectors where possible.
10. [x] Update `docs/wiki/rxt-telemetry.md` and operator-facing documentation
        only after implementation behavior matches the wider model.

Throughout these steps, continue extracting `reception.local` and
`reception.rxt` independently of TNC2 parsing, APRS semantics and AX.25
encodability. Make preservation or delivery failures observable without
terminating later valid stream records unnecessarily.

## Compatibility tests

- [x] Normal APRS packet with an AX.25-compatible source, destination and path.
- [x] `F4JJE-16` preserved exactly and not encoded as AX.25.
- [x] `NN7LE-S` and `NN7LE-GS` preserved exactly.
- [x] Extended identity used in source, destination and each path position.
- [x] Numeric suffix 0-15 converts to AX.25 without changing identity.
- [x] Numeric suffix greater than 15 remains valid TNC2 but is not AX.25
      encodable.
- [x] Numeric suffix too large for `uint64` is preserved exactly and rejected
      only by the AX.25 adapter.
- [x] A path element retains its exact trailing `*` in `Text` while also
      exposing `Repeated=true`.
- [x] The same extended textual identity appearing in multiple packets is
      recognized as the same station.
- [x] Binary Mic-E information remains byte-for-byte identical.
- [x] TNC2-compatible non-APRS application data is retained.
- [x] Valid non-APRS TNC2 with an extended suffix is retained.
- [x] Schema-valid `parse_status:"malformed"` with no possible `TNC2Packet` is
      retained without terminating the stream.
- [x] APRS information with an extended transport source is decoded where
      applicable without relaxing unrelated fixed-width APRS fields.
- [x] RXT accompanying non-APRS, malformed or non-AX.25-representable data is
      retained according to the JSON event.
- [x] Replay duplicates remain deduplicated by `event_id`/boot sequence.
- [x] A rejected output conversion does not lose the accepted input event.
- [x] An AX.25-representable JSON input is still not emitted without separate
      explicit output authorization.
- [x] Existing KISS, RF, digipeater and APRS-IS behavior does not regress.
- [x] Exercise the official protocol schema and example vectors as consumer
      compatibility fixtures, with their provenance recorded.

## Boundaries and non-goals for schema 1.0

- JSON remains the HTTP application transport, not the RF wire encoding.
- The producer snapshot is for dashboards and is not mixed into Graywolf's
  lossless stream ingestion.
- JSON TX remains optional.
- Connected-mode AX.25 is not represented by the current TNC2-compatible
  schema and remains future work.
- Full input compatibility does not authorize an unrepresentable or otherwise
  disallowed reception for AX.25/KISS/RF/APRS-IS output.
- Supporting extended transport identities does not automatically extend
  every legacy APRS field or application format.

## Open design decisions

- [x] `RawReception` lives in `pkg/rxttelemetry`; the independent textual model
      lives in `pkg/tnc2`, with the one-way adapter in `pkg/tnc2ax25`.
- [x] The existing packet-log entry now embeds the complete `RawReception`, so
      no database migration is required for the bounded in-memory log.
- [x] Extended JSON identities feed passive APRS decode, packet log, station
      cache and map semantics. Messages, actions, KISS, RF, digipeater and
      APRS-IS output remain unauthorized for JSON ingress.
- [x] A pinned subset of the official protocol vectors is vendored under
      `pkg/rxttelemetry/testdata`, with its source commit recorded.
- [x] Malformed and non-APRS receptions use their raw TNC2 display plus notes
      and the embedded JSON reception metadata in the existing packet log.
- [x] The durability point is the successful synchronous packet-log record;
      handler failure leaves the resume cursor unchanged for replay.

## Completion criteria

Graywolf accepts every schema-valid `rx` event in the supported protocol and
schema version and preserves its authoritative bytes and reception metadata.
When a TNC2 envelope is parsable, Graywolf preserves its exact textual
identities and processes all applicable APRS/RXT semantics. AX.25 limitations
apply only at the one-way AX.25 adapter, and output additionally requires
explicit authorization. No valid input is silently truncated, reinterpreted
or discarded because of parsing depth, APRS applicability or the legacy AX.25
address model.
