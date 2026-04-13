//go:build linux && !android

package rfcomm_listener

// Context: This file provides shared Core framework logic around bluez_linux.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
	core "github.com/yttydcs/myflowhub-core"
	"golang.org/x/sys/unix"
)

const (
	bluezService              = "org.bluez"
	bluezProfileManagerPath   = dbus.ObjectPath("/org/bluez")
	bluezProfileManagerIface  = "org.bluez.ProfileManager1"
	bluezProfileIface         = "org.bluez.Profile1"
	bluezDeviceIface          = "org.bluez.Device1"
	bluezRegisterProfile      = bluezProfileManagerIface + ".RegisterProfile"
	bluezUnregisterProfile    = bluezProfileManagerIface + ".UnregisterProfile"
	bluezDeviceConnectProfile = bluezDeviceIface + ".ConnectProfile"
)

var bluezProfileSeq atomic.Uint64

func listenRFCOMMBlueZ(opts Options) (nativeListener, error) {
	bus, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("dbus system bus: %w", err)
	}

	path := newBluezProfilePath("listen")
	uuid := strings.ToLower(strings.TrimSpace(opts.UUID))
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	prof := newBluezProfile(bluezProfileConfig{
		log:             log,
		uuid:            uuid,
		expectedAdapter: strings.TrimSpace(opts.Adapter),
		addrRole:        "listen",
		singleShot:      false,
		wrapPipe:        nil,
	})

	cleanupOnce := sync.Once{}
	cleanup := func() {
		cleanupOnce.Do(func() {
			prof.finish(errors.New("listener closed"))
			_ = bus.Object(bluezService, bluezProfileManagerPath).Call(bluezUnregisterProfile, 0, path).Err
			bus.Export(nil, path, bluezProfileIface)
			_ = bus.Close()
		})
	}

	if err := bus.Export(prof, path, bluezProfileIface); err != nil {
		cleanup()
		return nil, err
	}

	options := map[string]dbus.Variant{
		"Name":                  dbus.MakeVariant("MyFlowHub"),
		"Role":                  dbus.MakeVariant("server"),
		"AutoConnect":           dbus.MakeVariant(true),
		"RequireAuthentication": dbus.MakeVariant(!opts.Insecure),
		"RequireAuthorization":  dbus.MakeVariant(false),
	}
	if opts.Channel > 0 {
		options["Channel"] = dbus.MakeVariant(uint16(opts.Channel))
	}

	if err := bus.Object(bluezService, bluezProfileManagerPath).Call(bluezRegisterProfile, 0, path, uuid, options).Err; err != nil {
		cleanup()
		return nil, fmt.Errorf("bluez register profile: %w", err)
	}

	return &linuxBluezListener{
		bus:     bus,
		path:    path,
		profile: prof,
		uuid:    uuid,
		channel: opts.Channel,
		closeFn: cleanup,
	}, nil
}

func dialRFCOMMByUUIDBlueZ(ctx context.Context, opts DialOptions) (core.IPipe, error) {
	bdaddr, err := normalizeBDAddr(opts.BDAddr)
	if err != nil {
		return nil, err
	}
	uuid := strings.ToLower(strings.TrimSpace(opts.UUID))
	adapter := strings.TrimSpace(opts.Adapter)

	bus, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("dbus system bus: %w", err)
	}

	path := newBluezProfilePath("dial")
	log := slog.Default()

	cleanupOnce := sync.Once{}
	cleanup := func() {
		cleanupOnce.Do(func() {
			_ = bus.Object(bluezService, bluezProfileManagerPath).Call(bluezUnregisterProfile, 0, path).Err
			bus.Export(nil, path, bluezProfileIface)
			_ = bus.Close()
		})
	}

	wrap := func(f *os.File) core.IPipe {
		return &bluezDialPipe{file: f, cleanup: cleanup}
	}

	prof := newBluezProfile(bluezProfileConfig{
		log:             log,
		uuid:            uuid,
		expectedAdapter: adapter,
		addrRole:        "dial",
		singleShot:      true,
		wrapPipe:        wrap,
	})

	if err := bus.Export(prof, path, bluezProfileIface); err != nil {
		cleanup()
		return nil, err
	}

	options := map[string]dbus.Variant{
		"Name":                  dbus.MakeVariant("MyFlowHub"),
		"Role":                  dbus.MakeVariant("client"),
		"RequireAuthentication": dbus.MakeVariant(!opts.Insecure),
		"RequireAuthorization":  dbus.MakeVariant(false),
	}
	if err := bus.Object(bluezService, bluezProfileManagerPath).Call(bluezRegisterProfile, 0, path, uuid, options).Err; err != nil {
		cleanup()
		return nil, fmt.Errorf("bluez register profile: %w", err)
	}

	devicePath := bluezDeviceObjectPath(adapter, bdaddr)
	if err := bus.Object(bluezService, devicePath).Call(bluezDeviceConnectProfile, 0, uuid).Err; err != nil {
		cleanup()
		// Most common: device object not present. The user can either pair/discover first, or specify channel.
		return nil, fmt.Errorf("bluez connect profile (%s): %w; hint: ensure the device is paired/known, or specify channel=... for channel-first dial", devicePath, err)
	}

	select {
	case <-ctx.Done():
		prof.finish(ctx.Err())
		cleanup()
		return nil, ctx.Err()
	case <-prof.doneCh:
		cleanup()
		return nil, errors.New("bluez dial aborted")
	case res := <-prof.acceptCh:
		if res.err != nil {
			prof.finish(res.err)
			cleanup()
			return nil, res.err
		}
		if res.pipe == nil {
			prof.finish(errors.New("nil pipe"))
			cleanup()
			return nil, errors.New("bluez dial returned nil pipe")
		}
		// NOTE: cleanup is bound to pipe.Close() via bluezDialPipe; do not call it here.
		return res.pipe, nil
	}
}

func newBluezProfilePath(kind string) dbus.ObjectPath {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "rfcomm"
	}
	n := bluezProfileSeq.Add(1)
	return dbus.ObjectPath(fmt.Sprintf("/github/yttydcs/myflowhub/rfcomm/%s/p%d", kind, n))
}

func bluezDeviceObjectPath(adapter string, bdaddr string) dbus.ObjectPath {
	adapter = strings.TrimSpace(adapter)
	if adapter == "" {
		adapter = "hci0"
	}
	bdaddr, _ = normalizeBDAddr(bdaddr)
	dev := "dev_" + strings.ReplaceAll(bdaddr, ":", "_")
	return dbus.ObjectPath("/org/bluez/" + adapter + "/" + dev)
}

func bluezBDAddrFromDevicePath(device dbus.ObjectPath) string {
	s := string(device)
	i := strings.LastIndex(s, "/dev_")
	if i < 0 {
		return ""
	}
	tail := s[i+len("/dev_"):]
	if tail == "" {
		return ""
	}
	addr := strings.ReplaceAll(tail, "_", ":")
	if bd, err := normalizeBDAddr(addr); err == nil {
		return bd
	}
	return ""
}

type bluezProfileConfig struct {
	log             *slog.Logger
	uuid            string
	expectedAdapter string
	addrRole        string
	singleShot      bool
	wrapPipe        func(*os.File) core.IPipe
}

type bluezProfile struct {
	log *slog.Logger

	uuid            string
	expectedAdapter string
	addrRole        string
	singleShot      bool
	wrapPipe        func(*os.File) core.IPipe

	acceptCh chan bluezAccept
	doneCh   chan struct{}

	closed atomic.Bool
}

type bluezAccept struct {
	pipe   core.IPipe
	local  net.Addr
	remote net.Addr
	err    error
}

func newBluezProfile(cfg bluezProfileConfig) *bluezProfile {
	log := cfg.log
	if log == nil {
		log = slog.Default()
	}
	return &bluezProfile{
		log:             log,
		uuid:            strings.ToLower(strings.TrimSpace(cfg.uuid)),
		expectedAdapter: strings.TrimSpace(cfg.expectedAdapter),
		addrRole:        strings.TrimSpace(cfg.addrRole),
		singleShot:      cfg.singleShot,
		wrapPipe:        cfg.wrapPipe,
		acceptCh:        make(chan bluezAccept, 16),
		doneCh:          make(chan struct{}),
	}
}

func (p *bluezProfile) finish(reason error) {
	if p.closed.Swap(true) {
		return
	}
	close(p.doneCh)
	// Best-effort notify accept waiters.
	select {
	case p.acceptCh <- bluezAccept{err: reason}:
	default:
	}
}

// Release is called when the profile gets unregistered.
func (p *bluezProfile) Release() *dbus.Error {
	p.finish(errors.New("bluez profile released"))
	return nil
}

// Cancel is called when a profile request gets canceled before a reply.
func (p *bluezProfile) Cancel() *dbus.Error {
	p.finish(errors.New("bluez profile canceled"))
	return nil
}

// RequestDisconnection is called when remote requests disconnection.
func (p *bluezProfile) RequestDisconnection(device dbus.ObjectPath) *dbus.Error {
	// We don't keep per-device state here. The RFCOMM socket FD should be closed by the peer / higher layers.
	p.log.Debug("bluez request disconnection", "device", string(device))
	return nil
}

// NewConnection is called when a new connection has been made.
func (p *bluezProfile) NewConnection(device dbus.ObjectPath, fd dbus.UnixFD, props map[string]dbus.Variant) *dbus.Error {
	if p.closed.Load() {
		_ = unix.Close(int(fd))
		return dbus.MakeFailedError(errors.New("profile closed"))
	}
	if p.expectedAdapter != "" {
		wantPrefix := "/org/bluez/" + p.expectedAdapter + "/"
		if !strings.HasPrefix(string(device), wantPrefix) {
			_ = unix.Close(int(fd))
			return dbus.MakeFailedError(fmt.Errorf("unexpected adapter: want %s", p.expectedAdapter))
		}
	}

	f := os.NewFile(uintptr(fd), "rfcomm")
	if f == nil {
		_ = unix.Close(int(fd))
		return dbus.MakeFailedError(errors.New("os.NewFile returned nil"))
	}

	pipe := core.IPipe(f)
	if p.wrapPipe != nil {
		pipe = p.wrapPipe(f)
	}

	remoteBD := bluezBDAddrFromDevicePath(device)
	local := &Addr{UUID: p.uuid, Role: p.addrRole}
	remote := &Addr{BDAddr: remoteBD, UUID: p.uuid, Role: p.addrRole}

	select {
	case p.acceptCh <- bluezAccept{pipe: pipe, local: local, remote: remote}:
		if p.singleShot {
			p.closed.Store(true)
		}
		return nil
	default:
		_ = pipe.Close()
		return dbus.MakeFailedError(errors.New("accept backlog full"))
	}
}

type linuxBluezListener struct {
	bus     *dbus.Conn
	path    dbus.ObjectPath
	profile *bluezProfile

	uuid    string
	channel int

	closeOnce sync.Once
	closeFn   func()
}

func (l *linuxBluezListener) Addr() net.Addr {
	return &Addr{UUID: l.uuid, Channel: l.channel, Role: "listen"}
}

func (l *linuxBluezListener) Accept() (core.IPipe, net.Addr, net.Addr, error) {
	select {
	case <-l.profile.doneCh:
		return nil, nil, nil, errors.New("listener closed")
	case res := <-l.profile.acceptCh:
		if res.err != nil {
			return nil, nil, nil, res.err
		}
		return res.pipe, res.local, res.remote, nil
	}
}

func (l *linuxBluezListener) Close() error {
	if l == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		if l.closeFn != nil {
			l.closeFn()
		}
	})
	return nil
}

type bluezDialPipe struct {
	file    *os.File
	cleanup func()
	closed  atomic.Bool
}

func (p *bluezDialPipe) Read(b []byte) (int, error)  { return p.file.Read(b) }
func (p *bluezDialPipe) Write(b []byte) (int, error) { return p.file.Write(b) }

func (p *bluezDialPipe) Close() error {
	if p == nil {
		return nil
	}
	if p.closed.Swap(true) {
		return nil
	}
	err := p.file.Close()
	if p.cleanup != nil {
		p.cleanup()
	}
	return err
}
