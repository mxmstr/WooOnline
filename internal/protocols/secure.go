package protocols

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/rmc"
	"stranglehold-go-server/internal/wire"
)

const (
	secureProtocol       uint8  = 11
	registerMethod       uint32 = 1
	registerExMethod     uint32 = 3
	quazalSuccess        uint32 = 0x00010001
	notificationProtocol uint8  = 14
	processEventMethod   uint32 = 1
)

func (s *Services) registerSecure(dispatcher *rmc.Dispatcher) {
	dispatcher.Register(secureProtocol, registerMethod, s.registerServices)
	dispatcher.Register(secureProtocol, registerExMethod, s.registerEx)
}

func (s *Services) attachIdentity(connection *prudp.Connection) {
	if connection.UserPID == 0 {
		connection.UserPID = s.Identity.AuthenticatedPID(connection.RemoteKey())
	}
}

func (s *Services) registerEx(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	reader := wire.NewReader(request.Params)
	count, err := reader.U32()
	if err != nil || count > 32 {
		return rmc.ResponseError{Protocol: secureProtocol, Call: request.Call, Code: 0x80030001}
	}
	urls := make([]string, 0, count)
	for range count {
		stationURL, err := reader.QString()
		if err != nil {
			return rmc.ResponseError{Protocol: secureProtocol, Call: request.Call, Code: 0x80030001}
		}
		urls = append(urls, stationURL)
	}
	if _, _, err := parseAny(reader); err != nil {
		return rmc.ResponseError{Protocol: secureProtocol, Call: request.Call, Code: 0x80030001}
	}
	s.attachIdentity(connection)
	if connection.RVConnectionID == 0 {
		connection.RVConnectionID = s.Identity.NextConnectionID()
	}
	cid := connection.RVConnectionID
	for index := range urls {
		urls[index] += fmt.Sprintf(";RVCID=%d", cid)
	}
	p2pPort := connection.Remote.Port
	if len(urls) > 0 {
		if port, ok := extractURLPort(urls[0]); ok {
			p2pPort = port
		}
	}
	urls = append(urls, fmt.Sprintf("prudp:/address=%s;port=%d;sid=%d;type=3;RVCID=%d",
		connection.Remote.IP, p2pPort, prudp.ClientPort, cid))
	connection.StationURLs = urls

	publicURL := fmt.Sprintf("prudp:/address=%s;port=%d;sid=%d;type=3",
		connection.Remote.IP, connection.Remote.Port, prudp.ClientPort)
	var out wire.Writer
	out.U32(quazalSuccess)
	out.U32(cid)
	out.QString(publicURL)
	return rmc.ResponseOK{Protocol: secureProtocol, Method: registerExMethod, Call: request.Call, Return: out.Data()}
}

func (s *Services) registerServices(dispatcher *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	s.attachIdentity(connection)
	if connection.RVConnectionID == 0 {
		connection.RVConnectionID = s.Identity.NextConnectionID()
	}
	publicURL := fmt.Sprintf("prudp:/address=%s;port=%d;sid=15;PID=%d;CID=%d;type=3",
		connection.Remote.IP, connection.Remote.Port, connection.UserPID, connection.RVConnectionID)
	connection.StationURLs = []string{publicURL}
	var out wire.Writer
	out.U32(quazalSuccess)
	out.U32(connection.RVConnectionID)
	out.QString(publicURL)

	time.AfterFunc(time.Second, func() {
		for _, eventType := range []uint32{30009, 30016} {
			var event wire.Writer
			event.U32(2)
			event.U32(eventType)
			event.U32(0)
			event.U32(0)
			if eventType == 30009 {
				event.QString(publicURL)
			} else {
				event.QString("")
			}
			dispatcher.SendRequest(connection, notificationProtocol, processEventMethod, event.Data())
		}
	})
	return rmc.ResponseOK{Protocol: secureProtocol, Method: request.Method, Call: request.Call, Return: out.Data()}
}

func parseAny(reader *wire.Reader) (string, []byte, error) {
	nameSize, err := reader.U16()
	if err != nil {
		return "", nil, err
	}
	rawName, err := reader.Take(int(nameSize))
	if err != nil {
		return "", nil, err
	}
	name := strings.TrimRight(string(rawName), "\x00")
	dataSize, err := reader.U32()
	if err != nil {
		return "", nil, err
	}
	data, err := reader.Take(int(dataSize))
	return name, data, err
}

func extractURLPort(stationURL string) (int, bool) {
	for _, token := range strings.Split(stationURL, ";") {
		if strings.HasPrefix(token, "port=") {
			port, err := strconv.Atoi(strings.TrimPrefix(token, "port="))
			return port, err == nil
		}
	}
	return 0, false
}
