// Code generated by "stringer -type=FrameEmphasis"; DO NOT EDIT

package mp3parser

import "fmt"

const _FrameEmphasis_name = "EmphNoneEmph5015EmphReservedEmphCCITJ17EmphMax"

var _FrameEmphasis_index = [...]uint8{0, 8, 16, 28, 39, 46}

func (i FrameEmphasis) String() string {
	if i >= FrameEmphasis(len(_FrameEmphasis_index)-1) {
		return fmt.Sprintf("FrameEmphasis(%d)", i)
	}
	return _FrameEmphasis_name[_FrameEmphasis_index[i]:_FrameEmphasis_index[i+1]]
}
