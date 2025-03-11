package tools

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"gocv.io/x/gocv"

	"videoplayer/ffmpeg"
	"videoplayer/pb"

	"videoplayer/joy4/av"

	"github.com/gorilla/websocket"
)

func prepare() (http.Header, http.Header) {
	var dataHeader = http.Header{}
	// 设置 Host
	dataHeader.Set("Host", "10.9.244.166:30080")

	// 设置 Connection
	// dataHeader.Set("Connection", "Upgrade")

	// 设置 Pragma
	dataHeader.Set("Pragma", "no-cache")

	// 设置 Cache-Control
	dataHeader.Set("Cache-Control", "no-cache")

	// 设置 User-Agent
	dataHeader.Set("User-Agent", "client")

	// 设置 Upgrade
	// dataHeader.Set("Upgrade", "websocket")

	// 设置 Origin
	dataHeader.Set("Origin", "http://10.9.244.166:9999")

	// 设置 Sec-WebSocket-Version
	// dataHeader.Set("Sec-WebSocket-Version", "13")

	// 设置 Accept-Encoding
	dataHeader.Set("Accept-Encoding", "gzip, deflate")

	// 设置 Accept-Language
	dataHeader.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8,ja;q=0.7")

	// 设置 Sec-WebSocket-Key
	// dataHeader.Set("Sec-WebSocket-Key", "99gEvMc5BpwZFPNxTzhvCA==")

	// 设置 Sec-WebSocket-Extensions
	// dataHeader.Set("Sec-WebSocket-Extensions", "permessage-deflate; client_max_window_bits")

	// 设置 Sec-WebSocket-Protocol
	dataHeader.Set("Sec-WebSocket-Protocol", "data")

	controllHeadler := http.Header{}
	// Set Host
	controllHeadler.Add("Host", "10.9.244.166:30080")

	// Set Connection
	// controllHeadler.Add("Connection", "Upgrade")

	// Set Pragma
	controllHeadler.Add("Pragma", "no-cache")

	// Set Cache-Control
	controllHeadler.Add("Cache-Control", "no-cache")

	// Set User-Agent
	controllHeadler.Add("User-Agent", "client")

	// Set Upgrade
	// controllHeadler.Add("Upgrade", "websocket")

	// Set Origin
	controllHeadler.Add("Origin", "http://10.9.244.166:9999")

	// Set Sec-WebSocket-Version
	// controllHeadler.Add("Sec-WebSocket-Version", "13")

	// Set Accept-Encoding
	controllHeadler.Add("Accept-Encoding", "gzip, deflate")

	// Set Accept-Language
	controllHeadler.Add("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8,ja;q=0.7")

	// Set Sec-WebSocket-Key
	// controllHeadler.Add("Sec-WebSocket-Key", "H5CkKlHtbLcKhEXp5jwNIQ==")

	// Set Sec-WebSocket-Extensions
	// controllHeadler.Add("Sec-WebSocket-Extensions", "permessage-deflate; client_max_window_bits")

	// Set Sec-WebSocket-Protocol
	controllHeadler.Add("Sec-WebSocket-Protocol", "control")

	return dataHeader, controllHeadler
}

func getChannelID(message string) string {
	// 分割字符串为行
	lines := strings.Split(message, "\r\n")
	var channel string
	// 遍历每一行查找channel值
	for _, line := range lines {
		if strings.HasPrefix(line, "channel:") {
			// 提取channel值
			channel = strings.TrimSpace(strings.TrimPrefix(line, "channel:"))
			break
		}
	}

	return channel
}

func getSessionID(message string) string {
	// 分割字符串为行
	lines := strings.Split(message, "\r\n")
	var session string
	// 遍历每一行查找channel值
	for _, line := range lines {
		if strings.HasPrefix(line, "Session:") {
			// 提取channel值
			session = strings.TrimSpace(strings.TrimPrefix(line, "Session:"))
			break
		}
	}

	return session
}

var (
	seq     = 1
	cChan   = make(chan string, 1)
	cSeq    = 3
	rtspURL = "rtsp://10.9.244.166:8554/fpach_1080p_osd.mp4"
)

func main1() {
	// 创建 WebSocket 连接
	u := url.URL{Scheme: "wss", Host: "10.9.19.159:30443", Path: "/engine/rtsp-preview/rtsp-over-ws?jwt=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJDNzJabEZtQ2ZkdzV5N05ySEYxRVpyZmQiLCJuYmYiOjE3MDMyMTY0MDQsImV4cCI6MTcwMzIyMzYwNH0.oVooTC5IeymJ53sVrL4CD7DLReW7CeHAS8vSD95HQeA"}

	dataHeader, controllHeadler := prepare()
	ctlDialer := websocket.DefaultDialer
	ctlDialer.TLSClientConfig.InsecureSkipVerify = true
	ctlConn, _, err := ctlDialer.Dial(u.String(), controllHeadler)
	if err != nil {
		log.Fatal(err)
	}
	defer ctlConn.Close()

	dataDialer := websocket.DefaultDialer
	dataDialer.TLSClientConfig.InsecureSkipVerify = true
	dataConn, _, err := dataDialer.Dial(u.String(), dataHeader)
	if err != nil {
		log.Fatal(err)
	}
	defer dataConn.Close()

	// go handleControlMessages(ctlConn)
	// handleDataMessages(dataConn)
	// 启动处理 WebSocket 消息的协程

	// 播放 RTSP 流并叠加图案和文字
	playRTSPWithOverlay(rtspURL)
}

type ParsedData struct {
	Frame       []byte
	PreviewInfo *pb.PreviewInfo
}

var parsedDataChan = make(chan ParsedData, 32)

// 将 image.YCbCr 转换为 gocv.Mat
func yCbCrToMat(img image.Image) gocv.Mat {
	bounds := img.Bounds()
	mat := gocv.NewMatWithSize(bounds.Dy(), bounds.Dx(), gocv.MatTypeCV8UC3)

	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			ycbcr := img.At(x, y).(color.YCbCr)
			// mat.SetUCharAt(y, x*3, ycbcr.Y)
			// mat.SetUCharAt(y, x*3+1, ycbcr.Cb)
			// mat.SetUCharAt(y, x*3+2, ycbcr.Cr)

			mat.SetUCharAt(y, x*3, ycbcr.Cr)
			mat.SetUCharAt(y, x*3+1, ycbcr.Cb)
			mat.SetUCharAt(y, x*3+2, ycbcr.Y)
		}
	}
	// gocv.CvtColor()
	return mat
}

func ConvertYUV420ToMat(yuvImage image.Image) gocv.Mat {
	bounds := yuvImage.Bounds()
	mat := gocv.NewMatWithSize(bounds.Dy(), bounds.Dx(), gocv.MatTypeCV8UC3)

	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			yuv := yuvImage.At(x, y).(color.YCbCr)

			// Convert YUV to RGB
			r, g, b := color.YCbCrToRGB(yuv.Y, yuv.Cb, yuv.Cr)

			// Set RGB values in the gocv.Mat
			mat.SetUCharAt(y, x*3, r)
			mat.SetUCharAt(y, x*3+1, g)
			mat.SetUCharAt(y, x*3+2, b)
		}
	}

	return mat
}

func playRTSPWithOverlay1(codec *av.CodecData) {
	// 设置Goroutine在主线程上运行
	runtime.LockOSThread()
	// 设置Goroutine在主线程上运行
	defer runtime.UnlockOSThread()
	// 创建窗口
	window := gocv.NewWindow("RTSP Stream")
	defer window.Close()

	// 创建画布
	// canvas := gocv.NewMat()
	// defer canvas.Close()

	decoder, err := ffmpeg.NewVideoDecoder(*codec, true)
	if err != nil {
		log.Fatal(err)
	}

	var lastPreviewInfo *pb.PreviewInfo
	// 循环播放流
	for data := range parsedDataChan {
		if data.PreviewInfo != nil {
			lastPreviewInfo = data.PreviewInfo
			data.PreviewInfo = nil
		}

		if data.Frame == nil {
			continue
		}

		log.Debug("parsedDataChan:", len(parsedDataChan))
		start := time.Now()
		// image, err := decoder.Decode(data.Frame)
		image, err := decoder.Decode1(data.Frame, false)
		data.Frame = nil
		if err != nil {
			log.Debug("error:", err)
			continue
		}

		decodeCost := time.Now().Sub(start)
		log.Debug("decodeCost:", decodeCost)

		if image == nil {
			continue
		}

		// canvas = ConvertYUV420ToMat(&image.Image)
		// convertCost := time.Now().Sub(start)
		// log.Debug("convertCost:", convertCost)
		// 显示画面
		if image.Mat != nil {

			getOverlayImage(lastPreviewInfo, image.Mat)

			window.IMShow(*image.Mat)
			image.Mat.Close()

			key := window.WaitKey(1)
			if key == 27 || key == int('q') {
				close(parsedDataChan)
				break
			}

			showCost := time.Now().Sub(start)
			log.Debug("showCost:", showCost)
		}
		image.Free()
	}
}

func playRTSPWithOverlay(rtspURL string) {
	// 打开 RTSP 流
	stream, err := gocv.OpenVideoCapture(rtspURL)
	if err != nil {
		log.Fatal("Failed to open RTSP stream:", err)
	}
	defer stream.Close()

	// 创建窗口
	window := gocv.NewWindow("RTSP Stream")
	defer window.Close()

	// 创建画布
	canvas := gocv.NewMat()
	defer canvas.Close()

	// 循环播放流
	// for data := range parsedDataChan {
	// if ok := stream.Read(&canvas); !ok {
	// 	log.Debug("Stream ended")
	// 	break
	// }

	// rawData := canvas.ToBytes()
	// log.Debug(canvas.Type().String(), len(rawData))

	// img, err := gocv.IMDecode(data.Frame, gocv.IMReadColor)
	// if err != nil {
	// 	log.Debug("无法解码图像")
	// 	break
	// }

	// // 显示画面
	// window.IMShow(img)
	// if window.WaitKey(1) >= 0 {
	// 	continue
	// }

	// }
}

func decodeRtp(data []byte) []byte {
	data = data[4:]
	// 解析RTP数据包的头部
	// hdr := data[0:12]
	// version := hdr[0] >> 6
	// padding := (hdr[0] & 0x20) >> 5
	// extension := (hdr[0] & 0x10) >> 4
	// csrccount := hdr[0] & 0x0f
	// marker := hdr[1] >> 7
	// payloadtype := hdr[1] & 0x7f
	// seqnum := binary.BigEndian.Uint16(hdr[2:4])
	// timestamp := binary.BigEndian.Uint32(hdr[4:8])
	// ssrc := binary.BigEndian.Uint32(hdr[8:12])

	// 解码RTP数据包的数据部分
	// payload := data[12:]
	// log.Debug(version, padding, extension, csrccount, marker, payloadtype, seqnum, timestamp, ssrc)
	// 解析 RTP 头部字段
	// version := (data[0] & 0xC0) >> 6
	payloadType := data[1] & 0x7F
	// sequenceNumber := binary.BigEndian.Uint16(data[2:4])
	// timestamp := binary.BigEndian.Uint32(data[4:8])
	// ssrc := binary.BigEndian.Uint32(rtpPacket[8:12])

	// 提取有效载荷数据
	payloadData := data[12:]

	log.Debug("payloadType:", payloadType)

	// // 判断是否为 H.264 数据包
	if payloadType == 96 {
		// 过滤 SEI 数据包
		if payloadData[0] == 0x00 && payloadData[1] == 0x00 && payloadData[2] == 0x01 {
			nalUnitType := payloadData[3] & 0x1F
			log.Debug("nalUnitType:", nalUnitType)

			if nalUnitType == 6 {
				// 是 SEI 数据包
				// 继续读取下一个 RTP 数据包
			}
		}

		return payloadData

	} else if payloadType == 98 {
		return nil
	} else if payloadType == 6 {
		return nil
	} else if payloadType == 46 {
		return nil
	}

	return nil
}

func handleDataMessages(conn *websocket.Conn) {
	// 创建窗口
	window := gocv.NewWindow("RTSP Stream")
	defer window.Close()

	// 创建画布
	canvas := gocv.NewMat()
	defer canvas.Close()

	// 	WSP/1.1 JOIN
	// channel: 218
	// seq: 2
	channelID := <-cChan
	message := fmt.Sprintf("WSP/1.1 JOIN\r\nchannel: %s\r\nseq: %d\r\n\r\n", channelID, seq)
	log.Debug("data：", message)
	err := conn.WriteMessage(websocket.TextMessage, []byte(message))
	if err != nil {
		log.Fatal("握手发送", err)
	}
	seq++

	// decoder := h264decoder.New(h264decoder.PixelFormatRGB)
	// defer decoder.Close()

	func() {
		for {
			var err error
			message_type, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
					log.Printf("message_type: %v, error: %v", message_type, err)
				}
				return

			}

			log.Debug("data receive:", message_type, len(message))
			payloadData := decodeRtp(message)
			if payloadData == nil {
				continue
			}
			log.Debug("payloadData:", len(payloadData))

			// // // 解码视频帧
			// canvas, err = gocv.IMDecode(payloadData, gocv.IMReadColor)
			// if err != nil {
			// 	log.Debug("Error decoding frame:", err)
			// 	return
			// }

			// // 显示视频帧
			// window.IMShow(canvas)
			// window.WaitKey(1)
		}
	}()
}

func handleControlMessages(conn *websocket.Conn) {

	// 	WSP/1.1 INIT
	// proto: rtsp
	// host: 10.9.244.166
	// port: 8554
	// seq: 1

	ping := fmt.Sprintf("WSP/1.1 INIT\r\nproto: rtsp\r\nhost: %s\r\nport: %d\r\nseq: %d\r\n\r\n", "10.9.244.166", 8554, seq)
	log.Debug("第一次握手\n：", ping)
	err := conn.WriteMessage(websocket.TextMessage, []byte(ping))
	if err != nil {
		log.Fatal("握手发送", err)
	}
	seq++

	var pong []byte
	_, pong, err = conn.ReadMessage()
	if err != nil {
		log.Fatal("握手返回", err)
	}
	log.Debug("第一次握手\n：", string(pong))

	cChan <- getChannelID(string(pong))

	/*----------------------------------------------------------------------------------------*/

	content := fmt.Sprintf("OPTIONS %s RTSP/1.0\r\nUser-Agent: shandowc/1.0\r\nCSeq: %d\r\n\r\n",
		rtspURL, cSeq)
	header := fmt.Sprintf("WSP/1.1 WRAP\r\ncontentLength: %d\r\nseq: %d\r\n\r\n", len(content), seq)
	ping = header + content
	log.Debug("第二次握手\n：", ping)
	err = conn.WriteMessage(websocket.TextMessage, []byte(ping))
	if err != nil {
		log.Fatal("握手发送", err)
	}
	seq++
	cSeq += 2

	_, pong, err = conn.ReadMessage()
	if err != nil {
		log.Fatal("握手返回", err)
	}
	log.Debug("第二次握手\n：", string(pong))

	/*----------------------------------------------------------------------------------------*/

	content = fmt.Sprintf("DESCRIBE %s RTSP/1.0\r\nAccept: application/sdp\r\nUser-Agent: shandowc/1.0\r\nCSeq: %d\r\n\r\n",
		rtspURL, cSeq)
	header = fmt.Sprintf("WSP/1.1 WRAP\r\ncontentLength: %d\r\nseq: %d\r\n\r\n", len(content), seq)
	ping = header + content
	log.Debug("第三次握手\n：", ping)
	err = conn.WriteMessage(websocket.TextMessage, []byte(ping))
	if err != nil {
		log.Fatal("握手发送", err)
	}
	seq++
	cSeq += 2

	_, pong, err = conn.ReadMessage()
	if err != nil {
		log.Fatal("握手返回", err)
	}
	log.Debug("第三次握手\n：", string(pong))

	/*----------------------------------------------------------------------------------------*/
	rfc1123Time := time.Now().Format(time.RFC1123)
	content = fmt.Sprintf("SETUP %s/trackID=0 RTSP/1.0\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\nDate: %s\r\nUser-Agent: shandowc/1.0\r\nCSeq: %d\r\n\r\n",
		rtspURL, rfc1123Time, cSeq)
	header = fmt.Sprintf("WSP/1.1 WRAP\r\ncontentLength: %d\r\nseq: %d\r\n\r\n", len(content), seq)
	ping = header + content
	log.Debug("第四次握手\n：", ping)
	err = conn.WriteMessage(websocket.TextMessage, []byte(ping))
	if err != nil {
		log.Fatal("握手发送", err)
	}
	seq++
	cSeq += 2

	_, pong, err = conn.ReadMessage()
	if err != nil {
		log.Fatal("握手返回", err)
	}
	log.Debug("第四次握手\n：", string(pong))
	session := getSessionID(string(pong))
	/*----------------------------------------------------------------------------------------*/

	content = fmt.Sprintf("PLAY %s RTSP/1.0\r\nUser-Agent: shandowc/1.0\r\nSession: %s\r\nCSeq: %d\r\n\r\n",
		rtspURL, session, cSeq)
	header = fmt.Sprintf("WSP/1.1 WRAP\r\ncontentLength: %d\r\nseq: %d\r\n\r\n", len(content), seq)
	ping = header + content
	log.Debug("第五次握手\n：", ping)
	err = conn.WriteMessage(websocket.TextMessage, []byte(ping))
	if err != nil {
		log.Fatal("握手发送", err)
	}
	seq++
	cSeq += 2

	_, pong, err = conn.ReadMessage()
	if err != nil {
		log.Fatal("握手返回", err)
	}
	log.Debug("第五次握手\n：", string(pong))

	go func() {
		for {
			message_type, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
					log.Printf("message_type: %v, error: %v", message_type, err)
				}
				return

			}

			log.Debug("Control receive:", string(message))
		}
	}()

}

func getOverlayImage(objectInfos *pb.PreviewInfo, frame *gocv.Mat) {
	if objectInfos == nil || objectInfos.Objects == nil || len(objectInfos.Objects) == 0 {
		log.Debug("getOverlayImage objectInfos was invalid!!!")
		return
	}

	for _, obj := range objectInfos.Objects {
		if obj.Bounding == nil || obj.Bounding.Vertices == nil || len(obj.Bounding.Vertices) != 2 {
			log.Debug("getOverlayImage objectInfos.Objects was invalid!!!")
			continue
		}

		if obj.ObjectType != pb.ObjectType_OBJECT_FACE {
			continue
		}

		log.Debug(obj.ObjectType, obj.Attributes)
		x1 := int(obj.Bounding.Vertices[0].X)
		y1 := int(obj.Bounding.Vertices[0].Y)
		x2 := int(obj.Bounding.Vertices[1].X)
		y2 := int(obj.Bounding.Vertices[1].Y)

		rect := image.Rect(x1, y1, x2, y2)
		margin := 100
		expandedRect := rect.Inset(-margin)

		var objColor = color.RGBA{255, 0, 0, 0}

		if obj.Attributes != nil {
			if ifd, ok := obj.Attributes["ifd_extra_info"]; ok {
				extraInfo := map[string]string{}
				err := json.Unmarshal([]byte(ifd), &extraInfo)
				if err == nil {
					if text, ok1 := extraInfo["name"]; ok1 {
						objColor = color.RGBA{0, 255, 0, 0}
						getOverlayText(text, frame, expandedRect.Min, objColor)
					}
				}
			}
		}

		// 在视频帧上绘制矩形框
		gocv.Rectangle(frame, expandedRect, objColor, 2)
	}

}

func getOverlayText(text string, frame *gocv.Mat, position image.Point, color color.RGBA) {
	fontFace := gocv.FontHersheyPlain
	fontScale := 1.2
	thickness := 2
	gocv.PutText(frame, text, position, fontFace, fontScale, color, thickness)
}
