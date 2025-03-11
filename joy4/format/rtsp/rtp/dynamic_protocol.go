package rtp

import (
	"videoplayer/joy4/av"
	"videoplayer/joy4/format/rtsp/sdp"
)

type DynamicProtocol interface {
	av.CodecData

	// return 0 on packet, no more left, 1 on packet, -1 on partial packet
	ParsePacket(pkt *av.Packet, buf []byte, timestamp uint32, flags int) (uint32, int)
	ParseSDP(*sdp.Media) error

	CodecData() av.CodecData
	DefaultTimeScale() int

	PayloadType() int
	SDPLines() []string
}
