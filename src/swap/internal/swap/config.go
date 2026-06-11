package swap

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type ChainConfig struct {
	ChainID      int64
	Name         string
	RPCURL       string
	GasType      GasType
	NativeSymbol string
}

type TokenConfig struct {
	Address       string
	Symbol        string
	Decimals      int
	RiskTier      string
	FeeOnTransfer bool
}

type ProviderConfig struct {
	ZeroXBaseURL      string
	ZeroXAPIKey       string
	OneInchBaseURL    string
	OneInchAPIKey     string
	KyberSwapBaseURL  string
	KyberSwapClientID string
	RequestTimeout    time.Duration
}

type Config struct {
	Chains                   map[int64]ChainConfig
	Tokens                   map[int64]map[string]TokenConfig
	Provider                 ProviderConfig
	QuoteTTL                 time.Duration
	MaxMainstreamSlippageBps int64
	SpenderWhitelist         map[int64]map[string]struct{}
	RouterWhitelist          map[int64]map[string]struct{}
	TokenBlacklist           map[int64]map[string]struct{}
}

func DefaultConfig() Config {
	cfg := Config{
		Chains: map[int64]ChainConfig{
			1: {
				ChainID:      1,
				Name:         "Ethereum",
				GasType:      GasTypeEIP1559,
				NativeSymbol: "ETH",
			},
			56: {
				ChainID:      56,
				Name:         "BSC",
				GasType:      GasTypeLegacy,
				NativeSymbol: "BNB",
			},
			8453: {
				ChainID:      8453,
				Name:         "Base",
				GasType:      GasTypeEIP1559,
				NativeSymbol: "ETH",
			},
		},
		Tokens: map[int64]map[string]TokenConfig{},
		Provider: ProviderConfig{
			ZeroXBaseURL:      "https://api.0x.org",
			OneInchBaseURL:    "https://api.1inch.dev",
			KyberSwapBaseURL:  "https://aggregator-api.kyberswap.com",
			KyberSwapClientID: "web3-wallet",
			RequestTimeout:    16 * time.Second,
		},
		QuoteTTL:                 20 * time.Second,
		MaxMainstreamSlippageBps: 300,
		SpenderWhitelist:         map[int64]map[string]struct{}{},
		RouterWhitelist:          map[int64]map[string]struct{}{},
		TokenBlacklist:           map[int64]map[string]struct{}{},
	}

	cfg.addToken(1, TokenConfig{Address: NativeTokenAddress, Symbol: "ETH", Decimals: 18, RiskTier: "mainstream"})
	cfg.addToken(1, TokenConfig{Address: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", Symbol: "USDC", Decimals: 6, RiskTier: "mainstream"})
	cfg.addToken(1, TokenConfig{Address: "0xdAC17F958D2ee523a2206206994597C13D831ec7", Symbol: "USDT", Decimals: 6, RiskTier: "mainstream"})
	cfg.addToken(1, TokenConfig{Address: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", Symbol: "WETH", Decimals: 18, RiskTier: "mainstream"})
	cfg.addToken(1, TokenConfig{Address: "0x6B175474E89094C44Da98b954EedeAC495271d0F", Symbol: "DAI", Decimals: 18, RiskTier: "mainstream"})

	cfg.addToken(56, TokenConfig{Address: NativeTokenAddress, Symbol: "BNB", Decimals: 18, RiskTier: "mainstream"})
	cfg.addToken(56, TokenConfig{Address: "0x55d398326f99059fF775485246999027B3197955", Symbol: "USDT", Decimals: 18, RiskTier: "mainstream"})
	cfg.addToken(56, TokenConfig{Address: "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d", Symbol: "USDC", Decimals: 18, RiskTier: "mainstream"})
	cfg.addToken(56, TokenConfig{Address: "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c", Symbol: "WBNB", Decimals: 18, RiskTier: "mainstream"})

	cfg.addToken(8453, TokenConfig{Address: NativeTokenAddress, Symbol: "ETH", Decimals: 18, RiskTier: "mainstream"})
	cfg.addToken(8453, TokenConfig{Address: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", Symbol: "USDC", Decimals: 6, RiskTier: "mainstream"})
	cfg.addToken(8453, TokenConfig{Address: "0x4200000000000000000000000000000000000006", Symbol: "WETH", Decimals: 18, RiskTier: "mainstream"})

	return cfg
}

func (c *Config) LoadFromEnv() error {
	c.Provider.ZeroXAPIKey = strings.TrimSpace(os.Getenv("ZEROX_API_KEY"))
	c.Provider.OneInchAPIKey = strings.TrimSpace(os.Getenv("ONEINCH_API_KEY"))
	if v := strings.TrimSpace(os.Getenv("KYBERSWAP_CLIENT_ID")); v != "" {
		c.Provider.KyberSwapClientID = v
	}

	if v := strings.TrimSpace(os.Getenv("ZEROX_BASE_URL")); v != "" {
		c.Provider.ZeroXBaseURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("ONEINCH_BASE_URL")); v != "" {
		c.Provider.OneInchBaseURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("KYBERSWAP_BASE_URL")); v != "" {
		c.Provider.KyberSwapBaseURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("SWAP_QUOTE_TTL_SECONDS")); v != "" {
		seconds, err := strconv.Atoi(v)
		if err != nil || seconds <= 0 {
			return fmt.Errorf("SWAP_QUOTE_TTL_SECONDS must be positive integer")
		}
		c.QuoteTTL = time.Duration(seconds) * time.Second
	}
	if v := strings.TrimSpace(os.Getenv("SWAP_RPC_ETHEREUM")); v != "" {
		cc := c.Chains[1]
		cc.RPCURL = v
		c.Chains[1] = cc
	}
	if v := strings.TrimSpace(os.Getenv("SWAP_RPC_BSC")); v != "" {
		cc := c.Chains[56]
		cc.RPCURL = v
		c.Chains[56] = cc
	}
	if v := strings.TrimSpace(os.Getenv("SWAP_RPC_BASE")); v != "" {
		cc := c.Chains[8453]
		cc.RPCURL = v
		c.Chains[8453] = cc
	}

	loadAddressSet(os.Getenv("SWAP_SPENDER_WHITELIST"), c.SpenderWhitelist)
	loadAddressSet(os.Getenv("SWAP_ROUTER_WHITELIST"), c.RouterWhitelist)
	loadAddressSet(os.Getenv("SWAP_TOKEN_BLACKLIST"), c.TokenBlacklist)
	return nil
}

func (c Config) Chain(chainID int64) (ChainConfig, bool) {
	cc, ok := c.Chains[chainID]
	return cc, ok
}

func (c Config) Token(chainID int64, address string) TokenConfig {
	if isNativeToken(address) {
		if tokens, ok := c.Tokens[chainID]; ok {
			if token, ok := tokens[normalizeAddress(NativeTokenAddress)]; ok {
				return token
			}
		}
	}
	if tokens, ok := c.Tokens[chainID]; ok {
		if token, ok := tokens[normalizeAddress(address)]; ok {
			return token
		}
	}
	return TokenConfig{Address: address, Symbol: "UNKNOWN", Decimals: 18, RiskTier: "unknown"}
}

func (c Config) CheckRunnable() error {
	if len(c.Chains) == 0 {
		return errors.New("no chains configured")
	}
	return nil
}

func (c *Config) addToken(chainID int64, token TokenConfig) {
	if c.Tokens[chainID] == nil {
		c.Tokens[chainID] = map[string]TokenConfig{}
	}
	c.Tokens[chainID][normalizeAddress(token.Address)] = token
}

func loadAddressSet(raw string, target map[int64]map[string]struct{}) {
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, ":", 2)
		if len(parts) != 2 {
			continue
		}
		chainID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		if target[chainID] == nil {
			target[chainID] = map[string]struct{}{}
		}
		target[chainID][normalizeAddress(parts[1])] = struct{}{}
	}
}
