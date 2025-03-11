package rtsp

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"videoplayer/transport"

	log "github.com/sirupsen/logrus"

	"videoplayer/joy4/av"

	"videoplayer/joy4/format/rtsp/rtp"

	"videoplayer/joy4/format/rtsp/sdp"
)

type ResponseWriter interface {
	http.ResponseWriter
}

type Request struct {
	Method        string
	URL           *url.URL
	Proto         string
	ProtoMajor    int
	ProtoMinor    int
	Header        http.Header
	ContentLength int
	Body          io.ReadCloser
}

func (r *Request) Bytes() []byte {
	s := fmt.Sprintf("%s %s %s/%d.%d\r\n", r.Method, r.URL, r.Proto, r.ProtoMajor, r.ProtoMinor)
	for k, v := range r.Header {
		for _, v := range v {
			s += fmt.Sprintf("%s: %s\r\n", k, v)
		}
	}
	var buf bytes.Buffer
	buf.WriteString(s)
	buf.WriteString("\r\n")
	if (r.Body) != nil {
		bd, _ := ioutil.ReadAll(r.Body)
		buf.Write(bd)
	}
	return buf.Bytes()
}

func NewRequest(method, urlStr, cSeq string, body io.ReadCloser) (*Request, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	req := &Request{
		Method:     method,
		URL:        u,
		Proto:      "RTSP",
		ProtoMajor: 1,
		ProtoMinor: 0,
		Header:     map[string][]string{"CSeq": []string{cSeq}},
		Body:       body,
	}
	return req, nil
}

type Client struct {
	cSeq       int
	sessionId  string
	requestUri string
	trans      transport.Transporter

	// ported from joy4
	url                 *url.URL
	streams             []*Stream
	authen              func(method string) string
	RtpKeepAliveTimeout time.Duration
	supportedMethods    []string
	stage               int
	sdp                 sdp.SDPInfo
	tcpStreamIndex      map[int]int
	lastRTCPSent        time.Time
	redirectTimes       int
	more                bool
	moreStreamIdx       int
	rtpKeepaliveTimer   time.Time
}

func NewClient(uri string, trans transport.Transporter) (*Client, error) {
	var URL *url.URL
	var err error
	if URL, err = url.Parse(uri); err != nil {
		return nil, err
	}

	if _, _, err := net.SplitHostPort(URL.Host); err != nil {
		URL.Host = URL.Host + ":554"
	}

	u2 := *URL
	u2.User = nil

	return &Client{
		trans:               trans,
		url:                 URL,
		requestUri:          u2.String(),
		tcpStreamIndex:      make(map[int]int),
		RtpKeepAliveTimeout: 30 * time.Second,
	}, nil
}

func (s *Client) nextCSeq() string {
	s.cSeq++
	return strconv.Itoa(s.cSeq)
}

func (self *Client) Streams() (streams []av.CodecData, err error) {
	if err = self.prepare(stageCodecDataDone); err != nil {
		return
	}
	for _, s := range self.streams {
		streams = append(streams, s.CodecData)
	}

	return
}

func (s *Client) sendRequest(req *Request) (*Response, error) {
	if s.sessionId != "" {
		req.Header.Add("Session", s.sessionId)
	}
	req.Header.Add("User-Agent", "go_html_player")
	if s.authen != nil {
		req.Header.Add("Authorization", s.authen(req.Method))
	}
	resbody, err := s.trans.Send(req.Bytes())
	if err != nil {
		return nil, err
	}
	return ReadResponse(resbody)
}

func (self *Client) handleResp(res *Response) (err error) {
	if sess := res.Header.Get("Session"); sess != "" && self.sessionId == "" {
		if fields := strings.Split(sess, ";"); len(fields) > 0 {
			self.sessionId = fields[0]
			if len(fields) > 1 {
				var timeout int
				n, err := fmt.Sscanf(fields[1], "timeout=%d", &timeout)
				if err == nil && n == 1 && timeout > 1 {
					log.Debug("rtsp session timeout: ", timeout)
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
	authval := res.Header.Get("WWW-Authenticate")
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

			self.authen = func(method string) string {
				if nonce != "" {
					hs1 := md5hash(username + ":" + realm + ":" + password)
					hs2 := md5hash(method + ":" + self.requestUri)
					response := md5hash(hs1 + ":" + nonce + ":" + hs2)
					return fmt.Sprintf(
						`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
						username, realm, nonce, self.requestUri, response)
				} else {
					return fmt.Sprintf(`Authorization: Basic %s`, base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
				}
			}
		}
	}

	return
}

func (self *Client) parsePublic(res *Response) {
	if len(self.supportedMethods) > 0 {
		return
	}
	header := res.Header.Get("public")
	for _, e := range strings.Split(header, ",") {
		self.supportedMethods = append(self.supportedMethods, strings.TrimSpace(e))
	}
}

func (self *Client) parseInterleaved(si int, resp *Response) {
	if nil == resp || nil == resp.Header {
		return
	}

	transport := resp.Header.Get("Transport")

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
		log.Debug("rtp and rtcp channel id not returned from server")
		rtpChan = 2 * si
		rtcpChan = 2*si + 1
	}

	if _, ok := self.tcpStreamIndex[rtpChan]; ok {
		log.Debug("rtp channel id already exists: ", rtpChan)
	} else {
		self.tcpStreamIndex[rtpChan] = 2 * si
	}
	if _, ok := self.tcpStreamIndex[rtcpChan]; ok {
		log.Debug("rtcp channel id already exists: ", rtcpChan)
	} else {
		self.tcpStreamIndex[rtcpChan] = 2*si + 1
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

func (self *Client) sendRtpKeepalive(readResponse bool) (err error) {
	if self.RtpKeepAliveTimeout <= 0 {
		return
	}
	if self.rtpKeepaliveTimer.IsZero() {
		self.rtpKeepaliveTimer = time.Now()
	} else if time.Now().Sub(self.rtpKeepaliveTimer) > self.RtpKeepAliveTimeout {
		self.rtpKeepaliveTimer = time.Now()
		log.Info("rtp: keep alive")
		var req *Request
		// refer to: https://github.com/FFmpeg/FFmpeg/blob/master/libavformat/rtspdec.c#L914
		if self.sessionId != "" && self.isMethodSupported("GET_PARAMETER") {
			req, err = NewRequest("GET_PARAMETER", self.requestUri, self.nextCSeq(), nil)
		} else {
			req, err = NewRequest("OPTIONS", "*", self.nextCSeq(), nil)
		}
		if err != nil {
			return
		}
		_, err = self.sendRequest(req)
		if err != nil {
			return
		}
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
		// TODO: ignore now
	}

	return
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
		log.Debug("rtsp: rtcp block len ", len(block)-4, " no ", blockno)
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
			log.Debug("raw")
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
		log.Debug("rtp: stream ", i, " codec data not valid")
		return
	}

	self.sdp.CodecDatas[i] = stream.CodecData
	self.more = more
	if more {
		self.moreStreamIdx = i
	}

	ok = true
	pkt.Idx = int8(i)

	// log.Debug("rtp: packet len: ", len(pkt.Data), ", stream: ", i, ", time: ", pkt.Time, ", more: ", more)
	/*
		l := 32
		if l > len(pkt.Data) {
			l = len(pkt.Data)
		}
		log.Log(log.TRACE, hex.Dump(pkt.Data[:l]))
	*/

	return
}

func (self *Client) readTCPPacket() (pkt av.Packet, err error) {
	for {
		var res Response
		var ok bool
		for {
			if res, ok, err = self.parseOnePacket(false); err != nil {
				log.Errorf("failed to read rtsp packet, err: %v ", err)
				return
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

	buf, err := self.trans.ReadData()
	if err != nil || len(buf) == 0 {
		return
	}
	first = buf[:1]

	if first[0] == 'R' {
		peek = buf[:4]
		if err != nil {
			return
		}
		if !(peek[0] == 'R' && peek[1] == 'T' && peek[2] == 'S' && peek[3] == 'P') {
			log.Debug("invalid RTSP mark: ", hex.Dump(peek))
			return
		}
		ok = true
		return
	} else if first[0] == '$' {
		self.rectifyPacket(buf)
		size, _, _, _, errRet := self.parseBlockHeader(buf)
		if errRet == io.EOF {
			err = io.EOF
			return
		}

		if errRet != nil {
			// skip
			log.Debug("invalid dollor RTP header: ", hex.Dump(peek))
			return
		}
		res.Block = buf[:4+size]
		ok = true
		return
	} else {
		// skip
		return
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
	if err = self.sendRtpKeepalive(false); err != nil {
		return
	}
	return self.readTCPPacket()
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
		// self.rtspProbeCount++
		// if self.rtspProbeCount > RtspMaxProbeCount {
		// 	err = ErrMaxProbe
		// 	return
		// }
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
			if err = self.Setup(); err != nil {
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

func (s *Client) Describe() (streams []sdp.Media, err error) {
	var (
		res *Response
		req *Request
	)
	for i := 0; i < 2; i++ {
		req, err = NewRequest("DESCRIBE", s.requestUri, s.nextCSeq(), nil)
		if err != nil {
			return
		}
		req.Header.Add("Accept", "application/sdp")
		if res, err = s.sendRequest(req); err != nil {
			return
		}
		if err = s.handleResp(res); err != nil {
			return
		}
		if res.StatusCode != 401 {
			break
		}
	}
	if res.StatusCode < 200 || res.StatusCode > 299 || res.ContentLength <= 0 {
		err = NewRTSPError(res.StatusCode, "DESCRIBE failed")
		return
	}
	bodyb, err := ioutil.ReadAll(res.Body)
	body := string(bodyb)
	log.Debug("DESCRIBE< ", body)

	_, sdp := sdp.Parse(body)
	medias := sdp.Medias

	s.streams = []*Stream{}
	for i, media := range medias {
		// skip unsupport stream eg: applicion
		if !isSupportedMedia(&media) {
			log.Debug("rtsp: unsupported media type: %s %s", media.AVType, media.Type.String())
			continue
		}
		stream := &Stream{Sdp: media, client: s, Idx: i}
		if err := stream.MakeCodecData(nil); err != nil {
			log.Debug("rtsp: stream error: ", err, media)
			// continue
		}

		log.Debug("rtsp: stream %d codec data: %+v", i, stream)
		s.streams = append(s.streams, stream)
		streams = append(streams, media)

		sdp.CodecDatas = append(sdp.CodecDatas, stream.CodecData)
	}

	if s.stage == stageOptionsDone {
		s.stage = stageDescribeDone
	}

	if len(s.streams) == 0 {
		err = ErrNoSupportedStream
		return
	}

	s.sdp = sdp

	return
}

func (s *Client) Options() (err error) {
	var (
		res *Response
		req *Request
	)
	for i := 0; i < 2; i++ {
		req, err = NewRequest("OPTIONS", s.requestUri, s.nextCSeq(), nil)
		if err != nil {
			return
		}
		if res, err = s.sendRequest(req); err != nil {
			return
		}
		if err = s.handleResp(res); err != nil {
			return
		}
		log.Debug("OPTIONS< ", res)
		if res.StatusCode != 401 {
			break
		}
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		err = NewRTSPError(res.StatusCode, "OPTIONS failed")
		return
	}

	s.parsePublic(res)

	if s.stage == 0 {
		s.stage = stageOptionsDone
	}
	return
}

func (self *Client) Setup() (err error) {
	if err = self.prepare(stageDescribeDone); err != nil {
		return
	}
	var (
		res *Response
		req *Request
	)
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
		req, err = NewRequest("SETUP", uri, self.nextCSeq(), nil)
		if err != nil {
			return
		}
		req.Header.Add("Transport", fmt.Sprintf("RTP/AVP/TCP;unicast;interleaved=%d-%d", si*2, si*2+1))
		if res, err = self.sendRequest(req); err != nil {
			return
		}
		if err = self.handleResp(res); err != nil {
			return
		}
		self.parseInterleaved(si, res)
	}

	self.redirectTimes = 0
	self.lastRTCPSent = time.Now()
	if self.stage == stageDescribeDone {
		self.stage = stageSetupDone
	}

	return
}

func (self *Client) allCodecDataReady() bool {
	for _, s := range self.streams {
		if s.CodecData == nil {
			return false
		}
	}
	log.Debug("all codecdata ready")
	return true
}

func (self *Client) Play() (err error) {
	req, err := NewRequest("PLAY", self.requestUri, self.nextCSeq(), nil)
	if err != nil {
		return
	}
	_, err = self.sendRequest(req)
	if err != nil {
		return
	}
	if self.allCodecDataReady() {
		self.stage = stageCodecDataDone
	} else {
		self.stage = stageWaitCodecData
	}
	return
}

func (self *Client) Teardown() (err error) {
	req, err := NewRequest("TEARDOWN", self.requestUri, self.nextCSeq(), nil)
	if err != nil {
		return
	}
	_, err = self.sendRequest(req)
	if err != nil {
		return
	}
	return nil
}

func (self *Client) SDP() (sdp sdp.SDPInfo, err error) {
	if err = self.prepare(stageCodecDataDone); err != nil {
		return
	}
	sdp = self.sdp

	return
}

func (self *Client) ReadPacket() (pkt av.Packet, err error) {
	if err = self.prepare(stageCodecDataDone); err != nil {
		return
	}
	return self.readPacket()
}

type closer struct {
	*bufio.Reader
	r io.Reader
}

func (c closer) Close() error {
	if c.Reader == nil {
		return nil
	}
	defer func() {
		c.Reader = nil
		c.r = nil
	}()
	if r, ok := c.r.(io.ReadCloser); ok {
		return r.Close()
	}
	return nil
}

func ParseRTSPVersion(s string) (proto string, major int, minor int, err error) {
	parts := strings.SplitN(s, "/", 2)
	proto = parts[0]
	parts = strings.SplitN(parts[1], ".", 2)
	if major, err = strconv.Atoi(parts[0]); err != nil {
		return
	}
	if minor, err = strconv.Atoi(parts[0]); err != nil {
		return
	}
	return
}

// super simple RTSP parser; would be nice if net/http would allow more general parsing
func ReadRequest(r io.Reader) (req *Request, err error) {
	req = new(Request)
	req.Header = make(map[string][]string)

	b := bufio.NewReader(r)
	var s string

	// TODO: allow CR, LF, or CRLF
	if s, err = b.ReadString('\n'); err != nil {
		return
	}

	parts := strings.SplitN(s, " ", 3)
	req.Method = parts[0]
	if req.URL, err = url.Parse(parts[1]); err != nil {
		return
	}

	req.Proto, req.ProtoMajor, req.ProtoMinor, err = ParseRTSPVersion(parts[2])
	if err != nil {
		return
	}

	// read headers
	for {
		if s, err = b.ReadString('\n'); err != nil {
			return
		} else if s = strings.TrimRight(s, "\r\n"); s == "" {
			break
		}

		parts := strings.SplitN(s, ":", 2)
		req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}

	req.ContentLength, _ = strconv.Atoi(req.Header.Get("Content-Length"))
	log.Debug("Content Length:", req.ContentLength)
	req.Body = closer{b, r}
	return
}

type Response struct {
	Proto      string
	ProtoMajor int
	ProtoMinor int

	StatusCode int
	Status     string

	ContentLength int64

	Header http.Header
	Body   io.ReadCloser

	Block []byte
}

func (res Response) String() string {
	s := fmt.Sprintf("%s/%d.%d %d %s\n", res.Proto, res.ProtoMajor, res.ProtoMinor, res.StatusCode, res.Status)
	for k, v := range res.Header {
		for _, v := range v {
			s += fmt.Sprintf("%s: %s\n", k, v)
		}
	}
	return s
}

func ReadResponse(buf []byte) (res *Response, err error) {
	res = new(Response)
	res.Header = make(map[string][]string)

	r := bytes.NewReader(buf)
	b := bufio.NewReader(r)
	var s string

	// TODO: allow CR, LF, or CRLF
	if s, err = b.ReadString('\n'); err != nil {
		return
	}

	parts := strings.SplitN(s, " ", 3)
	res.Proto, res.ProtoMajor, res.ProtoMinor, err = ParseRTSPVersion(parts[0])
	if err != nil {
		return
	}

	if res.StatusCode, err = strconv.Atoi(parts[1]); err != nil {
		return
	}

	res.Status = strings.TrimSpace(parts[2])

	// read headers
	for {
		if s, err = b.ReadString('\n'); err != nil {
			return
		} else if s = strings.TrimRight(s, "\r\n"); s == "" {
			break
		}

		parts := strings.SplitN(s, ":", 2)
		res.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
	}

	res.ContentLength, _ = strconv.ParseInt(res.Header.Get("Content-Length"), 10, 64)

	res.Body = closer{b, r}
	return
}
