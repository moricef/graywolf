# Historical Python reference — not a Graywolf runtime

These files are retained for protocol comparison and development only. They
are no longer embedded, shipped as a runtime, extracted or executed by Graywolf.
The OTA implementation is native Go in `../protocol.go`, `../bridge.go` and
`../native.go`. Python and PySerial are not required on the Graywolf host.

The preserved snapshot derives from the iGate client at base 834bf7c with
bridge cleanup/POSIX exclusivity adaptations and the later explicit board-ID
selection. The independent frozen vectors in `../testdata/` were generated
from the sibling iGate client at the recorded commit and source hashes,
not from the Go implementation or its simulator. Regeneration is development
work only and is not part of a build, test run or installation.

The historical Python CLI physical cycle on 14 November used 160-byte blocks.
It does not validate Graywolf’s native Go implementation, 128-byte radio path,
or Heltec/QRP hardware operation. Those still require bench validation.
