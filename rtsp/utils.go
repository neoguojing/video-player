package rtsp

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"

	"videoplayer/joy4/format/rtsp/sdp"
)

const (
	stageOptionsDone = iota + 1
	stageDescribeDone
	stageSetupDone
	stageWaitCodecData
	stageCodecDataDone
)

func md5hash(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func buildStreamUri(requestUri string, control string) (uri string, err error) {

	var u *url.URL
	if u, err = url.Parse(requestUri); nil != err {
		return
	}

	u.Path = u.Path + "/" + control

	uri = u.String()

	return
}

func isSupportedMedia(media *sdp.Media) bool {
	if media.AVType != "audio" && media.AVType != "video" {
		return false
	}
	pt := media.PayloadType
	if pt >= 96 && pt <= 127 {
		pt = 96
	}
	// unassigned
	if pt >= 35 && pt <= 71 {
		pt = 35
	}
	switch pt {
	case 0: // PCMU
		return true
	case 8: // PCMA
		return true
	case 14: // MP3
		return true
	case 26: // MJPEG
		return true
	case 32: // MPEG1/2
		return true
	case 35, 96: // dynamic
		return media.Type.IsValid()
	default:
		return false
	}
}
