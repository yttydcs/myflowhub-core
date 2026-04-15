//go:build android

package rfcomm_listener

// 本文件承载 Core 框架中与 `android_provider` 相关的通用逻辑。

import (
	"errors"
	"sync"
)

// AndroidRFCOMMPipe is a gomobile-friendly byte stream abstraction.
//
// Java/Kotlin should implement it by delegating to BluetoothSocket's InputStream/OutputStream.
type AndroidRFCOMMPipe interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error
	// RemoteBDAddr returns remote bluetooth device address (if known), e.g. "AA:BB:CC:DD:EE:FF".
	RemoteBDAddr() string
}

// AndroidRFCOMMListener is a gomobile-friendly listener abstraction.
type AndroidRFCOMMListener interface {
	// Accept blocks until a new connection arrives.
	Accept() (AndroidRFCOMMPipe, error)

	// Close stops listening and wakes Accept.
	Close() error

	// Addr returns a human-readable listener identifier (optional).
	Addr() string
}

// AndroidRFCOMMProvider bridges Android Bluetooth Classic APIs to Go.
//
// Design notes:
// - Keep signatures gomobile-friendly (basic types only).
// - uuid is canonical "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx".
// - channel is kept for cross-platform parity; Android can ignore it when UUID is used.
type AndroidRFCOMMProvider interface {
	Listen(uuid string, secure bool) (AndroidRFCOMMListener, error)
	Dial(bdaddr string, uuid string, channel int, secure bool) (AndroidRFCOMMPipe, error)
}

var (
	androidProviderMu sync.RWMutex
	androidProvider   AndroidRFCOMMProvider
)

func SetAndroidRFCOMMProvider(p AndroidRFCOMMProvider) {
	androidProviderMu.Lock()
	androidProvider = p
	androidProviderMu.Unlock()
}

func getAndroidRFCOMMProvider() (AndroidRFCOMMProvider, error) {
	androidProviderMu.RLock()
	p := androidProvider
	androidProviderMu.RUnlock()
	if p == nil {
		return nil, errors.New("android rfcomm provider not set")
	}
	return p, nil
}
