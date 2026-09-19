# LoRa APRS RXT telemetry

Graywolf can consume the RXT JSON side channel exposed by compatible LoRa APRS
iGates. RXT metadata remains separate from the normal APRS/TNC2 packet stream;
Graywolf uses it to display per-hop reception measurements and recent RF links.

## Configure an iGate

Open **Settings -> RXT**, enter the absolute URL of the iGate endpoint, and
select **Save**. For example:

```text
http://192.168.1.161/rxt.json
```

Graywolf polls the endpoint every five seconds. The same page reports the last
attempt, last successful response, the latest error, the number of JSON records
received, and the number of active links. An empty URL disables polling.

The **RXT Telemetry** page lists decoded links and their RSSI, SNR, frequency
offset, time-to-hop, packet, age, and map-position state. The Live Map draws a
link only after APRS has supplied positions for both endpoint callsigns. The
newest observation replaces older data for the same directed link, and links
expire after 30 minutes.

## Expected JSON

The iGate endpoint returns an array. Graywolf consumes `age_ms`, `packet`, and
the `rxt_hops` entries whose `has_data` value is true:

```json
[
  {
    "rx_time": "09:25:13",
    "age_ms": 203355,
    "packet": "F5BYL-10>APLRG1,F6DEV-10*,F4MLV-10*:...",
    "local": {
      "rssi_dbm": -69,
      "snr_db": 9.5,
      "fo_hz": 2540
    },
    "rxt_hops": [
      {
        "from": "F5BYL-10",
        "to": "F6DEV-10",
        "has_data": true,
        "rssi_dbm": -120,
        "snr_db": 1.25,
        "fo_hz": -2177,
        "tth_ms": 6791
      }
    ]
  }
]
```

Polling uses a four-second HTTP timeout and accepts at most 1 MiB per response.
The iGate and Graywolf do not need to share a timezone because `age_ms`, rather
than the wall-clock-only `rx_time`, determines the observation time.

## Portable Windows test build

The Windows ZIP contains both `graywolf.exe` and `graywolf-modem.exe`. Extract
the complete archive, keep both executables in the same directory, and start
`graywolf.exe`. The modem helper is discovered and launched automatically; it
does not need to be installed or started separately.

Graywolf stores its configuration and logs under `C:\ProgramData\Graywolf`, so
replacing an extracted test-build directory does not erase the saved RXT URL.
