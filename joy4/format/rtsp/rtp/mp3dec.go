package rtp

import (
	"videoplayer/joy4/av"
	"videoplayer/joy4/codec/mp3parser"
	"videoplayer/joy4/format/rtsp/sdp"
)

type MP3DynamicProtocol struct {
	hasCodecData bool
	codecData    mp3parser.CodecData
}

func (h *MP3DynamicProtocol) Type() av.CodecType {
	return av.MP3
}

func (h *MP3DynamicProtocol) ParsePacket(pkt *av.Packet, buf []byte, timestamp uint32, flags int) (uint32, int) {
	if len(buf) < 4 {
		return timestamp, -1
	}
	if !h.hasCodecData {
		d, err := mp3parser.NewCodecDataFromMP3AudioConfigBytes(buf)
		if err != nil {
			return timestamp, -1
		}
		h.codecData = d
		h.hasCodecData = true
	}
	// head := binary.BigEndian.Uint32(buf)
	pkt.MediaCodecType = h.Type()
	buf = buf[4:]
	pkt.Data = make([]byte, len(buf))
	copy(pkt.Data, buf)
	return timestamp, 0
}

func (h *MP3DynamicProtocol) ParseSDP(sdp *sdp.Media) error {
	return nil
}

func (h *MP3DynamicProtocol) DefaultTimeScale() int {
	return 90000
}

func (h *MP3DynamicProtocol) CodecData() av.CodecData {
	if h.hasCodecData {
		return h.codecData
	}
	return nil
}

func (h *MP3DynamicProtocol) PayloadType() int {
	return 14
}

func (h *MP3DynamicProtocol) SDPLines() []string {
	return nil
}
