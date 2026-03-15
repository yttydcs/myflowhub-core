package core

import (
	"errors"
	"io"
)

// ErrNilWriter indicates the caller passed a nil writer.
var ErrNilWriter = errors.New("writer nil")

// WriteAll writes the full payload to w, retrying when the underlying writer
// reports a short write without consuming all bytes.
func WriteAll(w io.Writer, data []byte) error {
	if w == nil {
		return ErrNilWriter
	}
	for len(data) > 0 {
		n, err := w.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if len(data) == 0 {
			return nil
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

// WriteAllBuffers writes each chunk fully in order.
func WriteAllBuffers(w io.Writer, chunks ...[]byte) error {
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		if err := WriteAll(w, chunk); err != nil {
			return err
		}
	}
	return nil
}
