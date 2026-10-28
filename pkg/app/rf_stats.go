package app

import (
	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// recordIGateMessageTX runs at the successful downstream-send hook. Ordinary
// local messages, beacons, dedup drops and failed sends must not bump MSG_CNT.
func (a *App) recordIGateMessageTX(channel uint32, p *aprs.DecodedAPRSPacket, source txgovernor.SubmitSource) {
	if source.Kind != "igate" || source.Detail != "is2rf" || p == nil || p.ThirdParty == nil || p.ThirdParty.Message == nil {
		return
	}
	a.rfDX.RecordMessageDownlink(channel)
}
