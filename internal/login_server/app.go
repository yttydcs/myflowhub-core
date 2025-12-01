package login_server

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/bootstrap"
	"MyFlowHub-Core/internal/core/config"
	cfgbuilder "MyFlowHub-Core/internal/core/config/builder"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/listener/tcp_listener"
	"MyFlowHub-Core/internal/core/process"
	"MyFlowHub-Core/internal/core/server"
	"MyFlowHub-Core/internal/handler"
)

type App struct {
	cfg       Config
	coreCfg   core.IConfig
	log       *slog.Logger
	store     Store
	registrar *Registrar
	srv       *server.Server
	cred      string
}

func NewApp(cfg Config, log *slog.Logger) (*App, error) {
	if cfg.DSN == "" {
		return nil, errors.New("dsn required")
	}
	if cfg.Addr == "" {
		cfg.Addr = ":9100"
	}
	if cfg.RootNodeID == 0 {
		cfg.RootNodeID = 1
	}
	if cfg.SelfID == "" {
		cfg.SelfID = defaultSelfID()
	}
	if !cfg.ParentEnable && cfg.NodeID == 0 {
		cfg.NodeID = 1
	}
	if log == nil {
		log = slog.Default()
	}
	// 自注册获取 node_id（有父节点且未预设 node_id 时）
	if cfg.ParentEnable && cfg.NodeID == 0 && cfg.ParentAddr != "" {
		nodeID, cred, err := bootstrap.SelfRegister(context.Background(), bootstrap.SelfRegisterOptions{
			ParentAddr:  cfg.ParentAddr,
			SelfID:      cfg.SelfID,
			Timeout:     10 * time.Second,
			DoLogin:     false, // 允许后续补登录
			Logger:      log,
			DialTimeout: 5 * time.Second,
		})
		if err != nil {
			return nil, err
		}
		cfg.NodeID = nodeID
		cfg.RootToken = cred // 缓存凭证（当前未用，可后续扩展）
	}

	coreCfg := cfg.toCoreConfig()
	store, err := NewPostgresStore(cfg.DSN)
	if err != nil {
		return nil, err
	}
	reg := NewRegistrar(cfg.RootToken, cfg.RootNodeID, log)
	app := &App{
		cfg:       cfg,
		coreCfg:   coreCfg,
		log:       log,
		store:     store,
		registrar: reg,
		cred:      cfg.RootToken,
	}
	if err := app.initServer(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return app, nil
}

func (a *App) initServer() error {
	cfgMap := a.coreCfg
	cm := connmgr.New()
	base := process.NewPreRoutingProcess(a.log).WithConfig(cfgMap)
	dispatcher, err := process.NewDispatcherFromConfig(cfgMap, base, a.log)
	if err != nil {
		return err
	}
	if err := dispatcher.RegisterHandler(NewAuthorityHandlerWithConfig(a.store, cfgMap, a.log)); err != nil {
		return err
	}
	dispatcher.RegisterDefaultHandler(handler.NewDefaultForwardHandler(cfgMap, a.log))

	proc := NewProcessWrapper(dispatcher, a.registrar)
	lst := tcp_listener.New(a.cfg.Addr, tcp_listener.Options{
		KeepAlive:       true,
		KeepAlivePeriod: 30 * time.Second,
		Logger:          a.log,
	})
	codec := header.HeaderTcpCodec{}
	srv, err := server.New(server.Options{
		Name:     "LoginServer",
		Logger:   a.log,
		Process:  proc,
		Codec:    codec,
		Listener: lst,
		Config:   cfgMap,
		Manager:  cm,
		NodeID:   a.cfg.NodeID,
	})
	if err != nil {
		return err
	}
	proc.SetServerProvider(func() core.IServer { return srv })
	a.srv = srv
	return nil
}

func (a *App) Start(ctx context.Context) error {
	if a.srv == nil {
		return errors.New("server not initialized")
	}
	return a.srv.Start(ctx)
}

func (a *App) Stop(ctx context.Context) error {
	var first error
	if a.srv != nil {
		if err := a.srv.Stop(ctx); err != nil && first == nil {
			first = err
		}
	}
	if a.store != nil {
		if err := a.store.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func LoadConfigFromEnv() Config {
	finalCfg := buildMergedConfig()
	return Config{
		Addr:               readString(finalCfg, "addr", ":9100"),
		DSN:                pickFirstNonEmpty(getenv("LOGIN_PG_DSN", ""), readString(finalCfg, "pg.dsn", "")),
		NodeID:             readUint32(finalCfg, "node.id", 0),
		ParentAddr:         readString(finalCfg, config.KeyParentAddr, ""),
		ParentEnable:       readBool(finalCfg, config.KeyParentEnable, false),
		ParentReconnectSec: int(readInt(finalCfg, config.KeyParentReconnectSec, 3)),
		RootToken:          readString(finalCfg, "root.token", getenv("LOGIN_ROOT_TOKEN", "")),
		RootNodeID:         readUint32(finalCfg, "root.node_id", 1),
		SelfID:             readString(finalCfg, "self.id", defaultSelfID()),
		ProcessChannels:    int(readInt(finalCfg, config.KeyProcChannelCount, 2)),
		ProcessWorkers:     int(readInt(finalCfg, config.KeyProcWorkersPerChan, 2)),
		ProcessBuffer:      int(readInt(finalCfg, config.KeyProcChannelBuffer, 128)),
		SendChannels:       int(readInt(finalCfg, config.KeySendChannelCount, 1)),
		SendWorkers:        int(readInt(finalCfg, config.KeySendWorkersPerChan, 1)),
		SendChannelBuffer:  int(readInt(finalCfg, config.KeySendChannelBuffer, 64)),
		SendConnBuffer:     int(readInt(finalCfg, config.KeySendConnBuffer, 64)),
	}
}

func parseBool(v string, def bool) bool {
	if v == "" {
		return def
	}
	return core.ParseBool(v, def)
}

func buildMergedConfig() core.IConfig {
	defaults := config.NewMap(map[string]string{
		"addr":                       ":9100",
		"node.id":                    "0",
		config.KeyParentEnable:       "false",
		config.KeyParentAddr:         "",
		config.KeyParentReconnectSec: "3",
		"root.token":                 "",
		"root.node_id":               "1",
		"self.id":                    defaultSelfID(),
		config.KeyProcChannelCount:   "2",
		config.KeyProcWorkersPerChan: "2",
		config.KeyProcChannelBuffer:  "128",
		config.KeySendChannelCount:   "1",
		config.KeySendWorkersPerChan: "1",
		config.KeySendChannelBuffer:  "64",
		config.KeySendConnBuffer:     "64",
	})
	cfgs := []core.IConfig{defaults}
	if path := getenv("LOGIN_CONFIG_FILE", ""); path != "" {
		if cfg, err := (cfgbuilder.YAMLBuilder{Path: path}).Load(); err == nil {
			cfgs = append(cfgs, cfg)
		}
	}
	if envCfg, err := (cfgbuilder.EnvBuilder{Prefix: "LOGIN_"}).Load(); err == nil {
		cfgs = append(cfgs, envCfg)
	}
	return mergeConfigs(cfgs...)
}

func mergeConfigs(cfgs ...core.IConfig) core.IConfig {
	if len(cfgs) == 0 {
		return nil
	}
	base := cfgs[0]
	for _, c := range cfgs[1:] {
		if base != nil {
			base = base.Merge(c)
		}
	}
	return base
}

func readString(cfg core.IConfig, key, def string) string {
	if cfg == nil {
		return def
	}
	if v, ok := cfg.Get(key); ok {
		return v
	}
	return def
}

func readUint32(cfg core.IConfig, key string, def uint32) uint32 {
	if cfg == nil {
		return def
	}
	if v, ok := cfg.Get(key); ok {
		if u, err := parseUint32Local(v); err == nil {
			return u
		}
	}
	return def
}

func readInt(cfg core.IConfig, key string, def int64) int64 {
	if cfg == nil {
		return def
	}
	if v, ok := cfg.Get(key); ok {
		if i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return i
		}
	}
	return def
}

func parseUint32Local(v string) (uint32, error) {
	val, err := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
	return uint32(val), err
}

func readBool(cfg core.IConfig, key string, def bool) bool {
	if cfg == nil {
		return def
	}
	if v, ok := cfg.Get(key); ok {
		return core.ParseBool(v, def)
	}
	return def
}

func pickFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func defaultSelfID() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return "hub-" + h
	}
	return "hub-unknown"
}
