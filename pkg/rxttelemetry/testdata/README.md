# LoRa APRS JSON protocol fixtures

`lora-aprs-json-v1-consumer-vectors.ndjson` is an exact subset of
`examples/lora-aprs-json-vectors.ndjson` from the
`moricef/LoRa_APRS_JSON_Protocol` repository at commit
`c260d45bb52d70e8a5cf1d6b74ab73ca2d85071c` (2026-09-21).

The subset covers the versioned handshake, an opaque alphanumeric station
suffix, and valid TNC2-compatible non-APRS traffic with RXT metadata. Local
Graywolf-only edge cases live in Go tests beside the consumer. The source
vector file passed the protocol repository's draft-2020-12 schema validation
at the pinned commit.
