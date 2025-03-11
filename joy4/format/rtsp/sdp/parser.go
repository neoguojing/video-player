package sdp

import (
	"encoding/hex"
	"strconv"
	"strings"

	"videoplayer/joy4/av"
)

type Session struct {
	Uri string
}

type Media struct {
	AVType      string
	Type        av.CodecType
	TimeScale   int
	Control     string
	Rtpmap      int
	Config      []byte
	PayloadType int
	SizeLength  int
	IndexLength int

	ALines map[string]string
	// SpropParameterSets [][]byte
}

type SDPInfo struct {
	RangeStart float64 // a=range:npt=0-60.120(seconds)
	RangeEnd   float64 // a=range:npt=0-60.120
	Medias     []Media
	CodecDatas []av.CodecData
	ExtraLines map[string][]string
}

func Parse(content string) (sess Session, sdp SDPInfo) {
	var medias []Media
	var media *Media

	sdp.ExtraLines = make(map[string][]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		typeval := strings.SplitN(line, "=", 2)
		if len(typeval) != 2 {
			continue
		}
		fields := strings.SplitN(typeval[1], " ", 2)

		switch typeval[0] {
		case "m":
			if len(fields) < 1 {
				break
			}
			// should be "audio", "video", "application"
			medias = append(medias, Media{AVType: fields[0], ALines: make(map[string]string)})
			media = &medias[len(medias)-1]
			mfields := strings.Split(fields[1], " ")
			if len(mfields) >= 3 {
				media.PayloadType, _ = strconv.Atoi(mfields[2])
			}
		case "u":
			sess.Uri = typeval[1]

		case "a":
			if "h264-esid:201" == typeval[1] {
				if media == nil {
					sdp.ExtraLines[typeval[0]] = append(sdp.ExtraLines[typeval[0]], typeval[1])
				}
				continue
			}

			// get range time for playback mode
			if strings.HasPrefix(typeval[1], "range:") {
				//range:npt=0-60.120
				keyval := strings.SplitN(typeval[1], ":", 2)
				if len(keyval) >= 2 {
					//npt=0-60.120
					ranges := strings.SplitN(keyval[1], "=", 2)
					if len(ranges) == 2 {
						//0-60.120
						times := strings.SplitN(ranges[1], "-", 2)
						if len(times) == 2 {
							sdp.RangeStart, _ = strconv.ParseFloat(times[0], 64)
							sdp.RangeEnd, _ = strconv.ParseFloat(times[1], 64)
						}
					}
				}
				continue
			}

			if media == nil {
				sdp.ExtraLines[typeval[0]] = append(sdp.ExtraLines[typeval[0]], typeval[1])
				break
			}
			valid := true
			for _, field := range fields {
				keyval := strings.SplitN(field, ":", 2)
				if len(keyval) >= 2 {
					key := keyval[0]
					val := keyval[1]
					switch key {
					case "control":
						media.Control = val
					case "rtpmap":
						rtpmap, _ := strconv.Atoi(val)
						if rtpmap == media.PayloadType {
							media.Rtpmap, _ = strconv.Atoi(val)
						} else {
							valid = false
						}
					}
				}
				if !valid {
					break
				}
				keyval = strings.Split(field, "/")
				if len(keyval) >= 2 {
					key := keyval[0]
					switch strings.ToUpper(key) {
					case "MPEG4-GENERIC":
						media.Type = av.AAC
					case "H264":
						media.Type = av.H264
					case "H265":
						media.Type = av.H265
					}
					if i, err := strconv.Atoi(keyval[1]); err == nil {
						media.TimeScale = i
					}
				}
				keyval = strings.Split(field, ";")
				if len(keyval) > 1 {
					for _, field := range keyval {
						keyval := strings.SplitN(field, "=", 2)
						if len(keyval) == 2 {
							key := strings.TrimSpace(keyval[0])
							val := keyval[1]
							switch key {
							case "config":
								media.Config, _ = hex.DecodeString(val)
							case "sizelength":
								media.SizeLength, _ = strconv.Atoi(val)
							case "indexlength":
								media.IndexLength, _ = strconv.Atoi(val)
							default:
								media.ALines[key] = val
							}
						}
					}
				}
			}
		default:
			if media == nil {
				sdp.ExtraLines[typeval[0]] = append(sdp.ExtraLines[typeval[0]], typeval[1])
			}
		}
	}

	sdp.Medias = medias
	return
}
