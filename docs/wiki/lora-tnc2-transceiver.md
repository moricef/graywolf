# Direct LoRa TNC2 transceiver (TCP or serial)

Graywolf can connect directly to a LoRa transceiver that sends and accepts
newline-delimited TNC2 packets. This connection is separate from the LoRa APRS
JSON/RXT source and from KISS. It keeps extended textual identities such as
`F4MLV-16` or `F4MLV-GS` and binary Mic-E information without AX.25 address
conversion.

Use `-tnc2-tcp HOST:PORT` or `-tnc2-serial DEVICE -tnc2-baud 115200` for
reception. Both may be configured at once. Incoming TNC2 packets are decoded
for Graywolf's packet log and station map; they are not automatically forwarded
to KISS, the digipeater, APRS-IS, or RF. Non-packet console and RXT diagnostic
lines on the same port are ignored. RXT measurements still come from the
separate JSON/RXT source.

Transmission is disabled by default. To authorize it, additionally configure
exactly one output transport, one textual source identity, and one Graywolf
channel. For example:

```text
-tnc2-tcp 192.168.1.161:8001
-tnc2-tx-transport tcp
-tnc2-tx-source F4MLV-GS
-tnc2-tx-channel 1
-tnc2-max-tx-bytes 255
```

For a serial-connected transceiver, select `-tnc2-tx-transport serial` and
provide `-tnc2-serial DEVICE -tnc2-baud BAUD` instead. The configured source
must match the source in each submitted packet exactly. The byte limit is the
peer's input-record limit; 255 is the current CA2RXU TNC2 buffer limit.
Packets containing CR or LF are rejected because either byte would split the
wire record. TX requests use Graywolf's priority, deduplication and per-channel
rate governor, but never pass through the AX.25 encoder.

The first explicit TX entry point is the authenticated
`POST /api/tnc2/tx` endpoint. Its JSON body contains one complete TNC2 packet
as `raw_tnc2_base64`. A `202` response means the packet was accepted into the
TX queue, **not** that the transceiver transmitted it over RF. The source
check, output permission, connection state, and peer byte limit are enforced
before queueing. A normal receive connection does not authorize transmission.

The message composer uses this textual output for RF sends on the authorized
channel, including retries. Incoming TNC2 APRS messages reach the normal
Graywolf inbox. By default, the connected iGate/TNC owns their auto-ACKs;
Graywolf does not send a second ACK. Only use `-tnc2-autoack` when the
connected device does not generate ACKs itself. This option requires explicit
TNC2 TX authorization. The configured station callsign must exactly match
`-tnc2-tx-source`; a different source is refused rather than converted to
AX.25. Other channels retain their existing AX.25/KISS behavior. APRS message
addressees still have their standard nine-character limit. Beacon TX is not
yet routed through this textual output.

The iGate must have its TNC port configured for TNC2 input and LoRa TX
enabled. If the submitted source
equals the iGate's own callsign, CA2RXU also requires its "accept own" TNC
setting; otherwise it deliberately ignores the frame. CA2RXU currently trims
the incoming text record, so Graywolf's exact bytes-to-port guarantee does not
imply byte-for-byte identity after the firmware's input processing for records
with leading or trailing whitespace.
