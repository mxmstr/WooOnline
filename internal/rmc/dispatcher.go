package rmc

import (
	"log/slog"
	"sync/atomic"

	"stranglehold-go-server/internal/prudp"
)

type Handler func(*Dispatcher, *prudp.Connection, Request) Message

type route struct {
	protocol uint8
	method   uint32
}

type Dispatcher struct {
	PRUDP        *prudp.Server
	logger       *slog.Logger
	handlers     map[route]Handler
	nextServerID atomic.Uint32
}

func NewDispatcher(server *prudp.Server, logger *slog.Logger) *Dispatcher {
	dispatcher := &Dispatcher{
		PRUDP: server, logger: logger.With("component", "rmc"),
		handlers: make(map[route]Handler),
	}
	dispatcher.nextServerID.Store(0xffff)
	return dispatcher
}

func (d *Dispatcher) Register(protocol uint8, method uint32, handler Handler) {
	d.handlers[route{protocol: protocol, method: method}] = handler
}

func (d *Dispatcher) HandlerCount() int {
	return len(d.handlers)
}

func (d *Dispatcher) OnPayload(connection *prudp.Connection, body []byte) {
	message, err := Decode(body)
	if err != nil {
		d.logger.Warn("decode request", "remote", connection.Remote, "error", err)
		return
	}
	request, ok := message.(Request)
	if !ok {
		switch response := message.(type) {
		case ResponseOK:
			d.logger.Info("client response OK", "remote", connection.Remote, "protocol", response.Protocol, "method", response.Method, "call_id", response.Call)
		case ResponseError:
			d.logger.Info("client response error", "remote", connection.Remote, "protocol", response.Protocol, "call_id", response.Call, "code", response.Code)
		}
		return
	}
	d.logger.Info("request", "remote", connection.Remote, "protocol", request.Protocol, "method", request.Method, "call_id", request.Call, "params_bytes", len(request.Params))
	handler := d.handlers[route{protocol: request.Protocol, method: request.Method}]
	if handler == nil {
		d.logger.Warn("no handler", "protocol", request.Protocol, "method", request.Method)
		d.Send(connection, ResponseError{Protocol: request.Protocol, Call: request.Call, Code: 0x80010001})
		return
	}

	var response Message
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				d.logger.Error("handler panic", "protocol", request.Protocol, "method", request.Method, "panic", recovered)
				response = ResponseError{Protocol: request.Protocol, Call: request.Call, Code: 0x80020001}
			}
		}()
		response = handler(d, connection, request)
	}()
	if response != nil {
		d.Send(connection, response)
	}
}

func (d *Dispatcher) Send(connection *prudp.Connection, message Message) {
	body, err := Encode(message)
	if err != nil {
		d.logger.Error("encode response", "error", err)
		return
	}
	if err := d.PRUDP.SendReliableData(connection, body); err != nil {
		d.logger.Error("send response", "remote", connection.Remote, "error", err)
	}
}

func (d *Dispatcher) SendRequest(connection *prudp.Connection, protocol uint8, method uint32, params []byte) uint32 {
	callID := d.nextServerID.Add(1)
	d.Send(connection, Request{Protocol: protocol, Method: method, Call: callID, Params: params})
	d.logger.Info("server push", "remote", connection.Remote, "protocol", protocol, "method", method, "call_id", callID, "params_bytes", len(params))
	return callID
}
