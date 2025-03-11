package rtp

import (
	"encoding/base64"
	"fmt"
	"strconv"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec/h265parser"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/log"

	"github.com/nareix/bits/pio"
)

const (
	RTP_HEVC_PAYLOAD_HEADER_SIZE       = 2
	RTP_HEVC_FU_HEADER_SIZE            = 1
	RTP_HEVC_DONL_FIELD_SIZE           = 2
	RTP_HEVC_DOND_FIELD_SIZE           = 1
	RTP_HEVC_AP_NALU_LENGTH_FIELD_SIZE = 2
	HEVC_SPECIFIED_NAL_UNIT_TYPES      = 48
)

type H265DynamicProtocol struct {
	codecData      h265parser.CodecData
	hasCodecData   bool
	profileID      int
	usingDonlField bool

	vps []byte
	sps []byte
	pps []byte
	sei []byte

	fuStarted bool
	fuBuffer  []byte

	lastNalType byte
}

func (h *H265DynamicProtocol) Type() av.CodecType {
	return av.H265
}

func (h *H265DynamicProtocol) DefaultTimeScale() int {
	return 90000
}

func (h *H265DynamicProtocol) resetFUState() {
	h.fuStarted = false
	h.fuBuffer = h.fuBuffer[:0]
}

func (h *H265DynamicProtocol) parseFUPacket(pkt *av.Packet, buf []byte) int {
	// fmt.Println("XX ", hex.Dump(buf))
	rtpPL := buf
	buf = buf[RTP_HEVC_PAYLOAD_HEADER_SIZE:]
	/*
	 *    decode the FU header
	 *
	 *     0 1 2 3 4 5 6 7
	 *    +-+-+-+-+-+-+-+-+
	 *    |S|E|  FuType   |
	 *    +---------------+
	 *
	 *       Start fragment (S): 1 bit
	 *       End fragment (E): 1 bit
	 *       FuType: 6 bits
	 */
	isStart := buf[0]&0x80 != 0
	isEnd := buf[0]&0x40 != 0
	fuType := buf[0] & 0x3f

	if len(buf) < RTP_HEVC_FU_HEADER_SIZE {
		log.Log(log.WARN, "HEVC FU packet too small")
		return -1
	}

	buf = buf[RTP_HEVC_FU_HEADER_SIZE:]

	if h.usingDonlField {
		if len(buf) < RTP_HEVC_DONL_FIELD_SIZE {
			log.Log(log.WARN, "HEVC DONL packet too small")
			return -1
		}
		buf = buf[RTP_HEVC_DONL_FIELD_SIZE:]
	}

	log.Logf(log.TRACE, " FU type %d with %d bytes", fuType, len(buf))

	if isStart {
		h.fuStarted = true
		h.fuBuffer = append(h.fuBuffer, []byte{0, 0, 0, 0, (rtpPL[0] & 0x81) | (fuType << 1), rtpPL[1]}...)
	}
	if h.fuStarted {
		h.fuBuffer = append(h.fuBuffer, buf...)
		if isEnd {
			if len(h.fuBuffer) > 4 {
				nalType := (h.fuBuffer[4] >> 1) & 0x3f
				pkt.FrameType = nalType
				pkt.IsKeyFrame = isHEVCKeyframe(nalType)
			}
			// TODO set key frame
			pkt.Data = make([]byte, len(h.fuBuffer))
			copy(pkt.Data, h.fuBuffer)

			pio.PutU32BE(pkt.Data[0:4], uint32(len(h.fuBuffer)-4))
			h.handleVPSSPSPPS(pkt.Data[4:])
			h.resetFUState()
			return 0
		}
	}
	if len(h.fuBuffer) > MaxFUBufferSize {
		log.Log(log.WARN, "h.fuBuffer is too long, len:", len(h.fuBuffer))
		h.fuBuffer = make([]byte, 0, AllocFUBufferSize)
		h.resetFUState()
	}

	return -1
}

func isHEVCKeyframe(nalType byte) bool {
	return nalType == h265parser.NALU_IDR_W_RADL || nalType == h265parser.NALU_IDR_N_LP
}

func (h *H265DynamicProtocol) handleVPSSPSPPS(buf []byte) {
	if len(buf) == 0 {
		return
	}
	nalType := (buf[0] >> 1) & 0x3f
	// log.Log(log.TRACE, "h265 nal: ", nalType)
	switch nalType {
	case h265parser.NALU_VPS_NUT:
		h.vps = make([]byte, len(buf))
		copy(h.vps, buf)
		h.setCodecData()
	case h265parser.NALU_SPS_NUT:
		h.sps = make([]byte, len(buf))
		copy(h.sps, buf)
		h.setCodecData()
	case h265parser.NALU_PPS_NUT:
		h.pps = make([]byte, len(buf))
		copy(h.pps, buf)
		h.setCodecData()
	}
}

func (h *H265DynamicProtocol) ParsePacket(pkt *av.Packet, buf []byte, timestamp uint32, flags int) (uint32, int) {
	if len(buf) < RTP_HEVC_PAYLOAD_HEADER_SIZE+1 {
		log.Logf(log.ERROR, "Too short RTP/HEVC packet, got %d bytes", len(buf))
		return timestamp, -1
	}
	/*
	 * decode the HEVC payload header according to section 4 of draft version 6:
	 *
	 *    0                   1
	 *    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5
	 *   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	 *   |F|   Type    |  LayerId  | TID |
	 *   +-------------+-----------------+
	 *
	 *      Forbidden zero (F): 1 bit
	 *      NAL unit type (Type): 6 bits
	 *      NUH layer ID (LayerId): 6 bits
	 *      NUH temporal ID plus 1 (TID): 3 bits
	 */
	nalType := (buf[0] >> 1) & 0x3f
	lid := ((buf[0] << 5) & 0x20) | ((buf[1] >> 3) & 0x1f)
	tid := buf[1] & 0x07

	if nalType != h.lastNalType {
		h.resetFUState()
	}
	h.lastNalType = nalType
	// log.Log(log.INFO, "XX ", nalType)

	/* sanity check for correct layer ID */
	if lid != 0 {
		log.Log(log.WARN, "missing feature: Multi-layer HEVC coding")
		return timestamp, -2
	}

	/* sanity check for correct temporal ID */
	if tid == 0 {
		log.Log(log.WARN, "Illegal temporal ID in RTP/HEVC packet")
		return timestamp, -1
	}

	/* sanity check for correct NAL unit type */
	if nalType > 50 {
		log.Log(log.WARN, "Unsupported (HEVC) NAL type (%d)", nalType)
		return timestamp, -1
	}
	pkt.MediaCodecType = av.H265

	switch nalType {
	/* aggregated packet (AP) - with two or more NAL units */
	case 48:
		buf = buf[RTP_HEVC_PAYLOAD_HEADER_SIZE:]
		skipSize := 0
		if h.usingDonlField {
			if len(buf) < RTP_HEVC_DONL_FIELD_SIZE {
				log.Log(log.WARN, "HEVC DONL packet too small")
				return timestamp, -1
			}
			buf = buf[RTP_HEVC_DONL_FIELD_SIZE:]
			skipSize = RTP_HEVC_DOND_FIELD_SIZE
		}
		rv := h264ParseAggregatedPacket(pkt, buf, skipSize, 0, h.handleVPSSPSPPS)
		return timestamp, rv
		/* fragmentation unit (FU) */
	case 49:
		/* pass the HEVC payload header */
		rv := h.parseFUPacket(pkt, buf)
		return timestamp, rv
	/* PACI packet */
	case 50:
		/* Temporal scalability control information (TSCI) */
		log.Log(log.WARN, "missing feature: PACI packets for RTP/HEVC")
		return timestamp, -2
	/* video parameter set (VPS) */
	case 32:
		fallthrough
	/* sequence parameter set (SPS) */
	case 33:
		fallthrough
	/* picture parameter set (PPS) */
	case 34:
		fallthrough
	/*  supplemental enhancement information (SEI) */
	case 39:
		fallthrough
	/* single NAL unit packet */
	default:
		/* create A/V packet */
		h.handleVPSSPSPPS(buf)
		pkt.FrameType = nalType
		pkt.IsKeyFrame = isHEVCKeyframe(nalType)
		pkt.Data = make([]byte, 4+len(buf))
		pio.PutU32BE(pkt.Data, uint32(len(buf)))
		copy(pkt.Data[4:], buf)
		return timestamp, 0
	}

	return timestamp, -2
}

func (h *H265DynamicProtocol) setCodecData() {
	if len(h.sps) > 0 && len(h.pps) > 0 && len(h.vps) > 0 {
		d, err := h265parser.NewCodecDataFromPS(h.vps, h.sps, h.pps)
		if err == nil {
			h.codecData = d
			h.hasCodecData = true
		} else {
			log.Log(log.ERROR, "bad codec data: ", err)
		}
	}
}

func (h *H265DynamicProtocol) ParseSDP(sdp *sdp.Media) error {
	for k, v := range sdp.ALines {
		if k == "sprop-sps" {
			val, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				continue
			}
			h.sps = val
		} else if k == "sprop-vps" {
			val, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				continue
			}
			h.vps = val
		} else if k == "sprop-pps" {
			val, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				continue
			}
			h.pps = val
		} else if k == "sprop-sei" {
			val, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				continue
			}
			h.sei = val
		} else if k == "sprop-max-don-diff" || k == "sprop-max-don-diff" {
			/* sprop-max-don-diff: 0-32767
			   When the RTP stream depends on one or more other RTP
			   streams (in this case tx-mode MUST be equal to "MSM" and
			   MSM is in use), this parameter MUST be present and the
			   value MUST be greater than 0.

			   sprop-depack-buf-nalus: 0-32767
			*/

			t, _ := strconv.Atoi(v)
			if t > 0 {
				h.usingDonlField = true
				log.Logf(log.DEBUG, "Found %s in SDP, DON field usage is: %d", k, h.usingDonlField)
			}
		}
	}

	h.setCodecData()

	if !h.hasCodecData {
		log.Log(log.WARN, "H265 PS not available in SDP")
	}

	return nil
}

func (h *H265DynamicProtocol) CodecData() av.CodecData {
	if !h.hasCodecData {
		return nil
	}
	return h.codecData
}

func (h *H265DynamicProtocol) PayloadType() int {
	return 96
}

func (h *H265DynamicProtocol) SDPLines() []string {
	vps := base64.StdEncoding.EncodeToString(h.codecData.VPS())
	sps := base64.StdEncoding.EncodeToString(h.codecData.SPS())
	pps := base64.StdEncoding.EncodeToString(h.codecData.PPS())

	rtpmap := fmt.Sprintf("a=rtpmap:%d H265/%d", h.PayloadType(), h.DefaultTimeScale())
	fmtp := fmt.Sprintf("a=fmtp:%d sprop-vps=%s; sprop-sps=%s; sprop-pps=%s", h.PayloadType(), vps, sps, pps)
	return []string{fmtp, rtpmap}
}

func NewH265DynamicProtocol(codecData *h265parser.CodecData) *H265DynamicProtocol {
	if codecData == nil {
		return &H265DynamicProtocol{
			fuBuffer: make([]byte, 0, AllocFUBufferSize),
		}
	}

	return &H265DynamicProtocol{
		hasCodecData: true,
		codecData:    *codecData,
		fuBuffer:     make([]byte, 0, 1024),
	}
}
