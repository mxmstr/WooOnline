package prudp

import (
	"crypto/rc4"
	"fmt"
)

type PayloadCodec struct {
	key []byte
}

func NewPayloadCodec(key string) PayloadCodec {
	return PayloadCodec{key: []byte(key)}
}

func (p PayloadCodec) crypt(data []byte) ([]byte, error) {
	if len(p.key) == 0 {
		return append([]byte(nil), data...), nil
	}
	cipher, err := rc4.NewCipher(p.key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.XORKeyStream(out, data)
	return out, nil
}

func (p PayloadCodec) Encode(body []byte) ([]byte, error) {
	framed := make([]byte, 1, len(body)+1)
	framed = append(framed, body...)
	return p.crypt(framed)
}

func (p PayloadCodec) Decode(payload []byte) ([]byte, error) {
	plain, err := p.crypt(payload)
	if err != nil {
		return nil, err
	}
	if len(plain) == 0 {
		return plain, nil
	}
	switch plain[0] {
	case 0:
		return append([]byte(nil), plain[1:]...), nil
	case 1:
		return lzo1xDecompress(plain[1:])
	default:
		return append([]byte(nil), plain[1:]...), nil
	}
}

func lzo1xDecompress(src []byte) ([]byte, error) {
	dst := make([]byte, 0, len(src)*2)
	ip := 0
	if len(src) == 0 {
		return dst, nil
	}
	next := func() (byte, error) {
		if ip >= len(src) {
			return 0, fmt.Errorf("LZO input exhausted")
		}
		value := src[ip]
		ip++
		return value, nil
	}
	copyLiteral := func(count int) error {
		if count < 0 || ip+count > len(src) {
			return fmt.Errorf("LZO literal overruns input")
		}
		dst = append(dst, src[ip:ip+count]...)
		ip += count
		return nil
	}
	copyMatch := func(position, count int) error {
		if position < 0 || position >= len(dst) {
			return fmt.Errorf("LZO invalid match position %d at output %d", position, len(dst))
		}
		for i := 0; i < count; i++ {
			index := position + i
			if index < 0 || index >= len(dst) {
				return fmt.Errorf("LZO match overruns output")
			}
			dst = append(dst, dst[index])
		}
		return nil
	}

	tByte, err := next()
	if err != nil {
		return nil, err
	}
	t := int(tByte)
	if t > 17 {
		count := t - 17
		if err := copyLiteral(count); err != nil {
			return nil, err
		}
		if ip >= len(src) {
			return dst, nil
		}
		tByte, err = next()
		if err != nil {
			return nil, err
		}
		t = int(tByte)
		if count >= 4 && t < 16 {
			b, err := next()
			if err != nil {
				return nil, err
			}
			position := len(dst) - 1 - 0x800 - (t >> 2) - (int(b) << 2)
			if err := copyMatch(position, 3); err != nil {
				return nil, err
			}
			count = t & 3
			if err := copyLiteral(count); err != nil {
				return nil, err
			}
			if ip >= len(src) {
				return dst, nil
			}
			tByte, err = next()
			if err != nil {
				return nil, err
			}
			t = int(tByte)
		}
	}

	for ip <= len(src) {
		if t < 16 {
			if t == 0 {
				for ip < len(src) && src[ip] == 0 {
					t += 255
					ip++
				}
				b, err := next()
				if err != nil {
					return nil, err
				}
				t += int(b) + 15
			}
			if err := copyLiteral(t + 3); err != nil {
				return nil, err
			}
			tByte, err = next()
			if err != nil {
				return nil, err
			}
			t = int(tByte)
			if t < 16 {
				b, err := next()
				if err != nil {
					return nil, err
				}
				position := len(dst) - 1 - 0x800 - (t >> 2) - (int(b) << 2)
				if err := copyMatch(position, 3); err != nil {
					return nil, err
				}
				count := t & 3
				if err := copyLiteral(count); err != nil {
					return nil, err
				}
				if ip >= len(src) {
					break
				}
				tByte, err = next()
				if err != nil {
					return nil, err
				}
				t = int(tByte)
				continue
			}
		}

		for {
			var position, count int
			switch {
			case t >= 64:
				b, err := next()
				if err != nil {
					return nil, err
				}
				count = (t >> 5) + 1
				position = len(dst) - (1 + ((t >> 2) & 7) + (int(b) << 3))
			case t >= 32:
				count = t & 0x1f
				if count == 0 {
					for ip < len(src) && src[ip] == 0 {
						count += 255
						ip++
					}
					b, err := next()
					if err != nil {
						return nil, err
					}
					count += int(b) + 31
				}
				count += 2
				if ip+2 > len(src) {
					return nil, fmt.Errorf("LZO match offset truncated")
				}
				off := int(src[ip]) | int(src[ip+1])<<8
				ip += 2
				position = len(dst) - (1 + (off >> 2))
			case t >= 16:
				count = t & 7
				if count == 0 {
					for ip < len(src) && src[ip] == 0 {
						count += 255
						ip++
					}
					b, err := next()
					if err != nil {
						return nil, err
					}
					count += int(b) + 7
				}
				count += 2
				if ip+2 > len(src) {
					return nil, fmt.Errorf("LZO match offset truncated")
				}
				off := int(src[ip]) | int(src[ip+1])<<8
				ip += 2
				if 2048*(t&8)+(off>>2) == 0 {
					return dst, nil
				}
				position = len(dst) - (2048*(t&8) + (off >> 2) + 0x4000)
			default:
				b, err := next()
				if err != nil {
					return nil, err
				}
				count = 2
				position = len(dst) - (1 + (t >> 2) + (int(b) << 2))
			}
			if err := copyMatch(position, count); err != nil {
				return nil, err
			}

			if ip < 2 {
				return nil, fmt.Errorf("LZO invalid trailing literal marker")
			}
			literals := int(src[ip-2] & 3)
			if literals == 0 {
				break
			}
			if err := copyLiteral(literals); err != nil {
				return nil, err
			}
			tByte, err = next()
			if err != nil {
				return nil, err
			}
			t = int(tByte)
			if t >= 16 {
				continue
			}
			b, err := next()
			if err != nil {
				return nil, err
			}
			position = len(dst) - (1 + (t >> 2) + (int(b) << 2))
			if err := copyMatch(position, 2); err != nil {
				return nil, err
			}
			literals = t & 3
			if literals == 0 {
				break
			}
			if err := copyLiteral(literals); err != nil {
				return nil, err
			}
			tByte, err = next()
			if err != nil {
				return nil, err
			}
			t = int(tByte)
		}

		if ip >= len(src) {
			break
		}
		tByte, err = next()
		if err != nil {
			return nil, err
		}
		t = int(tByte)
	}
	return dst, nil
}
