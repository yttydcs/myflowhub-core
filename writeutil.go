package core

// 本文件承载 Core 框架中与 `writeutil` 相关的通用逻辑。

import (
	"errors"
	"io"
)

// ErrNilWriter 表示调用方传入了空 writer。
var ErrNilWriter = errors.New("writer nil")

// WriteAll 保证把整段数据写完；底层若出现短写，会继续把剩余部分补写出去。
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

// WriteAllBuffers 按顺序写完每一段数据，供 header/payload 分段发送时复用同一套短写处理。
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
