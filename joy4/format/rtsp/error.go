package rtsp

import (
	"fmt"
	"net/http"
)

type RTSPError struct {
	Code        int
	ErrorString string
}

func (e *RTSPError) Error() string {
	return fmt.Sprintf("RTSP %d-%s, %s", e.Code, http.StatusText(e.Code), e.ErrorString)
}

func (e *RTSPError) StatusText() string {
	return http.StatusText(e.Code)
}

func NewRTSPError(code int, err string) *RTSPError {
	return &RTSPError{
		Code:        code,
		ErrorString: err,
	}
}
