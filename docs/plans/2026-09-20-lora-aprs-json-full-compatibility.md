# LoRa APRS JSON full compatibility TODO

Status: paused at the user's request on 2026-09-20.

## Objective

Make Graywolf a fully compatible LoRa APRS JSON consumer while keeping its
AX.25 implementation strict. Valid TNC2-compatible receptions must not be
rejected or altered merely because their textual addresses cannot be encoded
as classic AX.25 addresses.

This includes identities such as:

- `F4JJE-16`
- `NN7LE-S`
- `NN7LE-GS`

The complete textual address and its opaque suffix remain authoritative. A
numeric suffix greater than 15 is not an AX.25 SSID.

## Historical intent

Jon Adams, N7UV, asked that non-legacy LoRa TNCs not remain constrained by
KISS and classic AX.25 addressing. He specifically cited high and
alphanumeric suffixes, graphical application integration, and possible use
with TNC2-compatible non-APRS packet-radio applications. JSON is the modern
application interface; it is not the current on-air RF encoding.

## Current defect

The JSON service correctly decodes `packet.raw_tnc2_base64`, but
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

## Architectural direction

Add a lossless TNC2-level packet model above `ax25.Frame`. Do not relax or
overload `ax25.Address`, because it represents the real binary AX.25 format.

Candidate address model:

```go
type PacketAddress struct {
    Text          string
    Call          string
    Suffix        string
    NumericSuffix *uint64
    Repeated      bool
}
```

`Text` preserves the exact received representation. `Suffix` is opaque.
`NumericSuffix` is only a parsed view and does not imply AX.25 encodability.

Candidate packet model:

```go
type TNC2Packet struct {
    Raw         []byte
    Source      PacketAddress
    Destination PacketAddress
    Path        []PacketAddress
    Information []byte
}
```

Provide an explicit checked adapter from this model to `ax25.Frame`. It must
fail without mutation when an address, path, or other field is not
representable in classic AX.25.

## Required work

- [ ] Add a lossless parser for the extended TNC2 envelope.
- [ ] Preserve exact raw bytes, including binary information fields.
- [ ] Represent opaque suffixes independently from AX.25 SSIDs.
- [ ] Add an explicit `IsAX25Representable`/conversion boundary.
- [ ] Keep the existing strict `ax25.Address` and AX.25 encoder semantics.
- [ ] Refactor APRS semantic decoding so it does not require first converting
      the complete packet to binary AX.25.
- [ ] Feed extended identities to the packet log, station cache and map without
      truncation or normalization into a different identity.
- [ ] Retain valid non-APRS TNC2-compatible receptions even when no APRS
      semantic decoder accepts their information field.
- [ ] Continue extracting `reception.local` and `reception.rxt` independently
      of APRS decoding and AX.25 encodability.
- [ ] Prevent JSON ingress from being emitted to KISS, digipeating, RF or
      APRS-IS unless an explicitly authorized output path can represent it.
- [ ] Define cursor advancement as successful protocol-level acceptance and
      preservation, not successful AX.25 conversion.
- [ ] Make packet-delivery failures observable without breaking later stream
      records.
- [ ] Update `docs/wiki/rxt-telemetry.md` after the implementation matches the
      wider model.

## Compatibility tests

- [ ] Normal APRS packet with an AX.25-compatible source, destination and path.
- [ ] `F4JJE-16` preserved exactly and not encoded as AX.25.
- [ ] `NN7LE-S` and `NN7LE-GS` preserved exactly.
- [ ] Extended identity used in source, destination and each path position.
- [ ] Numeric suffix 0-15 converts to AX.25 without changing identity.
- [ ] Numeric suffix greater than 15 remains valid TNC2 but is not AX.25
      encodable.
- [ ] Binary Mic-E information remains byte-for-byte identical.
- [ ] TNC2-compatible non-APRS application data is retained.
- [ ] `parse_status:"malformed"` is retained without terminating the stream.
- [ ] RXT accompanying non-APRS or non-AX.25-representable data is retained.
- [ ] Replay duplicates remain deduplicated by `event_id`/boot sequence.
- [ ] A rejected output conversion does not lose the accepted input event.
- [ ] Existing KISS, RF, digipeater and APRS-IS behavior does not regress.
- [ ] Exercise the official protocol schema and example vectors as consumer
      compatibility fixtures, with their provenance recorded.

## Boundaries and non-goals for schema 1.0

- JSON remains the HTTP application transport, not the RF wire encoding.
- The producer snapshot is for dashboards and is not mixed into Graywolf's
  lossless stream ingestion.
- JSON TX remains optional.
- Connected-mode AX.25 is not represented by the current TNC2-compatible
  schema and remains future work.
- Full compatibility does not require forcing an unrepresentable reception
  onto an AX.25/KISS/RF output.

## Open design decisions

- [ ] Choose the package and public API for the transport-independent packet
      model.
- [ ] Decide whether the existing packet-log schema can preserve these events
      or needs a migration/new reception record.
- [ ] Define which APRS semantic features apply to extended identities,
      especially messages, actions, filters and APRS-IS policy.
- [ ] Decide how Graywolf consumes the protocol repository's validation
      vectors without allowing the two projects to drift.
- [ ] Define UI presentation for a valid retained packet that is not APRS or
      cannot be represented as AX.25.

## Completion criteria

Graywolf accepts every schema-valid `rx` event in the supported protocol and
schema version, preserves its authoritative bytes and identities, processes
all applicable APRS/RXT semantics, and applies AX.25 limitations only at an
explicit AX.25 output boundary. No valid input is silently truncated,
reinterpreted, or discarded because of the legacy address model.
