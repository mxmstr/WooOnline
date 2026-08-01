package rmc

import (
	"encoding/binary"
	"fmt"
)

const (
	RequestBit        = 0x80
	ResponseMethodBit = 0x8000
)

type Message interface {
	message()
	ProtocolID() uint8
	CallID() uint32
}

type Request struct {
	Protocol uint8
	Method   uint32
	Call     uint32
	Params   []byte
}

func (Request) message()            {}
func (r Request) ProtocolID() uint8 { return r.Protocol }
func (r Request) CallID() uint32    { return r.Call }

type ResponseOK struct {
	Protocol uint8
	Method   uint32
	Call     uint32
	Return   []byte
}

func (ResponseOK) message()            {}
func (r ResponseOK) ProtocolID() uint8 { return r.Protocol }
func (r ResponseOK) CallID() uint32    { return r.Call }

type ResponseError struct {
	Protocol uint8
	Call     uint32
	Code     uint32
}

func (ResponseError) message()            {}
func (r ResponseError) ProtocolID() uint8 { return r.Protocol }
func (r ResponseError) CallID() uint32    { return r.Call }

func Decode(data []byte) (Message, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("RMC body too short: %d bytes", len(data))
	}
	size := binary.LittleEndian.Uint32(data[:4])
	if int(size)+4 != len(data) {
		return nil, fmt.Errorf("RMC size mismatch: declared %d, actual %d", size, len(data)-4)
	}
	protocolByte := data[4]
	protocol := protocolByte & 0x7f
	if protocolByte&RequestBit != 0 {
		if len(data) < 13 {
			return nil, fmt.Errorf("RMC request truncated: %d bytes", len(data))
		}
		return Request{
			Protocol: protocol,
			Call:     binary.LittleEndian.Uint32(data[5:9]),
			Method:   binary.LittleEndian.Uint32(data[9:13]),
			Params:   append([]byte(nil), data[13:]...),
		}, nil
	}
	if len(data) < 14 {
		return nil, fmt.Errorf("RMC response truncated: %d bytes", len(data))
	}
	if data[5] != 0 {
		return ResponseOK{
			Protocol: protocol,
			Call:     binary.LittleEndian.Uint32(data[6:10]),
			Method:   binary.LittleEndian.Uint32(data[10:14]) &^ ResponseMethodBit,
			Return:   append([]byte(nil), data[14:]...),
		}, nil
	}
	return ResponseError{
		Protocol: protocol,
		Code:     binary.LittleEndian.Uint32(data[6:10]),
		Call:     binary.LittleEndian.Uint32(data[10:14]),
	}, nil
}

func Encode(message Message) ([]byte, error) {
	var body []byte
	switch value := message.(type) {
	case Request:
		body = append(body, value.Protocol|RequestBit)
		body = binary.LittleEndian.AppendUint32(body, value.Call)
		body = binary.LittleEndian.AppendUint32(body, value.Method)
		body = append(body, value.Params...)
	case ResponseOK:
		body = append(body, value.Protocol&0x7f, 1)
		body = binary.LittleEndian.AppendUint32(body, value.Call)
		body = binary.LittleEndian.AppendUint32(body, value.Method|ResponseMethodBit)
		body = append(body, value.Return...)
	case ResponseError:
		body = append(body, value.Protocol&0x7f, 0)
		body = binary.LittleEndian.AppendUint32(body, value.Code)
		body = binary.LittleEndian.AppendUint32(body, value.Call)
	default:
		return nil, fmt.Errorf("unknown RMC message type %T", message)
	}
	out := make([]byte, 0, len(body)+4)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(body)))
	out = append(out, body...)
	return out, nil
}
