package swap

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	applog "github.com/web3-wallet-org/web3-wallet/src/swap/pkg/log"
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
	r := gin.New()
	r.Use(h.requestLogger()) // 注入 requestId/method/path 到 context
	r.Use(gin.Recovery())    // panic → 500，防止进程崩溃

	r.POST("/swap/quote", h.handleQuote)           // 获取报价
	r.POST("/swap/allowance", h.handleAllowance)   // 检查授权
	r.POST("/swap/approve-tx", h.handleApproveTx)  // 构造 approve 交易
	r.POST("/swap/execute", h.handleExecute)        // 执行 swap
	r.POST("/swap/submit-hash", h.handleSubmitHash) // 提交 txHash
	r.GET("/swap/status/:orderId", h.handleStatus)  // 查询订单状态
	r.GET("/healthz", h.handleHealthz)              // 健康检查
	return r
}

// requestLogger 为每个请求生成 requestId 并注入 context，
// 使后续所有 applog.FromContext(ctx) 的日志都自动携带 requestId / method / path。
func (h *HTTPHandler) requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		l := applog.FromContext(c.Request.Context()).With(
			"requestId", newRequestID(),
			"method", c.Request.Method,
			"path", c.FullPath(),
		)
		c.Request = c.Request.WithContext(applog.WithContext(c.Request.Context(), l))
		c.Next()
	}
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func (h *HTTPHandler) handleQuote(c *gin.Context) {
	var input QuoteInput
	if err := bindJSON(c, &input); err != nil {
		writeError(c, err)
		return
	}
	out, err := h.service.Quote(c.Request.Context(), input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *HTTPHandler) handleAllowance(c *gin.Context) {
	var req struct {
		QuoteID       string `json:"quoteId"`
		WalletAddress string `json:"walletAddress"`
	}
	if err := bindJSON(c, &req); err != nil {
		writeError(c, err)
		return
	}
	out, err := h.service.Allowance(c.Request.Context(), req.QuoteID, req.WalletAddress)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *HTTPHandler) handleApproveTx(c *gin.Context) {
	var req struct {
		QuoteID       string `json:"quoteId"`
		WalletAddress string `json:"walletAddress"`
	}
	if err := bindJSON(c, &req); err != nil {
		writeError(c, err)
		return
	}
	out, err := h.service.ApproveTx(c.Request.Context(), req.QuoteID, req.WalletAddress)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *HTTPHandler) handleExecute(c *gin.Context) {
	var req struct {
		QuoteID       string     `json:"quoteId"`
		WalletAddress string     `json:"walletAddress"`
		WalletType    WalletType `json:"walletType"`
	}
	if err := bindJSON(c, &req); err != nil {
		writeError(c, err)
		return
	}
	// walletType 缺省时视为外部钱包（MetaMask 等），兼容老客户端不传此字段的情况
	if req.WalletType == "" {
		req.WalletType = WalletTypeExternal
	}
	out, err := h.service.Execute(c.Request.Context(), req.QuoteID, req.WalletAddress, req.WalletType)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *HTTPHandler) handleSubmitHash(c *gin.Context) {
	var req SubmitHashRequest
	if err := bindJSON(c, &req); err != nil {
		writeError(c, err)
		return
	}
	if err := h.service.SubmitHash(c.Request.Context(), req); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *HTTPHandler) handleStatus(c *gin.Context) {
	orderID := c.Param("orderId")
	out, err := h.service.Status(c.Request.Context(), orderID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *HTTPHandler) handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HTTP 工具函数

// bindJSON 解析请求体为目标结构体。
// DisallowUnknownFields 确保客户端传了不认识的字段时直接报错，
// 避免因字段名拼写错误导致参数被静默忽略。
// Gin 默认不做此校验，这里手动用 json.Decoder 保持原有行为。
func bindJSON(c *gin.Context, out any) error {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return invalidArgument("invalid json: %v", err)
	}
	return nil
}

// writeError 将内部错误转换为统一的 JSON 错误响应。
// errors.As 优先匹配已经是 APIError 的情况（避免二次转换）；
// 否则通过 apiError() 将业务错误（ErrNotFound、ErrConflict 等）映射到对应 HTTP 状态码。
func writeError(c *gin.Context, err error) {
	var api APIError
	if errors.As(err, &api) {
		c.JSON(api.HTTPStatus, api)
		return
	}
	api = apiError(err)
	if api.HTTPStatus >= 500 {
		applog.FromContext(c.Request.Context()).Errorw("INTERNAL_ERROR", "err", err)
	}
	c.JSON(api.HTTPStatus, api)
}
