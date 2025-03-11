package rtp

import (
	"fmt"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec"
	"videoplayer/joy4/format/rtsp/sdp"
)

type PCMMDynamicProtocol struct {
	codecData av.AudioCodecData
}

func (h *PCMMDynamicProtocol) Type() av.CodecType {
	return av.PCM_MULAW
}

func (h *PCMMDynamicProtocol) ParseSDP(sdp *sdp.Media) error {
	if sdp != nil {
		h.codecData = codec.NewPCMMulawCodecData(sdp.TimeScale)
	}

	return nil
}

func (h *PCMMDynamicProtocol) DefaultTimeScale() int {
	return 8000
}

func (h *PCMMDynamicProtocol) ParsePacket(pkt *av.Packet, buf []byte, timestamp uint32, flags int) (uint32, int) {
	pkt.MediaCodecType = h.Type()
	pkt.Data = make([]byte, len(buf))
	copy(pkt.Data, buf)
	return timestamp, 0
}

func (h *PCMMDynamicProtocol) CodecData() av.CodecData {
	if h.codecData == nil {
		h.codecData = codec.NewPCMMulawCodecData(h.DefaultTimeScale())
	}
	return h.codecData
}

func (h *PCMMDynamicProtocol) PayloadType() int {
	return 0
}

func (h *PCMMDynamicProtocol) SDPLines() []string {
	rtpmap := fmt.Sprintf("a=rtpmap:%d PCMU/%d/%d", h.PayloadType(), h.codecData.SampleRate(), h.codecData.ChannelLayout().Count())
	return []string{rtpmap}
}
