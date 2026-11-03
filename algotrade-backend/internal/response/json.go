package response

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	IsSuccess bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     *ErrorBody  `json:"error,omitempty"`
	Message   string      `json:"message,omitempty"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func JSON(responseWriter http.ResponseWriter, status int, payload interface{}) {
	buf, err := json.Marshal(payload)
	if err != nil {
		http.Error(responseWriter, `{"success":false,"error":{"code":"SERIALIZATION_ERROR","message":"Failed to serialize response"}}`, http.StatusInternalServerError)
		return
	}
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(status)
	_, _ = responseWriter.Write(buf)
}

func Success(responseWriter http.ResponseWriter, status int, data interface{}) {
	JSON(responseWriter, status, Envelope{
		IsSuccess: true,
		Data:      data,
	})
}

func Message(responseWriter http.ResponseWriter, status int, messageText string) {
	JSON(responseWriter, status, Envelope{
		IsSuccess: true,
		Message:   messageText,
	})
}

func Error(responseWriter http.ResponseWriter, status int, code, messageText string) {
	JSON(responseWriter, status, Envelope{
		IsSuccess: false,
		Error: &ErrorBody{
			Code:    code,
			Message: messageText,
		},
	})
}
