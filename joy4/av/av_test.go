package av

import "testing"

func TestRescale(t *testing.T) {
	if Rescale(41343243242234, 213123133, 55555) != 158603213539123175 {
		t.Error("bad")
	}
	if Rescale(3242, 1221, 44) != 89966 {
		t.Error("bad")
	}
}
