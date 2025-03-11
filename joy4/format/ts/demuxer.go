package ts

import (
	"bufio"
	"fmt"
	"io"
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec/aacparser"
	"videoplayer/joy4/codec/h264parser"
	"videoplayer/joy4/format/ts/tsio"
	"videoplayer/joy4/log"

	"github.com/nareix/bits/pio"
)

type Demuxer struct {
	r *bufio.Reader

	pkts []av.Packet

	pat     *tsio.PAT
	pmt     *tsio.PMT
	streams []*Stream
	tshdr   []byte

	stage int
}

func NewDemuxer(r io.Reader) *Demuxer {
	return &Demuxer{
		tshdr: make([]byte, tsio.TSPacketSize),
		r:     bufio.NewReaderSize(r, pio.RecommendBufioSize),
	}
}

func (self *Demuxer) Streams() (streams []av.CodecData, err error) {
	if err = self.probe(); err != nil {
		return
	}
	for _, stream := range self.streams {
		streams = append(streams, stream.CodecData)
	}
	return
}

func (self *Demuxer) probe() (err error) {
	if self.stage == 0 {
		for {
			videoIdx := -1
			audioIdx := -1
			if self.pmt != nil {
				n := 0
				for idx, stream := range self.streams {
					if stream.CodecData != nil {
						n++
					}

					if tsio.IsTSVideoType(stream.streamType) {
						videoIdx = idx
					}
					if tsio.IsTSAudioType(stream.streamType) {
						audioIdx = idx
					}
				}
				if n == len(self.streams) {
					break
				}
			}
			if err = self.poll(); err != nil {

				// Compatible with some HLS media sources without audio
				if videoIdx >= 0 && self.streams[videoIdx].CodecData != nil &&
					audioIdx >= 0 && self.streams[audioIdx].CodecData == nil {

					self.streams = append(self.streams[:audioIdx], self.streams[audioIdx+1:]...) // Remove from streams list if audio data cannot be parsed
					log.Logf(log.DEBUG, "streams[%d] miss audio CodecData, remove it.", audioIdx)
					err = nil
					break
				}

				return
			}
		}
		self.stage++
	}
	return
}

// ReadPacket return demuxed video/audio packet.
// NOTE: for H264, on pkt contains multiple AVC nalus, use SplitNALUs to split
// it into nalus for RTSP forwarding or other purpose.
func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
	if err = self.probe(); err != nil {
		return
	}

	for len(self.pkts) == 0 {
		if err = self.poll(); err != nil {
			return
		}
	}

	pkt = self.pkts[0]
	self.pkts = self.pkts[1:]
	return
}

func (self *Demuxer) poll() (err error) {
	if err = self.readTSPacket(); err == io.EOF {
		var n int
		if n, err = self.payloadEnd(); err != nil {
			return
		}
		if n == 0 {
			err = io.EOF
		}
	}
	return
}

func (self *Demuxer) initPMT(payload []byte) (err error) {
	var psihdrlen int
	var datalen int
	if _, _, psihdrlen, datalen, err = tsio.ParsePSI(payload); err != nil {
		return
	}
	self.pmt = &tsio.PMT{}
	if _, err = self.pmt.Unmarshal(payload[psihdrlen : psihdrlen+datalen]); err != nil {
		return
	}

	self.streams = []*Stream{}
	for i, info := range self.pmt.ElementaryStreamInfos {
		stream := &Stream{}
		stream.idx = i
		stream.demuxer = self
		stream.pid = info.ElementaryPID
		stream.streamType = info.StreamType
		switch info.StreamType {
		case tsio.ElementaryStreamTypeH264:
			self.streams = append(self.streams, stream)
		case tsio.ElementaryStreamTypeAdtsAAC:
			self.streams = append(self.streams, stream)
		}
	}
	return
}

func (self *Demuxer) payloadEnd() (n int, err error) {
	for _, stream := range self.streams {
		var i int
		if i, err = stream.payloadEnd(); err != nil {
			return
		}
		n += i
	}
	return
}

func (self *Demuxer) readTSPacket() (err error) {
	var hdrlen int
	var pid uint16
	var start, hasPayload bool
	var iskeyframe bool

	if _, err = io.ReadFull(self.r, self.tshdr); err != nil {
		return
	}

	if pid, start, iskeyframe, hdrlen, hasPayload, err = tsio.ParseTSHeader(self.tshdr); err != nil {
		return
	}
	payload := self.tshdr[hdrlen:]

	if self.pat == nil {
		if pid == 0 {
			var psihdrlen int
			var datalen int
			if _, _, psihdrlen, datalen, err = tsio.ParsePSI(payload); err != nil {
				return
			}
			self.pat = &tsio.PAT{}
			if _, err = self.pat.Unmarshal(payload[psihdrlen : psihdrlen+datalen]); err != nil {
				return
			}
		}
	} else if self.pmt == nil {
		for _, entry := range self.pat.Entries {
			if entry.ProgramMapPID == pid {
				if err = self.initPMT(payload); err != nil {
					return
				}
				break
			}
		}
	} else {
		if !hasPayload {
			return
		}
		for _, stream := range self.streams {
			if pid == stream.pid {
				if err = stream.handleTSPacket(start, iskeyframe, payload); err != nil {
					return
				}
				break
			}
		}
	}

	return
}

func (self *Stream) addPacket(payload []byte, codecType av.CodecType, timedelta time.Duration) {
	dts := self.dts
	pts := self.pts
	if dts == 0 {
		dts = pts
	}

	demuxer := self.demuxer
	pkt := av.Packet{
		MediaCodecType: codecType,
		Idx:            int8(self.idx),
		IsKeyFrame:     self.iskeyframe,
		Time:           dts + timedelta,
		Data:           payload,
	}
	if pts != dts {
		pkt.CompositionTime = pts - dts
	}
	demuxer.pkts = append(demuxer.pkts, pkt)
}

func (self *Stream) payloadEnd() (n int, err error) {
	payload := self.data
	if len(payload) == 0 {
		return
	}
	if self.datalen != 0 && len(payload) != self.datalen {
		err = fmt.Errorf("ts: packet size mismatch size=%d correct=%d", len(payload), self.datalen)
		return
	}
	self.data = nil

	switch self.streamType {
	case tsio.ElementaryStreamTypeAdtsAAC:
		var config aacparser.MPEG4AudioConfig

		delta := time.Duration(0)
		for len(payload) > 0 {
			var hdrlen, framelen, samples int
			if config, hdrlen, framelen, samples, err = aacparser.ParseADTSHeader(payload); err != nil {
				return
			}
			if self.CodecData == nil {
				if self.CodecData, err = aacparser.NewCodecDataFromMPEG4AudioConfig(config); err != nil {
					return
				}
			}
			self.addPacket(payload[hdrlen:framelen], av.AAC, delta)
			n++
			delta += time.Duration(samples) * time.Second / time.Duration(config.SampleRate)
			payload = payload[framelen:]
		}

	case tsio.ElementaryStreamTypeH264:
		nalus, _ := h264parser.GuessNALUType(payload)
		// fmt.Println("D", hex.Dump(payload[:8]))
		// nalus := h264parser.SplitNALUs(payload, false, 4, av.H264, true)
		var sps, pps []byte
		var outdata []byte
		var b [4]byte
		for _, nalu := range nalus {
			// nalu := nalu.Raw
			if len(nalu) == 0 {
				continue
			}
			naltype := nalu[0] & 0x1f
			switch {
			case naltype == 7:
				sps = nalu
			case naltype == 8:
				pps = nalu
			default:
				// raw nalu to avcc
				pio.PutU32BE(b[:], uint32(len(nalu)))
				outdata = append(outdata, b[:]...)
				outdata = append(outdata, nalu...)
				// copy(b[4:], nalu)
				n++
			}
		}
		if len(outdata) > 0 {
			self.addPacket(outdata, av.H264, time.Duration(0))
		}

		if self.CodecData == nil && len(sps) > 0 && len(pps) > 0 {
			if self.CodecData, err = h264parser.NewCodecDataFromSPSAndPPS(sps, pps); err != nil {
				return
			}
		}
	}

	return
}

func (self *Stream) handleTSPacket(start bool, iskeyframe bool, payload []byte) (err error) {
	if start {
		if _, err = self.payloadEnd(); err != nil {
			return
		}
		var hdrlen int
		if hdrlen, _, self.datalen, self.pts, self.dts, err = tsio.ParsePESHeader(payload); err != nil {
			return
		}
		self.iskeyframe = iskeyframe
		if self.datalen == 0 {
			self.data = make([]byte, 0, 4096)
		} else {
			self.data = make([]byte, 0, self.datalen)
		}
		self.data = append(self.data, payload[hdrlen:]...)
	} else {
		self.data = append(self.data, payload...)
	}
	return
}
