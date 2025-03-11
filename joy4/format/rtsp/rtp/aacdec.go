package rtp

import (
	"encoding/hex"
	"fmt"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec/aacparser"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/log"
)

type AACDynamicProtocol struct {
	codecData aacparser.CodecData
}

/* Follows RFC 3640 */
func (h *AACDynamicProtocol) Type() av.CodecType {
	return av.AAC
}

func (h *AACDynamicProtocol) ParseSDP(sdp *sdp.Media) error {
	if len(sdp.Config) == 0 {
		return fmt.Errorf("rtp: aac sdp config missing")
	}
	// TODO: parse fmtp
	config, err := aacparser.NewCodecDataFromMPEG4AudioConfigBytes(sdp.Config)
	if err == nil {
		h.codecData = config
	}
	return err
}

func (h *AACDynamicProtocol) ParsePacket(pkt *av.Packet, buf []byte, timestamp uint32, flags int) (uint32, int) {
	if len(buf) < 4 {
		log.Log(log.ERROR, "rtp: aac packet too short")
		return timestamp, -1
	}
	payload := buf[4:] // TODO: remove this hack
	pkt.MediaCodecType = av.AAC
	pkt.Data = make([]byte, len(payload))
	copy(pkt.Data, payload)
	return timestamp, 0
}

func (h *AACDynamicProtocol) DefaultTimeScale() int {
	return 48000
}

func (h *AACDynamicProtocol) CodecData() av.CodecData {
	return h.codecData
}

func (h *AACDynamicProtocol) PayloadType() int {
	return 97
}

func (h *AACDynamicProtocol) SDPLines() []string {
	rtpmap := fmt.Sprintf("a=rtpmap:%d MPEG4-GENERIC/%d/%d", h.PayloadType(), h.codecData.SampleRate(), h.codecData.ChannelLayout().Count())
	fmtp := fmt.Sprintf("a=fmtp:%d profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3;indexdeltalength=3", h.PayloadType())
	if len(h.codecData.ConfigBytes) > 0 {
		fmtp += ";config=" + hex.EncodeToString(h.codecData.ConfigBytes)
	}
	return []string{rtpmap, fmtp}
}
