# Independent client reference vectors

`reference-vectors.json` was generated on 14 November 2026 from the sibling
`LoRa_APRS_iGate-ota/tools/lora_ota` client. The JSON records the repository
commit and SHA-256 of each source module, all non-secret deterministic inputs,
and exact expected wire bytes. The key is the public test sequence 00..1f;
the route key is 20..3f, not an operational credential.

Generation used the reference client's `send_integrated.make_frame` and
`decode_reply`, `RouteTransport.encode` (request phase 0, reply phase 3), and
`RadioProfile.encode`. Window manifests were captured from the actual first
`window_transfer.transfer(..., block_size=128)` request, with each explicit
board ID; a capture-only client raised an exception at START, before any I/O.
No USB device or radio was opened. The reference source hashes allow a reviewer
to detect client changes; these vectors do not come from the Go simulator.

Coverage: HELLO counter 71, temporary profile at 434.5 MHz/BW500/SF7/CR5/9 dBm,
three board manifests, five 128-byte or final-short blocks of a deterministic
513-byte image, bitmap query, finish/activation, confirmation/reactivation,
status/return/offline/abort, boot proofs and FORMAT refusals. Both end-to-end
and routed HMACs are frozen. These fixtures are at 128 bytes, unlike the
historical 160-byte physical-transfer log.

Normal Go tests read the fixture without Python or any generator. They compare
codecs and actual engine transmissions up to activation, direct and routed, for
all three boards. New reference vectors require review against the firmware;
do not regenerate them from the Go implementation under test.

This is an interoperability reference, not physical validation on any board.

`rendezvous-state.hex` is the 31-byte coordination-state extension emitted by
the firmware C++ `encodeCoordinationState` in `include/ota_rendezvous.h`.
Regenerate independently in `LoRa_APRS_iGate-ota`:

```sh
g++ -std=c++11 -Wall -Wextra -Werror -Iinclude tools/lora_ota/test_rendezvous.cpp -o /tmp/test-ota-rendezvous
/tmp/test-ota-rendezvous --vector
```

It describes phase CONTROL, armed, no active routed transfer, known applied
profile, service 433.775 MHz and rendezvous 434.100 MHz (both BW125/SF12/CR5/
20 dBm), keepalive sequence 17. `TestFirmwareCoordinationVector` decodes those
firmware bytes with the Go decoder. Test keys and challenges are public,
never operational keys. Coordination remains untested on physical radios.
