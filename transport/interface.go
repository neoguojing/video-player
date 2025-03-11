package transport

type Transporter interface {
	Send(payload []byte) ([]byte, error)
	ReadData() ([]byte, error)
}
