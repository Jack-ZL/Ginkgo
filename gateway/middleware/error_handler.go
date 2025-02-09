package middleware

import (
	"encoding/json"
	"log"
	"net/http"
)

// ErrorResponse 错误响应格式
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ErrorHandler 统一错误处理
func ErrorHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("Error: %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(ErrorResponse{
					Code:    http.StatusInternalServerError,
					Message: "Internal Server Error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
