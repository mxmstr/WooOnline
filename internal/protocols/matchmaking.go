package protocols

import (
	"encoding/binary"

	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/rmc"
)

const (
	matchmakingProtocol     uint8  = 21
	destroyGatheringMethod  uint32 = 36
	updateParticipantMethod uint32 = 39
)

func (s *Services) registerMatchmaking(dispatcher *rmc.Dispatcher) {
	dispatcher.Register(matchmakingProtocol, destroyGatheringMethod, s.destroyGathering)
	dispatcher.Register(matchmakingProtocol, updateParticipantMethod, s.updateParticipant)
}

func (s *Services) updateParticipant(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	s.Logger.Info("accepted participant update", "remote", connection.Remote, "bytes", len(request.Params))
	return rmc.ResponseOK{Protocol: matchmakingProtocol, Method: updateParticipantMethod, Call: request.Call}
}

func (s *Services) destroyGathering(dispatcher *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	var id uint32
	if len(request.Params) >= 4 {
		id = binary.LittleEndian.Uint32(request.Params[:4])
	}
	gathering := s.Gatherings.Get(id)
	if gathering != nil && gathering.OwnerPID == connection.UserPID {
		s.pushLeave(dispatcher, gathering, connection.UserPID)
		s.Gatherings.Destroy(id)
	}
	return rmc.ResponseOK{Protocol: matchmakingProtocol, Method: destroyGatheringMethod, Call: request.Call}
}
