package process

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/header"
)

var (
	errNilConn          = errors.New("nil connection")
	errNilCodec         = errors.New("nil codec")
	errNilRawConn       = errors.New("nil raw conn")
	errWriterClosed     = errors.New("writer closed")
	errEnqueueTimeout   = errors.New("enqueue timeout")
	errDispatcherClosed = errors.New("dispatcher closed")
)

// SendOptions configures the send dispatcher.
type SendOptions struct {
	Logger         *slog.Logger
	ChannelCount   int
	WorkersPerChan int
	ChannelBuffer  int
	ConnBuffer     int           // per-connection send queue length
	EnqueueTimeout time.Duration // enqueue timeout for both shard and per-conn queues
	EncodeInWriter bool          // encode in per-connection writer goroutine (strategy B)
}

type sendTask struct {
	ctx     context.Context
	conn    core.IConnection
	hdr     core.IHeader
	payload []byte
	codec   core.IHeaderCodec
	cb      func(error)
}

type connWriter struct {
	conn           core.IConnection
	ch             chan sendTask
	log            *slog.Logger
	encodeInWriter bool
	enqueueTimeout time.Duration

	closeOnce sync.Once
	closed    bool
	mu        sync.RWMutex
	wg        sync.WaitGroup
}

func (w *connWriter) start() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for task := range w.ch {
			err := w.write(task)
			if task.cb != nil {
				task.cb(err)
			}
		}
	}()
}

func (w *connWriter) write(task sendTask) error {
	if task.codec == nil {
		return errNilCodec
	}
	raw := w.conn.RawConn()
	if raw == nil {
		return errNilRawConn
	}

	if w.encodeInWriter {
		return writeFrame(raw, task.codec, task.hdr, task.payload)
	}
	// payload assumed encoded already
	_, err := raw.Write(task.payload)
	return err
}

func (w *connWriter) enqueue(task sendTask) (err error) {
	w.mu.RLock()
	closed := w.closed
	w.mu.RUnlock()
	if closed {
		return errWriterClosed
	}
	defer func() {
		if r := recover(); r != nil {
			w.mu.Lock()
			w.closed = true
			w.mu.Unlock()
			err = errWriterClosed
		}
	}()
	if w.enqueueTimeout <= 0 {
		w.ch <- task
		return nil
	}
	timer := time.NewTimer(w.enqueueTimeout)
	defer timer.Stop()
	select {
	case w.ch <- task:
		return nil
	case <-timer.C:
		return errEnqueueTimeout
	}
}

func (w *connWriter) stop() {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.ch)
		w.mu.Unlock()
	})
	w.wg.Wait()
}

// SendDispatcher fan-outs send requests to per-connection serial writers.
type SendDispatcher struct {
	log            *slog.Logger
	shards         []chan sendTask
	shardCount     int
	workersPerChan int
	connBuffer     int
	enqueueTimeout time.Duration
	encodeInWriter bool

	startOnce    sync.Once
	shutdownOnce sync.Once
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup

	mu      sync.RWMutex
	writers map[string]*connWriter
}

func NewSendDispatcher(opts SendOptions) (*SendDispatcher, error) {
	if opts.ChannelCount <= 0 {
		opts.ChannelCount = 1
	}
	if opts.WorkersPerChan <= 0 {
		opts.WorkersPerChan = 1
	}
	if opts.ChannelBuffer < 0 {
		opts.ChannelBuffer = 0
	}
	if opts.ConnBuffer <= 0 {
		opts.ConnBuffer = 64
	}
	if opts.EnqueueTimeout < 0 {
		opts.EnqueueTimeout = 0
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if !opts.EncodeInWriter {
		opts.EncodeInWriter = true
	}
	shards := make([]chan sendTask, opts.ChannelCount)
	for i := range shards {
		shards[i] = make(chan sendTask, opts.ChannelBuffer)
	}
	return &SendDispatcher{
		log:            opts.Logger,
		shards:         shards,
		shardCount:     opts.ChannelCount,
		workersPerChan: opts.WorkersPerChan,
		connBuffer:     opts.ConnBuffer,
		enqueueTimeout: opts.EnqueueTimeout,
		encodeInWriter: opts.EncodeInWriter,
		writers:        make(map[string]*connWriter),
	}, nil
}

// NewSendDispatcherFromConfig builds a dispatcher from config values.
func NewSendDispatcherFromConfig(cfg core.IConfig, logger *slog.Logger) (*SendDispatcher, error) {
	opts := SendOptions{
		Logger:         logger,
		ChannelCount:   readPositiveInt(cfg, coreconfig.KeySendChannelCount, 1),
		WorkersPerChan: readPositiveInt(cfg, coreconfig.KeySendWorkersPerChan, 1),
		ChannelBuffer:  readPositiveInt(cfg, coreconfig.KeySendChannelBuffer, 64),
		ConnBuffer:     readPositiveInt(cfg, coreconfig.KeySendConnBuffer, 64),
		EnqueueTimeout: readDurationMs(cfg, coreconfig.KeySendEnqueueTimeoutMS, 100),
		EncodeInWriter: true,
	}
	return NewSendDispatcher(opts)
}

func (d *SendDispatcher) ensureStarted(ctx context.Context) {
	d.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		d.ctx, d.cancel = context.WithCancel(ctx)
		for i := range d.shards {
			q := d.shards[i]
			d.wg.Add(1)
			go func(ch <-chan sendTask) {
				defer d.wg.Done()
				for task := range ch {
					if task.conn == nil {
						d.log.Warn("nil conn in send task")
						continue
					}
					writer := d.getOrCreateWriter(task.conn)
					if writer == nil {
						if task.cb != nil {
							task.cb(errWriterClosed)
						}
						continue
					}
					err := writer.enqueue(task)
					if err != nil && task.cb != nil {
						task.cb(err)
					}
				}
			}(q)
		}
		go func() {
			<-d.ctx.Done()
			for _, q := range d.shards {
				close(q)
			}
			d.mu.Lock()
			for _, w := range d.writers {
				w.stop()
			}
			d.writers = make(map[string]*connWriter)
			d.mu.Unlock()
		}()
	})
}

// Dispatch queues a send task.
func (d *SendDispatcher) Dispatch(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, codec core.IHeaderCodec, cb func(error)) error {
	if conn == nil {
		return errNilConn
	}
	d.ensureStarted(ctx)
	idx := d.selectQueue(conn, hdr)
	task := sendTask{ctx: ctx, conn: conn, hdr: hdr, payload: payload, codec: codec, cb: cb}
	if d.enqueueTimeout <= 0 {
		select {
		case d.shards[idx] <- task:
			return nil
		case <-d.ctx.Done():
			return errDispatcherClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	timer := time.NewTimer(d.enqueueTimeout)
	defer timer.Stop()
	select {
	case d.shards[idx] <- task:
		return nil
	case <-timer.C:
		return errEnqueueTimeout
	case <-d.ctx.Done():
		return errDispatcherClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *SendDispatcher) selectQueue(conn core.IConnection, hdr core.IHeader) int {
	if d.shardCount == 1 {
		return 0
	}
	if conn != nil {
		h := fnv.New32a()
		_, _ = h.Write([]byte(conn.ID()))
		return int(h.Sum32() % uint32(d.shardCount))
	}
	if hdr != nil {
		return int(hdr.SubProto()) % d.shardCount
	}
	return 0
}

func (d *SendDispatcher) getOrCreateWriter(conn core.IConnection) *connWriter {
	id := conn.ID()
	d.mu.RLock()
	if w, ok := d.writers[id]; ok {
		d.mu.RUnlock()
		return w
	}
	d.mu.RUnlock()

	d.mu.Lock()
	defer d.mu.Unlock()
	if w, ok := d.writers[id]; ok {
		return w
	}
	w := &connWriter{
		conn:           conn,
		ch:             make(chan sendTask, d.connBuffer),
		log:            d.log,
		encodeInWriter: d.encodeInWriter,
		enqueueTimeout: d.enqueueTimeout,
	}
	w.start()
	d.writers[id] = w
	return w
}

// CloseConn stops and removes the writer for a connection.
func (d *SendDispatcher) CloseConn(connID string) {
	d.mu.Lock()
	w, ok := d.writers[connID]
	if ok {
		delete(d.writers, connID)
	}
	d.mu.Unlock()
	if ok {
		w.stop()
	}
}

// Shutdown stops the dispatcher and waits for workers to exit.
func (d *SendDispatcher) Shutdown() {
	d.shutdownOnce.Do(func() {
		if d.cancel != nil {
			d.cancel()
		}
		d.wg.Wait()
	})
}

func (d *SendDispatcher) Snapshot() (channels, workers, buffer int) {
	channels = len(d.shards)
	workers = d.workersPerChan
	if channels > 0 {
		buffer = cap(d.shards[0])
	}
	return
}

func (d *SendDispatcher) String() string {
	ch, w, b := d.Snapshot()
	return fmt.Sprintf("SendDispatcher{channels=%d workers=%d buffer=%d connBuffer=%d enqueueTimeout=%s}", ch, w, b, d.connBuffer, d.enqueueTimeout)
}

// writeFrame encodes and writes a frame, preferring a zero-copy path for HeaderTcp.
func writeFrame(conn net.Conn, codec core.IHeaderCodec, hdr core.IHeader, payload []byte) error {
	switch c := codec.(type) {
	case header.HeaderTcpCodec:
		return writeTCPFrame(conn, c, hdr, payload)
	case *header.HeaderTcpCodec:
		return writeTCPFrame(conn, *c, hdr, payload)
	default:
		frame, err := codec.Encode(hdr, payload)
		if err != nil {
			return err
		}
		_, err = conn.Write(frame)
		return err
	}
}

func writeTCPFrame(conn net.Conn, _ header.HeaderTcpCodec, hdr core.IHeader, payload []byte) error {
	tcpHdr := header.CloneToTCP(hdr)
	if tcpHdr == nil {
		return errNilCodec
	}
	if tcpHdr.HopLimit == 0 {
		tcpHdr.HopLimit = header.DefaultHopLimit
	}
	if uint32(len(payload)) != tcpHdr.PayloadLen {
		tcpHdr.PayloadLen = uint32(len(payload))
	}
	buf := make([]byte, 32)
	binary.BigEndian.PutUint16(buf[0:2], header.HeaderTcpMagicV2)
	buf[2] = header.HeaderTcpVersionV2
	buf[3] = 32 // hdr_len
	buf[4] = tcpHdr.TypeFmt
	buf[5] = tcpHdr.Flags
	buf[6] = tcpHdr.HopLimit
	buf[7] = tcpHdr.RouteFlags
	binary.BigEndian.PutUint32(buf[8:12], tcpHdr.MsgID)
	binary.BigEndian.PutUint32(buf[12:16], tcpHdr.Source)
	binary.BigEndian.PutUint32(buf[16:20], tcpHdr.Target)
	binary.BigEndian.PutUint32(buf[20:24], tcpHdr.TraceID)
	binary.BigEndian.PutUint32(buf[24:28], tcpHdr.Timestamp)
	binary.BigEndian.PutUint32(buf[28:32], tcpHdr.PayloadLen)
	if len(payload) == 0 {
		_, err := conn.Write(buf)
		return err
	}
	_, err := (&net.Buffers{buf, payload}).WriteTo(conn)
	return err
}

func readDurationMs(cfg core.IConfig, key string, def int) time.Duration {
	if cfg == nil {
		return time.Duration(def) * time.Millisecond
	}
	if raw, ok := cfg.Get(key); ok {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			return time.Duration(v) * time.Millisecond
		}
	}
	return time.Duration(def) * time.Millisecond
}
