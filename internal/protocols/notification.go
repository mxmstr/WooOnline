package protocols

import (
	"context"
	"fmt"

	"stranglehold-go-server/internal/rmc"
	"stranglehold-go-server/internal/state"
	"stranglehold-go-server/internal/wire"
)

const (
	eventParticipation    uint32 = 900002
	eventEndParticipation uint32 = 900000
	eventStatsProcessed   uint32 = 901001
)

func buildNotification(source, eventType, parameter1, parameter2 uint32, text string) []byte {
	var out wire.Writer
	out.U32(source)
	out.U32(eventType)
	out.U32(parameter1)
	out.U32(parameter2)
	out.QString(text)
	return out.Data()
}

func (s *Services) broadcastParticipantEvent(dispatcher *rmc.Dispatcher, gathering *state.Gathering, subjectPID, eventType uint32) int {
	payload := buildNotification(subjectPID, eventType, subjectPID, gathering.ID, "")
	sent := 0
	for _, participant := range gathering.Participants {
		if participant.PID == subjectPID {
			continue
		}
		connection := dispatcher.PRUDP.Connection(participant.RemoteKey)
		if connection == nil {
			continue
		}
		dispatcher.SendRequest(connection, notificationProtocol, processEventMethod, payload)
		sent++
	}
	return sent
}

func (s *Services) pushJoin(dispatcher *rmc.Dispatcher, gathering *state.Gathering, joinerPID uint32) int {
	return s.broadcastParticipantEvent(dispatcher, gathering, joinerPID, eventParticipation)
}

func (s *Services) pushLeave(dispatcher *rmc.Dispatcher, gathering *state.Gathering, leaverPID uint32) int {
	return s.broadcastParticipantEvent(dispatcher, gathering, leaverPID, eventEndParticipation)
}

func (s *Services) pushParticipantsToJoiner(dispatcher *rmc.Dispatcher, gathering *state.Gathering, joinerPID uint32) int {
	var remoteKey string
	for _, participant := range gathering.Participants {
		if participant.PID == joinerPID {
			remoteKey = participant.RemoteKey
			break
		}
	}
	connection := dispatcher.PRUDP.Connection(remoteKey)
	if connection == nil {
		return 0
	}
	for _, participant := range gathering.Participants {
		payload := buildNotification(participant.PID, eventParticipation, participant.PID, gathering.ID, "")
		dispatcher.SendRequest(connection, notificationProtocol, processEventMethod, payload)
	}
	return len(gathering.Participants)
}

func (s *Services) pushStatsProcessed(dispatcher *rmc.Dispatcher, gathering *state.Gathering, sourcePID uint32) int {
	sent := 0
	for _, participant := range gathering.Participants {
		connection := dispatcher.PRUDP.Connection(participant.RemoteKey)
		if connection == nil {
			continue
		}
		s.statsMu.Lock()
		txn := s.statsTxn[participant.PID]
		s.statsMu.Unlock()
		cash := s.Store.CareerCash(context.Background(), participant.PID)
		var stats wire.Writer
		stats.U32(1)
		stats.U32(participant.PID)
		stats.QString(fmt.Sprintf("PID%d", participant.PID))
		stats.U32(txn)
		stats.U32(122)
		for range 122 {
			stats.F32(0)
		}
		stats.U32(31)
		for index := range 31 {
			if index == 10 {
				stats.F32(cash)
			} else {
				stats.F32(0)
			}
		}
		stats.U32(0)

		var event wire.Writer
		event.U32(sourcePID)
		event.U32(eventStatsProcessed)
		event.U32(gathering.ID)
		event.U32(txn)
		event.U16(uint16(stats.Len()))
		event.Bytes(stats.Data())
		dispatcher.SendRequest(connection, notificationProtocol, processEventMethod, event.Data())
		sent++
	}
	return sent
}
