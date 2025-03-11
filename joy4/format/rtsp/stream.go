package rtsp

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/url"
	"strings"

	"videoplayer/joy4/av"
	"videoplayer/joy4/format/rtsp/rtp"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/log"
)

type Stream struct {
	av.CodecData
	Sdp    sdp.Media
	Idx    int
	client *Client

	remoteHost string

	ctx *rtp.RTPDemuxContext

	udpConns []*net.UDPConn
	udpAddrs []*net.UDPAddr
}

func (self *Stream) setupUDP(uri string) (err error) {
	self.udpConns = FindUDPPair(0) // for default random idle port
	if self.udpConns == nil {
		err = ErrNoUDPPortPair
		return
	}
	self.udpAddrs = make([]*net.UDPAddr, 2)

	if t, perr := url.Parse(uri); perr == nil {
		self.remoteHost = t.Hostname()
	}

	go self.readUDP(0)
	go self.readUDP(1)
	log.Log(log.DEBUG, "rtsp: stream ", self.Idx, ": rtp-rtcp: ",
		self.udpConns[0].LocalAddr(), "-", self.udpConns[1].LocalAddr())
	return nil
}

func (self *Stream) sendPunchInternal(idx int) {
	udpAddr := self.udpAddrs[idx]
	if udpAddr == nil {
		return
	}
	var dummy []byte
	if idx == 0 {
		// small RTP
		dummy = []byte{
			rtp.RTP_VERSION << 6, 0, 0, 0,
			0, 0, 0, 0,
			0, 0, 0, 0,
		}
	} else {
		// small RTCP
		dummy = []byte{rtp.RTP_VERSION << 6, rtp.RTCP_RR, 0, 1,
			0, 0, 0, 0,
		}
	}
	for i := 0; i < 5; i++ {
		self.udpConns[idx].WriteToUDP(dummy, udpAddr)
	}
}

func (self *Stream) sendRR() {
	if len(self.udpConns) == 0 {
		return
	}

	if self.remoteHost == "" || self.udpAddrs[1] == nil {
		return
	}

	if self.ctx == nil {
		return
	}

	buf := self.ctx.GenerateRTCPRR()
	self.udpConns[1].WriteToUDP(buf, self.udpAddrs[1])
}

func (self *Stream) sendPunch(resp *Response) {
	if len(self.udpConns) == 0 {
		return
	}

	if self.remoteHost == "" {
		return
	}

	transport := resp.Headers.Get("Transport")
	for _, e := range strings.Split(transport, ";") {
		var spMin, spMax int
		n, _ := fmt.Sscanf(e, "server_port=%d-%d", &spMin, &spMax)
		if n == 2 {
			udpAddr1, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", self.remoteHost, spMin))
			if err == nil {
				self.udpAddrs[0] = udpAddr1
			}
			udpAddr2, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", self.remoteHost, spMax))
			if err == nil {
				self.udpAddrs[1] = udpAddr2
			}

			self.sendPunchInternal(0)
			self.sendPunchInternal(1)
		}
	}
}

func (self *Stream) readUDP(idx int) {
	b := make([]byte, 65536+4)
	for {
		n, _, err := self.udpConns[idx].ReadFromUDP(b[4:])
		if err != nil {
			log.Log(log.WARN, "rtp: ReadFromUDP: ", err)
			break
		}
		b[0] = '$'
		b[1] = byte(self.Idx*2 + idx)
		binary.BigEndian.PutUint16(b[2:4], uint16(n))

		out := make([]byte, 4+n)
		copy(out, b)

		select {
		case self.client.udpCh <- udpPacket{
			Idx:  idx,
			Data: out,
		}:
		default:
			log.Log(log.WARN, "rtp: drop udp packet, remoteIP:", self.remoteHost)
		}
	}
}

func (self *Stream) rtpUdpPort() int {
	return self.udpConns[0].LocalAddr().(*net.UDPAddr).Port
}

func (self *Stream) rtcpUdpPort() int {
	return self.udpConns[1].LocalAddr().(*net.UDPAddr).Port
}

func (self *Stream) Close() error {
	if self == nil {
		return nil
	}
	log.Log(log.DEBUG, "close stream:", self.remoteHost)
	for _, udp := range self.udpConns {
		if udp != nil {
			udp.Close()
		}
	}

	// self.udpConns = nil
	return nil
}

func (self *Stream) MakeCodecData(buf []byte) (err error) {
	media := self.Sdp

	queueSize := 100
	if self.client != nil && !self.client.UseUDP {
		queueSize = 0
	}

	if self.ctx == nil {
		self.ctx = rtp.NewRTPDemuxContext(media.PayloadType, queueSize)
		switch {
		// Unassigned
		case media.PayloadType >= 35 && media.PayloadType <= 71:
			fallthrough
		case media.PayloadType >= 96 && media.PayloadType <= 127:
			if !self.ctx.SetDynamicHandlerByCodecType(media.Type) {
				err = fmt.Errorf("rtp: unsupported codec type: %v", media.Type)
				return
			}
		default:
			if !self.ctx.SetDynamicHandlerByStaticId(int(media.PayloadType)) {
				err = fmt.Errorf("rtsp: PayloadType=%d unsupported", media.PayloadType)
				return
			}
		}
	}

	if buf == nil {
		if err = self.ctx.DynamicProtocol.ParseSDP(&media); err != nil {
			return
		}
	}

	self.ctx.TimeScale = media.TimeScale
	// https://tools.ietf.org/html/rfc5391
	if self.ctx.TimeScale == 0 {
		self.ctx.TimeScale = self.ctx.DynamicProtocol.DefaultTimeScale()
	}
	if self.ctx.TimeScale == 0 {
		self.ctx.TimeScale = 8000
	}

	// TODO handle codec data change
	if self.CodecData == nil {
		d := self.ctx.DynamicProtocol.CodecData()
		if d != nil {
			self.CodecData = d
		}
	}
	if self.CodecData == nil {
		err = fmt.Errorf("rtp: codec data invalid")
		return
	}

	return
}
