package swap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type RPCClient struct {
	httpClient *http.Client
	cfg        Config
}

type ChainClient interface {
	GetAllowance(ctx context.Context, chainID int64, token, owner, spender string) (string, error)
	GetBalance(ctx context.Context, chainID int64, address string) (string, error)
	EstimateGas(ctx context.Context, chainID int64, tx map[string]any) (string, error)
	GasPrice(ctx context.Context, chainID int64) (string, error)
	GetTransaction(ctx context.Context, chainID int64, hash string) (*rpcTx, error)
	ChainGasType(chainID int64) GasType
}

func NewRPCClient(httpClient *http.Client, cfg Config) *RPCClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &RPCClient{httpClient: httpClient, cfg: cfg}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *RPCClient) Call(ctx context.Context, chainID int64, method string, params any, out any) error {
	chain, ok := c.cfg.Chain(chainID)
	if !ok || chain.RPCURL == "" {
		return fmt.Errorf("%w: chain %d rpc missing", ErrInvalidArgument, chainID)
	}

	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      time.Now().UnixNano(),
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chain.RPCURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var decoded rpcResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	if decoded.Error != nil {
		return fmt.Errorf("rpc %s error %d: %s", method, decoded.Error.Code, decoded.Error.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(decoded.Result, out); err != nil {
		return err
	}
	return nil
}

func (c *RPCClient) GetAllowance(ctx context.Context, chainID int64, token, owner, spender string) (string, error) {
	data := "0xdd62ed3e"
	data += leftPadHexNoPrefix(strip0x(owner))
	data += leftPadHexNoPrefix(strip0x(spender))
	params := []any{map[string]any{
		"to":   token,
		"data": data,
	}, "latest"}
	var out string
	if err := c.Call(ctx, chainID, "eth_call", params, &out); err != nil {
		return "", err
	}
	return decimalize(out), nil
}

func (c *RPCClient) GetBalance(ctx context.Context, chainID int64, address string) (string, error) {
	params := []any{address, "latest"}
	var out string
	if err := c.Call(ctx, chainID, "eth_getBalance", params, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (c *RPCClient) EstimateGas(ctx context.Context, chainID int64, tx map[string]any) (string, error) {
	var out string
	if err := c.Call(ctx, chainID, "eth_estimateGas", []any{tx}, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (c *RPCClient) GasPrice(ctx context.Context, chainID int64) (string, error) {
	var out string
	if err := c.Call(ctx, chainID, "eth_gasPrice", []any{}, &out); err != nil {
		return "", err
	}
	return out, nil
}

type rpcTx struct {
	Hash             string `json:"hash"`
	From             string `json:"from"`
	To               string `json:"to"`
	Input            string `json:"input"`
	Value            string `json:"value"`
	BlockNumber      string `json:"blockNumber"`
	TransactionIndex string `json:"transactionIndex"`
}

func (c *RPCClient) GetTransaction(ctx context.Context, chainID int64, hash string) (*rpcTx, error) {
	var out *rpcTx
	if err := c.Call(ctx, chainID, "eth_getTransactionByHash", []any{hash}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *RPCClient) ChainGasType(chainID int64) GasType {
	if chain, ok := c.cfg.Chain(chainID); ok {
		if chain.GasType == GasTypeLegacy {
			return GasTypeLegacy
		}
	}
	return GasTypeEIP1559
}

func strip0x(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}
