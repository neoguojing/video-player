package format

import (
	"videoplayer/joy4/av/avutil"
	"videoplayer/joy4/format/aac"
	"videoplayer/joy4/format/flv"
	"videoplayer/joy4/format/mp4"
	"videoplayer/joy4/format/rtmp"
	"videoplayer/joy4/format/rtsp"
	"videoplayer/joy4/format/ts"
)

func RegisterAll() {
	avutil.DefaultHandlers.Add(mp4.Handler)
	avutil.DefaultHandlers.Add(ts.Handler)
	avutil.DefaultHandlers.Add(rtmp.Handler)
	avutil.DefaultHandlers.Add(rtsp.Handler)
	avutil.DefaultHandlers.Add(flv.Handler)
	avutil.DefaultHandlers.Add(aac.Handler)
}
