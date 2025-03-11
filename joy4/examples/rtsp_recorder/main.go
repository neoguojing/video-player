package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/av/avutil"
	"videoplayer/joy4/format"
	"videoplayer/joy4/format/rtsp"
	"videoplayer/joy4/log"
)

func init() {
	format.RegisterAll()
}

func main() {
	srcfile := flag.String("src", "rtsp://localhost:554/test.flv", "Source file")
	dstfile := flag.String("dst", "output.flv", "Output file")
	useTCP := flag.Bool("tcp", false, "Use TCP")
	printTrace := flag.Bool("trace", false, "Print trace")
	max := flag.Int("max", 5, "Max seconds")
	flag.Parse()

	if *printTrace {
		log.DefaultStandardLogger.Level = log.TRACE
	} else {
		log.DefaultStandardLogger.Level = log.DEBUG
	}

	src, err := rtsp.Dial(*srcfile)
	if err != nil {
		panic(err)
	}
	defer src.Close()
	src.UseUDP = !*useTCP
	src.RtpTimeout = 5 * time.Second
	src.RtspTimeout = 5 * time.Second
	if *dstfile == "" {
		for {
			pkt, err := src.ReadPacket()
			if err != nil {
				fmt.Println("exit: ", err)
				break
			}
			fmt.Println("pkt: ", pkt.Idx, pkt.Time)
			l := 32
			if l > len(pkt.Data) {
				l = len(pkt.Data)
			}
			fmt.Println(hex.Dump(pkt.Data[:l]))
		}
	} else {
		dst, err := avutil.Create(*dstfile)
		if err != nil {
			panic(err)
		}
		// same as calling avutil.CopyFile(dst, src) but added
		// max duration in case the src is live and never ends
		err = CopyFileMax(dst, src, time.Duration(*max)*time.Second)
		if err != nil {
			fmt.Println("Copy error: ", err)
		}
	}
}

func CopyPacketsMax(dst av.PacketWriter, src av.PacketReader, max time.Duration) (err error) {
	for {
		var pkt av.Packet
		if pkt, err = src.ReadPacket(); err != nil {
			if err == io.EOF {
				err = nil
				break
			}
			return
		}

		// skip SEI/SPS/PPS
		/*
			if pkt.Idx != 0 {
				continue
			}
			if len(pkt.Data) < 5 || pkt.Data[4]&0x1f > 5 {
				fmt.Println("XXXX")
				continue
			}
		*/
		if pkt.IsKeyFrame {
			fmt.Println("key frame: ", time.Now())
		}
		// break when max time has been reached
		if max > 0 && pkt.Time >= max {
			return
		}

		if err = dst.WritePacket(pkt); err != nil {
			return
		}
	}
	return
}

func CopyFileMax(dst av.Muxer, src av.Demuxer, max time.Duration) (err error) {
	var streams []av.CodecData
	if streams, err = src.Streams(); err != nil {
		return
	}
	fmt.Printf("codec data: %+v\n", streams)
	if err = dst.WriteHeader(streams); err != nil {
		return
	}
	if err = CopyPacketsMax(dst, src, max); err != nil {
		return
	}
	if err = dst.WriteTrailer(); err != nil {
		return
	}
	return
}
