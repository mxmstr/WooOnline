package prudp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

type PayloadHandler func(*Connection, []byte)
type CloseHandler func(string)
type ConnectACKHandler func(*Connection, []byte) ([]byte, error)

type Server struct {
	localPort int
	logger    *slog.Logger
	packet    Codec
	payload   PayloadCodec

	mu           sync.RWMutex
	socket       *net.UDPConn
	connections  map[string]*Connection
	onPayload    PayloadHandler
	onClose      []CloseHandler
	onConnectACK ConnectACKHandler
}

func NewServer(localPort int, accessKey, rc4Key string, logger *slog.Logger) *Server {
	return &Server{
		localPort:   localPort,
		logger:      logger.With("component", "prudp", "port", localPort),
		packet:      Codec{AccessKey: []byte(accessKey)},
		payload:     NewPayloadCodec(rc4Key),
		connections: make(map[string]*Connection),
		onPayload:   func(*Connection, []byte) {},
	}
}

func (s *Server) SetPayloadHandler(handler PayloadHandler) {
	if handler == nil {
		handler = func(*Connection, []byte) {}
	}
	s.onPayload = handler
}

func (s *Server) AddCloseHandler(handler CloseHandler) {
	s.onClose = append(s.onClose, handler)
}

func (s *Server) SetConnectACKHandler(handler ConnectACKHandler) {
	s.onConnectACK = handler
}

func (s *Server) Listen(bindHost string) error {
	address, err := net.ResolveUDPAddr("udp", net.JoinHostPort(bindHost, fmt.Sprint(s.localPort)))
	if err != nil {
		return err
	}
	socket, err := net.ListenUDP("udp", address)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.socket = socket
	s.mu.Unlock()
	s.logger.Info("listening", "address", socket.LocalAddr())
	return nil
}

func (s *Server) Serve(ctx context.Context) error {
	s.mu.RLock()
	socket := s.socket
	s.mu.RUnlock()
	if socket == nil {
		return errors.New("PRUDP server is not listening")
	}
	go func() {
		<-ctx.Done()
		_ = socket.Close()
	}()

	buffer := make([]byte, 64*1024)
	for {
		size, remote, err := socket.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.logger.Warn("read datagram", "error", err)
			continue
		}
		raw := append([]byte(nil), buffer[:size]...)
		s.handleDatagram(raw, remote)
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.socket == nil {
		return nil
	}
	err := s.socket.Close()
	s.socket = nil
	return err
}

func (s *Server) Connection(remoteKey string) *Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connections[remoteKey]
}

func (s *Server) Connections() []*Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Connection, 0, len(s.connections))
	for _, connection := range s.connections {
		out = append(out, connection)
	}
	return out
}

func (s *Server) handleDatagram(raw []byte, remote *net.UDPAddr) {
	if len(raw) >= 5 && bytes.Equal(raw[:4], []byte{0x69, 0x69, 0x69, 0x69}) {
		stationURL := fmt.Sprintf("prudp:/address=%s;port=%d;sid=15;PID=2;type=3", remote.IP, remote.Port)
		response := binary.LittleEndian.AppendUint32(nil, 0x69696969)
		response = append(response, 2)
		response = append(response, stationURL...)
		response = append(response, 0)
		s.write(response, remote)
		s.logger.Info("raw NAT probe reply", "remote", remote, "station_url", stationURL)
		return
	}

	if len(raw) >= 16 && raw[2]&7 == TypeUser && raw[14] == 2 {
		stationURL := fmt.Sprintf("udp:/address=%s;port=%d;sid=15;type=3", remote.IP, remote.Port)
		body := append([]byte(nil), raw[10:14]...)
		body = append(body, 2)
		body = append(body, stationURL...)
		body = append(body, 0)
		var checksum byte
		for _, value := range body {
			checksum += value
		}
		s.write(append(body, checksum), remote)
		s.logger.Info("Stranglehold NAT echo reply", "remote", remote, "station_url", stationURL)
		return
	}

	packets, err := s.packet.Decode(raw)
	if err != nil {
		s.logger.Warn("decode datagram", "remote", remote, "bytes", len(raw), "error", err)
		return
	}
	for _, packet := range packets {
		s.handlePacket(packet, remote)
	}
}

func (s *Server) handlePacket(packet Packet, remote *net.UDPAddr) {
	key := remote.String()
	connection := s.Connection(key)
	switch packet.Type {
	case TypeSYN:
		s.handleSYN(remote)
	case TypeConnect:
		s.handleConnect(packet, remote, connection)
	case TypeData:
		s.handleData(packet, remote, connection)
	case TypePing:
		s.handlePing(packet, connection)
	case TypeDisconnect:
		s.handleDisconnect(packet, key, connection)
	default:
		s.logger.Warn("unknown packet type", "remote", remote, "type", packet.Type)
	}
}

func (s *Server) handleSYN(remote *net.UDPAddr) {
	connection := NewConnection(remote, s.localPort)
	connection.State = "SYN_RECV"
	s.mu.Lock()
	s.connections[remote.String()] = connection
	s.mu.Unlock()
	s.logger.Info("SYN", "remote", remote, "connection_signature", fmt.Sprintf("%x", connection.ServerConnSig))
	s.sendSYNACK(connection)
}

func (s *Server) handleConnect(packet Packet, remote *net.UDPAddr, connection *Connection) {
	if connection == nil || connection.State != "SYN_RECV" {
		s.logger.Warn("CONNECT without SYN", "remote", remote)
		return
	}
	if packet.Signature != connection.ServerConnSig {
		s.logger.Warn("CONNECT signature mismatch", "remote", remote)
		return
	}
	connection.ClientSessionID = packet.SessionID
	connection.ClientConnSig = packet.ConnectionSignature
	connection.State = "ESTABLISHED"
	connection.Touch()
	s.logger.Info("CONNECT established", "remote", remote, "client_session", packet.SessionID)
	var responsePayload []byte
	if s.onConnectACK != nil && len(packet.Payload) > 0 {
		incoming, err := s.payload.Decode(packet.Payload)
		if err == nil {
			responsePayload, err = s.onConnectACK(connection, incoming)
		}
		if err != nil {
			s.logger.Warn("CONNECT credential proof failed", "remote", remote, "error", err)
			responsePayload = nil
		}
	}
	s.sendConnectACK(connection, packet.PacketID, responsePayload)
}

func (s *Server) handleData(packet Packet, remote *net.UDPAddr, connection *Connection) {
	if connection == nil || connection.State != "ESTABLISHED" {
		s.logger.Warn("DATA before connection established", "remote", remote)
		return
	}
	connection.Touch()
	if packet.Flags&FlagACK != 0 && len(packet.Payload) == 0 {
		return
	}
	if packet.Flags&FlagReliable != 0 {
		s.sendSimpleACK(connection, TypeData, packet.PacketID)
		connection.LastClientPacketID = packet.PacketID
	}
	if len(packet.Payload) == 0 {
		return
	}
	body, err := s.payload.Decode(packet.Payload)
	if err != nil {
		s.logger.Warn("decode payload", "remote", remote, "error", err)
		return
	}
	if packet.FragmentID > 0 {
		connection.FragmentBuffer = append(connection.FragmentBuffer, body...)
		return
	}
	if len(connection.FragmentBuffer) > 0 {
		connection.FragmentBuffer = append(connection.FragmentBuffer, body...)
		body = append([]byte(nil), connection.FragmentBuffer...)
		connection.FragmentBuffer = connection.FragmentBuffer[:0]
		s.logger.Info("reassembled fragmented RMC", "remote", remote, "bytes", len(body))
	}
	s.onPayload(connection, body)
}

func (s *Server) handlePing(packet Packet, connection *Connection) {
	if connection == nil {
		return
	}
	connection.Touch()
	if packet.Flags&FlagNeedACK != 0 {
		s.sendSimpleACK(connection, TypePing, packet.PacketID)
	}
}

func (s *Server) handleDisconnect(packet Packet, key string, connection *Connection) {
	if connection != nil {
		s.sendSimpleACK(connection, TypeDisconnect, packet.PacketID)
		connection.State = "CLOSED"
	}
	s.fireClose(key)
	s.mu.Lock()
	delete(s.connections, key)
	s.mu.Unlock()
	s.logger.Info("DISCONNECT", "remote", key)
}

func (s *Server) ReapIdle(timeout time.Duration) int {
	now := time.Now()
	var stale []string
	s.mu.RLock()
	for key, connection := range s.connections {
		if now.Sub(connection.LastActivity) > timeout {
			stale = append(stale, key)
		}
	}
	s.mu.RUnlock()
	for _, key := range stale {
		s.fireClose(key)
		s.mu.Lock()
		delete(s.connections, key)
		s.mu.Unlock()
		s.logger.Info("reaped idle connection", "remote", key)
	}
	return len(stale)
}

func (s *Server) fireClose(remoteKey string) {
	for _, handler := range s.onClose {
		handler(remoteKey)
	}
}

func (s *Server) basePacket(connection *Connection, packetType uint8, flags uint16) Packet {
	return Packet{
		Type:            packetType,
		Flags:           flags,
		SourceType:      ServerType,
		SourcePort:      ServerPort,
		DestinationType: ClientType,
		DestinationPort: ClientPort,
		Signature:       connection.ClientConnSig,
	}
}

func (s *Server) sendSYNACK(connection *Connection) {
	packet := s.basePacket(connection, TypeSYN, FlagACK)
	packet.ConnectionSignature = connection.ServerConnSig
	packet.Signature = [4]byte{}
	s.sendPacket(packet, connection)
}

func (s *Server) sendConnectACK(connection *Connection, packetID uint16, body []byte) {
	packet := s.basePacket(connection, TypeConnect, FlagACK)
	packet.SessionID = connection.ServerSessionID
	packet.PacketID = packetID
	if len(body) > 0 {
		payload, err := s.payload.Encode(body)
		if err != nil {
			s.logger.Warn("encode CONNECT response", "remote", connection.Remote, "error", err)
		} else {
			packet.Flags |= FlagHasSize
			packet.Payload = payload
		}
	}
	s.sendPacket(packet, connection)
}

func (s *Server) sendSimpleACK(connection *Connection, packetType uint8, packetID uint16) {
	packet := s.basePacket(connection, packetType, FlagACK)
	packet.SessionID = connection.ServerSessionID
	packet.PacketID = packetID
	s.sendPacket(packet, connection)
}

func (s *Server) SendReliableData(connection *Connection, body []byte) error {
	payload, err := s.payload.Encode(body)
	if err != nil {
		return err
	}
	packet := s.basePacket(connection, TypeData, FlagReliable|FlagNeedACK|FlagHasSize)
	packet.SessionID = connection.ServerSessionID
	packet.PacketID = connection.NextReliablePacketID()
	packet.Payload = payload
	s.sendPacket(packet, connection)
	return nil
}

func (s *Server) sendPacket(packet Packet, connection *Connection) {
	s.write(s.packet.Encode(packet), connection.Remote)
}

func (s *Server) write(data []byte, remote *net.UDPAddr) {
	s.mu.RLock()
	socket := s.socket
	s.mu.RUnlock()
	if socket == nil {
		return
	}
	if _, err := socket.WriteToUDP(data, remote); err != nil {
		s.logger.Warn("write datagram", "remote", remote, "error", err)
	}
}
