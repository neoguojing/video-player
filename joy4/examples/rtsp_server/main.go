package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/av/avutil"
	"videoplayer/joy4/codec/h264parser"
	"videoplayer/joy4/format"
	"videoplayer/joy4/format/rtsp"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/log"
)

func init() {
	format.RegisterAll()
}

type sourceFile struct {
	name string
	path string
	cd   []av.CodecData
}

func findSrc(m map[string]*sourceFile, path string) *sourceFile {
	for k, v := range m {
		if strings.HasPrefix(path, k) {
			return v
		}
	}
	return nil
}

func main() {
	// srcfile := flag.String("src", "big_buck_bunny.flv", "Source file")
	port := flag.String("port", ":8888", "Port")
	loop := flag.Int("loop", 1, "loop count, 0 for infinity")
	flag.Parse()

	// fmt.Println(flag.Args())
	sources := make(map[string]*sourceFile)

	for _, v := range flag.Args() {
		src, err := avutil.Open(v)
		if err != nil {
			log.Logf(log.WARN, "open error %s: %v", v, err)
			continue
		}
		srcStreams, err := src.Streams()
		if err != nil {
			log.Logf(log.WARN, "read streams error %s: %v", v, err)
			continue
		}
		src.Close()
		base := path.Base(v)
		sources["/"+base] = &sourceFile{
			name: base,
			path: v,
			cd:   srcStreams,
		}
		log.Log(log.INFO, "open: ", v)
	}
	log.Log(log.INFO, "sources: ", sources)

	log.Log(log.INFO, "service at: ", *port)
	server := rtsp.NewServer(*port)
	server.HandlePublishV2 = func(conn *rtsp.Conn, u *url.URL) (*sdp.SDPInfo, error) {
		sf := findSrc(sources, u.Path)
		if sf == nil {
			return nil, rtsp.NewRTSPError(404, "Not Found")
		}
		// if strings.HasPrefix(u.Path, "/test.flv") {
		// 	return srcStreams, nil
		// }

		return &sdp.SDPInfo{CodecDatas: sf.cd}, nil
	}
	server.HandlePlay = func(session *rtsp.Session) error {
		u := session.Uri
		sf := findSrc(sources, u.Path)
		if sf == nil {
			return rtsp.NewRTSPError(404, "Not Found")
		}

		src, err := avutil.Open(sf.path)
		if err != nil {
			return err
		}

		go func() {
			defer src.Close()
			defer session.Close()
			// avutil.CopyPackets(session, src)
			var err error

			<-session.Events()
			startTime := time.Now()
			n := 0
			pktBaseTime := time.Duration(0)
		Loop:
			for *loop <= 0 || n < *loop {
				if !session.IsPlaying() {
					break
				}
				fmt.Println("loop: ", n, pktBaseTime)
				n++
				for session.IsPlaying() {
					var pkt av.Packet
					if pkt, err = src.ReadPacket(); err != nil {
						if err == io.EOF {
							src.Close()
							src, err = avutil.Open(sf.path)
							if err != nil {
								break Loop
							}
							pktBaseTime = time.Now().Sub(startTime)
							err = nil
							break
						}
					}
					pkt.Time += pktBaseTime
					fromStart := time.Now().Sub(startTime)
					if pkt.Idx == 0 && len(pkt.Data) > 4 {
						nalus := h264parser.SplitNALUs(pkt.Data, true, 4, av.H264, true)
						for _, nal := range nalus {
							switch h264parser.NALUType(nal.Rbsp) {
							case h264parser.NALU_IDR_SLICE, h264parser.NALU_NON_IDR_SLICE:
								header, _ := h264parser.ParseSliceHeaderFromNALU(nal.Rbsp)
								fmt.Println(fromStart, "frame: ", pkt.IsKeyFrame, header)
							case h264parser.NALU_SEI:
								sei, _ := h264parser.ParseSEIMessageFromNALU(nal.Rbsp)
								fmt.Println(fromStart, "SEI: ", sei.Type, sei.PayloadSize)
								fmt.Print(hex.Dump(sei.Payload))
							}
							pkt.Data = nal.Raw

							if pkt.Time > fromStart {
								time.Sleep(pkt.Time - fromStart)
							}
							if err = session.WritePacket(pkt); err != nil {
								break Loop
							}
						}
					}
				}
			}
			fmt.Println("done: ", err)
		}()
		return nil
	}
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}

	// play with ffplay: ffplay -v debug -rtsp_transport tcp rtsp://localhost:8888/test.flv
}
