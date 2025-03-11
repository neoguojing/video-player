
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