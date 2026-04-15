package core

// 本文件覆盖 Core 框架中与 `writeutil` 相关的行为。

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type shortWriter struct {
	buf     bytes.Buffer
	limit   int
	failAt  int
	writes  int
	failErr error
}

func (w *shortWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.failAt > 0 && w.writes == w.failAt {
		if len(p) == 0 {
			return 0, w.failErr
		}
		n := 1
		if len(p) < n {
			n = len(p)
		}
		_, _ = w.buf.Write(p[:n])
		return n, w.failErr
	}
	n := len(p)
	if w.limit > 0 && n > w.limit {
		n = w.limit
	}
	if n > 0 {
		_, _ = w.buf.Write(p[:n])
	}
	return n, nil
}

func TestWriteAllRetriesShortWrite(t *testing.T) {
	dst := &shortWriter{limit: 3}
	data := []byte("hello world")
	if err := WriteAll(dst, data); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	if got := dst.buf.Bytes(); !bytes.Equal(got, data) {
		t.Fatalf("bytes mismatch: got=%q want=%q", got, data)
	}
}

func TestWriteAllBuffersWritesAllChunks(t *testing.T) {
	dst := &shortWriter{limit: 2}
	if err := WriteAllBuffers(dst, []byte("abc"), nil, []byte("defg")); err != nil {
		t.Fatalf("WriteAllBuffers: %v", err)
	}
	if got, want := dst.buf.String(), "abcdefg"; got != want {
		t.Fatalf("bytes mismatch: got=%q want=%q", got, want)
	}
}

func TestWriteAllReturnsWriteErrorWhenBytesRemain(t *testing.T) {
	wantErr := errors.New("write failed")
	dst := &shortWriter{limit: 4, failAt: 2, failErr: wantErr}
	err := WriteAll(dst, []byte("abcdef"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestWriteAllRejectsNilWriter(t *testing.T) {
	if err := WriteAll(nil, []byte("x")); !errors.Is(err, ErrNilWriter) {
		t.Fatalf("expected ErrNilWriter, got %v", err)
	}
}

func TestWriteAllReturnsShortWriteWhenWriterConsumesNothing(t *testing.T) {
	err := WriteAll(zeroWriter{}, []byte("x"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("expected io.ErrShortWrite, got %v", err)
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }
