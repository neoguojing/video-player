package rtsp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec/mjpegparser"
	"videoplayer/joy4/format/rtsp/rtp"
	"videoplayer/joy4/log"

	"github.com/32bitkid/bitreader"
)

type SubSession struct {
	conn   *Conn
	stream *rtp.RTPMuxContext
	isUDP  bool

	rtpSock  net.Conn
	rtcpSock net.Conn

	udpServerSocks []*net.UDPConn
}

// writeSRPacket write SR packet(refer by: https://github.com/FFmpeg/FFmpeg/blob/release/4.2/libavformat/rtpenc.c#L285)
//
//	idx:       the stream index
//	timeBase:  time base
//	ntpTime:   NTP time
//	cName:     Canonical End-Point Identifier SDES Item ?It can be empty?
//	bye:       Is bye package
//	isEOF:     Fill EOF flag(0xFFFFFFFF) for true, otherwise fill SSRC
func (self *SubSession) writeSRPacket(idx int8, timeBase int64, ntpTime int64, cName string, isBye, isEOF bool) (err error) {
	mux := self.stream
	log.Log(log.TRACE, "RTCP: ", ntpTime, mux.Timestamp)
	mux.LastRtcpNtpTime = ntpTime
	rtpTs := (ntpTime-mux.FirstRtcpNtpTime)*timeBase/1000000 + int64(mux.BaseTimestamp)
	w := new(bytes.Buffer)
	_ = binary.Write(w, binary.BigEndian, byte(rtp.RTP_VERSION<<6))
	_ = binary.Write(w, binary.BigEndian, byte(rtp.RTCP_SR))
	// length in words - 1
	_ = binary.Write(w, binary.BigEndian, uint16(6))
	// SSRC
	_ = binary.Write(w, binary.BigEndian, mux.SSRC)
	_ = binary.Write(w, binary.BigEndian, uint32(ntpTime/1000000))
	_ = binary.Write(w, binary.BigEndian, (uint32(ntpTime%1000000)<<32)/1000000)
	_ = binary.Write(w, binary.BigEndian, uint32(rtpTs))
	_ = binary.Write(w, binary.BigEndian, uint32(mux.PacketCount))
	_ = binary.Write(w, binary.BigEndian, uint32(mux.OctetCount))

	// CNAME
	if nameLen := len(cName); nameLen > 0 {
		if nameLen > 255 {
			nameLen = 255
		}
		_ = binary.Write(w, binary.BigEndian, byte(rtp.RTP_VERSION<<6)+1)
		_ = binary.Write(w, binary.BigEndian, byte(rtp.RTCP_SDES))
		// length in words - 1
		_ = binary.Write(w, binary.BigEndian, uint16((7+nameLen+3)/4))
		// SSRC
		_ = binary.Write(w, binary.BigEndian, mux.SSRC)
		_ = binary.Write(w, binary.BigEndian, byte(0x01))
		// CNAME
		_ = binary.Write(w, binary.BigEndian, byte(nameLen))
		_ = binary.Write(w, binary.BigEndian, []byte(cName))
		// END
		_ = binary.Write(w, binary.BigEndian, byte(0))
		for nameLen = (7 + nameLen) % 4; (nameLen % 4) != 0; nameLen++ {
			_ = binary.Write(w, binary.BigEndian, byte(0))
		}
	}

	// BYE
	if isBye {
		_ = binary.Write(w, binary.BigEndian, byte((rtp.RTP_VERSION<<6)|1))
		_ = binary.Write(w, binary.BigEndian, byte(rtp.RTCP_BYE))
		// length in words - 1
		_ = binary.Write(w, binary.BigEndian, uint16(1))
		if isEOF {
			// Extension for EOF flag(0xFFFFFFFF)
			_ = binary.Write(w, binary.BigEndian, uint32(rtp.RTCP_EOF_FLAG))
		} else {
			// SSRC
			_ = binary.Write(w, binary.BigEndian, mux.SSRC)
		}
	}

	err = self.writePacket(2*idx+1, w.Bytes())
	if err != nil {
		return
	}
	return nil
}

func (self *SubSession) writeRTPHeader(w io.Writer, pt uint8, marker bool) (err error) {
	mux := self.stream
	// rtp header
	err = binary.Write(w, binary.BigEndian, uint8(0x80))
	m := uint8(0)
	if marker {
		m = 0x80
	}
	err = binary.Write(w, binary.BigEndian, (pt&0x7f)|m)
	// sequence
	err = binary.Write(w, binary.BigEndian, uint16(mux.Seq))
	mux.Seq++
	// XXX timestamp should start randomly
	err = binary.Write(w, binary.BigEndian, mux.Timestamp)
	// SSRC
	err = binary.Write(w, binary.BigEndian, mux.SSRC)
	return
}

func (self *SubSession) writeFLVH264Packet(pkt av.Packet) (err error) {
	/*
		FU-A H264 https://tools.ietf.org/html/rfc3984

		0                   1                   2                   3
		0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		| FU indicator  |   FU header   |                               |
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               |
		|                                                               |
		|                         FU payload                            |
		|                                                               |
		|                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		|                               :...OPTIONAL RTP padding        |
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		Figure 14.  RTP payload format for FU-A

		The FU indicator octet has the following format:
		+---------------+
		|0|1|2|3|4|5|6|7|
		+-+-+-+-+-+-+-+-+
		|F|NRI|  Type   |
		+---------------+


		The FU header has the following format:
		+---------------+
		|0|1|2|3|4|5|6|7|
		+-+-+-+-+-+-+-+-+
		|S|E|R|  Type   |
		+---------------+

		S: 1 bit
		When set to one, the Start bit indicates the start of a fragmented
		NAL unit.  When the following FU payload is not the start of a
		fragmented NAL unit payload, the Start bit is set to zero.

		E: 1 bit
		When set to one, the End bit indicates the end of a fragmented NAL
		unit, i.e., the last byte of the payload is also the last byte of
		the fragmented NAL unit.  When the following FU payload is not the
		last fragment of a fragmented NAL unit, the End bit is set to
		zero.

		R: 1 bit
		The Reserved bit MUST be equal to 0 and MUST be ignored by the
		receiver.

		Type: 5 bits
		The NAL unit payload type as defined in table 7-1 of [1].
	*/

	if len(pkt.Data) < 1 {
		return
	}
	// strip first 4 bytes for flv nalu size
	data := pkt.Data
	// fmt.Println("write ", pkt.Idx, pkt.Time, len(pkt.Data))

	nalFirst := data[0]
	nalType := nalFirst & 0x1f

	/*
		Table 7-1 – NAL unit type codes
		1   ￼Coded slice of a non-IDR picture
		5    Coded slice of an IDR picture
		6    Supplemental enhancement information (SEI)
		7    Sequence parameter set
		8    Picture parameter set
		1-23     NAL unit  Single NAL unit packet             5.6
		24       STAP-A    Single-time aggregation packet     5.7.1
		25       STAP-B    Single-time aggregation packet     5.7.1
		26       MTAP16    Multi-time aggregation packet      5.7.2
		27       MTAP24    Multi-time aggregation packet      5.7.2
		28       FU-A      Fragmentation unit                 5.8
		29       FU-B      Fragmentation unit                 5.8
		30-31    reserved                                     -
	*/
	if nalType > 8 {
		return
	}
	maxFragmentSize := self.conn.server.MaxFragmentSize
	if maxFragmentSize == 0 {
		maxFragmentSize = 65000
	}
	proto := self.stream.Protocol.(*rtp.H264DynamicProtocol)
	mux := self.stream

	mux.Timestamp = mux.CurTimestamp

	if len(data) <= maxFragmentSize+1 {
		w := new(bytes.Buffer)
		err = self.writeRTPHeader(w, uint8(proto.PayloadType()), true)
		if err != nil {
			return
		}
		_, err = w.Write(pkt.Data)
		if err != nil {
			return
		}
		buf := w.Bytes()
		return self.writePacket(2*pkt.Idx, buf)
	}

	// FU-A fragmented
	first := true
	for start := 1; start < len(data); start += maxFragmentSize {
		w := new(bytes.Buffer)
		fragSize := len(data) - start
		if fragSize > maxFragmentSize {
			fragSize = maxFragmentSize
		}
		end := (start + fragSize) == len(data)

		// timestamp (90k / fps)
		// XXX timestamp should start randomly
		err = self.writeRTPHeader(w, uint8(proto.PayloadType()), end)
		if err != nil {
			return
		}

		// fmt.Print(hex.Dump(data[:4]))
		fuIndicator := byte((nalFirst & 0xe0) | 28)
		fuHeader := uint8(nalFirst & 0x1f)
		if first {
			fuHeader |= 0x80
			first = false
		}
		if end {
			fuHeader |= 0x40
		}
		err = w.WriteByte(fuIndicator)
		err = w.WriteByte(fuHeader)
		_, err = w.Write(data[start : start+fragSize])
		if err != nil {
			return
		}

		buf := w.Bytes()
		err = self.writePacket(2*pkt.Idx, buf)
		if err != nil {
			return
		}
	}
	return
}

func (self *SubSession) writeFLVH265Packet(pkt av.Packet) (err error) {
	// H265 Nalu header size == 2
	if len(pkt.Data) < 2 {
		return
	}

	maxFragmentSize := self.conn.server.MaxFragmentSize
	if maxFragmentSize == 0 {
		maxFragmentSize = 65000
	}
	proto := self.stream.Protocol.(*rtp.H265DynamicProtocol)
	mux := self.stream

	mux.Timestamp = mux.CurTimestamp

	data := pkt.Data

	if len(data) <= maxFragmentSize+1 {
		w := new(bytes.Buffer)
		err = self.writeRTPHeader(w, uint8(proto.PayloadType()), true)
		if err != nil {
			return
		}
		_, err = w.Write(pkt.Data)
		if err != nil {
			return
		}
		buf := w.Bytes()
		return self.writePacket(2*pkt.Idx, buf)
	}

	// FU-A
	nalType := (data[0] >> 1) & 0x3f
	first := true
	/* pass the original NAL header */
	for start := 2; start < len(data); start += maxFragmentSize {
		w := new(bytes.Buffer)
		fragSize := len(data) - start
		if fragSize > maxFragmentSize {
			fragSize = maxFragmentSize
		}
		end := (start + fragSize) == len(data)

		// timestamp (90k / fps)
		// XXX timestamp should start randomly
		err = self.writeRTPHeader(w, uint8(proto.PayloadType()), end)
		if err != nil {
			return
		}

		/*
		 * create the HEVC payload header and transmit the buffer as fragmentation units (FU)
		 *
		 *    0                   1
		 *    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5
		 *   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		 *   |F|   Type    |  LayerId  | TID |
		 *   +-------------+-----------------+
		 *
		 *      F       = 0
		 *      Type    = 49 (fragmentation unit (FU))
		 *      LayerId = 0
		 *      TID     = 1
		 */
		b0 := byte(49 << 1)
		b1 := byte(1)

		/*
		 *     create the FU header
		 *
		 *     0 1 2 3 4 5 6 7
		 *    +-+-+-+-+-+-+-+-+
		 *    |S|E|  FuType   |
		 *    +---------------+
		 *
		 *       S       = variable
		 *       E       = variable
		 *       FuType  = NAL unit type
		 */

		b2 := nalType
		/* set the S bit: mark as start fragment */
		if first {
			b2 |= 1 << 7
			first = false
		}
		if end {
			b2 |= 1 << 6
		}
		err = w.WriteByte(b0)
		err = w.WriteByte(b1)
		err = w.WriteByte(b2)
		_, err = w.Write(data[start : start+fragSize])
		if err != nil {
			return
		}

		buf := w.Bytes()
		err = self.writePacket(2*pkt.Idx, buf)
		if err != nil {
			return
		}
	}

	return
}

func (self *SubSession) writeMJPEGPacket(pkt av.Packet) (err error) {

	size := len(pkt.Data)
	if size < 1 {
		return
	}

	proto := self.stream.Protocol.(*rtp.JPEGDynamicProtocol)
	mux := self.stream

	mux.Timestamp = mux.CurTimestamp
	codeData := proto.CodecData().(mjpegparser.CodecData)

	/* jpeg RTP mux
	base by:
		https://tools.ietf.org/html/rfc2435
	    https://github.com/FFmpeg/FFmpeg/blob/master/libavformat/rtpenc_jpeg.c#L28
	*/

	headerType := uint8(1)
	maxPayloadSize := 1400
	var qtables [4]int
	nbQtables := 0
	defaultHuffmanTables := 0
	i := 0
	dataLen := 0
	offset := 0

	// preparse the header for getting some info
	for i = 0; i < size; i++ {
		if pkt.Data[i] != 0xff {
			continue
		}

		if int(pkt.Data[i+1]) == mjpegparser.DQT {
			var tables int
			var j int
			if (byte(pkt.Data[i+4]) & 0xF0) != 0 {
				log.Logf(log.WARN, "Only 8-bit precision is supported.")
			}

			// a quantization table is 64 bytes long
			tables = int(binary.BigEndian.Uint16(pkt.Data[i+2:i+4]) / 65)
			if i+5+tables*65 > size {
				log.Logf(log.WARN, "Only 8-bit precision is supported.")
				return
			}
			if nbQtables+tables > 4 {
				log.Logf(log.ERROR, "Invalid number of quantisation tables.")
				return
			}

			for j = 0; j < tables; j++ {
				qtables[nbQtables+j] = i + 5 + j*65
			}

			nbQtables += tables
		} else if int(pkt.Data[i+1]) == mjpegparser.SOF0 {
			if pkt.Data[i+14] != 17 || pkt.Data[i+17] != 17 {
				log.Logf(log.ERROR, "Only 1x1 chroma blocks are supported. Aborted!")
				return
			}
		} else if int(pkt.Data[i+1]) == mjpegparser.DHT {
			DHTSize := binary.BigEndian.Uint16(pkt.Data[i+2 : i+4])
			defaultHuffmanTables |= 1 << 4
			i += 3
			DHTSize -= 2
			if i+int(DHTSize) >= size {
				continue
			}

			for DHTSize > 0 {
				switch int(pkt.Data[i+1]) {
				case 0x00:
					if DHTSize >= 29 &&
						bytes.Compare(pkt.Data[i+2:i+18], mjpegparser.Avpriv_mjpeg_bits_dc_luminance[1:]) == 0 &&
						bytes.Compare(pkt.Data[i+18:i+30], mjpegparser.Avpriv_mjpeg_val_dc[:]) == 0 {
						defaultHuffmanTables |= 1
						i += 29
						DHTSize -= 29
					} else {
						i += int(DHTSize)
						DHTSize = 0
					}
				case 0x01:
					if DHTSize >= 29 &&
						bytes.Compare(pkt.Data[i+2:i+18], mjpegparser.Avpriv_mjpeg_bits_dc_chrominance[1:]) == 0 &&
						bytes.Compare(pkt.Data[i+18:i+30], mjpegparser.Avpriv_mjpeg_val_dc[:]) == 0 {
						defaultHuffmanTables |= 1 << 1
						i += 29
						DHTSize -= 29
					} else {
						i += int(DHTSize)
						DHTSize = 0
					}
				case 0x10:
					if DHTSize >= 179 &&
						bytes.Compare(pkt.Data[i+2:i+18], mjpegparser.Avpriv_mjpeg_bits_ac_luminance[1:]) == 0 &&
						bytes.Compare(pkt.Data[i+18:i+180], mjpegparser.Avpriv_mjpeg_val_ac_luminance) == 0 {
						defaultHuffmanTables |= 1 << 2
						i += 179
						DHTSize -= 179
					} else {
						i += int(DHTSize)
						DHTSize = 0
					}
				case 0x11:
					if DHTSize >= 179 &&
						bytes.Compare(pkt.Data[i+2:i+18], mjpegparser.Avpriv_mjpeg_bits_ac_chrominance[1:]) == 0 &&
						bytes.Compare(pkt.Data[i+18:i+180], mjpegparser.Avpriv_mjpeg_val_ac_chrominance) == 0 {
						defaultHuffmanTables |= 1 << 3
						i += 179
						DHTSize -= 179
					} else {
						i += int(DHTSize)
						DHTSize = 0
					}
				default:
					i += int(DHTSize)
					DHTSize = 0
					continue
				}
			}
		} else if int(pkt.Data[i+1]) == mjpegparser.SOS {
			// SOS is last marker in the header
			i += int(binary.BigEndian.Uint16(pkt.Data[i+2:i+4])) + 2
			if i > size {
				log.Logf(log.ERROR, "Insufficient data. Aborted!")
				return
			}
			break
		}
	}

	if defaultHuffmanTables != 0 && defaultHuffmanTables != 31 {
		log.Logf(log.ERROR, "RFC 2435 requires standard Huffman tables for jpeg")
		return
	}
	if nbQtables != 0 && nbQtables != 2 {
		log.Logf(log.ERROR, "RFC 2435 suggests two quantization tables")
	}

	// skip JPEG header
	skipPos := i
	size -= i

	for i = size - 2; i >= 0; i-- {
		if int(pkt.Data[skipPos+i]) == 0xff && int(pkt.Data[skipPos+i+1]) == mjpegparser.EOI {
			// Remove the EOI marker
			size = i
			break
		}
	}

	for size > 0 {
		HDRSize := 8

		if offset == 0 && nbQtables != 0 {
			HDRSize += (4 + 64*nbQtables)
		}

		// payload max in one packet
		if size <= maxPayloadSize-HDRSize {
			dataLen = size
		} else {
			dataLen = (maxPayloadSize - HDRSize)
		}

		w := new(bytes.Buffer)
		err = self.writeRTPHeader(w, uint8(proto.PayloadType()), bool(size == dataLen))

		// set main header
		_ = binary.Write(w, binary.BigEndian, uint32(offset&0xFFFFFF))
		_ = binary.Write(w, binary.BigEndian, byte(headerType))
		_ = binary.Write(w, binary.BigEndian, byte(255))
		_ = binary.Write(w, binary.BigEndian, byte(codeData.Width()>>3))
		_ = binary.Write(w, binary.BigEndian, byte(codeData.Height()>>3))

		if offset == 0 && nbQtables != 0 {
			// set quantization tables header
			_ = binary.Write(w, binary.BigEndian, byte(0))
			_ = binary.Write(w, binary.BigEndian, byte(0))
			_ = binary.Write(w, binary.BigEndian, uint16(64*nbQtables))

			for i = 0; i < nbQtables; i++ {
				_ = binary.Write(w, binary.BigEndian, pkt.Data[qtables[i]:qtables[i]+64])
			}
		}

		// copy payload data
		_ = binary.Write(w, binary.BigEndian, pkt.Data[skipPos+offset:skipPos+offset+dataLen])

		err = self.writePacket(2*pkt.Idx, w.Bytes())
		if err != nil {
			return err
		}
		size -= dataLen
		offset += dataLen
	}

	return nil
}

func (self *SubSession) writeAACPacket(pkt av.Packet) (err error) {
	// TODO batch frames
	// maxFramesPerPacket := 50
	// maxAuHeaderSize := 2 + 2*maxFramesPerPacket
	proto := self.stream.Protocol.(*rtp.AACDynamicProtocol)
	mux := self.stream
	auSize := 2

	mux.Timestamp = mux.CurTimestamp

	// +---------+-----------+-----------+---------------+
	// | RTP     | AU Header | Auxiliary | Access Unit   |
	// | Header  | Section   | Section   | Data Section  |
	// +---------+-----------+-----------+---------------+

	//           <----------RTP Packet Payload----------->

	//    Figure 1: Data sections within an RTP packet

	w := new(bytes.Buffer)
	err = self.writeRTPHeader(w, uint8(proto.PayloadType()), true)
	if err != nil {
		return
	}
	//      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+- .. -+-+-+-+-+-+-+-+-+-+
	//      |AU-headers-length|AU-header|AU-header|      |AU-header|padding|
	//      |                 |   (1)   |   (2)   |      |   (n)   | bits  |
	//      +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+- .. -+-+-+-+-+-+-+-+-+-+
	//
	//                   Figure 2: The AU Header Section

	// AU-headers-length
	err = binary.Write(w, binary.BigEndian, uint16(auSize*8))
	if err != nil {
		return
	}
	// AU Header 0
	size := len(pkt.Data) << 3
	err = binary.Write(w, binary.BigEndian, uint16(size))
	if err != nil {
		return
	}
	// AU Data
	_, err = w.Write(pkt.Data)
	if err != nil {
		return
	}

	err = self.writePacket(2*pkt.Idx, w.Bytes())
	return
}

// writePCMPacket write rtp mux packet for pcm_alaw and pcm_mulaw
func (self *SubSession) writePCMPacket(pkt av.Packet, isPCMA bool) (err error) {

	// refer to: https://github.com/FFmpeg/FFmpeg/blob/master/libavformat/rtpenc.c#L539
	var ptType int

	if isPCMA {
		proto := self.stream.Protocol.(*rtp.PCMADynamicProtocol)
		ptType = proto.PayloadType()
	} else {
		proto := self.stream.Protocol.(*rtp.PCMMDynamicProtocol)
		ptType = proto.PayloadType()
	}

	self.stream.Timestamp = self.stream.CurTimestamp
	w := new(bytes.Buffer)
	err = self.writeRTPHeader(w, uint8(ptType), true)
	if err != nil {
		return
	}

	_, err = w.Write(pkt.Data)
	if err != nil {
		return
	}

	err = self.writePacket(2*pkt.Idx, w.Bytes())

	return
}

func (self *SubSession) writePacket(id int8, body []byte) error {
	if self.isUDP {
		var sock net.Conn
		if id&0x01 == 0 {
			sock = self.rtpSock
		} else {
			sock = self.rtcpSock
		}
		n, err := sock.Write(body)
		if err != nil {
			log.Log(log.DEBUG, "UDP write error: ", err)
			return err
		}
		if n < len(body) {
			return io.ErrShortWrite
		}
	} else {
		if err := self.conn.writeEmbeded(id, body); err != nil {
			return err
		}
	}
	if id&0x01 == 0 {
		self.stream.PacketCount++
		self.stream.OctetCount += len(body) - 12
	}
	return nil
}

func (self *SubSession) readUDP(idx int) {
	var buf [2048]byte
	for {
		n, err := self.udpServerSocks[idx].Read(buf[:])
		if err != nil {
			log.Log(log.DEBUG, "udp server loop exit: ", self.udpServerSocks[idx].LocalAddr(), " ", err)
			break
		}
		if n < 8 && buf[0]&0xc0 != 0x80 {
			continue
		}
		ty := buf[1]
		if ty == rtp.RTCP_RR {
			log.Log(log.TRACE, "RTCP_RR received")
			self.conn.markLastTimeFromClient()
		}
	}
}

func (self *SubSession) setupUDP(addr string, p1, p2 uint16) error {
	if !self.isUDP {
		return nil
	}

	// hack to IPv6 address
	if strings.Contains(addr, ":") {
		addr = "[" + addr + "]"
	}
	a1 := fmt.Sprintf("%s:%d", addr, p1)
	s1, err := net.Dial("udp", a1)
	if err != nil {
		log.Log(log.ERROR, "failed to setup udp rtp sock to ", a1, " ", err)
		return err
	}
	a2 := fmt.Sprintf("%s:%d", addr, p2)
	s2, err := net.Dial("udp", a2)
	if err != nil {
		log.Log(log.ERROR, "failed to setup udp rtcp sock to ", a2, " ", err)
		s1.Close()
		return err
	}

	self.udpServerSocks = FindUDPPair(0) // for default random idle port
	if self.udpServerSocks == nil {
		s1.Close()
		s2.Close()
		return ErrNoUDPPortPair
	}

	self.rtpSock = s1
	self.rtcpSock = s2

	go self.readUDP(0)
	go self.readUDP(1)

	return nil
}

func (self *SubSession) WritePacket(pkt av.Packet) (err error) {
	mux := self.stream

	ntpTime := rtp.NtpTime()
	if pkt.ExtraData != nil && len(pkt.ExtraData) == 4 {
		// EOF event(0xFFFFFFFF)
		var eofFlag uint32
		br := bitreader.NewReader(bytes.NewReader(pkt.ExtraData))
		if errRet := binary.Read(br, binary.BigEndian, &eofFlag); errRet == nil && eofFlag == rtp.RTCP_EOF_FLAG {
			err = self.writeSRPacket(pkt.Idx, int64(mux.TimeBase), ntpTime, "", true, true)
			return err
		}
	}

	if mux.FirstPacket || ntpTime-mux.LastRtcpNtpTime > 5000000 {
		err = self.writeSRPacket(pkt.Idx, int64(mux.TimeBase), ntpTime, "", false, false)
		if err != nil {
			return
		}
		mux.FirstPacket = false
	}

	timeScale := mux.TimeBase
	mux.CurTimestamp = uint32(mux.BaseTimestamp) + uint32(pkt.Time*time.Duration(timeScale)/time.Second)

	switch mux.Protocol.CodecData().Type() {
	case av.H264:
		err = self.writeFLVH264Packet(pkt)
	case av.H265:
		err = self.writeFLVH265Packet(pkt)
	case av.MJPEG:
		err = self.writeMJPEGPacket(pkt)
	case av.AAC:
		err = self.writeAACPacket(pkt)
	case av.PCM_MULAW:
		err = self.writePCMPacket(pkt, false)
	case av.PCM_ALAW:
		err = self.writePCMPacket(pkt, true)
	default:
	}
	return
}

func (self *SubSession) Close() (err error) {
	if self == nil {
		return
	}
	if self.isUDP {
		if self.rtpSock != nil {
			log.Log(log.DEBUG, "closing rtp udp connection to ", self.rtpSock.RemoteAddr())
			self.rtpSock.Close()
		}
		if self.rtcpSock != nil {
			log.Log(log.DEBUG, "closing rtcp udp connection to ", self.rtcpSock.RemoteAddr())
			self.rtcpSock.Close()
		}
		for _, v := range self.udpServerSocks {
			v.Close()
		}
	}
	return
}
