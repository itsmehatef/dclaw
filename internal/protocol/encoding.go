package protocol

import (
	"encoding/json"
)

// SuccessResponse builds a JSON-RPC 2.0 success envelope from a result value.
func SuccessResponse(id any, result any) *Envelope {
	raw, _ := json.Marshal(result)
	return &Envelope{JSONRPC: "2.0", Result: raw, ID: id}
}

// ErrorResponse builds a JSON-RPC 2.0 error envelope.
func ErrorResponse(id any, code int, msg string, data any) *Envelope {
	return &Envelope{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: msg, Data: data},
		ID:      id,
	}
}

// Request builds a JSON-RPC 2.0 request envelope from a method + params.
// id should be the caller's monotonic counter.
func Request(id int, method string, params any) *Envelope {
	raw, _ := json.Marshal(params)
	return &Envelope{JSONRPC: "2.0", Method: method, Params: raw, ID: id}
}

// Notification builds a JSON-RPC 2.0 notification envelope.
func Notification(method string, params any) *Envelope {
	raw, _ := json.Marshal(params)
	return &Envelope{JSONRPC: "2.0", Method: method, Params: raw}
}

// DecodeResult unmarshals env.Result into v. Returns env.Error unwrapped if
// the response is an error.
func DecodeResult(env *Envelope, v any) error {
	if env.Error != nil {
		return env.Error
	}
	if len(env.Result) == 0 {
		return nil
	}
	return json.Unmarshal(env.Result, v)
}

// Error implements error on RPCError so DecodeResult can return it directly.
func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}
