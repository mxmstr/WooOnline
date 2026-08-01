package prudp

import (
	"crypto/rand"
	"net"
	"sync"
	"time"
)

const (
	ClientType = 3
	ClientPort = 0x0f
	ServerType = 3
	ServerPort = 0x01
)

type Connection struct {
	mu sync.Mutex

	Remote    *net.UDPAddr
	LocalPort int
	State     string

	ClientSessionID uint8
	ServerSessionID uint8
	ClientConnSig   [4]byte
	ServerConnSig   [4]byte

	OutgoingReliableSeq uint16
	LastClientPacketID  uint16
	UserPID             uint32
	RVConnectionID      uint32
	StationURLs         []string
	FragmentBuffer      []byte
	LastActivity        time.Time
}

func NewConnection(remote *net.UDPAddr, localPort int) *Connection {
	connection := &Connection{
		Remote:              cloneAddr(remote),
		LocalPort:           localPort,
		State:               "INITIAL",
		OutgoingReliableSeq: 1,
		LastActivity:        time.Now(),
	}
	_, _ = rand.Read(connection.ServerConnSig[:])
	var session [1]byte
	_, _ = rand.Read(session[:])
	connection.ServerSessionID = session[0] | 1
	return connection
}

func (c *Connection) Touch() {
	c.mu.Lock()
	c.LastActivity = time.Now()
	c.mu.Unlock()
}

func (c *Connection) NextReliablePacketID() uint16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	value := c.OutgoingReliableSeq
	c.OutgoingReliableSeq++
	return value
}

func (c *Connection) RemoteKey() string {
	return c.Remote.String()
}

func cloneAddr(address *net.UDPAddr) *net.UDPAddr {
	return &net.UDPAddr{IP: append(net.IP(nil), address.IP...), Port: address.Port, Zone: address.Zone}
}
