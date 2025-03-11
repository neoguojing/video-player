package rtp

import (
	"fmt"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec"
	"videoplayer/joy4/format/rtsp/sdp"
)

type PCMADynamicProtocol struct {
	codecData av.AudioCodecData
}

func (h *PCMADynamicProtocol) Type() av.CodecType {
	return av.PCM_ALAW
}

func (h *PCMADynamicProtocol) ParseSDP(sdp *sdp.Media) error {
	if sdp != nil {
		h.codecData = codec.NewPCMAlawCodecData(sdp.TimeScale)
	}

	return nil
}

func (h *PCMADynamicProtocol) DefaultTimeScale() int {
	return 8000
}

func (h *PCMADynamicProtocol) ParsePacket(pkt *av.Packet, buf []byte, timestamp uint32, flags int) (uint32, int) {
	pkt.MediaCodecType = h.Type()
	pkt.Data = make([]byte, len(buf))
	copy(pkt.Data, buf)
	return timestamp, 0
}

func (h *PCMADynamicProtocol) CodecData() av.CodecData {
	if h.codecData == nil {

		h.codecData = codec.NewPCMAlawCodecData(h.DefaultTimeScale())
	}
	return h.codecData
}

func (h *PCMADynamicProtocol) PayloadType() int {
	return 8
}

func (h *PCMADynamicProtocol) SDPLines() []string {
	rtpmap := fmt.Sprintf("a=rtpmap:%d PCMA/%d/%d", h.PayloadType(), h.codecData.SampleRate(), h.codecData.ChannelLayout().Count())
	return []string{rtpmap}
}
