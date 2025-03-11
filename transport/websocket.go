package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	// "nhooyr.io/websocket"
)

type WSPRequest struct {
	Cmd     string
	Seq     int
	Channel int64
	Headers map[string]string
	Body    []byte
}

func NewWSPRequest() *WSPRequest {
	return &WSPRequest{
		Headers: make(map[string]string),
	}
}

func (req *WSPRequest) Bytes() []byte {
	buf := bytes.Buffer{}
	buf.WriteString(fmt.Sprintf("WSP/1.1 %s\r\n", req.Cmd))
	for k, v := range req.Headers {
		buf.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	buf.WriteString("\r\n")
	buf.Write(req.Body)
	return buf.Bytes()
}

type WSPResponse struct {
	Code    int
	Message string
	Headers map[string]string
	Body    []byte
}

func NewWSPResponseFromBytes(b []byte) (*WSPResponse, error) {
	idx := strings.Index(string(b), "\r\n\r\n")
	if idx == -1 {
		return nil, errors.New("bad request")
	}
	lines := strings.Split(string(b[:idx+2]), "\r\n")
	if len(lines) < 2 {
		return nil, errors.New("bad request")
	}
	headers := make(map[string]string)
	resfields := strings.SplitN(lines[0], " ", 3)
	if len(resfields) != 3 || resfields[0] != "WSP/1.1" {
		return nil, fmt.Errorf("response line 1: %s", lines[0])
	}
	code, err := strconv.Atoi(resfields[1])
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(lines); i++ {
		arr := strings.SplitN(lines[i], ":", 2)
		if len(arr) != 2 {
			continue
		}
		headers[arr[0]] = strings.TrimSpace(arr[1])
	}
	return &WSPResponse{
		Code:    code,
		Message: resfields[2],
		Headers: headers,
		Body:    b[idx+4:],
	}, nil
}

type WebSocketProxy struct {
	seqChan     chan int64
	wsurl       string
	rtspurl     string
	dataChannel string
	ctrlConn    *websocket.Conn
	dataConn    *websocket.Conn
	fin         chan bool
}

func NewWebSocketProxy(wsurl string, rtspurl string) (*WebSocketProxy, error) {
	wsp := &WebSocketProxy{
		wsurl:   wsurl,
		rtspurl: rtspurl,
		seqChan: make(chan int64, 1),
		fin:     make(chan bool),
	}
	go wsp.genSeqs()
	runtime.SetFinalizer(wsp, func(wsp *WebSocketProxy) {
		close(wsp.fin)
	})
	return wsp, nil
}

func (wsp *WebSocketProxy) genSeqs() {
	seq := time.Now().Unix()
	for {
		select {
		case <-wsp.fin:
			break
		default:
		}
		seq = seq + 1
		wsp.seqChan <- seq
	}
}

func (wsp *WebSocketProxy) Connect() error {

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	ctlDialer := websocket.DefaultDialer
	ctlDialer.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	controllHeadler := http.Header{}
	controllHeadler.Add("Sec-WebSocket-Protocol", "control")
	ctrlConn, _, err := ctlDialer.DialContext(ctx, wsp.wsurl, controllHeadler)

	// ctrlConn, _, err := websocket.Dial(ctx, wsp.wsurl, &websocket.DialOptions{
	// 	Subprotocols: []string{"control"},
	// })
	if err != nil {
		return err
	}
	wsp.ctrlConn = ctrlConn

	err = wsp.doINIT()
	if err != nil {
		return err
	}

	err = wsp.doJOIN()
	if err != nil {
		return err
	}

	return nil
}

func (wsp *WebSocketProxy) Disconnect() {
	if wsp.ctrlConn != nil {
		// wsp.ctrlConn.Close(websocket.StatusNormalClosure, "")
		wsp.ctrlConn.Close()
		wsp.ctrlConn = nil
	}
	if wsp.dataConn != nil {
		// wsp.dataConn.Close(websocket.StatusNormalClosure, "")
		wsp.dataConn.Close()
		wsp.dataConn = nil
	}
}

func (wsp *WebSocketProxy) seq() int64 {
	seq := <-wsp.seqChan
	return seq
}

func (wsp *WebSocketProxy) sendRequest(c *websocket.Conn, req *WSPRequest) (*WSPResponse, error) {
	if c == nil {
		return nil, errors.New("sendRequest invalid conn")
	}
	req.Headers["seq"] = strconv.FormatInt(wsp.seq(), 10)
	// ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	// defer cancel()

	reqbody := req.Bytes()
	log.Infof("send:%v", string(reqbody))
	// err := c.Write(ctx, websocket.MessageText, reqbody)
	err := c.WriteMessage(websocket.TextMessage, reqbody)
	if err != nil {
		return nil, err
	}

	// _, buf, err := c.Read(ctx)
	_, buf, err := c.ReadMessage()
	if err != nil {
		return nil, err
	}
	log.Infof("receive:%v", string(buf))

	return NewWSPResponseFromBytes(buf)
}

func (wsp *WebSocketProxy) doINIT() error {
	u, err := url.Parse(wsp.rtspurl)
	if err != nil {
		return err
	}

	port := u.Port()
	if port == "" {
		port = "554"
	}

	req := NewWSPRequest()
	req.Cmd = "INIT"
	req.Headers["proto"] = "rtsp"
	req.Headers["host"] = u.Hostname()
	req.Headers["port"] = port
	res, err := wsp.sendRequest(wsp.ctrlConn, req)
	if err != nil {
		return err
	}
	wsp.dataChannel = res.Headers["channel"]
	if res.Code > 300 {
		return fmt.Errorf("%d %s", res.Code, res.Message)
	}
	return nil
}

func (wsp *WebSocketProxy) Send(payload []byte) ([]byte, error) {
	return wsp.doWrap(payload)
}

func (wsp *WebSocketProxy) doWrap(payload []byte) ([]byte, error) {
	req := NewWSPRequest()
	req.Cmd = "WRAP"
	req.Body = payload
	res, err := wsp.sendRequest(wsp.ctrlConn, req)
	if err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, fmt.Errorf("%d %s", res.Code, res.Message)
	}
	return res.Body, nil
}

func (wsp *WebSocketProxy) ReadData() ([]byte, error) {
	// _, buf, err := wsp.dataConn.Read(context.Background())
	_, buf, err := wsp.dataConn.ReadMessage()
	return buf, err
}

func (wsp *WebSocketProxy) doJOIN() error {
	if wsp.dataConn != nil {
		// wsp.dataConn.Close(websocket.StatusNormalClosure, "")
		wsp.dataConn.Close()
		wsp.dataConn = nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// dataConn, _, err := websocket.Dial(ctx, wsp.wsurl, &websocket.DialOptions{
	// 	Subprotocols: []string{"data"},
	// })
	dataDialer := websocket.DefaultDialer
	dataDialer.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	dataHeader := http.Header{}
	dataHeader.Add("Sec-WebSocket-Protocol", "data")
	dataConn, _, err := dataDialer.DialContext(ctx, wsp.wsurl, dataHeader)
	if err != nil {
		return err
	}
	wsp.dataConn = dataConn

	req := NewWSPRequest()
	req.Cmd = "JOIN"
	req.Headers["channel"] = wsp.dataChannel
	_, err = wsp.sendRequest(wsp.dataConn, req)
	if err != nil {
		return err
	}
	return nil
}
