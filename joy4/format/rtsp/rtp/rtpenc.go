package rtp

import (
	"math/rand"
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec/aacparser"
	"videoplayer/joy4/codec/h264parser"
	"videoplayer/joy4/codec/h265parser"
	"videoplayer/joy4/codec/mjpegparser"
	"videoplayer/joy4/log"
)

type RTPMuxContext struct {
	SSRC          uint32
	BaseTimestamp uint32

	Seq              uint16
	Timestamp        uint32
	CurTimestamp     uint32
	LastRtcpNtpTime  int64
	FirstRtcpNtpTime int64

	FirstPacket bool
	PacketCount int
	OctetCount  int

	Protocol DynamicProtocol

	TimeBase int
}

func supportedProtocolByCodecData(v av.CodecData) DynamicProtocol {
	switch v.Type() {
	case av.H264:
		codec := v.(h264parser.CodecData)
		return NewH264DynamicProtocol(&codec)
	case av.H265:
		codec := v.(h265parser.CodecData)
		return NewH265DynamicProtocol(&codec)
	case av.MJPEG:
		return &JPEGDynamicProtocol{
			hasCodecData: true,
			codecData:    v.(mjpegparser.CodecData),
		}
	case av.AAC:
		return &AACDynamicProtocol{
			codecData: v.(aacparser.CodecData),
		}
	case av.PCM_ALAW:
		return &PCMADynamicProtocol{
			codecData: v.(av.AudioCodecData),
		}
	case av.PCM_MULAW:
		return &PCMMDynamicProtocol{
			codecData: v.(av.AudioCodecData),
		}
	default:
		log.Log(log.WARN, "unsupported rtpenc type: ", v.Type())
		return nil
	}

}

func getTimeBase(h DynamicProtocol) int {
	switch ty := h.(type) {
	case *AACDynamicProtocol, *PCMADynamicProtocol, *PCMMDynamicProtocol:
		return ty.CodecData().(av.AudioCodecData).SampleRate()
	default:
		return ty.DefaultTimeScale()
	}
}

const (
	NTP_OFFSET    = 2208988800
	NTP_OFFSET_US = NTP_OFFSET * 1000000
)

// microseconds
func NtpTime() int64 {
	return (time.Now().UnixNano()/1000000)*1000 + NTP_OFFSET_US
}

func NewRTPMuxContextFromCodecData(v av.CodecData) *RTPMuxContext {
	p := supportedProtocolByCodecData(v)
	if p == nil {
		return nil
	}
	r := &RTPMuxContext{
		Seq:           uint16(rand.Int() % 65536),
		SSRC:          rand.Uint32(),
		BaseTimestamp: uint32(rand.Int31()),
		FirstPacket:   true,

		Protocol: p,

		TimeBase: getTimeBase(p),
	}

	r.Timestamp = r.BaseTimestamp
	r.FirstRtcpNtpTime = NtpTime()
	return r
}
