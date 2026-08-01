package wire

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"
)

type Writer struct {
	buf []byte
}

func (w *Writer) U8(value uint8) {
	w.buf = append(w.buf, value)
}

func (w *Writer) U16(value uint16) {
	w.buf = binary.LittleEndian.AppendUint16(w.buf, value)
}

func (w *Writer) U32(value uint32) {
	w.buf = binary.LittleEndian.AppendUint32(w.buf, value)
}

func (w *Writer) F32(value float32) {
	w.U32(math.Float32bits(value))
}

func (w *Writer) Bytes(value []byte) {
	w.buf = append(w.buf, value...)
}

func (w *Writer) QString(value string) {
	raw := append([]byte(value), 0)
	w.U16(uint16(len(raw)))
	w.Bytes(raw)
}

func (w *Writer) QBuffer(value []byte) {
	w.U32(uint32(len(value)))
	w.Bytes(value)
}

func (w *Writer) Data() []byte {
	return append([]byte(nil), w.buf...)
}

func (w *Writer) Len() int {
	return len(w.buf)
}

type Reader struct {
	data   []byte
	offset int
}

func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

func (r *Reader) Remaining() int {
	return len(r.data) - r.offset
}

func (r *Reader) Offset() int {
	return r.offset
}

func (r *Reader) Take(size int) ([]byte, error) {
	if size < 0 || r.Remaining() < size {
		return nil, fmt.Errorf("reader underflow: need %d, have %d", size, r.Remaining())
	}
	value := r.data[r.offset : r.offset+size]
	r.offset += size
	return value, nil
}

func (r *Reader) U8() (uint8, error) {
	value, err := r.Take(1)
	if err != nil {
		return 0, err
	}
	return value[0], nil
}

func (r *Reader) U16() (uint16, error) {
	value, err := r.Take(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(value), nil
}

func (r *Reader) U32() (uint32, error) {
	value, err := r.Take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(value), nil
}

func (r *Reader) F32() (float32, error) {
	value, err := r.U32()
	return math.Float32frombits(value), err
}

func (r *Reader) QString() (string, error) {
	size, err := r.U16()
	if err != nil {
		return "", err
	}
	raw, err := r.Take(int(size))
	if err != nil {
		return "", err
	}
	if len(raw) > 0 && raw[len(raw)-1] == 0 {
		raw = raw[:len(raw)-1]
	}
	if !utf8.Valid(raw) {
		return string([]rune(string(raw))), nil
	}
	return string(raw), nil
}

func (r *Reader) QBuffer() ([]byte, error) {
	size, err := r.U32()
	if err != nil {
		return nil, err
	}
	return r.Take(int(size))
}

func (r *Reader) Rest() []byte {
	value := r.data[r.offset:]
	r.offset = len(r.data)
	return value
}
