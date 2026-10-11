package takeover

import (
	"net"
	"time"
)

type testListener struct{ net.Listener }

func newListener() (*testListener, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	time.Sleep(50 * time.Millisecond)
	return &testListener{l}, nil
}

func (l *testListener) port() int { return l.Addr().(*net.TCPAddr).Port }
