# Weather display units: validation on 14 October 2026

The fix prevents an old preference response from replacing a newer choice
and serializes successive saves. The reproduced defect was an interface
ordering error: a GET started before a successful metric save could restore
imperial in the interface and its browser mirror. The original report from
F6DEV has not been reproduced on his installation.

## Automated checks

Five frontend tests exercise the actual compiled preference store: an old
GET after a save, entering Preferences during a save, successive choices,
failed saves, and loading a saved choice into a fresh browser mirror.
The existing configuration-store and HTTP units tests also pass. A separate
SQLite close/reopen check retains the saved metric value.

## Browser simulation

An isolated Graywolf instance uses the corrected local build
`v0.14.14-f81b6874-dirty`. Three synthetic, positioned APRS weather reports
enter through a local TCP TNC2 server. APRS-IS is disabled and no RF channel
or TNC2 transmit transport is configured. Chromium runs the real WebUI.

| Station | Imperial temperature | Metric temperature |
| --- | --- | --- |
| WXTEST-1 | 68 F | 20 C |
| WXTEST-2 | -4 F | -20 C |
| WXTEST-3 | 104 F | 40 C |

For each unit system, the test checks all three temperature labels, the
WXTEST-1 weather popup, and the preference returned by the server:

1. after navigation from General preferences to Live Map;
2. after browser reload;
3. after stopping and restarting the test server, then reloading the page.

All six cases pass. WXTEST-1 displays wind 10 mph / 16 km/h, gusts
20 mph / 32 km/h, hourly rain 0.10 inches / 2.5 mm, humidity 75%, and
pressure 1013.2 mb in both systems. Popup opening is triggered by a DOM
click event on the station marker.

## Evidence and scope

The fixture, injected frames, structured results and browser screenshots
are retained locally in `packages/weather-simulation-20261014/`, an untracked
directory. `README.md` describes the setup; `check_units.py` runs the six
browser cases, and `units-results.json` records their observations.
These local files are not included with this report.

The observations validate this fix with synthetic weather and the stated
local setup. They do not establish the cause of F6DEV's reported behavior,
validate RF delivery, or validate every browser and operating system.
