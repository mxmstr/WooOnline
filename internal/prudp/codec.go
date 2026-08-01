package prudp

import (
	"encoding/binary"
	"fmt"
)

const (
	TypeSYN        uint8 = 0
	TypeConnect    uint8 = 1
	TypeData       uint8 = 2
	TypeDisconnect uint8 = 3
	TypePing       uint8 = 4
	TypeUser       uint8 = 5

	FlagACK      uint16 = 1
	FlagReliable uint16 = 2
	FlagNeedACK  uint16 = 4
	FlagHasSize  uint16 = 8
	FlagMultiACK uint16 = 0x200
)

type Packet struct {
	Type                uint8
	Flags               uint16
	SourceType          uint8
	SourcePort          uint8
	DestinationType     uint8
	DestinationPort     uint8
	SessionID           uint8
	PacketID            uint16
	FragmentID          uint8
	Signature           [4]byte
	ConnectionSignature [4]byte
	Payload             []byte
}

type Codec struct {
	AccessKey []byte
}

func (c Codec) checksum(data []byte) byte {
	checksum := uint32(0)
	for _, value := range c.AccessKey {
		checksum += uint32(value)
	}
	var wordSum uint32
	fullWords := len(data) &^ 3
	for offset := 0; offset < fullWords; offset += 4 {
		wordSum += binary.LittleEndian.Uint32(data[offset : offset+4])
	}
	for _, value := range data[fullWords:] {
		checksum += uint32(value)
	}
	var packed [4]byte
	binary.LittleEndian.PutUint32(packed[:], wordSum)
	for _, value := range packed {
		checksum += uint32(value)
	}
	return byte(checksum)
}

func (c Codec) Encode(packet Packet) []byte {
	out := make([]byte, 0, 18+len(packet.Payload))
	out = append(out,
		(packet.SourcePort&0x0f)|((packet.SourceType&0x0f)<<4),
		(packet.DestinationPort&0x0f)|((packet.DestinationType&0x0f)<<4),
		packet.Type|byte(packet.Flags<<3),
		packet.SessionID,
	)
	out = append(out, packet.Signature[:]...)
	out = binary.LittleEndian.AppendUint16(out, packet.PacketID)
	if packet.Type == TypeSYN || packet.Type == TypeConnect {
		out = append(out, packet.ConnectionSignature[:]...)
	}
	if packet.Type == TypeData {
		out = append(out, packet.FragmentID)
	}
	if packet.Flags&FlagHasSize != 0 {
		out = binary.LittleEndian.AppendUint16(out, uint16(len(packet.Payload)))
	}
	out = append(out, packet.Payload...)
	return append(out, c.checksum(out))
}

func (c Codec) Decode(data []byte) ([]Packet, error) {
	var packets []Packet
	for offset := 0; offset < len(data); {
		start := offset
		if len(data)-offset < 11 {
			return nil, fmt.Errorf("PRUDP header truncated at byte %d", offset)
		}
		source := data[offset]
		destination := data[offset+1]
		typeFlags := data[offset+2]
		packet := Packet{
			Type:            typeFlags & 7,
			Flags:           uint16(typeFlags >> 3),
			SourceType:      source >> 4,
			SourcePort:      source & 0x0f,
			DestinationType: destination >> 4,
			DestinationPort: destination & 0x0f,
			SessionID:       data[offset+3],
		}
		copy(packet.Signature[:], data[offset+4:offset+8])
		packet.PacketID = binary.LittleEndian.Uint16(data[offset+8 : offset+10])
		offset += 10

		if packet.Type == TypeSYN || packet.Type == TypeConnect {
			if len(data)-offset < 4 {
				return nil, fmt.Errorf("PRUDP connection signature truncated")
			}
			copy(packet.ConnectionSignature[:], data[offset:offset+4])
			offset += 4
		}
		if packet.Type == TypeData {
			if len(data)-offset < 1 {
				return nil, fmt.Errorf("PRUDP fragment id truncated")
			}
			packet.FragmentID = data[offset]
			offset++
		}

		payloadSize := 0
		if packet.Flags&FlagHasSize != 0 {
			if len(data)-offset < 2 {
				return nil, fmt.Errorf("PRUDP payload size truncated")
			}
			payloadSize = int(binary.LittleEndian.Uint16(data[offset : offset+2]))
			offset += 2
		} else {
			payloadSize = len(data) - offset - 1
		}
		if payloadSize < 0 || len(data)-offset < payloadSize+1 {
			return nil, fmt.Errorf("PRUDP payload truncated: need %d bytes", payloadSize)
		}
		packet.Payload = append([]byte(nil), data[offset:offset+payloadSize]...)
		offset += payloadSize
		expected := c.checksum(data[start:offset])
		actual := data[offset]
		offset++
		if actual != expected {
			return nil, fmt.Errorf("invalid PRUDP checksum: expected %d, got %d", expected, actual)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
