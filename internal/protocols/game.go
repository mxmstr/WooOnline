package protocols

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"time"

	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/rmc"
	"stranglehold-go-server/internal/state"
	"stranglehold-go-server/internal/store"
	"stranglehold-go-server/internal/wire"
)

const (
	gameProtocol             uint8  = 60
	createGatheringMethod    uint32 = 4
	joinGatheringMethod      uint32 = 5
	leaderboardSelfMethod    uint32 = 7
	userStatsMethod          uint32 = 9
	friendsListMethod        uint32 = 10
	reportStatsMethod        uint32 = 12
	endGameMethod            uint32 = 15
	leaveGatheringMethod     uint32 = 16
	leaderboardGlobalMethod  uint32 = 17
	startGameMethod          uint32 = 18
	closeParticipationMethod uint32 = 19
	searchGatheringsMethod   uint32 = 20
	quickMatchMethod         uint32 = 21
	invitationsListMethod    uint32 = 22
	openParticipationMethod  uint32 = 23
	lifecycle26Method        uint32 = 26

	sessionVoidError uint32 = 0x80060019
)

var instantBlockCategory = map[int]uint32{0: 303, 1: 302, 2: 304, 3: 307, 4: 305, 5: 306}

func (s *Services) registerGame(dispatcher *rmc.Dispatcher) {
	dispatcher.Register(gameProtocol, createGatheringMethod, s.createGathering)
	dispatcher.Register(gameProtocol, joinGatheringMethod, s.joinGathering)
	dispatcher.Register(gameProtocol, leaderboardSelfMethod, s.leaderboard)
	dispatcher.Register(gameProtocol, userStatsMethod, s.userStats)
	dispatcher.Register(gameProtocol, friendsListMethod, s.socialList)
	dispatcher.Register(gameProtocol, reportStatsMethod, s.lifecycle)
	dispatcher.Register(gameProtocol, endGameMethod, s.lifecycle)
	dispatcher.Register(gameProtocol, leaveGatheringMethod, s.leaveGathering)
	dispatcher.Register(gameProtocol, leaderboardGlobalMethod, s.leaderboard)
	dispatcher.Register(gameProtocol, startGameMethod, s.lifecycle)
	dispatcher.Register(gameProtocol, closeParticipationMethod, s.closeParticipation)
	dispatcher.Register(gameProtocol, searchGatheringsMethod, s.searchGatherings)
	dispatcher.Register(gameProtocol, quickMatchMethod, s.quickMatch)
	dispatcher.Register(gameProtocol, invitationsListMethod, s.socialList)
	dispatcher.Register(gameProtocol, openParticipationMethod, s.openParticipation)
	dispatcher.Register(gameProtocol, lifecycle26Method, s.lifecycle)
}

func gameOK(request rmc.Request, body []byte) rmc.ResponseOK {
	return rmc.ResponseOK{Protocol: gameProtocol, Method: request.Method, Call: request.Call, Return: body}
}

func parseStatsPIDs(params []byte) []uint32 {
	if len(params) < 8 {
		return nil
	}
	count := int(binary.LittleEndian.Uint32(params[4:8]))
	if count > 64 {
		return nil
	}
	var out []uint32
	for offset := 8; len(out) < count && offset+4 <= len(params); offset += 4 {
		pid := binary.LittleEndian.Uint32(params[offset : offset+4])
		if pid != 0 {
			out = append(out, pid)
		}
	}
	return out
}

func (s *Services) careerVector(pid uint32) []float32 {
	vector, ok, err := s.Store.Career(context.Background(), pid)
	if err != nil {
		s.Logger.Error("read career", "pid", pid, "error", err)
	}
	if !ok {
		vector = make([]float32, store.CareerVectorLength)
		vector[0] = 1002
	}
	if len(vector) < store.CareerVectorLength {
		vector = append(vector, make([]float32, store.CareerVectorLength-len(vector))...)
	}
	return vector
}

func writeSparkStats(out *wire.Writer, pid uint32, name string, txn uint32, vec0, vec1, vec2 map[int]float32, vec0Length, vec1Length, vec2Length int) {
	out.U32(pid)
	if name == "" {
		name = fmt.Sprintf("PID%d", pid)
	}
	out.QString(name)
	out.U32(txn)
	out.U32(uint32(vec0Length))
	for index := range vec0Length {
		out.F32(vec0[index])
	}
	out.U32(uint32(vec1Length))
	for index := range vec1Length {
		out.F32(vec1[index])
	}
	out.U32(uint32(vec2Length))
	for index := range vec2Length {
		out.F32(vec2[index])
	}
}

func (s *Services) careerVec1(pid uint32) map[int]float32 {
	out := make(map[int]float32)
	for index, value := range s.careerVector(pid) {
		out[index+5] = value
	}
	return out
}

func (s *Services) careerCategory102(pid uint32) map[int]float32 {
	vector := s.careerVector(pid)
	out := map[int]float32{5: vector[61], 6: vector[62], 7: vector[63], 24: vector[80], 28: vector[84]}
	for index := range 16 {
		out[8+index] = vector[64+index]
	}
	return out
}

func (s *Services) userStats(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	var category uint32
	if len(request.Params) >= 4 {
		category = binary.LittleEndian.Uint32(request.Params[:4])
	}
	pids := parseStatsPIDs(request.Params)
	var out wire.Writer
	out.U32(uint32(len(pids)))
	for _, pid := range pids {
		switch {
		case category == 101:
			writeSparkStats(&out, pid, "", 0, nil, s.careerVec1(pid), nil, 122, 96, 0)
		case category == 102:
			writeSparkStats(&out, pid, "", 0, nil, s.careerCategory102(pid), nil, 122, 96, 0)
		case category >= 201:
			row, _, _ := s.Store.Leaderboard(context.Background(), pid, category)
			vec1 := map[int]float32{5: row.Cash, 6: row.ModeStat, 7: float32(row.MapID)}
			writeSparkStats(&out, pid, "", 0, nil, vec1, nil, 122, 96, 0)
		default:
			writeSparkStats(&out, pid, "", 0, nil, nil, nil, 122, 96, 0)
		}
	}
	s.Logger.Info("user stats", "remote", connection.Remote, "category", category, "pids", pids, "bytes", out.Len())
	return gameOK(request, out.Data())
}

func (s *Services) leaderboard(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	var category, offset, count uint32
	if len(request.Params) >= 4 {
		category = binary.LittleEndian.Uint32(request.Params[:4])
	}
	if len(request.Params) >= 8 {
		offset = binary.LittleEndian.Uint32(request.Params[4:8])
	}
	if len(request.Params) >= 12 {
		count = binary.LittleEndian.Uint32(request.Params[8:12])
	}
	rows, err := s.Store.Ranked(context.Background(), category)
	if err != nil {
		s.Logger.Error("rank leaderboard", "category", category, "error", err)
		rows = nil
	}
	start := int(offset)
	if request.Method == leaderboardSelfMethod {
		index := slices.IndexFunc(rows, func(row store.LeaderboardRow) bool { return row.PID == connection.UserPID })
		if index < 0 {
			index = 0
		}
		start = max(0, index-3)
	}
	if start > len(rows) {
		start = len(rows)
	}
	end := min(len(rows), start+int(count))
	page := rows[start:end]
	var out wire.Writer
	out.U32(uint32(len(page)))
	for index, row := range page {
		position := uint32(start + index + 1)
		vector := s.careerVector(row.PID)
		writeSparkStats(&out, row.PID, s.Store.NameForPID(context.Background(), row.PID), position,
			map[int]float32{19: vector[4]},
			map[int]float32{0: float32(position), 5: row.Cash, 6: row.ModeStat, 7: float32(row.MapID)},
			nil, 122, 96, 1)
	}
	return gameOK(request, out.Data())
}

func (s *Services) createGathering(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	s.attachIdentity(connection)
	gathering := s.Gatherings.Create(connection.UserPID, connection.RemoteKey(), request.Params, connection)
	var out wire.Writer
	out.U32(gathering.ID)
	s.Logger.Info("created gathering", "remote", connection.Remote, "pid", connection.UserPID, "gathering_id", gathering.ID, "params_bytes", len(request.Params))
	//s.Logger.Info("created gathering wire", "gathering_id", gathering.ID, "params_hex", hex.EncodeToString(request.Params))
	return gameOK(request, out.Data())
}

func (s *Services) joinGathering(dispatcher *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	if len(request.Params) < 4 {
		return rmc.ResponseError{Protocol: gameProtocol, Call: request.Call, Code: sessionVoidError}
	}
	s.attachIdentity(connection)
	id := binary.LittleEndian.Uint32(request.Params[:4])
	gathering := s.Gatherings.Get(id)
	if gathering == nil || gathering.Closed || gathering.IsFull() {
		return rmc.ResponseError{Protocol: gameProtocol, Call: request.Call, Code: sessionVoidError}
	}
	_, invited := gathering.InvitedPIDs[connection.UserPID]
	if !invited && gathering.PublicRemaining() <= 0 {
		return rmc.ResponseError{Protocol: gameProtocol, Call: request.Call, Code: sessionVoidError}
	}
	s.Gatherings.AddParticipant(id, connection.UserPID, fmt.Sprintf("pid_%d", connection.UserPID), connection.RemoteKey())

	var out wire.Writer
	if gathering.IsStrangleholdLayout() {
		urlGroup := gathering.RenderURLGroupForBrowse()
		if len(urlGroup) >= 8 && binary.LittleEndian.Uint32(urlGroup[:4]) == id {
			out.Bytes(urlGroup[4:])
		} else {
			out.U32(0)
		}
		var existing []*state.Participant
		for _, participant := range gathering.Participants {
			if participant.PID != connection.UserPID {
				existing = append(existing, participant)
			}
		}
		out.U32(uint32(len(existing)))
		for _, participant := range existing {
			writeSparkStats(&out, participant.PID, participant.DisplayName, 0, nil, nil, nil, 0, 0, 0)
		}
		out.Bytes(gathering.RenderSettingsForBrowse())
	} else {
		if gathering.HostConn == nil || len(gathering.HostConn.StationURLs) == 0 {
			return rmc.ResponseError{Protocol: gameProtocol, Call: request.Call, Code: sessionVoidError}
		}
		out.U32(uint32(len(gathering.HostConn.StationURLs)))
		for _, stationURL := range gathering.HostConn.StationURLs {
			out.QString(stationURL)
		}
		var existing []*state.Participant
		for _, participant := range gathering.Participants {
			if participant.PID != connection.UserPID {
				existing = append(existing, participant)
			}
		}
		out.U32(uint32(len(existing)))
		for _, participant := range existing {
			out.U32(participant.PID)
			out.QString(participant.DisplayName)
			out.Bytes(participant.State[:])
		}
		out.Bytes(gathering.RenderJoinEmbed(len(gathering.Participants)))
	}

	if gathering.HostConn != nil {
		for _, stationURL := range connection.StationURLs {
			var probe wire.Writer
			probe.QString(stationURL)
			dispatcher.SendRequest(gathering.HostConn, natProtocol, initiateProbeMethod, probe.Data())
		}
		s.pushJoin(dispatcher, gathering, connection.UserPID)
	}
	s.pushParticipantsToJoiner(dispatcher, gathering, connection.UserPID)
	s.Logger.Info("joined gathering", "remote", connection.Remote, "pid", connection.UserPID, "gathering_id", id, "participants", len(gathering.Participants))
	return gameOK(request, out.Data())
}

func (s *Services) leaveGathering(dispatcher *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	var id uint32
	if len(request.Params) >= 4 {
		id = binary.LittleEndian.Uint32(request.Params[:4])
	}
	gathering := s.Gatherings.Get(id)
	if gathering != nil && s.Gatherings.RemoveParticipant(id, connection.UserPID) {
		s.pushLeave(dispatcher, gathering, connection.UserPID)
	}
	return gameOK(request, nil)
}

type searchFilter struct {
	ranked     string
	levelID    string
	jobID      string
	difficulty string
	gameMode   string
}

func parseSearchFilter(data []byte) *searchFilter {
	if len(data) == 32 {
		reader := wire.NewReader(data)
		for range 8 {
			if _, err := reader.QString(); err != nil {
				return nil
			}
		}
		if _, err := reader.U32(); err != nil {
			return nil
		}
		if _, err := reader.U32(); err != nil {
			return nil
		}
		return &searchFilter{}
	}
	reader := wire.NewReader(data)
	ranked, err := reader.QString()
	if err != nil {
		return nil
	}
	count, err := reader.U32()
	if err != nil || count != 7 {
		return nil
	}
	slots := make([]string, 7)
	for index := range slots {
		slots[index], err = reader.QString()
		if err != nil {
			return nil
		}
	}
	gameMode, err := reader.QString()
	if err != nil {
		return nil
	}
	return &searchFilter{ranked: ranked, levelID: slots[0], jobID: slots[1], difficulty: slots[2], gameMode: gameMode}
}

func gatheringMatchesFilter(gathering *state.Gathering, filter *searchFilter) bool {
	if filter == nil {
		return true
	}
	settings := gathering.Settings()
	if len(settings) < 0x2a {
		return true
	}
	parseByte := func(value string) (byte, bool) {
		if len(value) != 1 || value[0] < '0' || value[0] > '9' {
			return 0, false
		}
		return value[0] - '0', true
	}
	if wanted, ok := parseByte(filter.ranked); ok && settings[0] != wanted {
		return false
	}
	if wanted, ok := parseByte(filter.gameMode); ok && settings[1] != wanted {
		return false
	}
	parseU32 := func(value string) (uint32, bool) {
		var parsed uint32
		if value == "" {
			return 0, false
		}
		for _, digit := range []byte(value) {
			if digit < '0' || digit > '9' {
				return 0, false
			}
			parsed = parsed*10 + uint32(digit-'0')
		}
		return parsed, true
	}
	if wanted, ok := parseU32(filter.levelID); ok && binary.LittleEndian.Uint32(settings[0x21:0x25]) != wanted {
		return false
	}
	if wanted, ok := parseU32(filter.jobID); ok && binary.LittleEndian.Uint32(settings[0x25:0x29]) != wanted {
		return false
	}
	if wanted, ok := parseByte(filter.difficulty); ok && settings[0x29] != wanted {
		return false
	}
	return true
}

func browseResponse(gatherings []*state.Gathering) []byte {
	var out wire.Writer
	out.U32(uint32(len(gatherings)))
	for _, gathering := range gatherings {
		out.Bytes(gathering.RenderSettingsForBrowse())
	}
	out.U32(uint32(len(gatherings)))
	for _, gathering := range gatherings {
		out.Bytes(gathering.RenderURLGroupForBrowse())
	}
	return out.Data()
}

func (s *Services) searchGatherings(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	filter := parseSearchFilter(request.Params)
	var active []*state.Gathering
	for _, gathering := range s.Gatherings.All() {
		if gathering.PublicRemaining() > 0 && !gathering.Closed && gatheringMatchesFilter(gathering, filter) {
			active = append(active, gathering)
		}
	}
	response := browseResponse(active)
	s.Logger.Info("search gatherings", "remote", connection.Remote, "matches", len(active))
	//s.Logger.Info("search gatherings wire", "remote", connection.Remote, "response_hex", hex.EncodeToString(response))
	return gameOK(request, response)
}

func (s *Services) quickMatch(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	ranked := byte(1)
	if len(request.Params) > 0 {
		ranked = request.Params[0]
	}
	var chosen *state.Gathering
	for _, gathering := range s.Gatherings.All() {
		if gathering.OwnerPID == connection.UserPID || gathering.Closed || gathering.PublicRemaining() <= 0 || gathering.HostConn == nil {
			continue
		}
		settings := gathering.Settings()
		if len(settings) > 0 && settings[0] != ranked {
			continue
		}
		chosen = gathering
		break
	}
	if chosen == nil {
		return gameOK(request, browseResponse(nil))
	}
	return gameOK(request, browseResponse([]*state.Gathering{chosen}))
}

func (s *Services) socialList(_ *rmc.Dispatcher, _ *prudp.Connection, request rmc.Request) rmc.Message {
	return gameOK(request, nil)
}

func parseReportVectors(params []byte, senderPID uint32) ([]float32, []float32, error) {
	reader := wire.NewReader(params)
	if _, err := reader.U32(); err != nil {
		return nil, nil, err
	}
	if _, err := reader.U32(); err != nil {
		return nil, nil, err
	}
	count, err := reader.U32()
	if err != nil || count > 64 {
		return nil, nil, fmt.Errorf("invalid report entry count %d", count)
	}
	for range count {
		pid, err := reader.U32()
		if err != nil {
			return nil, nil, err
		}
		if _, err := reader.QString(); err != nil {
			return nil, nil, err
		}
		if _, err := reader.U32(); err != nil {
			return nil, nil, err
		}
		readVector := func() ([]float32, error) {
			length, err := reader.U32()
			if err != nil || length > 4096 {
				return nil, fmt.Errorf("invalid vector length %d", length)
			}
			vector := make([]float32, length)
			for index := range vector {
				vector[index], err = reader.F32()
				if err != nil {
					return nil, err
				}
			}
			return vector, nil
		}
		vec0, err := readVector()
		if err != nil {
			return nil, nil, err
		}
		vec1, err := readVector()
		if err != nil {
			return nil, nil, err
		}
		if _, err := readVector(); err != nil {
			return nil, nil, err
		}
		if pid == senderPID {
			return vec0, vec1, nil
		}
	}
	return nil, nil, fmt.Errorf("sender pid %d not found in report", senderPID)
}

func leaderboardRows(vec0, vec1 []float32) map[uint32]store.LeaderboardRow {
	rows := make(map[uint32]store.LeaderboardRow)
	if len(vec1) >= 31 {
		for block, category := range instantBlockCategory {
			base := 7 + 4*block
			rows[category] = store.LeaderboardRow{Cash: vec1[base], ModeStat: vec1[base+1], MapID: uint32(vec1[base+2])}
		}
		if len(vec0) > 5 {
			rows[301] = store.LeaderboardRow{Cash: vec0[5]}
		}
	} else if len(vec1) >= 15 {
		rows[201] = store.LeaderboardRow{Cash: vec1[5]}
	}
	return rows
}

func (s *Services) lifecycle(dispatcher *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	if request.Method == endGameMethod {
		for _, gathering := range s.Gatherings.ContainingPID(connection.UserPID) {
			s.cancelReportBatch(gathering)
		}
		return gameOK(request, nil)
	}
	if request.Method != reportStatsMethod || len(request.Params) == 0 {
		return gameOK(request, nil)
	}
	vec0, vec1, err := parseReportVectors(request.Params, connection.UserPID)
	if err != nil {
		s.Logger.Warn("parse stats report", "pid", connection.UserPID, "error", err)
	} else {
		if err := s.Store.PersistMatch(context.Background(), connection.UserPID, vec0, leaderboardRows(vec0, vec1)); err != nil {
			s.Logger.Error("persist stats report", "pid", connection.UserPID, "error", err)
		}
	}

	s.statsMu.Lock()
	s.statsTxn[connection.UserPID]++
	s.statsMu.Unlock()

	var completedBatches []*state.Gathering
	for _, gathering := range s.Gatherings.ContainingPID(connection.UserPID) {
		gathering.Mu.Lock()
		if len(gathering.ExpectedReportPIDs) == 0 {
			for _, participant := range gathering.Participants {
				gathering.ExpectedReportPIDs[participant.PID] = struct{}{}
			}
		}
		gathering.ReceivedReportPIDs[connection.UserPID] = struct{}{}
		if setContainsAll(gathering.ReceivedReportPIDs, gathering.ExpectedReportPIDs) {
			gathering.Mu.Unlock()
			completedBatches = append(completedBatches, gathering)
		} else if gathering.ReportTimeout == nil {
			id := gathering.ID
			gathering.ReportTimeout = time.AfterFunc(30*time.Second, func() {
				if current := s.Gatherings.Get(id); current != nil {
					s.fireStatsBatch(dispatcher, current, "timeout")
				}
			})
			gathering.Mu.Unlock()
		} else {
			gathering.Mu.Unlock()
		}
	}

	// SparkProtocolClient method 12 registers no return object. When this report
	// completes the batch, queue that void response before sending the 901001
	// notification. The notification synchronously starts method 9 in the game;
	// delivering it while method 12 is still pending re-enters RVGameContext and
	// leaves the subsequent lobby-browser call context stuck.
	response := gameOK(request, nil)
	if len(completedBatches) == 0 {
		return response
	}
	dispatcher.Send(connection, response)
	s.Logger.Info("report stats response queued before notification", "remote", connection.Remote, "batches", len(completedBatches))
	for _, gathering := range completedBatches {
		s.fireStatsBatch(dispatcher, gathering, "all-reported")
	}
	return nil
}

func setContainsAll(have, want map[uint32]struct{}) bool {
	for pid := range want {
		if _, ok := have[pid]; !ok {
			return false
		}
	}
	return true
}

func (s *Services) closeParticipation(_ *rmc.Dispatcher, connection *prudp.Connection, request rmc.Request) rmc.Message {
	var id uint32
	if len(request.Params) >= 4 {
		id = binary.LittleEndian.Uint32(request.Params[:4])
	}
	s.Gatherings.SetClosed(id, true)
	if gathering := s.Gatherings.Get(id); gathering != nil {
		gathering.Mu.Lock()
		if len(gathering.ExpectedReportPIDs) == 0 {
			for _, participant := range gathering.Participants {
				gathering.ExpectedReportPIDs[participant.PID] = struct{}{}
			}
		}
		gathering.Mu.Unlock()
	}
	s.Logger.Info("closed participation", "remote", connection.Remote, "gathering_id", id)
	return gameOK(request, nil)
}

func (s *Services) openParticipation(_ *rmc.Dispatcher, _ *prudp.Connection, request rmc.Request) rmc.Message {
	if len(request.Params) >= 4 {
		s.Gatherings.SetClosed(binary.LittleEndian.Uint32(request.Params[:4]), false)
	}
	return gameOK(request, nil)
}

func (s *Services) fireStatsBatch(dispatcher *rmc.Dispatcher, gathering *state.Gathering, reason string) {
	gathering.Mu.Lock()
	defer gathering.Mu.Unlock()
	if len(gathering.ReceivedReportPIDs) == 0 {
		return
	}
	if gathering.ReportTimeout != nil {
		gathering.ReportTimeout.Stop()
		gathering.ReportTimeout = nil
	}
	var sourcePID uint32
	for sourcePID = range gathering.ReceivedReportPIDs {
		break
	}
	s.pushStatsProcessed(dispatcher, gathering, sourcePID)
	clear(gathering.ExpectedReportPIDs)
	clear(gathering.ReceivedReportPIDs)
	s.Logger.Info("stats notification batch", "gathering_id", gathering.ID, "reason", reason)
}

func (s *Services) cancelReportBatch(gathering *state.Gathering) {
	gathering.Mu.Lock()
	defer gathering.Mu.Unlock()
	if gathering.ReportTimeout != nil {
		gathering.ReportTimeout.Stop()
		gathering.ReportTimeout = nil
	}
	clear(gathering.ExpectedReportPIDs)
	clear(gathering.ReceivedReportPIDs)
}
