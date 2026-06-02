package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/web3-wallet-org/web3-wallet/src/swap/internal/swap"
)

func main() {
	cfg := swap.DefaultConfig()
	if err := cfg.LoadFromEnv(); err != nil {
		log.Fatalf("load config: %v", err)
	}

	repo := swap.NewMemoryRepository(time.Now)
	rpc := swap.NewRPCClient(http.DefaultClient, cfg)

	providers := []swap.QuoteProvider{
		swap.NewZeroXProvider(http.DefaultClient, cfg),
		swap.NewOneInchProvider(http.DefaultClient, cfg),
	}

	service := swap.NewService(cfg, repo, rpc, providers, time.Now)
	handler := swap.NewHTTPHandler(service)

	addr := os.Getenv("SWAP_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	log.Printf("swap api listening on %s", addr)
	if err := http.ListenAndServe(addr, handler.Routes()); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
