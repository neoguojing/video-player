package rtp

import (
	"videoplayer/joy4/av"
	"videoplayer/joy4/codec"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/log"

	"github.com/nareix/bits/pio"
)

const (
	MPEGVIDEO_SEQ_START_CODE = 0x000001b3
	MPEGVIDEO_EXT_START_CODE = 0x000001b5
	MPEGVIDEO_SEQ_END_CODE   = 0x000001b7
)

type MPEG12DynamicProtocol struct {
	hasCodecData bool
	codecData    av.VideoCodecData
}

func (h *MPEG12DynamicProtocol) Type() av.CodecType {
	if h.hasCodecData {
		return h.codecData.Type()
	}
	return av.VIDEO_UNKNOWN
}

func (h *MPEG12DynamicProtocol) ParseSDP(sdp *sdp.Media) error {
	return nil
}

func (h *MPEG12DynamicProtocol) DefaultTimeScale() int {
	return 90000
}

func codecFromSequenceHeader(buf []byte) av.VideoCodecData {
	raw := buf
	if len(buf) < 12 {
		return nil
	}

	// Sequence header
	// http://dvd.sourceforge.net/dvdinfo/mpeghdrs.html#seq
	n := 0
	startcode := pio.U32BE(buf)
	n += 4
	if startcode != MPEGVIDEO_SEQ_START_CODE {
		return nil
	}
	t := pio.U32BE(buf[n:])
	n += 4
	width := t >> 20
	height := (t >> 8) & 0xfff

	t = pio.U32BE(buf[n:])
	n += 4
	if n&1 != 0 || n&2 != 0 {
		if len(buf) < n+64 {
			log.Log(log.ERROR, "MPEG1/2 sequence header too short")
			return nil
		}
	}
	//  64 byte table
	n += 64

	buf = buf[n:]
	// extension header

	ty := av.MPEG1
	if len(buf) > 4 {
		startcode = pio.U32BE(buf)
		if startcode == MPEGVIDEO_EXT_START_CODE {
			ty = av.MPEG2
		}
	}

	extra := make([]byte, len(raw))
	copy(extra, raw)
	return codec.NewFFMPEGVideoCodecData(ty, int(width), int(height), extra)
}

func (h *MPEG12DynamicProtocol) ParsePacket(pkt *av.Packet, buf []byte, timestamp uint32, flags int) (uint32, int) {
	if len(buf) <= 4 {
		return timestamp, -1
	}

	//  0                   1                   2                   3
	//  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
	// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	// |    MBZ  |T|         TR        | |N|S|B|E|  P  | | BFC | | FFC |
	// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	//                                 AN              FBV     FFV

	t := pio.U32BE(buf)
	buf = buf[4:]
	if t&(1<<26) != 0 {
		if len(buf) <= 4 {
			return timestamp, -1
		}
		buf = buf[4:]
		// h.ty = av.MPEG2
	} else {
		// h.ty = av.MPEG1
	}

	if !h.hasCodecData {
		c := codecFromSequenceHeader(buf)
		if c != nil {
			h.codecData = c
			log.Log(log.DEBUG, "MPEG codec: ", h.codecData.Type(), h.codecData.Width(), h.codecData.Height())
			h.hasCodecData = true
		}
	}
	pkt.MediaCodecType = h.Type()
	pkt.Data = make([]byte, len(buf))
	copy(pkt.Data, buf)
	return timestamp, 0
}

func (h *MPEG12DynamicProtocol) PayloadType() int {
	return 32
}

func (h *MPEG12DynamicProtocol) CodecData() av.CodecData {
	if h.hasCodecData {
		return h.codecData
	}
	return nil
}

func (h *MPEG12DynamicProtocol) SDPLines() []string {
	return nil
}
