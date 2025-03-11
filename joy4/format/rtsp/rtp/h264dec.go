package rtp

import (
	"encoding/base64"
	"fmt"
	"strings"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec/h264parser"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/log"

	"github.com/nareix/bits/pio"
)

type H264DynamicProtocol struct {
	ProfileIdc        uint8
	ProfileIop        uint8
	LevelIdc          uint8
	PacketizationMode int

	sps          []byte
	pps          []byte
	hasCodecData bool
	codecData    h264parser.CodecData

	fuStarted bool
	fuBuffer  []byte

	lastNalType byte
}

var startSequence = []byte{0, 0, 0, 1}

func (h *H264DynamicProtocol) Type() av.CodecType {
	return av.H264
}

const NAL_MASK = 0x1f
const AllocFUBufferSize = 1024 * 1024
const MaxFUBufferSize = 4 * AllocFUBufferSize

func (h *H264DynamicProtocol) parseFragPacket(pkt *av.Packet, buf []byte, startBit byte, nalHeader []byte) {
	totLen := len(buf)
	pos := 0
	if startBit != 0 {
		totLen += 4 + len(nalHeader)
	}
	pkt.Data = make([]byte, totLen)
	if startBit != 0 {
		copy(pkt.Data, startSequence)
		pos += len(startSequence)
		copy(pkt.Data[pos:], nalHeader)
		pos += len(nalHeader)
	}
	copy(pkt.Data[pos:], buf)
}

func (h *H264DynamicProtocol) resetFUState() {
	h.fuStarted = false
	h.fuBuffer = h.fuBuffer[:0]
}

func (h *H264DynamicProtocol) setCodecData() {
	if len(h.sps) > 0 && len(h.pps) > 0 {
		d, err := h264parser.NewCodecDataFromSPSAndPPS(h.sps, h.pps)
		if err == nil {
			h.codecData = d
			h.hasCodecData = true
		} else {
			log.Log(log.ERROR, "rtp: bad h264 codec data: ", err)
		}
	}
}

/*
	0                   1                   2                   3
	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                          RTP Header                           |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|STAP-A NAL HDR |         NALU 1 Size           | NALU 1 HDR    |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                         NALU 1 Data                           |
	:                                                               :
	+               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|               | NALU 2 Size                   | NALU 2 HDR    |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                         NALU 2 Data                           |
	:                                                               :
	|                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                               :...OPTIONAL RTP padding        |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

	Figure 7.  An example of an RTP packet including an STAP-A
	containing two single-time aggregation units
*/

func h264ParseAggregatedPacket(pkt *av.Packet, buf []byte, skipBetween int, nalMask int, h func([]byte)) int {
	count := 0
	for len(buf) >= 2 {
		size := int(pio.U16BE(buf))
		if size <= 0 {
			break
		}
		if size+2 > len(buf) {
			break
		}
		offset := len(pkt.Data)
		data := buf[2 : size+2]
		pkt.Data = append(pkt.Data, []byte{0, 0, 0, 0}...)
		pkt.Data = append(pkt.Data, data...)
		pio.PutU32BE(pkt.Data[offset:], uint32(size))
		if h != nil {
			// h.handleSPSPPS(data)
			h(data)
		}
		pkt.FrameType = data[0] & 0x1f
		buf = buf[size+2:]
		count++
	}
	if count == 0 {
		return -1
	}
	return 0
}

func (h *H264DynamicProtocol) parseFUAPacket(pkt *av.Packet, buf []byte, nalMask int) int {
	/*
		0                   1                   2                   3
		0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		| FU indicator  |   FU header   |                               |
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               |
		|                                                               |
		|                         FU payload                            |
		|                                                               |
		|                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		|                               :...OPTIONAL RTP padding        |
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		Figure 14.  RTP payload format for FU-A

		The FU indicator octet has the following format:
		+---------------+
		|0|1|2|3|4|5|6|7|
		+-+-+-+-+-+-+-+-+
		|F|NRI|  Type   |
		+---------------+


		The FU header has the following format:
		+---------------+
		|0|1|2|3|4|5|6|7|
		+-+-+-+-+-+-+-+-+
		|S|E|R|  Type   |
		+---------------+

		S: 1 bit
		When set to one, the Start bit indicates the start of a fragmented
		NAL unit.  When the following FU payload is not the start of a
		fragmented NAL unit payload, the Start bit is set to zero.

		E: 1 bit
		When set to one, the End bit indicates the end of a fragmented NAL
		unit, i.e., the last byte of the payload is also the last byte of
		the fragmented NAL unit.  When the following FU payload is not the
		last fragment of a fragmented NAL unit, the End bit is set to
		zero.

		R: 1 bit
		The Reserved bit MUST be equal to 0 and MUST be ignored by the
		receiver.

		Type: 5 bits
		The NAL unit payload type as defined in table 7-1 of [1].
	*/

	if len(buf) < 3 {
		log.Log(log.ERROR, "Too short data for FU-A/B H.264 RTP packet")
		return -1
	}

	fuIndicator := buf[0]
	fuHeader := buf[1]
	isStart := fuHeader&0x80 != 0
	isEnd := fuHeader&0x40 != 0

	naltype := fuHeader & 0x1f
	nal := fuIndicator&0xe0 | naltype

	isFUB := naltype == 29
	if isFUB && len(buf) < 5 {
		log.Log(log.ERROR, "Too short data for FU-B H.264 RTP packet")
		return -1
	}

	if isStart {
		h.fuStarted = true
		h.fuBuffer = append(h.fuBuffer, []byte{0, 0, 0, 0, nal}...)
	}
	if h.fuStarted {
		// ignoring FU-indicator & FU-header
		payloadStart := 2
		if isFUB {
			// XXX ignoring 2-byte DON
			payloadStart += 2
		}

		h.fuBuffer = append(h.fuBuffer, buf[payloadStart:]...)
		if isEnd {
			if len(h.fuBuffer) > 4 {
				pkt.IsKeyFrame = h.fuBuffer[4]&0x1f == h264parser.NALU_IDR_SLICE
				pkt.FrameType = h.fuBuffer[4] & 0x1f
			}

			pkt.Data = make([]byte, len(h.fuBuffer))
			copy(pkt.Data, h.fuBuffer)

			pio.PutU32BE(pkt.Data[0:4], uint32(len(h.fuBuffer)-4))
			h.handleSPSPPS(pkt.Data[4:])
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

// return 0 on packet, no more left, 1 on packet, -1 on partial packet
// return AVCC packet
func (h *H264DynamicProtocol) ParsePacket(pkt *av.Packet, buf []byte, timestamp uint32, flags int) (uint32, int) {
	rv := 0
	if len(buf) == 0 {
		return timestamp, -1
	}
	nal := buf[0]
	naltype := nal & 0x1f

	/*
		Table 7-1 – NAL unit type codes
		1   ￼Coded slice of a non-IDR picture
		5    Coded slice of an IDR picture
		6    Supplemental enhancement information (SEI)
		7    Sequence parameter set
		8    Picture parameter set
		1-23     NAL unit  Single NAL unit packet             5.6
		24       STAP-A    Single-time aggregation packet     5.7.1
		25       STAP-B    Single-time aggregation packet     5.7.1
		26       MTAP16    Multi-time aggregation packet      5.7.2
		27       MTAP24    Multi-time aggregation packet      5.7.2
		28       FU-A      Fragmentation unit                 5.8
		29       FU-B      Fragmentation unit                 5.8
		30-31    reserved                                     -
	*/

	if naltype != h.lastNalType {
		h.resetFUState()
	}
	h.lastNalType = naltype
	pkt.MediaCodecType = av.H264
	switch {
	case naltype == 0: // undefined, but pass them through
		fallthrough
	case naltype >= 9 && naltype <= 23:
		fallthrough
	case naltype >= 1 && naltype <= 8:
		h.handleSPSPPS(buf)
		// isSEI := naltype == h264parser.NALU_SEI
		isKeyFrame := naltype == h264parser.NALU_IDR_SLICE
		// avcc mode
		pkt.Data = make([]byte, 4+len(buf))
		pio.PutU32BE(pkt.Data, uint32(len(buf)))
		copy(pkt.Data[4:], buf)
		if isKeyFrame {
			pkt.IsKeyFrame = true
		}
		pkt.FrameType = buf[0] & 0x1f
	case naltype == 24: // STAP-A (one packet, multiple nals)
		buf = buf[1:]
		rv = h264ParseAggregatedPacket(pkt, buf, 0, NAL_MASK, h.handleSPSPPS)
	case naltype == 28: // FU-A (fragmented nal)
		fallthrough
	case naltype == 29: // FU-B (fragmented nal)
		rv = h.parseFUAPacket(pkt, buf, NAL_MASK)
	default:
		log.Log(log.WARN, "rtp: unknown nal type: ", naltype)
		return timestamp, -1
	}

	return timestamp, rv
}

func (h *H264DynamicProtocol) handleSPSPPS(buf []byte) {
	if len(buf) == 0 {
		return
	}
	naluType := buf[0] & 0x1f
	if naluType == h264parser.NALU_SPS {

		//Split and continue to find out if PPS exists, copy if it exists
		startCode := []byte{0x00, 0x00, 0x00, 0x01}
		rawBuf := append(startCode, buf[0:]...) //The initial position is filled with the start code, in order to call the SplitNALUs function.
		nalus := h264parser.SplitNALUs(rawBuf, false, 4, av.H264, true)
		for _, nalu := range nalus {
			if nalu.Type == h264parser.NALU_SPS {
				h.sps = make([]byte, len(nalu.Raw))
				copy(h.sps, nalu.Raw)
			}
			if nalu.Type == h264parser.NALU_PPS {
				h.pps = make([]byte, len(nalu.Raw))
				copy(h.pps, nalu.Raw)
			}
		}

		h.setCodecData()
	} else if naluType == h264parser.NALU_PPS {
		h.pps = make([]byte, len(buf))
		copy(h.pps, buf)
		h.setCodecData()
	}
}

func (h *H264DynamicProtocol) ParseSDP(sdp *sdp.Media) error {
	var sprop [][]byte
	for k, v := range sdp.ALines {
		if k == "sprop-parameter-sets" {
			fields := strings.Split(v, ",")
			for _, field := range fields {
				val, _ := base64.StdEncoding.DecodeString(field)
				sprop = append(sprop, val)

			}
		}
	}
	for _, nalu := range sprop {
		if len(nalu) > 0 {
			h.handleSPSPPS(nalu)
		}
	}

	if len(h.sps) == 0 || len(h.pps) == 0 {
		if nalus, typ := h264parser.GuessNALUType(sdp.Config); typ != h264parser.NALU_RAW {
			for _, nalu := range nalus {
				if len(nalu) > 0 {
					h.handleSPSPPS(nalu)
				}
			}
		}
	}

	if !h.hasCodecData {
		log.Log(log.WARN, "H264 PS not available in SDP")
	}
	return nil
}

func (h *H264DynamicProtocol) DefaultTimeScale() int {
	return 90000
}

func (h *H264DynamicProtocol) CodecData() av.CodecData {
	if !h.hasCodecData {
		return nil
	}
	return h.codecData
}

func (h *H264DynamicProtocol) PayloadType() int {
	return 96
}

func h264ProfileLevelID(info h264parser.SPSInfo) string {
	return fmt.Sprintf("%02x%02x%02x", info.ProfileIdc, info.ConstraintSetFlags, info.LevelIdc)
}

func (h *H264DynamicProtocol) SDPLines() []string {
	profile := h264ProfileLevelID(h.codecData.SPSInfo)
	sps := base64.StdEncoding.EncodeToString(h.codecData.SPS())
	pps := base64.StdEncoding.EncodeToString(h.codecData.PPS())

	rtpmap := fmt.Sprintf("a=rtpmap:%d H264/%d", h.PayloadType(), h.DefaultTimeScale())
	fmtp := fmt.Sprintf("a=fmtp:%d packetization-mode=1; sprop-parameter-sets=%s,%s; profile-level-id=%s", h.PayloadType(), sps, pps, profile)
	return []string{fmtp, rtpmap}
}

func NewH264DynamicProtocol(codecData *h264parser.CodecData) *H264DynamicProtocol {

	// receive h264 stream need
	if codecData == nil {
		return &H264DynamicProtocol{
			fuBuffer: make([]byte, 0, AllocFUBufferSize),
		}
	}

	// send h264 stream need
	return &H264DynamicProtocol{
		hasCodecData: true,
		codecData:    *codecData,
		fuBuffer:     make([]byte, 0, 1024),
	}
}
