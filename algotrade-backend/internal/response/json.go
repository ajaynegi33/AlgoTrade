package response

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorBody  `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func JSON(w http.ResponseWriter, status int, payload interface{}) {
	buf, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, `{"success":false,"error":{"code":"SERIALIZATION_ERROR","message":"Failed to serialize response"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

func Success(w http.ResponseWriter, status int, data interface{}) {
	JSON(w, status, Envelope{
		Success: true,
		Data:    data,
	})
}

func Message(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, Envelope{
		Success: true,
		Message: msg,
	})
}

func Error(w http.ResponseWriter, status int, code, msg string) {
	JSON(w, status, Envelope{
		Success: false,
		Error: &ErrorBody{
			Code:    code,
			Message: msg,
		},
	})
}
