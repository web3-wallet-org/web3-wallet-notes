package swap

import (
	"encoding/json"
	"net/http"
)

// writeJSON 是测试用的辅助函数，用于在 fake HTTP server 里返回 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
