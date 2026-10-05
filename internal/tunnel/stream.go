package tunnel

// Funcs adapts a pair of send/receive functions (a ConnectRPC bidi stream on
// either end) to Stream, so this package needs no generated code.
type Funcs struct {
	SendFn  func([]byte) error
	RecvFn  func() ([]byte, error)
	CloseFn func() error
}

func (f Funcs) Send(b []byte) error   { return f.SendFn(b) }
func (f Funcs) Recv() ([]byte, error) { return f.RecvFn() }
func (f Funcs) Close() error {
	if f.CloseFn == nil {
		return nil
	}
	return f.CloseFn()
}

// Join pumps a and b into each other until either side ends, then closes both.
// It returns once both directions have stopped.
func Join(a, b interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}) {
	done := make(chan struct{}, 2)
	cp := func(dst, src interface {
		Read([]byte) (int, error)
		Write([]byte) (int, error)
		Close() error
	}) {
		buf := make([]byte, MaxFrame)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		// One side ending ends the connection: closing both unblocks the
		// other copy.
		_ = a.Close()
		_ = b.Close()
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
}
