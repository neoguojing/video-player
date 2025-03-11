package rtsp

import (
	"bufio"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/format/rtsp/rtp"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/log"
)

type AuthStage int

const (
	StageAuthEnable = iota + 1
	StageAuthCheck
	StageAuthSuccess
)

type AuthNotify struct {
	Conn       *Conn
	Url        *url.URL
	Stage      AuthStage
	Method     string
	AuthField  string
	EntityBody string
}

// Server for rtsp server class
type Server struct {
	Addr                 string
	WriteTimeout         time.Duration
	ReadTimeout          time.Duration
	HeartbeatTimeout     time.Duration
	MaxFragmentSize      int
	TCPBufferChannelSize int

	HandleAuth      func(*AuthNotify) error
	HandlePublish   func(*Conn, *url.URL) ([]av.CodecData, error) // Deprecated
	HandlePublishV2 func(*Conn, *url.URL) (*sdp.SDPInfo, error)
	HandlePlay      func(*Session) error

	listener *net.TCPListener
	closing  int32
	dead     chan bool
}

type Conn struct {
	netconn     net.Conn
	limitReader *io.LimitedReader
	reader      *bufio.Reader
	writer      *bufio.Writer
	sendCh      chan interface{}
	closeCh     chan bool

	cseq uint

	server    *Server
	auth      *Authorization
	authStage AuthStage

	sessions           map[string]*Session
	lastTimeFromClient int64

	closing int32
}

type SessionEvent int32

const (
	SessionPause SessionEvent = iota + 1
	SessionPlay
	SessionTeardown
)

const (
	maxRTSPSize = 64 * 1024
)

type Session struct {
	Uri  *url.URL
	Conn *Conn

	event chan SessionEvent

	ID string

	subSessions []*SubSession

	state int32
}

type response struct {
	lines []string
	body  []byte
}

type embedResponse struct {
	id   int8
	body []byte
}

func newConn(conn net.Conn, bufSize int) *Conn {
	limitReader := &io.LimitedReader{
		R: conn,
		N: maxRTSPSize,
	}
	auth := &Authorization{
		authType: AuthDigest,
		realm:    defaultRealm,
	}

	return &Conn{
		netconn:     conn,
		limitReader: limitReader,
		reader:      bufio.NewReader(limitReader),
		writer:      bufio.NewWriter(conn),
		sendCh:      make(chan interface{}, bufSize),
		closeCh:     make(chan bool),
		sessions:    make(map[string]*Session),
		auth:        auth,
		authStage:   StageAuthEnable,
	}
}

func (self *Session) IsPlaying() bool {
	return atomic.LoadInt32(&self.state) == int32(SessionPlay)
}

func (self *Session) Events() <-chan SessionEvent {
	return self.event
}

func (self *Session) Streams() (streams []av.CodecData, err error) {
	streams = make([]av.CodecData, len(self.subSessions))
	for i := range self.subSessions {
		streams[i] = self.subSessions[i].stream.Protocol.CodecData()
	}
	return streams, nil
}

func (self *Session) WritePacket(pkt av.Packet) (err error) {
	if !self.IsPlaying() {
		return
	}

	if int(pkt.Idx) >= len(self.subSessions) {
		return
	}
	sub := self.subSessions[pkt.Idx]
	// subsession not setup
	if sub == nil {
		return
	}

	return sub.WritePacket(pkt)
}

func (self *Session) WriteTrailer() (err error) {
	return
}

func (self *Session) WriteHeader(streams []av.CodecData) (err error) {
	return
}

func (self *Session) Close() (err error) {
	for _, v := range self.subSessions {
		v.Close()
	}
	return nil
}

func (self *Session) teardownStream(idx int) {
	if int(idx) >= len(self.subSessions) {
		return
	}
	sub := self.subSessions[idx]
	if sub != nil {
		sub.Close()
	}

	self.subSessions = append(self.subSessions[:idx], self.subSessions[idx+1:]...)
	log.Logf(log.INFO, "teardown specified stream(%v) of session(%v)", idx, self.ID)
}

func (self *Conn) Address() (localAddr string, remoteAddr string) {
	localAddr, remoteAddr = "", ""
	if self.netconn != nil {
		return self.netconn.LocalAddr().String(), self.netconn.RemoteAddr().String()
	}
	return
}

func (self *Conn) Close() (err error) {
	if atomic.SwapInt32(&self.closing, 1) == 0 {
		close(self.closeCh)
	}
	return
}

func getMD5(data string) string {
	w := md5.New()
	if _, err := io.WriteString(w, data); err != nil {
		return ""
	}

	return fmt.Sprintf("%x", w.Sum(nil))
}

func (self *Conn) writeUnauthorizedResp() error {
	code := http.StatusUnauthorized
	nonce := getMD5(time.Now().Format("2006-01-02 15:04:05"))
	if self.auth != nil {
		self.auth.digestParams.nonce = nonce
	}
	wwwAuth := fmt.Sprintf("WWW-Authenticate: %v realm=\"%v\", nonce=\"%v\"", self.auth.authType, defaultRealm, nonce)

	lines := []string{
		fmt.Sprintf("RTSP/1.0 %d %s", code, http.StatusText(code)),
		fmt.Sprintf("CSeq: %d", self.cseq),
		wwwAuth,
	}
	return self.writeResponse(response{
		lines: lines,
	})
}

func (self *Conn) writeStatus(code int) error {
	log.Log(log.DEBUG, "rtsp: write status: ", code)
	lines := []string{
		fmt.Sprintf("RTSP/1.0 %d %s", code, http.StatusText(code)),
		fmt.Sprintf("CSeq: %d", self.cseq),
		fmt.Sprintf("Public: OPTIONS, DESCRIBE, SETUP, TEARDOWN, PLAY"),
	}
	return self.writeResponse(response{
		lines: lines,
	})
}

func (self *Conn) writeError(err error) {
	if err == nil {
		self.writeStatus(http.StatusOK)
		return
	}
	if rerr, ok := err.(*RTSPError); ok {
		self.writeStatus(rerr.Code)
		return
	}
	self.writeStatus(http.StatusBadRequest)
}

func (self *Conn) publish(uri *url.URL, headers textproto.MIMEHeader) (*sdp.SDPInfo, error) {
	var sdpInfo *sdp.SDPInfo
	var streams []av.CodecData
	var err error
	if self.server.HandlePublishV2 != nil {
		sdpInfo, err = self.server.HandlePublishV2(self, uri)
	} else if self.server.HandlePublish != nil {
		streams, err = self.server.HandlePublish(self, uri)
		if err == nil {
			sdpInfo = &sdp.SDPInfo{
				CodecDatas: streams,
			}
		}
	}

	if err != nil {
		switch v := err.(type) {
		case ErrRedirect:
			lines := []string{
				fmt.Sprintf("RTSP/1.0 %d %s", http.StatusMovedPermanently, http.StatusText(http.StatusMovedPermanently)),
				fmt.Sprintf("CSeq: %d", self.cseq),
				fmt.Sprintf("Location: %s", v.URL),
			}
			err = self.writeResponse(response{lines: lines})

		default:
			self.writeError(err)
		}
		return nil, err
	}

	if sdpInfo.CodecDatas == nil {
		return nil, self.writeStatus(http.StatusBadRequest)
	}
	return sdpInfo, nil
}

func (self *Conn) doOptions(uri *url.URL, headers textproto.MIMEHeader) error {
	if uri != nil {
		if self.server.HandlePublishV2 != nil {
			if _, err := self.publish(uri, headers); err != nil {
				return err
			}
		} else if self.server.HandlePublish != nil {
			if _, err := self.publish(uri, headers); err != nil {
				return err
			}
		}
	}
	return self.writeStatus(http.StatusOK)
}

func sdpType(t av.CodecType) string {
	if t.IsVideo() {
		return "video"
	} else if t.IsAudio() {
		return "audio"
	} else {
		return "unknown"
	}
}

func (self *Conn) processAuth(method string, uri *url.URL, headers textproto.MIMEHeader) (code int) {

	if self.server.HandleAuth == nil { // No authentication check required
		return http.StatusOK
	}
	code = http.StatusOK

	switch self.authStage {
	case StageAuthEnable:
		auth := &AuthNotify{
			Conn:      self,
			Url:       uri,
			Stage:     StageAuthEnable,
			Method:    method,
			AuthField: "",
		}
		if err := self.server.HandleAuth(auth); err != nil {
			code = http.StatusUnauthorized
			self.authStage = StageAuthCheck
		} else {
			self.authStage = StageAuthSuccess
		}
	case StageAuthCheck:
		authval := headers.Get("Authorization")
		ai := self.auth
		auth, errp := parseAuthorization(authval)
		if errp != nil || ai.authType != auth.authType ||
			ai.realm != auth.realm || ai.digestParams.nonce != auth.digestParams.nonce {
			log.Logf(log.ERROR, "mismatch auth info, local auth(%#v), recv auth(%#v)", *ai, *auth)
			code = http.StatusUnauthorized
		} else {
			auth := &AuthNotify{
				Conn:      self,
				Url:       uri,
				Stage:     StageAuthCheck,
				Method:    method,
				AuthField: authval,
			}
			if err := self.server.HandleAuth(auth); err == nil {
				self.authStage = StageAuthSuccess
			} else {
				code = http.StatusUnauthorized
			}
		}
	case StageAuthSuccess:
	default:
		log.Logf(log.WARN, "abnormal auth stage(%v)", self.authStage)
		code = http.StatusBadRequest
	}

	return code
}

func (self *Conn) doDescribe(uri *url.URL, headers textproto.MIMEHeader) error {

	if code := self.processAuth("DESCRIBE", uri, headers); code != http.StatusOK {
		if code == http.StatusUnauthorized {
			if err := self.writeUnauthorizedResp(); err != nil {
				log.Logf(log.WARN, "Faild to writeUnauthorizedResp with error(%v)", code)
			}
		} else {
			if err := self.writeStatus(code); err != nil {
				log.Logf(log.WARN, "Faild to writeStatus with error(%v)", code)
			}
		}

		return nil // ignore error
	}

	sdpInfo, err := self.publish(uri, headers)
	if err != nil {
		return err
	}

	if sdpInfo.CodecDatas == nil {
		return fmt.Errorf("emptey codec data")
	}

	streams := sdpInfo.CodecDatas

	sdps := make([]string, 0)
	sdps = append(sdps, "v=0")

	if sdpInfo.RangeEnd > sdpInfo.RangeStart {
		sdps = append(sdps, fmt.Sprintf("a=range:npt=%v-%v", sdpInfo.RangeStart, sdpInfo.RangeEnd))
	}

	for typeVal, attrs := range sdpInfo.ExtraLines {
		for _, attr := range attrs {
			if !strings.HasPrefix(attr, "v=") && !strings.HasPrefix(attr, "a=range") {
				sdps = append(sdps, fmt.Sprintf("%s=%v", typeVal, attr))
			}
		}
	}

	for i := range streams {
		dp := rtp.NewRTPMuxContextFromCodecData(streams[i])
		if dp == nil {
			continue
		}
		pt := dp.Protocol.PayloadType()

		log.Logf(log.TRACE, "stream: %+v\n", streams[i])
		sdps = append(sdps, fmt.Sprintf("m=%s 0 RTP/AVP %d", sdpType(streams[i].Type()), pt))
		sdps = append(sdps, fmt.Sprintf("a=control:streamid=%d", i))

		fmtp := dp.Protocol.SDPLines()
		if len(fmtp) > 0 {
			sdps = append(sdps, fmtp...)
		}
	}
	sdps = append(sdps, "\r\n")

	buf := strings.Join(sdps, "\r\n")

	lines := []string{
		"RTSP/1.0 200 OK",
		fmt.Sprintf("CSeq: %d", self.cseq),
		fmt.Sprintf("Content-Base: %s/", uri.String()),
		"Content-Type: application/sdp",
		fmt.Sprintf("Content-Length: %d", len(buf)),
	}
	return self.writeResponse(response{
		lines: lines,
		body:  []byte(buf),
	})
}

func (self *Conn) newSession(uri *url.URL, count int) *Session {
	var sessionID string
	for {
		sessionID = fmt.Sprint(rand.Int())
		if _, ok := self.sessions[sessionID]; !ok {
			break
		}
	}
	log.Log(log.DEBUG, "new session: ", uri, ", sessionID=", sessionID)

	return &Session{
		Uri:         uri,
		Conn:        self,
		event:       make(chan SessionEvent, 8),
		ID:          sessionID,
		subSessions: make([]*SubSession, count),
		state:       int32(SessionPause),
	}
}

func parseControl(uri *url.URL) int {
	arr := strings.Split(uri.Path, "/")
	if len(arr) > 0 {
		last := arr[len(arr)-1]
		var control int
		n, err := fmt.Sscanf(last, "streamid=%d", &control)
		if err == nil && n == 1 {
			return control
		}
	}
	return 0
}

func (self *Conn) doSetup(uri *url.URL, headers textproto.MIMEHeader) error {
	idx := parseControl(uri)

	transportParams := strings.Split(headers.Get("Transport"), ";")
	isUDP := true
	if len(transportParams) == 0 {
		self.writeStatus(http.StatusBadRequest)
		return nil
	}
	if transportParams[0] == "RTP/AVP/TCP" {
		isUDP = false
	} else if transportParams[0] == "RTP/AVP/UDP" || transportParams[0] == "RTP/AVP" {
		isUDP = true
	} else {
		self.writeStatus(http.StatusNotImplemented)
		return nil
	}
	// check
	sdpInfo, err := self.publish(uri, headers)
	if err != nil {
		return err
	}
	streams := sdpInfo.CodecDatas

	if idx >= len(streams) {
		return self.writeStatus(http.StatusNotFound)
	}

	mux := rtp.NewRTPMuxContextFromCodecData(streams[idx])
	if mux == nil {
		return self.writeStatus(http.StatusNotFound)
	}

	var session *Session
	sessionID := getSessionID(headers)
	if sessionID == "" {
		session = self.newSession(uri, len(streams))
		sessionID = session.ID
	} else {
		session = self.findSession(headers)
		if session == nil {
			return self.writeStatus(http.StatusBadRequest)
		}
	}

	if session.subSessions[idx] != nil {
		log.Logf(log.WARN, "session %s, stream %d already setup", sessionID, idx)
		return self.writeStatus(http.StatusBadRequest)
	}

	sub := &SubSession{
		conn:   self,
		stream: mux,
		isUDP:  isUDP,
	}

	var p1, p2 uint16
	if isUDP {
		for _, v := range transportParams {
			arr := strings.SplitN(strings.TrimSpace(v), "=", 2)
			if len(arr) != 2 {
				continue
			}
			n, err := fmt.Sscanf(arr[1], "%d-%d", &p1, &p2)
			if err != nil || n != 2 {
				continue
			}
		}
		if p1 == 0 || p2 == 0 {
			self.writeStatus(http.StatusBadRequest)
			return nil
		}
		addr := self.netconn.RemoteAddr().(*net.TCPAddr)
		if err := sub.setupUDP(addr.IP.String(), p1, p2); err != nil {
			self.writeStatus(http.StatusInternalServerError)
			return nil
		}
	}

	session.subSessions[idx] = sub
	self.sessions[session.ID] = session

	transport := ""
	if isUDP {
		// sp1 := session.rtpSock.LocalAddr().(*net.UDPAddr).Port
		sp1 := sub.udpServerSocks[0].LocalAddr().(*net.UDPAddr).Port
		sp2 := sub.udpServerSocks[1].LocalAddr().(*net.UDPAddr).Port
		transport = fmt.Sprintf("Transport: RTP/AVP/UDP;unicast;client_port=%d-%d;server_port=%d-%d", p1, p2, sp1, sp2)
	} else {
		transport = fmt.Sprintf("Transport: RTP/AVP/TCP;unicast;interleaved=%d-%d", idx*2, idx*2+1)
	}

	sessionLine := ""
	if self.server.HeartbeatTimeout > 0 {
		sessionLine = fmt.Sprintf("Session: %s;timeout=%d", sessionID, int(self.server.HeartbeatTimeout.Seconds()))
	} else {
		sessionLine = fmt.Sprintf("Session: %s", sessionID)
	}

	lines := []string{
		"RTSP/1.0 200 OK",
		fmt.Sprintf("CSeq: %d", self.cseq),
		sessionLine,
		transport,
	}
	return self.writeResponse(response{
		lines: lines,
	})
}

func getSessionID(headers textproto.MIMEHeader) string {
	session := headers.Get("Session")
	if session == "" {
		return ""
	}
	arr := strings.Split(session, ";")
	return strings.TrimSpace(arr[0])
}

func (self *Conn) findSession(headers textproto.MIMEHeader) *Session {
	sessionID := getSessionID(headers)
	if sessionID == "" {
		return nil
	}
	session, ok := self.sessions[sessionID]
	if !ok {
		return nil
	}

	return session
}

func (self *Conn) doPlay(uri *url.URL, headers textproto.MIMEHeader) (err error) {
	session := self.findSession(headers)
	if session == nil {
		self.writeStatus(http.StatusBadRequest)
		return
	}
	if self.server.HandlePlay != nil {
		err = self.server.HandlePlay(session)
		if err != nil {
			self.writeError(err)
			return
		}
	}

	self.writeStatus(http.StatusOK)
	atomic.StoreInt32(&session.state, int32(SessionPlay))
	session.event <- SessionPlay
	return
}

func (self *Conn) doTeardown(uri *url.URL, headers textproto.MIMEHeader) (err error) {
	session := self.findSession(headers)
	if session == nil {
		self.writeStatus(http.StatusBadRequest)
		return
	}

	// specified streamid, remove the related subsession
	if session.Uri.Path != uri.Path {
		idx := parseControl(uri)
		session.teardownStream(idx)
		if len(session.subSessions) > 0 {
			return
		}
	}
	// otherwise, or all subsessions removed, close this session
	atomic.StoreInt32(&session.state, int32(SessionTeardown))
	session.event <- SessionTeardown
	close(session.event)
	delete(self.sessions, session.ID)
	self.writeStatus(http.StatusOK)
	return
}

func (self *Conn) dispatch() error {
	reader := textproto.NewReader(self.reader)
	line, err := reader.ReadLine()
	if err != nil {
		return err
	}
	var cmd, raw, proto string
	n, err := fmt.Sscanf(line, "%s %s %s", &cmd, &raw, &proto)
	if err != nil {
		return err
	}
	if n != 3 || proto != "RTSP/1.0" {
		log.Log(log.ERROR, "bad request: \n", hex.Dump([]byte(line)))
		return errors.New("invalid request")
	}
	var uri *url.URL
	if !(cmd == "OPTIONS" && raw == "*") {
		uri, err = url.Parse(raw)
	}
	if err != nil {
		self.writeStatus(http.StatusBadRequest)
		return nil
	}

	headers, err := reader.ReadMIMEHeader()
	if err != nil {
		return err
	}
	log.Log(log.TRACE, line, headers)

	if cseq := headers.Get("Cseq"); cseq != "" {
		cseq, err := strconv.Atoi(cseq)
		if err != nil {
			return err
		}
		self.cseq = uint(cseq)
	}

	switch cmd {
	case "OPTIONS":
		err = self.doOptions(uri, headers)
	case "DESCRIBE":
		err = self.doDescribe(uri, headers)
	case "SETUP":
		err = self.doSetup(uri, headers)
	case "PLAY":
		err = self.doPlay(uri, headers)
	case "TEARDOWN":
		err = self.doTeardown(uri, headers)
	default:
		self.writeStatus(http.StatusNotImplemented)
	}
	if err != nil {
		log.Log(log.WARN, "rtsp server commond error: ", err)
	}
	return err
}

func (self *Conn) writeEmbeded(id int8, body []byte) (err error) {
	if atomic.LoadInt32(&self.closing) != 0 {
		return io.ErrClosedPipe
	}

	select {
	case self.sendCh <- embedResponse{id: id, body: body}:
		return nil
	default:
		// drop current packet
		log.Log(log.TRACE, "short write")
		return io.ErrShortWrite
	}
}

func (self *Conn) netWriteEmbeded(r embedResponse) (err error) {
	w := self.writer
	// frame header
	if len(r.body) > 65535 {
		err = errors.New("rtp frame too large")
		return
	}
	err = binary.Write(w, binary.BigEndian, uint8(0x24))
	err = binary.Write(w, binary.BigEndian, r.id)
	size := uint16(len(r.body))
	err = binary.Write(w, binary.BigEndian, size)

	_, err = w.Write(r.body)
	if err != nil {
		return
	}
	err = w.Flush()
	return
}

func (self *Conn) writeResponse(v response) (err error) {
	if atomic.LoadInt32(&self.closing) != 0 {
		return io.ErrClosedPipe
	}

	timer := time.NewTimer(self.server.WriteTimeout)
	defer timer.Stop()

	if self.server.WriteTimeout > 0 {
		select {
		case self.sendCh <- v:
			return nil
		case <-timer.C:
			return io.ErrUnexpectedEOF
		}
	} else {
		self.sendCh <- v
		return nil
	}
}

func (self *Conn) netWriteResponse(v response) (err error) {
	for _, line := range v.lines {
		_, err = self.writer.WriteString(line + "\r\n")
		if err != nil {
			break
		}
	}
	_, err = self.writer.WriteString("\r\n")
	if err != nil {
		return
	}
	if v.body != nil {
		_, err = self.writer.Write(v.body)
		if err != nil {
			return
		}
	}
	err = self.writer.Flush()
	return
}

func (self *Conn) markLastTimeFromClient() {
	atomic.StoreInt64(&self.lastTimeFromClient, rtp.NtpTime())
}

func (self *Conn) hasHeartbeatTimeout() bool {
	if self.server.HeartbeatTimeout <= 0 {
		return false
	}
	t := atomic.LoadInt64(&self.lastTimeFromClient)
	return rtp.NtpTime()-t > int64(self.server.HeartbeatTimeout/time.Microsecond)
}

func (self *Conn) readLoop() error {
	hasHeartbeat := self.server.HeartbeatTimeout > 0
	for {
		if hasHeartbeat {
			self.netconn.SetReadDeadline(time.Now().Add(self.server.HeartbeatTimeout))
		}
		self.limitReader.N = maxRTSPSize
		peek, err := self.reader.Peek(4)
		if hasHeartbeat {
			if err, ok := err.(net.Error); ok && err.Timeout() {
				if !self.hasHeartbeatTimeout() {
					continue
				} else {
					log.Log(log.ERROR, "rtsp heartbeat timeout after ", self.server.HeartbeatTimeout)
					return err
				}
			}
		}
		if err != nil {
			return err
		}

		self.markLastTimeFromClient()
		if self.server.ReadTimeout > 0 {
			self.netconn.SetReadDeadline(time.Now().Add(self.server.ReadTimeout))
		} else {
			self.netconn.SetReadDeadline(time.Time{})
		}
		if peek[0] >= 'A' && peek[0] <= 'Z' {
			if err := self.dispatch(); err != nil {
				return err
			}
		} else if peek[0] == '$' {
			peek, err := self.reader.Peek(4 + 8)
			if err != nil {
				return err
			}
			// ch := peek[1]
			size := int(binary.BigEndian.Uint16(peek[2:]))
			header := peek[4:]
			if header[0]&0xc0 != 0x80 {
				return errors.New("invalid RTCP packet")
			}
			if _, err := self.reader.Discard(4 + size); err != nil {
				return err
			}
			continue
		} else {
			return errors.New("invalid RTSP packet")
		}
	}
}

func (self *Conn) writeLoop() {
	var err error
Loop:
	for {
		select {
		case <-self.closeCh:
			err = io.ErrClosedPipe
			break Loop
		case req := <-self.sendCh:
			if self.server.WriteTimeout != 0 {
				self.netconn.SetWriteDeadline(time.Now().Add(self.server.WriteTimeout))
			}
			switch v := req.(type) {
			case embedResponse:
				err = self.netWriteEmbeded(v)
			case response:
				err = self.netWriteResponse(v)
			default:
				panic("no supported")
			}
			if err != nil {
				break Loop
			}
		}
	}
	if err != nil {
		log.Log(log.ERROR, "write error: ", err)
	}
	err = self.netconn.Close()
	log.Log(log.INFO, "rtsp connection to ", self.netconn.RemoteAddr().String(), " closed: ", err)
}

func (self *Server) handleConn(conn *Conn) (err error) {
	log.Log(log.INFO, conn.netconn.RemoteAddr().String(), " connected")

	go conn.writeLoop()
	err = conn.readLoop()

	log.Log(log.INFO, conn.netconn.RemoteAddr().String(), " disconnected: ", err)

	for _, v := range conn.sessions {
		atomic.StoreInt32(&v.state, int32(SessionTeardown))
		v.event <- SessionTeardown
		close(v.event)
		v.Close()
	}

	return conn.Close()
}

func (self *Server) ListenAndServe() (err error) {
	addr := self.Addr
	if addr == "" {
		addr = ":554"
	}
	var tcpaddr *net.TCPAddr
	if tcpaddr, err = net.ResolveTCPAddr("tcp", addr); err != nil {
		err = fmt.Errorf("rtsp: ListenAndServe: %s", err)
		return
	}

	var listener *net.TCPListener
	if listener, err = net.ListenTCP("tcp", tcpaddr); err != nil {
		return
	}
	self.listener = listener

	log.Log(log.DEBUG, "rtsp: server: listening on: ", addr)

	for atomic.LoadInt32(&self.closing) == 0 {
		var netconn net.Conn

		listener.SetDeadline(time.Now().Add(1 * time.Second))
		if netconn, err = listener.Accept(); err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			log.Log(log.DEBUG, "rtsp: server: ", err)
			return
		}

		log.Log(log.DEBUG, "rtsp: server: accepted")

		conn := newConn(netconn, self.TCPBufferChannelSize)
		conn.server = self
		go func() {
			err := self.handleConn(conn)
			log.Log(log.DEBUG, "rtsp: server: client closed err:", err)
		}()
	}
	log.Log(log.INFO, "rtsp: server accept stopped")
	err = self.listener.Close()
	self.dead <- true
	return err
}

func (self *Server) Close() (err error) {
	atomic.StoreInt32(&self.closing, 1)
	<-self.dead
	return nil
}

func NewServer(addr string) *Server {
	return &Server{
		Addr:             addr,
		WriteTimeout:     10 * time.Second,
		ReadTimeout:      10 * time.Second,
		HeartbeatTimeout: 65 * time.Second,
		MaxFragmentSize:  1450,
		// buffer ~2.5 seconds
		TCPBufferChannelSize: 1024,

		dead: make(chan bool, 1),
	}
}
