package rtsp

import (
	"strings"

	"videoplayer/joy4/format/rtsp/sdp"
)

type buggyChecker struct {
	MustKeepaliveGetParameter bool
}

func (b *buggyChecker) CheckSDP(sdp *sdp.SDPInfo) {
	if sdp == nil {
		return
	}

	onvifTypeMedia := false

	for _, e := range sdp.ExtraLines["a"] {
		if strings.HasPrefix(e, "tool:LIVE555") {
			b.MustKeepaliveGetParameter = true
			return
		}

		if strings.HasPrefix(e, "h264-esid:201") {
			onvifTypeMedia = true
		}
	}

	if onvifTypeMedia {
		for _, e := range sdp.ExtraLines["s"] {
			if strings.HasPrefix(e, "Media Presentation") {
				b.MustKeepaliveGetParameter = true
				return
			}
		}
	}
}

func (b *buggyChecker) CheckOPTIONS(res *Response) {
}
