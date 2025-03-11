package rtsp

import (
	"fmt"

	"videoplayer/joy4/av"

	"videoplayer/joy4/format/rtsp/rtp"

	"videoplayer/joy4/format/rtsp/sdp"
)

type Stream struct {
	av.CodecData
	Sdp    sdp.Media
	Idx    int
	client *Client

	remoteHost string

	ctx *rtp.RTPDemuxContext
}

func (self *Stream) Close() error {
	return nil
}

func (self *Stream) MakeCodecData(buf []byte) (err error) {
	media := self.Sdp

	if self.ctx == nil {
		self.ctx = rtp.NewRTPDemuxContext(media.PayloadType, 0)
		switch {
		// Unassigned
		case media.PayloadType >= 35 && media.PayloadType <= 71:
			fallthrough
		case media.PayloadType >= 96 && media.PayloadType <= 127:
			if !self.ctx.SetDynamicHandlerByCodecType(media.Type) {
				err = fmt.Errorf("rtp: unsupported codec type: %v", media.Type)
				return
			}
		default:
			if !self.ctx.SetDynamicHandlerByStaticId(int(media.PayloadType)) {
				err = fmt.Errorf("rtsp: PayloadType=%d unsupported", media.PayloadType)
				return
			}
		}
	}

	if buf == nil {
		if err = self.ctx.DynamicProtocol.ParseSDP(&media); err != nil {
			return
		}
	}

	self.ctx.TimeScale = media.TimeScale
	// https://tools.ietf.org/html/rfc5391
	if self.ctx.TimeScale == 0 {
		self.ctx.TimeScale = self.ctx.DynamicProtocol.DefaultTimeScale()
	}
	if self.ctx.TimeScale == 0 {
		self.ctx.TimeScale = 8000
	}

	// TODO handle codec data change
	if self.CodecData == nil {
		d := self.ctx.DynamicProtocol.CodecData()
		if d != nil {
			self.CodecData = d
		}
	}
	if self.CodecData == nil {
		err = fmt.Errorf("rtp: codec data invalid")
		return
	}

	return
}
