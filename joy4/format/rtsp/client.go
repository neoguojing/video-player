package rtsp

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/av/avutil"
	"videoplayer/joy4/format/rtsp/rtp"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/log"

	"github.com/32bitkid/bitreader"
)

var (
	ErrCodecDataChange   = fmt.Errorf("rtsp: codec data change, please call HandleCodecDataChange()")
	ErrTimeout           = errors.New("rtsp or rtp timeout")
	ErrMaxProbe          = errors.New("rtsp max probe reached")
	ErrBadServer         = errors.New("rtsp bad server")
	ErrNoSupportedStream = errors.New("rtsp no supported stream")
)

const (
	stageOptionsDone = iota + 1
	stageDescribeDone
	stageSetupDone
	stageWaitCodecData
	stageCodecDataDone
)

const RtspMaxProbeCount = 20

const (
	defaultUserAgent = "STREAM CLIENT"
	bufSize          = 4096
)

type udpPacket struct {
	Idx  int
	Data []byte

	Timestamp uint32
	Sequence  uint16
}

type Client struct {
	Headers []string

	connTimeout         time.Duration
	RtspTimeout         time.Duration
	RtpTimeout          time.Duration
	RtpKeepAliveTimeout time.Duration
	rtpKeepaliveTimer   time.Time
	rtspProbeCount      int
	// rtpKeepaliveEnterCnt int

	stage int

	// setupIdx []int
	// setupMap []int

	authHeaders func(method string) []string

	url        *url.URL
	conn       *connWithTimeout
	brconn     *bufio.Reader
	requestUri string
	cseq       uint
	sdp        sdp.SDPInfo
	streams    []*Stream
	session    string
	body       io.Reader

	// UDP
	UseUDP bool
	udpCh  chan udpPacket

	// TCP interleave channel number to index of streams
	tcpStreamIndex map[int]int

	// more packets
	more          bool
	moreStreamIdx int

	supportedMethods []string
	buggy            buggyChecker
	readTCP          bool

	lastRTCPSent  time.Time
	redirectTimes int

	buf *bytes.Buffer // make sure no race
}

type Request struct {
	Header []string
	Uri    string
	Method string
}

type Response struct {
	StatusCode    int
	Headers       textproto.MIMEHeader
	ContentLength int
	Body          []byte

	Block []byte
}

func DialTimeout(uri string, timeout time.Duration) (*Client, error) {
	var URL *url.URL
	var err error
	if URL, err = url.Parse(uri); err != nil {
		return nil, err
	}

	if _, _, err := net.SplitHostPort(URL.Host); err != nil {
		URL.Host = URL.Host + ":554"
	}

	dailer := net.Dialer{Timeout: timeout}
	var conn net.Conn
	if conn, err = dailer.Dial("tcp", URL.Host); err != nil {
		return nil, err
	}

	u2 := *URL
	u2.User = nil

	connt := &connWithTimeout{Conn: conn}

	return &Client{
		conn:       connt,
		brconn:     bufio.NewReaderSize(connt, bufSize),
		url:        URL,
		requestUri: u2.String(),

		connTimeout:         timeout,
		RtpKeepAliveTimeout: 30 * time.Second,
		RtspTimeout:         10 * time.Second,
		RtpTimeout:          10 * time.Second,

		tcpStreamIndex: make(map[int]int),

		udpCh: make(chan udpPacket, 1024),
		buf:   &bytes.Buffer{},
	}, nil
}

func Dial(uri string) (*Client, error) {
	return DialTimeout(uri, 0)
}

func (self *Client) allCodecDataReady() bool {
	for _, s := range self.streams {
		if s.CodecData == nil {
			return false
		}
	}
	log.Log(log.TRACE, "all codecdata ready")
	return true
}

func (self *Client) probe() (err error) {
	for {
		if self.allCodecDataReady() {
			break
		}
		if _, err = self.readPacket(); err != nil {
			return
		}
	}
	self.stage = stageCodecDataDone
	return
}

func (self *Client) prepare(stage int) (err error) {
	for self.stage < stage {
		self.rtspProbeCount++
		if self.rtspProbeCount > RtspMaxProbeCount {
			err = ErrMaxProbe
			return
		}
		switch self.stage {
		case 0:
			if err = self.Options(); err != nil {
				return
			}
		case stageOptionsDone:
			if _, err = self.Describe(); err != nil {
				return
			}
		case stageDescribeDone:
			if err = self.SetupAll(); err != nil {
				return
			}

		case stageSetupDone:
			if err = self.Play(); err != nil {
				return
			}

		case stageWaitCodecData:
			if err = self.probe(); err != nil {
				return
			}
		}
	}
	return
}

// SDP return sdp info
func (self *Client) SDP() (sdp sdp.SDPInfo, err error) {
	if err = self.prepare(stageCodecDataDone); err != nil {
		return
	}
	sdp = self.sdp

	return
}

// Streams deprecated
func (self *Client) Streams() (streams []av.CodecData, err error) {
	if err = self.prepare(stageCodecDataDone); err != nil {
		return
	}
	for _, s := range self.streams {
		streams = append(streams, s.CodecData)
	}

	return
}

func (self *Client) sendRtpKeepalive(readResponse bool) (err error) {
	if self.RtpKeepAliveTimeout <= 0 {
		return
	}
	if self.rtpKeepaliveTimer.IsZero() {
		self.rtpKeepaliveTimer = time.Now()
	} else if time.Now().Sub(self.rtpKeepaliveTimer) > self.RtpKeepAliveTimeout {
		self.rtpKeepaliveTimer = time.Now()
		log.Log(log.DEBUG, "rtp: keep alive")
		var req Request
		// refer to: https://github.com/FFmpeg/FFmpeg/blob/master/libavformat/rtspdec.c#L914
		if self.session != "" && self.isMethodSupported("GET_PARAMETER") {
			req = Request{
				Method: "GET_PARAMETER",
				Uri:    self.requestUri,
				Header: []string{"Session: " + self.session},
			}
		} else {
			req = Request{
				Method: "OPTIONS",
				Uri:    "*",
			}
		}
		if err = self.WriteRequest(req); err != nil {
			return
		}
		if readResponse {
			var r Response
			if r, err = self.ReadResponse(); err != nil {
				return
			} else {
				log.Log(log.DEBUG, "keepalive response: ", r)
			}
		}
	}
	return
}

func (self *Client) WriteRequest(req Request) (err error) {
	self.conn.Timeout = self.RtspTimeout
	self.cseq++

	buf := &bytes.Buffer{}

	fmt.Fprintf(buf, "%s %s RTSP/1.0\r\n", req.Method, req.Uri)
	fmt.Fprintf(buf, "CSeq: %d\r\n", self.cseq)
	fmt.Fprintf(buf, "User-Agent: %s\r\n", defaultUserAgent)

	if self.authHeaders != nil {
		headers := self.authHeaders(req.Method)
		for _, s := range headers {
			io.WriteString(buf, s)
			io.WriteString(buf, "\r\n")
		}
	}
	for _, s := range req.Header {
		io.WriteString(buf, s)
		io.WriteString(buf, "\r\n")
	}
	for _, s := range self.Headers {
		io.WriteString(buf, s)
		io.WriteString(buf, "\r\n")
	}
	io.WriteString(buf, "\r\n")

	bufout := buf.Bytes()

	log.Log(log.DEBUG, "> ", string(bufout))

	if _, err = self.conn.Write(bufout); err != nil {
		return
	}

	return
}

func (self *Client) parseBlockHeader(h []byte) (length int, no int, timestamp uint32, seq uint16, err error) {
	length = int(h[2])<<8 + int(h[3])
	no = int(h[1])
	if no/2 >= len(self.streams) {
		err = fmt.Errorf("invalid RTSP index")
		return
	}

	if no%2 == 0 { // rtp
		if length < 8 {
			err = fmt.Errorf("invalid RTP data length")
			return
		}

		// V=2
		if h[4]&0xc0 != 0x80 {
			err = fmt.Errorf("invalid RTP version")
			return
		}

		stream := self.streams[no/2]
		if int(h[5]&0x7f) != stream.Sdp.PayloadType {
			err = fmt.Errorf("invalid RTP PayloadType(%v)", int(h[5]&0x7f))
			return
		}

		seq = binary.BigEndian.Uint16(h[6:8])
		timestamp = binary.BigEndian.Uint32(h[8:12])
	} else { // rtcp
		if len(h) <= 4 {
			err = fmt.Errorf("invalid rtcp data length")
			return
		}

		index := 4
		var rtcpPacket interface{}
		var errRet error
		for {
			if index >= len(h) {
				break
			}

			var readLen int
			readLen, rtcpPacket, errRet = self.parseRtcpPacket(h[index:])
			if errRet != nil {
				break
			}
			index += readLen

			switch v := rtcpPacket.(type) {
			case rtp.RtcpPacketSR:
			case rtp.RtcpPacketSDES:
			case rtp.RtcpPacketBYE:
				if v.SSRC == rtp.RTCP_EOF_FLAG {
					err = io.EOF
					log.Log(log.WARN, "Recv EOF flag")
					return
				}
			default:
			}
		}
	}

	return
}

// parseRtcpPacket parse rtcp packet (refer by: https://github.com/FFmpeg/FFmpeg/blob/release/4.2/libavformat/rtpenc.c#L285)
func (self *Client) parseRtcpPacket(buf []byte) (readLen int, rtcpPacket interface{}, err error) {
	if len(buf) <= 2 {
		return 0, nil, fmt.Errorf("too small buffer size len(%v)", len(buf))
	}

	t := buf[1]
	switch t {
	case rtp.RTCP_SR:
		br := bitreader.NewReader(bytes.NewReader(buf))
		var sr rtp.RtcpPacketSR
		_ = binary.Read(br, binary.BigEndian, &sr.Version)
		readLen++
		_ = binary.Read(br, binary.BigEndian, &sr.Type)
		readLen++
		_ = binary.Read(br, binary.BigEndian, &sr.Length)
		if sr.Length != 6 {
			return 0, nil, fmt.Errorf("error RTCP SR payload len(%v)", sr.Length)
		}
		readLen += 2
		_ = binary.Read(br, binary.BigEndian, &sr.SSRC)
		readLen += 4
		_ = binary.Read(br, binary.BigEndian, &sr.NtpLeastTime)
		readLen += 4
		_ = binary.Read(br, binary.BigEndian, &sr.NtpMostTime)
		readLen += 4
		_ = binary.Read(br, binary.BigEndian, &sr.RtpTime)
		readLen += 4
		_ = binary.Read(br, binary.BigEndian, &sr.PacketCount)
		readLen += 4
		_ = binary.Read(br, binary.BigEndian, &sr.OctetCount)
		readLen += 4

		rtcpPacket = sr
		err = nil
		return
	case rtp.RTCP_RR:
	case rtp.RTCP_SDES:
	case rtp.RTCP_BYE:
		br := bitreader.NewReader(bytes.NewReader(buf))
		var bye rtp.RtcpPacketBYE
		_ = binary.Read(br, binary.BigEndian, &bye.Version)
		readLen++
		_ = binary.Read(br, binary.BigEndian, &bye.Type)
		readLen++
		_ = binary.Read(br, binary.BigEndian, &bye.Length)
		if bye.Length != 1 {
			return 0, nil, fmt.Errorf("error RTCP SR payload len(%v)", bye.Length)
		}
		readLen += 2
		_ = binary.Read(br, binary.BigEndian, &bye.SSRC)
		readLen += 4

		rtcpPacket = bye
		err = nil
		return
	case rtp.RTCP_APP:
	case rtp.RTCP_RTPFB:
	case rtp.RTCP_PSFB:
	case rtp.RTCP_XR:
	case rtp.RTCP_AVB:
	case rtp.RTCP_RSI:
	case rtp.RTCP_TOKEN:
	default:
	}

	return 0, nil, fmt.Errorf("unsupported RTCP type(%d)", t)
}

func (self *Client) parsePublic(res *Response) {
	if len(self.supportedMethods) > 0 {
		return
	}
	header := res.Headers.Get("public")
	for _, e := range strings.Split(header, ",") {
		self.supportedMethods = append(self.supportedMethods, strings.TrimSpace(e))
	}
}

func (self *Client) isMethodSupported(m string) bool {
	for i := range self.supportedMethods {
		if m == self.supportedMethods[i] {
			return true
		}
	}
	return false
}

func (self *Client) handleResp(res *Response) (err error) {
	if sess := res.Headers.Get("Session"); sess != "" && self.session == "" {
		if fields := strings.Split(sess, ";"); len(fields) > 0 {
			self.session = fields[0]
			if len(fields) > 1 {
				var timeout int
				n, err := fmt.Sscanf(fields[1], "timeout=%d", &timeout)
				if err == nil && n == 1 && timeout > 1 {
					log.Log(log.DEBUG, "rtsp session timeout: ", timeout)
					self.RtpKeepAliveTimeout = time.Duration(timeout-1) * time.Second / 2
				}
			}
		}
	}
	if res.StatusCode == 401 {
		if err = self.handle401(res); err != nil {
			return
		}
	}
	return
}

func (self *Client) handle401(res *Response) (err error) {
	/*
		RTSP/1.0 401 Unauthorized
		CSeq: 2
		Date: Wed, May 04 2016 10:10:51 GMT
		WWW-Authenticate: Digest realm="LIVE555 Streaming Media", nonce="c633aaf8b83127633cbe98fac1d20d87"
	*/
	authval := res.Headers.Get("WWW-Authenticate")
	hdrval := strings.SplitN(authval, " ", 2)
	var realm, nonce string

	if len(hdrval) == 2 {
		for _, field := range strings.Split(hdrval[1], ",") {
			field = strings.Trim(field, ", ")
			if keyval := strings.SplitN(field, "=", 2); len(keyval) == 2 { // Value may contain '='
				key := keyval[0]
				val := strings.Trim(keyval[1], `"`)
				switch key {
				case "realm":
					realm = val
				case "nonce":
					nonce = val
				}
			}
		}

		if realm != "" {
			var username string
			var password string

			if self.url.User == nil {
				err = fmt.Errorf("rtsp: no username")
				return
			}
			username = self.url.User.Username()
			password, _ = self.url.User.Password()

			self.authHeaders = func(method string) []string {
				var headers []string
				if nonce != "" {
					hs1 := md5hash(username + ":" + realm + ":" + password)
					hs2 := md5hash(method + ":" + self.requestUri)
					response := md5hash(hs1 + ":" + nonce + ":" + hs2)
					headers = append(headers, fmt.Sprintf(
						`Authorization: Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
						username, realm, nonce, self.requestUri, response))
				} else {
					headers = append(headers, fmt.Sprintf(`Authorization: Basic %s`, base64.StdEncoding.EncodeToString([]byte(username+":"+password))))
				}
				return headers
			}
		}
	}

	return
}

func (self *Client) rectifyPacket(peek []byte) []byte {
	if len(peek) == 0 {
		return peek
	}

	streamNumber := int(peek[1])
	if streamIndex, exists := self.tcpStreamIndex[streamNumber]; exists {
		peek[1] = byte(streamIndex)
	}
	return peek
}

func (self *Client) parseOnePacket(strict bool) (res Response, ok bool, err error) {
	var first, peek []byte
	var line string

	first, err = self.brconn.Peek(1)
	if err != nil {
		return
	}
	// buf := &bytes.Buffer{}
	self.buf.Reset()

	r := self.brconn
	if first[0] == 'R' {
		peek, err = r.Peek(4)
		if err != nil {
			return
		}
		if !(peek[0] == 'R' && peek[1] == 'T' && peek[2] == 'S' && peek[3] == 'P') {
			log.Log(log.DEBUG, "invalid RTSP mark: ", hex.Dump(peek))
			_, err = r.ReadByte()
			return
		}

		tr := textproto.NewReader(r)
		line, err = tr.ReadLine()
		if err != nil {
			return
		}
		if codes := strings.Split(line, " "); len(codes) >= 2 {
			res.StatusCode, err = strconv.Atoi(strings.TrimSpace(codes[1]))
			if err != nil {
				log.Log(log.DEBUG, "invalid RTSP line: ", line, err)
				if !strict {
					err = nil
				}
				return
			}
		} else {
			log.Log(log.DEBUG, "invalid RTSP line: ", line)
			return
		}
		res.Headers, err = tr.ReadMIMEHeader()
		if err != nil {
			return
		}
		if v := res.Headers.Get("Content-Length"); v != "" {
			res.ContentLength, err = strconv.Atoi(v)
			if err != nil {
				return
			}
		}
		if res.ContentLength > 0 {
			_, err = io.CopyN(self.buf, r, int64(res.ContentLength))
			if err != nil {
				return
			}
			res.Body = self.buf.Bytes()
		}
		ok = true
		return
	} else if first[0] == '$' {
		peek, err = r.Peek(4 + 8)
		if err != nil {
			return
		}

		peek = self.rectifyPacket(peek)

		size, _, _, _, errRet := self.parseBlockHeader(peek)
		if errRet == io.EOF {
			err = io.EOF
			return
		}

		if errRet != nil {
			// skip
			log.Log(log.DEBUG, "invalid dollor RTP header: ", hex.Dump(peek))
			_, err = r.ReadByte()
			return
		}

		self.buf.Grow(4 + size)
		_, err = io.CopyN(self.buf, r, 4+int64(size))
		if err != nil {
			return
		}
		res.Block = self.buf.Bytes()
		ok = true
		return
	} else {
		// skip
		_, err = r.ReadByte()
		return
	}
	err = ErrBadServer
	return

}

func (self *Client) ReadResponse() (res Response, err error) {
	var ok bool
	for {
		res, ok, err = self.parseOnePacket(true)
		if err != nil {
			log.Log(log.ERROR, "rtsp: error response: ", err)
			return
		}
		if !ok {
			continue
		}
		err = self.handleResp(&res)
		return

	}
	return
}

func (self *Client) parseInterleaved(si int, resp *Response) {
	if nil == resp || nil == resp.Headers {
		return
	}

	transport := resp.Headers.Get("Transport")

	var ok bool
	var rtpChan, rtcpChan int
	for _, e := range strings.Split(transport, ";") {
		n, err := fmt.Sscanf(e, "interleaved=%d-%d", &rtpChan, &rtcpChan)
		if err == nil && n == 2 {
			ok = true
			break
		}
	}

	if !ok {
		log.Log(log.WARN, "rtp and rtcp channel id not returned from server")
		rtpChan = 2 * si
		rtcpChan = 2*si + 1
	}

	if _, ok := self.tcpStreamIndex[rtpChan]; ok {
		log.Log(log.WARN, "rtp channel id already exists: ", rtpChan)
	} else {
		self.tcpStreamIndex[rtpChan] = 2 * si
	}
	if _, ok := self.tcpStreamIndex[rtcpChan]; ok {
		log.Log(log.WARN, "rtcp channel id already exists: ", rtcpChan)
	} else {
		self.tcpStreamIndex[rtcpChan] = 2*si + 1
	}
}

func (self *Client) SetupAll() (err error) {
	return self.Setup()
}

func (self *Client) Setup() (err error) {
	if err = self.prepare(stageDescribeDone); err != nil {
		return
	}

	for si := range self.streams {
		uri := ""
		control := self.streams[si].Sdp.Control
		if strings.HasPrefix(control, "rtsp://") {
			uri = control
		} else {
			if uri, err = buildStreamUri(self.requestUri, control); nil != err {
				return
			}
		}
		req := Request{Method: "SETUP", Uri: uri}
		if self.UseUDP {
			err = self.streams[si].setupUDP(self.requestUri)
			if err != nil {
				return
			}
			p1 := self.streams[si].rtpUdpPort()
			p2 := self.streams[si].rtcpUdpPort()
			req.Header = append(req.Header, fmt.Sprintf("Transport: RTP/AVP;unicast;client_port=%d-%d", p1, p2))
		} else {
			req.Header = append(req.Header, fmt.Sprintf("Transport: RTP/AVP/TCP;unicast;interleaved=%d-%d", si*2, si*2+1))
		}
		if self.session != "" {
			req.Header = append(req.Header, "Session: "+self.session)
		}
		if err = self.WriteRequest(req); err != nil {
			return
		}

		//fix Aliyun platform issue:RTP is published before setup/play interaction.
		var resp Response
		deadline := time.Now().Add(30 * time.Second)
		for {
			if resp, err = self.ReadResponse(); err != nil {
				return
			}

			if resp.StatusCode == 200 {
				break
			}

			if time.Now().After(deadline) {
				err = NewRTSPError(resp.StatusCode, "SETUP failed")
				return
			}
		}

		if self.UseUDP {
			self.streams[si].sendPunch(&resp)
		} else {
			self.parseInterleaved(si, &resp)
		}
	}

	self.redirectTimes = 0
	self.lastRTCPSent = time.Now()
	if self.stage == stageDescribeDone {
		self.stage = stageSetupDone
	}

	return
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

func md5hash(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
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

func (self *Client) redirect(uri string) error {

	log.Logf(log.WARN, "before url(%v), redirect new  uri:%s", self.url, uri)
	var URL *url.URL
	var err error
	if URL, err = url.Parse(uri); err != nil {
		return err
	}

	if _, _, err := net.SplitHostPort(URL.Host); err != nil {
		URL.Host = URL.Host + ":554"
	}

	dailer := net.Dialer{Timeout: self.connTimeout}
	var conn net.Conn
	if conn, err = dailer.Dial("tcp", URL.Host); err != nil {
		return err
	}

	u2 := *URL
	u2.User = nil

	connt := &connWithTimeout{Conn: conn}

	self.brconn = bufio.NewReaderSize(connt, bufSize)
	self.conn = connt
	self.url = URL
	self.requestUri = u2.String()
	self.stage = 0
	self.redirectTimes++
	return nil
}

func (self *Client) Describe() (streams []sdp.Media, err error) {
	var res Response

	for i := 0; i < 2; i++ {
		req := Request{
			Method: "DESCRIBE",
			Uri:    self.requestUri,
			Header: []string{"Accept: application/sdp"},
		}
		if err = self.WriteRequest(req); err != nil {
			return
		}
		if res, err = self.ReadResponse(); err != nil {
			return
		}
		if res.StatusCode != 401 {
			break
		}
	}

	if res.StatusCode == 301 || res.StatusCode == 302 {
		if self.redirectTimes > 3 {
			err = NewRTSPError(res.StatusCode, "too many redirection")
		} else {
			if newURL := res.Headers.Get("Location"); newURL != "" {
				self.conn.Close()
				err = self.redirect(newURL)
			} else {
				err = NewRTSPError(res.StatusCode, "Not found redirect Location")
			}
		}
		return
	}

	if res.StatusCode < 200 || res.StatusCode > 299 || res.ContentLength <= 0 {
		err = NewRTSPError(res.StatusCode, "DESCRIBE failed")
		return
	}

	body := string(res.Body)

	log.Log(log.DEBUG, "DESCRIBE< ", body)

	_, sdp := sdp.Parse(body)
	self.buggy.CheckSDP(&sdp)
	medias := sdp.Medias
	log.Log(log.DEBUG, "unknown SDP lines: ", sdp.ExtraLines)

	self.streams = []*Stream{}
	for i, media := range medias {
		// skip unsupport stream eg: applicion
		if !isSupportedMedia(&media) {
			log.Logf(log.WARN, "rtsp: unsupported media type: %s %s", media.AVType, media.Type.String())
			continue
		}
		stream := &Stream{Sdp: media, client: self, Idx: i}
		if err := stream.MakeCodecData(nil); err != nil {
			log.Log(log.WARN, "rtsp: stream error: ", err, media)
			// continue
		}

		log.Logf(log.DEBUG, "rtsp: stream %d codec data: %+v", i, stream)
		self.streams = append(self.streams, stream)
		streams = append(streams, media)

		sdp.CodecDatas = append(sdp.CodecDatas, stream.CodecData)
	}

	if self.stage == stageOptionsDone {
		self.stage = stageDescribeDone
	}

	if len(self.streams) == 0 {
		err = ErrNoSupportedStream
		return
	}

	self.sdp = sdp
	return
}

func (self *Client) Options() (err error) {
	var res Response

	for i := 0; i < 2; i++ {
		req := Request{
			Method: "OPTIONS",
			Uri:    self.requestUri,
		}
		if err = self.WriteRequest(req); err != nil {
			return
		}
		if res, err = self.ReadResponse(); err != nil {
			return
		}

		log.Log(log.DEBUG, "OPTIONS< ", res)
		if res.StatusCode != 401 {
			break
		}
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		err = NewRTSPError(res.StatusCode, "OPTIONS failed")
		return
	}

	self.buggy.CheckOPTIONS(&res)
	self.parsePublic(&res)

	if self.stage == 0 {
		self.stage = stageOptionsDone
	}
	return
}

func (self *Client) Play() (err error) {
	req := Request{
		Method: "PLAY",
		Uri:    self.requestUri,
	}
	log.Logf(log.DEBUG, "server buggy: %+v", self.buggy)
	req.Header = append(req.Header, "Session: "+self.session)
	if err = self.WriteRequest(req); err != nil {
		return
	}

	if self.allCodecDataReady() {
		self.stage = stageCodecDataDone
	} else {
		self.stage = stageWaitCodecData
	}
	return
}

func (self *Client) closeUDP() {
	for _, s := range self.streams {
		s.Close()
	}
}

func (self *Client) Teardown() (err error) {
	defer self.closeUDP()
	req := Request{
		Method: "TEARDOWN",
		Uri:    self.requestUri,
	}
	req.Header = append(req.Header, "Session: "+self.session)
	if err = self.WriteRequest(req); err != nil {
		return
	}
	return
}

func (self *Client) Close() (err error) {
	self.closeUDP()
	return self.conn.Conn.Close()
}

func (self *Client) handleBlock(block []byte) (pkt av.Packet, ok bool, err error) {
	if self.more {
		panic("more should be false")
	}
	var errRet error
	_, blockno, _, _, errRet := self.parseBlockHeader(block)
	if errRet == io.EOF {
		err = io.EOF
		return
	}

	if blockno%2 != 0 {
		log.Log(log.DEBUG, "rtsp: rtcp block len ", len(block)-4, " no ", blockno)
	}

	i := blockno / 2
	if i >= len(self.streams) {
		err = fmt.Errorf("rtsp: block no=%d invalid", blockno)
		return
	}
	stream := self.streams[i]

	/*
		if self.DebugRtp {
			l := 32
			if l > len(block) {
				l = len(block)
			}
			fmt.Println("raw")
			fmt.Print(hex.Dump(block[:l]))
		}
	*/

	if stream.ctx == nil {
		err = fmt.Errorf("rtsp: block no=%d demux not avaiable", blockno)
		return
	}

	var more bool
	pkt, more, err = stream.ctx.RtpParsePacket(block[4:])
	if err == rtp.ErrNoPacket {
		err = nil
		return
	}
	if err != nil {
		return
	}

	if stream.CodecData == nil {
		stream.MakeCodecData(pkt.Data[4:])
	}

	if stream.CodecData == nil || i >= len(self.sdp.CodecDatas) {
		log.Log(log.WARN, "rtp: stream ", i, " codec data not valid")
		return
	}

	self.sdp.CodecDatas[i] = stream.CodecData
	self.more = more
	if more {
		self.moreStreamIdx = i
	}

	ok = true
	pkt.Idx = int8(i)

	log.Log(log.TRACE, "rtp: packet len: ", len(pkt.Data), ", stream: ", i, ", time: ", pkt.Time, ", more: ", more)
	/*
		l := 32
		if l > len(pkt.Data) {
			l = len(pkt.Data)
		}
		log.Log(log.TRACE, hex.Dump(pkt.Data[:l]))
	*/

	return
}

func (self *Client) readUDPPacket() (pkt av.Packet, err error) {
	var timeout *time.Timer
	var timeoutC <-chan time.Time
	if self.RtpTimeout > 0 {
		timeout = time.NewTimer(self.RtpTimeout)
		defer timeout.Stop()
		timeoutC = timeout.C
	}

	for {
		select {
		case p, ok := <-self.udpCh:
			if !ok {
				err = io.EOF
				return
			}
			var timestamp uint32
			var seq uint16
			var errRet error
			if _, _, timestamp, seq, errRet = self.parseBlockHeader(p.Data); errRet != nil {
				if errRet == io.EOF {
					err = io.EOF
					return
				}

				continue
			}
			p.Timestamp = timestamp
			p.Sequence = seq
			// if self.DebugRtp {
			// 	fmt.Println("rtp: ", p.Timestamp, p.Sequence)
			// }
			if pkt, ok, err = self.handleBlock(p.Data); err != nil {
				log.Log(log.WARN, "rtsp: bad block: ", err)
				return
			}
			if ok {
				return
			}
		case <-timeoutC:
			err = ErrTimeout
			return
		}
	}

	return
}

func (self *Client) readTCPPacket() (pkt av.Packet, err error) {
	for {
		var res Response
		var ok bool
		for {
			if res, ok, err = self.parseOnePacket(false); err != nil {
				log.Log(log.ERROR, "failed to read rtsp packet: ", err)
				return
			}
			if len(res.Headers) > 0 {
				log.Log(log.DEBUG, "RTSP repsonse: ", res)
			}
			if ok && len(res.Block) > 0 {
				break
			}
		}

		if pkt, ok, err = self.handleBlock(res.Block); err != nil {
			return
		}
		if ok {
			return
		}
	}
}

func (self *Client) readPacket() (pkt av.Packet, err error) {
	for self.more {
		pkt, self.more, err = self.streams[self.moreStreamIdx].ctx.RtpParsePacket(nil)
		if err == rtp.ErrNoPacket {
			err = nil
		} else if err != nil {
			return
		} else {
			return
		}
	}

	self.sendRTCPRR()

	if self.UseUDP {
		// we have a buffered channel to cache UDP packets, so it is OK
		// to block a little while for Keepalive response before readUDPPacket()
		if err = self.sendRtpKeepalive(true); err != nil {
			return
		}
		return self.readUDPPacket()
	} else {
		if err = self.sendRtpKeepalive(false); err != nil {
			return
		}
		return self.readTCPPacket()
	}
}

func (self *Client) sendRTCPRR() {
	// FFMPEG do not send RTCP_RR on TCP
	if !self.UseUDP {
		return
	}
	now := time.Now()
	if now.After(self.lastRTCPSent.Add(5 * time.Second)) {
		log.Log(log.DEBUG, "sending RTCP RR")
		for i := range self.streams {
			self.streams[i].sendRR()
		}
		self.lastRTCPSent = now
	}
}

func (self *Client) ReadPacket() (pkt av.Packet, err error) {
	if err = self.prepare(stageCodecDataDone); err != nil {
		return
	}
	return self.readPacket()
}

func (self *Client) Address() (localAddr string, remoteAddr string) {

	localAddr, remoteAddr = "", ""
	if self.UseUDP {
		for _, stream := range self.streams {
			if stream.Type() == av.H264 || stream.Type() == av.H265 || stream.Type() == av.MJPEG {
				if len(stream.udpConns) == 0 {
					return
				}
				return stream.udpConns[0].LocalAddr().String(), stream.udpAddrs[0].String()
			}
		}
	}
	return self.conn.Conn.LocalAddr().String(), self.conn.Conn.RemoteAddr().String()
}

func Handler(h *avutil.RegisterHandler) {
	h.UrlDemuxer = func(uri string) (ok bool, demuxer av.DemuxCloser, err error) {
		if !strings.HasPrefix(uri, "rtsp://") {
			return
		}
		ok = true
		demuxer, err = Dial(uri)
		return
	}
}
