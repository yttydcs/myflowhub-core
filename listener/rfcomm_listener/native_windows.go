//go:build windows

package rfcomm_listener

// 本文件承载 Core 框架中与 `native_windows` 相关的通用逻辑。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	core "github.com/yttydcs/myflowhub-core"
	"golang.org/x/sys/windows"
)

const (
	afBth          = 32 // AF_BTH
	sockStream     = 1  // SOCK_STREAM
	bthprotoRFCOMM = 3  // BTHPROTO_RFCOMM

	winRFCOMMReadChunkBytes  = 2048
	winRFCOMMWriteChunkBytes = 127
	winRFCOMMSendTimeoutMs   = 8000
	soSndTimeout             = 0x1005 // SO_SNDTIMEO (x/sys/windows currently未导出)
)

type sockaddrBth struct {
	AddressFamily uint16
	_             [6]byte // padding to align BtAddr (matches C layout)
	BtAddr        uint64
	ServiceClass  windows.GUID
	Port          uint32
}

var (
	ws2_32             = windows.NewLazySystemDLL("ws2_32.dll")
	procAccept         = ws2_32.NewProc("accept")
	procGetsockname    = ws2_32.NewProc("getsockname")
	procWSASetServiceW = ws2_32.NewProc("WSASetServiceW")
	wsaRecvFn          = windows.WSARecv
	wsaSendFn          = windows.WSASend
)

var wsaOnce sync.Once
var wsaInitErr error

// ensureWSA 只做一次 Winsock 初始化，供 dial/listen 共用。
func ensureWSA() error {
	wsaOnce.Do(func() {
		var data windows.WSAData
		// MakeVer(2,2)
		const ver = 0x0202
		wsaInitErr = windows.WSAStartup(ver, &data)
	})
	return wsaInitErr
}

// winsockCallErr 把 syscall 返回值规整成更稳定的 Winsock 错误对象。
func winsockCallErr(err error) error {
	if err == nil {
		return windows.WSAEINVAL
	}
	errno, ok := err.(syscall.Errno)
	if !ok {
		return err
	}
	if errno == 0 {
		return windows.WSAEINVAL
	}
	return errno
}

// newRawSockaddrBth 构造与 Windows API 布局兼容的原始蓝牙地址结构。
func newRawSockaddrBth(btAddr uint64, serviceClass windows.GUID, port uint32) windows.RawSockaddrBth {
	var raw windows.RawSockaddrBth
	family := uint16(afBth)
	raw.AddressFamily = *(*[2]byte)(unsafe.Pointer(&family))
	raw.BtAddr = *(*[8]byte)(unsafe.Pointer(&btAddr))
	raw.ServiceClassId = *(*[16]byte)(unsafe.Pointer(&serviceClass))
	raw.Port = *(*[4]byte)(unsafe.Pointer(&port))
	return raw
}

// sockaddrBthFromRaw 把系统返回的原始地址结构解码成本地便于访问的版本。
func sockaddrBthFromRaw(raw *windows.RawSockaddrBth) sockaddrBth {
	if raw == nil {
		return sockaddrBth{}
	}
	return sockaddrBth{
		AddressFamily: *(*uint16)(unsafe.Pointer(&raw.AddressFamily[0])),
		BtAddr:        *(*uint64)(unsafe.Pointer(&raw.BtAddr[0])),
		ServiceClass:  *(*windows.GUID)(unsafe.Pointer(&raw.ServiceClassId[0])),
		Port:          *(*uint32)(unsafe.Pointer(&raw.Port[0])),
	}
}

// newDialLocalSockaddrBth 生成客户端 bind `BT_PORT_ANY` 时使用的本地地址。
func newDialLocalSockaddrBth() *windows.SockaddrBth {
	return &windows.SockaddrBth{}
}

// newDialRemoteSockaddrBth 按 channel-first 或 UUID-first 规则构造远端地址。
func newDialRemoteSockaddrBth(btAddr uint64, serviceClass windows.GUID, channel int) *windows.SockaddrBth {
	sa := &windows.SockaddrBth{BtAddr: btAddr}
	if channel > 0 {
		sa.Port = uint32(channel)
		return sa
	}
	sa.ServiceClassId = serviceClass
	return sa
}

// newDialAddrFromSockaddr 把平台地址回填成框架层更易读的 `Addr`。
func newDialAddrFromSockaddr(raw *sockaddrBth, uuid string, fallbackChannel int) *Addr {
	addr := &Addr{UUID: uuid, Channel: fallbackChannel, Role: "dial"}
	if raw == nil {
		return addr
	}
	if raw.BtAddr != 0 {
		addr.BDAddr = bthAddrToBDAddr(raw.BtAddr)
	}
	if raw.Port > 0 {
		addr.Channel = int(raw.Port)
	}
	return addr
}

// acceptSock 封装原生 accept 调用，并同时拿到对端蓝牙地址。
func acceptSock(s windows.Handle) (windows.Handle, *sockaddrBth, error) {
	var raw windows.RawSockaddrBth
	l := int32(unsafe.Sizeof(raw))
	r0, _, callErr := procAccept.Call(uintptr(s), uintptr(unsafe.Pointer(&raw)), uintptr(unsafe.Pointer(&l)))
	if int32(r0) == -1 {
		return windows.InvalidHandle, nil, winsockCallErr(callErr)
	}
	nfd := windows.Handle(r0)
	sa := sockaddrBthFromRaw(&raw)
	return nfd, &sa, nil
}

// getsocknameBth 读取 socket 当前绑定的本地蓝牙地址信息。
func getsocknameBth(s windows.Handle) (*sockaddrBth, error) {
	var raw windows.RawSockaddrBth
	l := int32(unsafe.Sizeof(raw))
	r0, _, callErr := procGetsockname.Call(uintptr(s), uintptr(unsafe.Pointer(&raw)), uintptr(unsafe.Pointer(&l)))
	if int32(r0) == -1 {
		return nil, winsockCallErr(callErr)
	}
	sa := sockaddrBthFromRaw(&raw)
	return &sa, nil
}

// setSockSendTimeout 给 Winsock 套接字设置发送超时，避免写入无限阻塞。
func setSockSendTimeout(s windows.Handle, timeoutMs int) {
	if s == windows.InvalidHandle || timeoutMs <= 0 {
		return
	}
	_ = windows.SetsockoptInt(s, windows.SOL_SOCKET, soSndTimeout, timeoutMs)
}

type winSockPipe struct {
	sock windows.Handle
	// closed==1 means sock already closed.
	closed atomic.Uint32

	readMu      sync.Mutex
	readCache   []byte
	readScratch []byte
}

// Read 以缓存包的方式适配 RFCOMM 的分包读语义，向上层暴露标准 io.Reader。
func (p *winSockPipe) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	p.readMu.Lock()
	defer p.readMu.Unlock()

	if len(p.readCache) == 0 {
		if err := p.fillReadCacheLocked(); err != nil {
			return 0, err
		}
	}

	n := copy(b, p.readCache)
	p.readCache = p.readCache[n:]
	if len(p.readCache) == 0 {
		p.readCache = nil
	}
	return n, nil
}

// fillReadCacheLocked 执行一次底层接收，并把结果缓存为后续小块 Read 提供数据。
func (p *winSockPipe) fillReadCacheLocked() error {
	if cap(p.readScratch) < winRFCOMMReadChunkBytes {
		p.readScratch = make([]byte, winRFCOMMReadChunkBytes)
	}
	bufData := p.readScratch[:winRFCOMMReadChunkBytes]
	var recvd uint32
	flags := uint32(0)
	buf := windows.WSABuf{
		Len: uint32(len(bufData)),
		Buf: &bufData[0],
	}
	if err := wsaRecvFn(p.sock, &buf, 1, &recvd, &flags, nil, nil); err != nil {
		// Some providers may report partial read with EMSGSIZE; preserve the consumed bytes.
		if recvd > 0 && errors.Is(err, windows.WSAEMSGSIZE) {
			p.readCache = append(p.readCache[:0], bufData[:int(recvd)]...)
			return nil
		}
		return err
	}
	// RFCOMM socket closes gracefully with 0-byte recv, which should map to EOF for io.Reader contract.
	if recvd == 0 {
		return io.EOF
	}
	p.readCache = append(p.readCache[:0], bufData[:int(recvd)]...)
	return nil
}

// Write 按受控块大小发送数据，规避 Windows RFCOMM 对单次写入长度的限制。
func (p *winSockPipe) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	total := 0
	for total < len(b) {
		end := total + winRFCOMMWriteChunkBytes
		if end > len(b) {
			end = len(b)
		}
		n, err := p.writeChunkWithFallback(b[total:end])
		if n > 0 {
			total += n
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

// writeChunkWithFallback 先尝试直接写块，若遇到 `WSAEMSGSIZE` 再退到更小分片。
func (p *winSockPipe) writeChunkWithFallback(b []byte) (int, error) {
	n, err := p.writeOnce(b)
	if err == nil || !errors.Is(err, windows.WSAEMSGSIZE) {
		return n, err
	}
	if n >= len(b) {
		return n, nil
	}
	return p.writeWithMsgSizeFallback(b, n)
}

// writeOnce 对当前分片执行一次原生发送。
func (p *winSockPipe) writeOnce(b []byte) (int, error) {
	var sent uint32
	buf := windows.WSABuf{
		Len: uint32(len(b)),
		Buf: &b[0],
	}
	if err := wsaSendFn(p.sock, &buf, 1, &sent, 0, nil, nil); err != nil {
		return int(sent), err
	}
	return int(sent), nil
}

// writeWithMsgSizeFallback 在消息过大时逐步缩小块尺寸，直到本次发送可成功完成。
func (p *winSockPipe) writeWithMsgSizeFallback(b []byte, offset int) (int, error) {
	if offset < 0 {
		offset = 0
	}
	total := offset
	remaining := len(b) - total
	if remaining <= 0 {
		return total, nil
	}

	chunk := remaining / 2
	if chunk > winRFCOMMWriteChunkBytes {
		chunk = winRFCOMMWriteChunkBytes
	}
	if chunk < 1 {
		chunk = 1
	}

	for total < len(b) {
		end := total + chunk
		if end > len(b) {
			end = len(b)
		}
		n, err := p.writeOnce(b[total:end])
		if n > 0 {
			total += n
		}
		if err != nil {
			if errors.Is(err, windows.WSAEMSGSIZE) && n == 0 && chunk > 1 {
				chunk /= 2
				if chunk < 1 {
					chunk = 1
				}
				continue
			}
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

// Close 幂等关闭底层 socket。
func (p *winSockPipe) Close() error {
	if p == nil {
		return nil
	}
	if p.closed.Swap(1) == 1 {
		return nil
	}
	return windows.Closesocket(p.sock)
}

type winListener struct {
	log     *slog.Logger
	uuid    string
	uuidG   windows.GUID
	channel int

	lnSock windows.Handle
	closed atomic.Bool

	svcName    *uint16
	svcRegDone atomic.Bool
}

// dialNative 走 Windows RFCOMM socket API 建立客户端连接，并返回框架可用的 pipe。
func dialNative(ctx context.Context, opts DialOptions) (core.IPipe, net.Addr, net.Addr, error) {
	if err := ensureWSA(); err != nil {
		return nil, nil, nil, err
	}
	bd, _ := normalizeBDAddr(opts.BDAddr)
	uuid := strings.ToLower(strings.TrimSpace(opts.UUID))
	g, err := uuidToGUID(uuid)
	if err != nil {
		return nil, nil, nil, err
	}
	btAddr, err := bdAddrToBTHAddr(bd)
	if err != nil {
		return nil, nil, nil, err
	}

	s, err := windows.WSASocket(afBth, sockStream, bthprotoRFCOMM, nil, 0, windows.WSA_FLAG_OVERLAPPED)
	if err != nil {
		return nil, nil, nil, err
	}
	// Make sure we don't leak sockets on dial error.
	defer func() {
		if s != windows.InvalidHandle {
			_ = windows.Closesocket(s)
		}
	}()

	// Windows RFCOMM client sockets must bind a local BT_PORT_ANY endpoint before connect,
	// otherwise winsock may surface WSAEADDRINUSE/WSAENOTCONN during client dial/send.
	localBind := newDialLocalSockaddrBth()
	if err := windows.Bind(s, localBind); err != nil {
		return nil, nil, nil, err
	}
	remoteSockaddr := newDialRemoteSockaddrBth(btAddr, g, opts.Channel)

	errCh := make(chan error, 1)
	go func() { errCh <- windows.Connect(s, remoteSockaddr) }()

	select {
	case <-ctx.Done():
		_ = windows.Closesocket(s)
		return nil, nil, nil, ctx.Err()
	case err := <-errCh:
		if err != nil {
			return nil, nil, nil, err
		}
	}
	setSockSendTimeout(s, winRFCOMMSendTimeoutMs)

	pipe := &winSockPipe{sock: s}
	s = windows.InvalidHandle // transferred to pipe

	localSockaddr, _ := getsocknameBth(pipe.sock)
	local := newDialAddrFromSockaddr(localSockaddr, uuid, 0)
	remote := &Addr{BDAddr: bd, UUID: uuid, Channel: opts.Channel, Role: "dial"}
	return pipe, local, remote, nil
}

// listenNative 创建 Windows RFCOMM 监听 socket，并注册 SDP 服务记录。
func listenNative(opts Options) (nativeListener, error) {
	if err := ensureWSA(); err != nil {
		return nil, err
	}
	uuid := strings.ToLower(strings.TrimSpace(opts.UUID))
	g, err := uuidToGUID(uuid)
	if err != nil {
		return nil, err
	}
	name, _ := windows.UTF16PtrFromString("MyFlowHub")

	s, err := windows.WSASocket(afBth, sockStream, bthprotoRFCOMM, nil, 0, windows.WSA_FLAG_OVERLAPPED)
	if err != nil {
		return nil, err
	}
	ln := &winListener{
		log:     opts.Logger,
		uuid:    uuid,
		uuidG:   g,
		channel: opts.Channel,
		lnSock:  s,
		svcName: name,
	}

	// Bind.
	sa := &windows.SockaddrBth{}
	if opts.Channel > 0 {
		sa.Port = uint32(opts.Channel)
	} else {
		sa.Port = 0 // BT_PORT_ANY
	}
	if err := windows.Bind(s, sa); err != nil {
		_ = ln.Close()
		return nil, err
	}

	// Query assigned channel when using BT_PORT_ANY.
	if opts.Channel == 0 {
		if got, err := getsocknameBth(s); err == nil && got != nil && got.Port > 0 {
			ln.channel = int(got.Port)
		}
	}

	if err := windows.Listen(s, 8); err != nil {
		_ = ln.Close()
		return nil, err
	}

	// Register service record (UUID-first discoverability).
	if err := ln.registerService(); err != nil {
		// Not fatal for channel-first scenarios, but we treat it as error to keep behavior consistent.
		_ = ln.Close()
		return nil, err
	}

	return ln, nil
}

func (l *winListener) Addr() net.Addr {
	return &Addr{UUID: l.uuid, Channel: l.channel, Role: "listen"}
}

// Accept 接受一个新的 RFCOMM 客户端，并组装本地/远端地址摘要。
func (l *winListener) Accept() (core.IPipe, net.Addr, net.Addr, error) {
	if l.closed.Load() {
		return nil, nil, nil, errors.New("listener closed")
	}
	nfd, rsa, err := acceptSock(l.lnSock)
	if err != nil {
		return nil, nil, nil, err
	}
	setSockSendTimeout(nfd, winRFCOMMSendTimeoutMs)
	pipe := &winSockPipe{sock: nfd}

	remoteBD := ""
	if rsa != nil {
		remoteBD = bthAddrToBDAddr(rsa.BtAddr)
	}
	local := &Addr{UUID: l.uuid, Channel: l.channel, Role: "listen"}
	remote := &Addr{BDAddr: remoteBD, UUID: l.uuid, Channel: l.channel, Role: "listen"}
	return pipe, local, remote, nil
}

// Close 删除服务记录并关闭监听 socket。
func (l *winListener) Close() error {
	if l == nil {
		return nil
	}
	if l.closed.Swap(true) {
		return nil
	}
	// Best-effort delete.
	if l.svcRegDone.Load() {
		_ = l.deleteService()
	}
	if l.lnSock != windows.InvalidHandle {
		_ = windows.Closesocket(l.lnSock)
		l.lnSock = windows.InvalidHandle
	}
	return nil
}

// registerService 把当前 RFCOMM listener 注册到 Windows 蓝牙服务发现中。
func (l *winListener) registerService() error {
	if l.channel <= 0 {
		return errors.New("rfcomm channel not assigned")
	}

	raw := newRawSockaddrBth(0, windows.GUID{}, uint32(l.channel))

	// x/sys/windows expects RawSockaddrAny pointer, we pass SOCKADDR_BTH with same initial family field.
	localSockaddr := (*syscall.RawSockaddrAny)(unsafe.Pointer(&raw))
	cs := windows.CSAddrInfo{
		LocalAddr: windows.SocketAddress{
			Sockaddr:       localSockaddr,
			SockaddrLength: int32(unsafe.Sizeof(raw)),
		},
		RemoteAddr: windows.SocketAddress{},
		SocketType: int32(sockStream),
		Protocol:   int32(bthprotoRFCOMM),
	}
	qs := windows.WSAQUERYSET{
		Size:                uint32(unsafe.Sizeof(windows.WSAQUERYSET{})),
		ServiceInstanceName: l.svcName,
		ServiceClassId:      &l.uuidG,
		NameSpace:           windows.NS_BTH,
		NumberOfCsAddrs:     1,
		SaBuffer:            &cs,
	}
	if err := wsaSetService(&qs, wsaServiceRegister, 0); err != nil {
		return err
	}
	l.svcRegDone.Store(true)
	return nil
}

// deleteService 从 Windows SDP 中撤销当前 listener 的服务记录。
func (l *winListener) deleteService() error {
	cs := windows.CSAddrInfo{}
	qs := windows.WSAQUERYSET{
		Size:                uint32(unsafe.Sizeof(windows.WSAQUERYSET{})),
		ServiceInstanceName: l.svcName,
		ServiceClassId:      &l.uuidG,
		NameSpace:           windows.NS_BTH,
		NumberOfCsAddrs:     0,
		SaBuffer:            &cs,
	}
	if err := wsaSetService(&qs, wsaServiceDelete, 0); err != nil {
		return err
	}
	l.svcRegDone.Store(false)
	return nil
}

type wsaSetServiceOp uint32

const (
	wsaServiceRegister wsaSetServiceOp = 0 // RNRSERVICE_REGISTER
	wsaServiceDelete   wsaSetServiceOp = 2 // RNRSERVICE_DELETE
)

// wsaSetService 封装 `WSASetServiceW` 调用，统一错误转换。
func wsaSetService(qs *windows.WSAQUERYSET, op wsaSetServiceOp, flags uint32) error {
	r0, _, callErr := procWSASetServiceW.Call(uintptr(unsafe.Pointer(qs)), uintptr(op), uintptr(flags))
	if int32(r0) == -1 {
		return winsockCallErr(callErr)
	}
	return nil
}

// bdAddrToBTHAddr 把字符串蓝牙地址转成 Windows BTH_ADDR 使用的整数布局。
func bdAddrToBTHAddr(bdaddr string) (uint64, error) {
	bdaddr = strings.TrimSpace(bdaddr)
	bdaddr, err := normalizeBDAddr(bdaddr)
	if err != nil {
		return 0, err
	}
	// "AA:BB:CC:DD:EE:FF"
	parts := strings.Split(bdaddr, ":")
	if len(parts) != 6 {
		return 0, fmt.Errorf("invalid bdaddr: %q", bdaddr)
	}
	var b [6]byte
	for i := 0; i < 6; i++ {
		v, err := strconvHexByte(parts[i])
		if err != nil {
			return 0, err
		}
		b[i] = v
	}
	// BTH_ADDR uses NAP/SAP split; keep the same order as string.
	return uint64(b[0])<<40 | uint64(b[1])<<32 | uint64(b[2])<<24 | uint64(b[3])<<16 | uint64(b[4])<<8 | uint64(b[5]), nil
}

// bthAddrToBDAddr 把 Windows BTH_ADDR 还原成常见的冒号分隔蓝牙地址。
func bthAddrToBDAddr(addr uint64) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		byte(addr>>40),
		byte(addr>>32),
		byte(addr>>24),
		byte(addr>>16),
		byte(addr>>8),
		byte(addr),
	)
}

// uuidToGUID 将 RFCOMM 服务 UUID 文本转为 Windows API 需要的 GUID 结构。
func uuidToGUID(uuid string) (windows.GUID, error) {
	uuid = strings.ToLower(strings.TrimSpace(uuid))
	if !isUUIDLike(uuid) {
		return windows.GUID{}, ErrEndpointUUIDInvalid
	}
	// Parse hex digits.
	var b [16]byte
	j := 0
	for i := 0; i < len(uuid); i++ {
		c := uuid[i]
		if c == '-' {
			continue
		}
		if j >= 32 {
			return windows.GUID{}, ErrEndpointUUIDInvalid
		}
		hi, ok := fromHex(c)
		if !ok {
			return windows.GUID{}, ErrEndpointUUIDInvalid
		}
		i++
		if i >= len(uuid) {
			return windows.GUID{}, ErrEndpointUUIDInvalid
		}
		lo, ok := fromHex(uuid[i])
		if !ok {
			return windows.GUID{}, ErrEndpointUUIDInvalid
		}
		b[j/2] = (hi << 4) | lo
		j += 2
	}
	if j != 32 {
		return windows.GUID{}, ErrEndpointUUIDInvalid
	}
	g := windows.GUID{
		Data1: uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]),
		Data2: uint16(b[4])<<8 | uint16(b[5]),
		Data3: uint16(b[6])<<8 | uint16(b[7]),
	}
	copy(g.Data4[:], b[8:16])
	return g, nil
}
