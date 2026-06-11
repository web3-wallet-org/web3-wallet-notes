package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/web3-wallet-org/web3-wallet/src/swap/internal/swap"
	applog "github.com/web3-wallet-org/web3-wallet/src/swap/pkg/log"
)

// AppConfig 是 config.yaml 的完整映射。
// 每个顶层 key 对应一个子系统，后续加 DB 直接在 database 下扩展。
type AppConfig struct {
	Server   ServerConfig   `yaml:"server"`
	Swap     SwapConfig     `yaml:"swap"`
	Database DatabaseConfig `yaml:"database"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

type SwapConfig struct {
	QuoteTTLSeconds int                  `yaml:"quote_ttl_seconds"`
	MaxSlippageBps  int64                `yaml:"max_slippage_bps"`
	Providers       ProvidersConfig      `yaml:"providers"`
	Chains          map[int64]ChainEntry `yaml:"chains"`
}

type ProvidersConfig struct {
	ZeroX     ProviderEntry `yaml:"zerox"`
	OneInch   ProviderEntry `yaml:"oneinch"`
	KyberSwap ProviderEntry `yaml:"kyberswap"`
}

type ProviderEntry struct {
	Enabled   bool   `yaml:"enabled"`
	APIKey    string `yaml:"api_key"`
	ClientID  string `yaml:"client_id"`
	BaseURL   string `yaml:"base_url"`
	TimeoutMs int    `yaml:"timeout_ms"`
}

type ChainEntry struct {
	RPCURL string `yaml:"rpc_url"`
}

type DatabaseConfig struct {
	Postgres PostgresConfig `yaml:"postgres"`
	MySQL    MySQLConfig    `yaml:"mysql"`
	Mongo    MongoConfig    `yaml:"mongo"`
	Redis    RedisConfig    `yaml:"redis"`
}

type PostgresConfig struct {
	DSN string `yaml:"dsn"`
}

type MySQLConfig struct {
	DSN string `yaml:"dsn"`
}

type MongoConfig struct {
	URI string `yaml:"uri"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	defer logger.Sync()
	applog.Init(logger)

	appCfg := loadConfig(*configPath)
	cfg := buildSwapConfig(appCfg)

	repo := swap.NewMemoryRepository(time.Now)
	rpc := swap.NewRPCClient(http.DefaultClient, cfg)

	var providers []swap.QuoteProvider
	if appCfg.Swap.Providers.ZeroX.Enabled {
		providers = append(providers, swap.NewZeroXProvider(http.DefaultClient, cfg))
	}
	if appCfg.Swap.Providers.OneInch.Enabled {
		providers = append(providers, swap.NewOneInchProvider(http.DefaultClient, cfg))
	}
	if appCfg.Swap.Providers.KyberSwap.Enabled {
		providers = append(providers, swap.NewKyberSwapProvider(http.DefaultClient, cfg))
	}
	if len(providers) == 0 {
		log.Fatal("no providers enabled, check config.yaml")
	}

	service := swap.NewService(cfg, repo, rpc, providers, time.Now)
	handler := swap.NewHTTPHandler(service)

	addr := appCfg.Server.Addr
	if addr == "" {
		addr = ":8080"
	}

	log.Printf("swap api listening on %s", addr)
	if err := http.ListenAndServe(addr, handler.Routes()); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

// loadConfig 读取 YAML 配置文件，文件不存在时直接报错退出。
func loadConfig(path string) AppConfig {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("open config %s: %v", path, err)
	}
	defer f.Close()

	var cfg AppConfig
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		log.Fatalf("parse config %s: %v", path, err)
	}
	return cfg
}

// buildSwapConfig 把 AppConfig 里 swap 相关的部分转换成 swap.Config。
func buildSwapConfig(app AppConfig) swap.Config {
	cfg := swap.DefaultConfig()

	if app.Swap.QuoteTTLSeconds > 0 {
		cfg.QuoteTTL = time.Duration(app.Swap.QuoteTTLSeconds) * time.Second
	}
	if app.Swap.MaxSlippageBps > 0 {
		cfg.MaxMainstreamSlippageBps = app.Swap.MaxSlippageBps
	}

	cfg.Provider.ZeroXAPIKey = app.Swap.Providers.ZeroX.APIKey
	cfg.Provider.OneInchAPIKey = app.Swap.Providers.OneInch.APIKey
	if v := app.Swap.Providers.KyberSwap.ClientID; v != "" {
		cfg.Provider.KyberSwapClientID = v
	} else if v := app.Swap.Providers.KyberSwap.APIKey; v != "" {
		cfg.Provider.KyberSwapClientID = v
	}

	if v := app.Swap.Providers.ZeroX.BaseURL; v != "" {
		cfg.Provider.ZeroXBaseURL = v
	}
	if v := app.Swap.Providers.OneInch.BaseURL; v != "" {
		cfg.Provider.OneInchBaseURL = v
	}
	if v := app.Swap.Providers.KyberSwap.BaseURL; v != "" {
		cfg.Provider.KyberSwapBaseURL = v
	}
	if ms := app.Swap.Providers.ZeroX.TimeoutMs; ms > 0 {
		cfg.Provider.RequestTimeout = time.Duration(ms) * time.Millisecond
	}
	if ms := app.Swap.Providers.KyberSwap.TimeoutMs; ms > 0 {
		cfg.Provider.RequestTimeout = time.Duration(ms) * time.Millisecond
	}

	// 把 config.yaml 里的 rpc_url 写入对应链配置
	for chainID, entry := range app.Swap.Chains {
		if entry.RPCURL == "" {
			continue
		}
		if cc, ok := cfg.Chains[chainID]; ok {
			cc.RPCURL = entry.RPCURL
			cfg.Chains[chainID] = cc
		}
	}

	return cfg
}
