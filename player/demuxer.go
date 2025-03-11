package player

import (
	"bytes"
	"time"
	config "videoplayer/config"
	"videoplayer/ffmpeg"
	"videoplayer/pb"
	"videoplayer/rtsp"
	"videoplayer/transport"

	"videoplayer/joy4/av"

	"videoplayer/joy4/codec/aacparser"
	"videoplayer/joy4/codec/h264parser"
	jrtsp "videoplayer/joy4/format/rtsp"
	"videoplayer/joy4/format/rtsp/sdp"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

func pts(pkt av.Packet) float64 {
	// miliseconds
	return (pkt.Time + pkt.CompositionTime).Seconds() * 1000
}

type Demuxer struct {
	id             string
	stopChan       chan struct{}
	ws             *transport.WebSocketProxy
	client         *rtsp.Client
	joyClient      *jrtsp.Client
	sdpInfo        sdp.SDPInfo
	preCodecBuffer *bytes.Buffer

	videoIdx   int
	videoMedia sdp.Media

	audioIdx       int
	audioCodecData aacparser.CodecData
	adtsPrefix     []byte

	frameChan chan frameData
	// 上报错误信息
	stateChan       chan State
	statistics      map[int]int64
	decoder         *ffmpeg.VideoDecoder
	lastPreviewInfo []*pb.PreviewInfo

	UseOpenCV bool
	IsCuda    bool
}

func NewDemuxer(wsurl, rtspurl string, frameChan chan frameData, stateChan chan State, id string) (*Demuxer, error) {
	var err error
	var ws *transport.WebSocketProxy
	var rtspClient *rtsp.Client
	var joyClient *jrtsp.Client
	if wsurl != "" {
		ws, err = transport.NewWebSocketProxy(wsurl, rtspurl)
		if err != nil {
			return nil, err
		}
		rtspClient, err = rtsp.NewClient(rtspurl, ws)
		if err != nil {
			return nil, err
		}
	} else {
		joyClient, err = jrtsp.DialTimeout(rtspurl, time.Second*10)

		if err != nil {
			log.Errorf("dial rtsp err: %v", err)
			return nil, err
		}

		joyClient.UseUDP = false
	}

	d := &Demuxer{
		id:             id,
		ws:             ws,
		client:         rtspClient,
		joyClient:      joyClient,
		preCodecBuffer: &bytes.Buffer{},
		adtsPrefix:     make([]byte, 7),
		frameChan:      frameChan,
		stateChan:      stateChan,
		statistics:     make(map[int]int64),
		stopChan:       make(chan struct{}),

		UseOpenCV: config.GlobalConfig.UseOpenCV,
	}
	return d, nil
}

func (d *Demuxer) reportMediaInfo() {
	mediaInfo := map[string]interface{}{}
	if d.videoIdx != -1 {
		media := d.sdpInfo.Medias[d.videoIdx]
		ct := 0
		if media.Type == av.H265 {
			ct = 1
		}
		mediaInfo["video"] = map[string]interface{}{
			"codec": ct,
		}
		d.videoMedia = media
	}
	if d.audioIdx != -1 {
		mediaInfo["audio"] = map[string]interface{}{}
		d.audioCodecData = d.sdpInfo.CodecDatas[d.audioIdx].(aacparser.CodecData)
	}
}

func (d *Demuxer) getSdp() (sdp sdp.SDPInfo, err error) {
	if d.joyClient != nil {
		sdp, err = d.joyClient.SDP()
		return
	}
	sdp, err = d.client.SDP()

	return
}

func (d *Demuxer) Start() error {
	var err error
	if d.ws != nil {
		err = d.ws.Connect()
		if err != nil {
			log.Errorf("ws connnet failed: %v", err)
			return err
		}
	}

	sdpInfo, err := d.getSdp()
	if err != nil {
		log.Errorf("getSdp failed: %v", err)
		return err
	}
	d.sdpInfo = sdpInfo
	d.videoIdx, d.audioIdx = -1, -1
	for i, m := range sdpInfo.Medias {
		log.Debugf("mediaType:%v", m.Type)
		switch m.Type {
		case av.H264, av.H265:
			d.videoIdx = i
		case av.AAC:
			d.audioIdx = i
		}
	}

	d.decoder, err = ffmpeg.NewVideoDecoder(sdpInfo.CodecDatas[d.videoIdx])
	if err != nil {
		log.Fatalf("ffmpeg.NewVideoDecoder failed, err: %v", err)
	}
	d.IsCuda = d.decoder.Mode != ffmpeg.DecodeModeCPU

	d.reportMediaInfo()
	go d.run()
	return nil
}

func (d *Demuxer) dealWithAudioPacket(pkt av.Packet) {
	buffer := d.preCodecBuffer
	data := pkt.Data
	aacparser.FillADTSHeader(d.adtsPrefix, d.audioCodecData.Config, 1, len(data))
	buffer.Write(d.adtsPrefix)
	buffer.Write(data)
	buffer.Reset()
}

func (d *Demuxer) sendPacket(pkt av.Packet, buffer *bytes.Buffer, pktRecieveTime time.Time) {
	var err error
	codec := d.videoMedia.Type
	if codec != av.H264 {
		return
	}

	nalus := h264parser.SplitNALUs(pkt.Data, true, 4, codec, true)

	var seiPayLoad []byte
	var previewInfos []*pb.PreviewInfo
	for _, nalu := range nalus {
		if _, ok := d.statistics[nalu.Type]; !ok {
			d.statistics[nalu.Type] = 1
		} else {
			d.statistics[nalu.Type]++
		}

		if nalu.Type == h264parser.NALU_IDR_SLICE {
			log.Debugf("***********IDR************** nalus.len: %v, pkt.IsKeyFram: %v, pkt.Data.len: %v",
				len(nalus), pkt.IsKeyFrame, len(pkt.Data))
		}
		seiPayLoad = d.dealWithNalu(pkt, nalu)
		if len(seiPayLoad) > 16 {
			previewInfo := &pb.PreviewInfo{
				Timestamp: int64(pkt.Time),
			}
			err = proto.Unmarshal(seiPayLoad[16:], previewInfo)
			if err != nil {
				log.Errorf("pb解码失败:%v", err)
				continue
			}
			previewInfos = append(previewInfos, previewInfo)
			seiPayLoad = nil
		}
		nalu.Raw = nil
		nalu.Rbsp = nil
	}

	if len(nalus) > 1 {
		log.Debugf("av.Packet nalu num %d，sei nalu num：%d", len(nalus), len(previewInfos))
	}

	if len(previewInfos) > 0 {
		d.lastPreviewInfo = previewInfos
	}

	var videoFrame *ffmpeg.VideoFrame
	if nalus[0].Type == h264parser.NALU_NON_IDR_SLICE || nalus[0].Type == h264parser.NALU_IDR_SLICE {
		videoFrame, err = d.Decode(pkt.Data, int64(pkt.Time), pktRecieveTime)
		if err != nil {
			log.Errorf("Decode failed: %v", err)
		}
	}

	if videoFrame != nil {
		if d.UseOpenCV {
			elapsed := time.Since(pktRecieveTime)
			if elapsed < 35*time.Millisecond {
				time.Sleep(35*time.Millisecond - elapsed)
			}
		}

		d.frameChan <- frameData{
			frame:       videoFrame,
			id:          d.id,
			sei:         d.lastPreviewInfo,
			receiveTime: pktRecieveTime,
		}
	}

}

func (d *Demuxer) dealWithNalu(pkt av.Packet, nalu h264parser.H2645NAL) []byte {
	var isSEI bool
	var codec string
	isSEI = (nalu.Type == h264parser.NALU_SEI)

	switch d.videoMedia.Type {
	case av.H264:
		isSEI = (nalu.Type == h264parser.NALU_SEI)
		codec = "h264"
	case av.H265:
		// isSEI = (nalu.Type == h265parser.NALU_PREFIX_SEI_NUT || nalu.Type == h265parser.NALU_SUFFIX_SEI_NUT)
		codec = "h265"
	}
	if isSEI {
		// TODO, HEVC
		sei, err := h264parser.ParseSEIMessageFromNALU(nalu.Rbsp)
		if err != nil {
			log.Errorf("h264parser.ParseSEIMessageFromNALU failed, err: %v", err)
		}
		scaledPts := pts(pkt)
		log.Debugf("sei: %v", map[string]interface{}{
			"codec":    codec,
			"payload":  len(sei.Payload),
			"type":     "sei",
			"subtype":  sei.Type,
			"scalePts": scaledPts,
		})
		return sei.Payload
	}

	return nil
}

func (d *Demuxer) dealWithVideoPacket(pkt av.Packet, pktRecieveTime time.Time) {
	buffer := d.preCodecBuffer
	d.sendPacket(pkt, buffer, pktRecieveTime)
	if pkt.IsKeyFrame {
		log.Debugf("video: %v", map[string]interface{}{
			"timestamp": pts(pkt),
			// "data":      len(buffer.Bytes()),
			"idr": pkt.IsKeyFrame,
		})
	}

	buffer.Reset()
}

func (d *Demuxer) dispatchPacket(pkt av.Packet, pktRecieveTime time.Time) {
	if pkt.Idx < 0 {
		return
	}
	switch int(pkt.Idx) {
	case d.audioIdx:
		d.dealWithAudioPacket(pkt)
	case d.videoIdx:
		d.dealWithVideoPacket(pkt, pktRecieveTime)
	default:
		//skip
	}
}

func (d *Demuxer) ReadPacket() (pkt av.Packet, err error) {
	if d.joyClient != nil {
		return d.joyClient.ReadPacket()
	}

	return d.client.ReadPacket()
}

func (d *Demuxer) Release() {
	// 可能会NPE
	defer func() {
		if p := recover(); p != nil {
			log.Errorf("Release got err: %v", p)
		}
	}()
	close(d.stopChan)
	if d.client != nil {
		err := d.client.Teardown()
		if err != nil {
			log.Errorf("TEARDOWN failed: %v", err)
		}
	}
	if d.ws != nil {
		d.ws.Disconnect()
	}

	if d.joyClient != nil {
		err := d.joyClient.Teardown()
		if err != nil {
			log.Errorf("TEARDOWN failed: %v", err)
		}
	}
	if d.decoder != nil {
		d.decoder.Destroy()
		d.decoder = nil
	}
}

func (d *Demuxer) run() {
	// 创建一个接收信号的通道
	for {
		select {
		case <-d.stopChan:
			log.Debugf("receive stop singal. quit")
			return
		default:
			pkt, err := d.ReadPacket()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Errorf("websocet disconnected: %v", err)
				}
				log.Errorf("ReadPacket got error, needs to stop, err:%v", err)
				d.stateChan <- State{
					windowID: d.id,
					err:      err,
				}
				return
			}
			start := time.Now()
			d.dispatchPacket(pkt, start)

			cost := time.Since(start)
			log.Debugf("dispatchPacket cost:%v,pkt.Time:%v", cost, pkt.Time)
		}
	}
}
