package tools

import (
	"io"
	"time"

	log "github.com/sirupsen/logrus"

	"videoplayer/joy4/av"

	"videoplayer/joy4/codec/h264parser"

	"videoplayer/joy4/format/rtsp"
)

var (
	startCode = []byte{0x00, 0x00, 0x00, 0x01}
)

func rtsp_client() {
	// demuxer, err := NewDemuxer("ws://10.9.244.166:30080/rtsp-over-ws", "rtsp://10.9.244.166:8554/fpach_1080p_osd.mp4")
	// if err != nil {
	// 	log.Fatal(err)
	// }

	src, err := rtsp.DialTimeout("rtsp://10.9.244.166:8554/fpach_1080p_osd.mp4", time.Second*10)

	if nil != err {
		log.Debug("dial rtsp err:", err)
		return
	}

	src.UseUDP = false

	defer func() {

		if err := src.Close(); nil != err {
			log.Debug(" src close err:", err)
		}

	}()

	sdp, err := src.Streams()
	if nil != err {
		return
	}

	var count int
	go func() {
		for {
			var pkt av.Packet
			if pkt, err = src.ReadPacket(); err != nil {
				if err == io.EOF {
					break
				}
				continue
			}
			count++

			if pkt.MediaCodecType == av.H264 {
				parsedDataChan <- ParsedData{
					// NALU: buffer.Bytes(),
					Frame: pkt.Data,
					// SEI:  seiPayLoad,
				}
			}

		}
	}()

	for index, data := range sdp {
		log.Debug("sdp:", index, data.Type(), data)

		switch codec := data.(type) {
		case h264parser.CodecData:

			log.Printf("h264 sps(%#v), pps(%#v)", codec.SPS(), codec.PPS())
			playRTSPWithOverlay1(&data)
		}
	}

}
