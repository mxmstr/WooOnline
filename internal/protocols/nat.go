package protocols

import (
	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/rmc"
	"stranglehold-go-server/internal/wire"
)

const (
	natProtocol                uint8  = 3
	requestProbeMethod         uint32 = 1
	initiateProbeMethod        uint32 = 2
	requestProbeExtendedMethod uint32 = 3
)

func (s *Services) registerNAT(dispatcher *rmc.Dispatcher) {
	dispatcher.Register(natProtocol, requestProbeMethod, s.requestProbeInitiation)
	dispatcher.Register(natProtocol, requestProbeExtendedMethod, s.requestProbeInitiation)
}

func (s *Services) requestProbeInitiation(dispatcher *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	reader := wire.NewReader(request.Params)
	count, err := reader.U32()
	if err != nil || count > 32 {
		return rmc.ResponseOK{Protocol: natProtocol, Method: request.Method, Call: request.Call}
	}
	hostURLs := make(map[string]struct{}, count)
	for range count {
		stationURL, err := reader.QString()
		if err != nil {
			return rmc.ResponseOK{Protocol: natProtocol, Method: request.Method, Call: request.Call}
		}
		hostURLs[stationURL] = struct{}{}
	}
	var host *prudp.Connection
	for _, gathering := range s.Gatherings.All() {
		if gathering.HostConn == nil {
			continue
		}
		for _, stationURL := range gathering.HostConn.StationURLs {
			if _, ok := hostURLs[stationURL]; ok {
				host = gathering.HostConn
				break
			}
		}
	}
	if host != nil {
		for _, stationURL := range connection.StationURLs {
			var probe wire.Writer
			probe.QString(stationURL)
			dispatcher.SendRequest(host, natProtocol, initiateProbeMethod, probe.Data())
		}
	}
	return rmc.ResponseOK{Protocol: natProtocol, Method: request.Method, Call: request.Call}
}
