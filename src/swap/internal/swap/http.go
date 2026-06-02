package swap

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type HTTPHandler struct {
	service *Service
}

func NewHTTPHandler(service *Service) *HTTPHandler {
	return &HTTPHandler{service: service}
}

func (h *HTTPHandler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /swap/quote", h.handleQuote)
	mux.HandleFunc("POST /swap/allowance", h.handleAllowance)
	mux.HandleFunc("POST /swap/approve-tx", h.handleApproveTx)
	mux.HandleFunc("POST /swap/execute", h.handleExecute)
	mux.HandleFunc("POST /swap/submit-hash", h.handleSubmitHash)
	mux.HandleFunc("GET /swap/status/", h.handleStatus)
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	return mux
}

func (h *HTTPHandler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func writeError(w http.ResponseWriter, err error) {
	var api APIError
	if errors.As(err, &api) {
		writeJSON(w, api.HTTPStatus, api)
		return
	}
	api = apiError(err)
	writeJSON(w, api.HTTPStatus, api)
}
