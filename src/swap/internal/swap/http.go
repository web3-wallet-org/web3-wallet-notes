package swap

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

// HTTPHandler 是薄薄的 HTTP 层，只负责：解析请求 → 调 Service → 写响应。
// 所有业务逻辑都在 Service 里，handler 本身不做任何业务判断。
type HTTPHandler struct {
	service *Service
}

func NewHTTPHandler(service *Service) *HTTPHandler {
	return &HTTPHandler{service: service}
}

func (h *HTTPHandler) Routes() http.Handler {
	mux := http.NewServeMux()
	// Go 1.22+ ServeMux 支持 "METHOD /path" 格式，自动按 HTTP 方法路由
	mux.HandleFunc("POST /swap/quote", h.handleQuote)            // 获取报价：同时询问 0x 和 1inch，返回最优 quote 和 quoteId
	mux.HandleFunc("POST /swap/allowance", h.handleAllowance)    // 检查授权：查链上 ERC20 allowance，native token 直接返回充足
	mux.HandleFunc("POST /swap/approve-tx", h.handleApproveTx)   // 构造 approve 交易：allowance 不足时，生成待签名的 ERC20 approve tx
	mux.HandleFunc("POST /swap/execute", h.handleExecute)        // 执行 swap：校验 quote 未过期 → 风控 → 构造 swap tx → 创建订单（SIGNING）
	mux.HandleFunc("POST /swap/submit-hash", h.handleSubmitHash) // 提交 txHash：外部钱包广播后，把 txHash 告知后端以便 Monitor 轮询
	mux.HandleFunc("GET /swap/status/", h.handleStatus)          // 查询订单状态：前端轮询，返回当前状态和 nextAction 指引；末尾 / 匹配 /{orderId}
	mux.HandleFunc("GET /healthz", h.handleHealthz)              // 健康检查：供 load balancer / k8s 探针使用
	return mux
}

func (h *HTTPHandler) handleQuote(w http.ResponseWriter, r *http.Request) {
	var input QuoteInput
	if err := readJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	out, err := h.service.Quote(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *HTTPHandler) handleAllowance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuoteID       string `json:"quoteId"`
		WalletAddress string `json:"walletAddress"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	out, err := h.service.Allowance(r.Context(), req.QuoteID, req.WalletAddress)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *HTTPHandler) handleApproveTx(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuoteID       string `json:"quoteId"`
		WalletAddress string `json:"walletAddress"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	out, err := h.service.ApproveTx(r.Context(), req.QuoteID, req.WalletAddress)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *HTTPHandler) handleExecute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuoteID       string     `json:"quoteId"`
		WalletAddress string     `json:"walletAddress"`
		WalletType    WalletType `json:"walletType"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	// walletType 缺省时视为外部钱包（MetaMask 等），兼容老客户端不传此字段的情况
	if req.WalletType == "" {
		req.WalletType = WalletTypeExternal
	}
	out, err := h.service.Execute(r.Context(), req.QuoteID, req.WalletAddress, req.WalletType)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *HTTPHandler) handleSubmitHash(w http.ResponseWriter, r *http.Request) {
	var req SubmitHashRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := h.service.SubmitHash(r.Context(), req); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HTTPHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	// ServeMux 匹配到前缀 /swap/status/，手动截取后面的 orderId
	orderID := strings.TrimPrefix(r.URL.Path, "/swap/status/")
	if orderID == "" {
		writeError(w, invalidArgument("orderId is required"))
		return
	}
	out, err := h.service.Status(r.Context(), orderID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *HTTPHandler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── HTTP 工具函数 ──────────────────────────────────────────────────────────────

// readJSON 解析请求体为目标结构体。
// DisallowUnknownFields 确保客户端传了不认识的字段时直接报错，
// 避免因字段名拼写错误导致参数被静默忽略。
func readJSON(r *http.Request, out any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return invalidArgument("invalid json: %v", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeError 将内部错误转换为统一的 JSON 错误响应。
// errors.As 优先匹配已经是 APIError 的情况（避免二次转换）；
// 否则通过 apiError() 将业务错误（ErrNotFound、ErrConflict 等）映射到对应 HTTP 状态码。
func writeError(w http.ResponseWriter, err error) {
	var api APIError
	if errors.As(err, &api) {
		writeJSON(w, api.HTTPStatus, api)
		return
	}
	api = apiError(err)
	if api.HTTPStatus >= 500 {
		log.Printf("INTERNAL_ERROR: %v", err)
	}
	writeJSON(w, api.HTTPStatus, api)
}
