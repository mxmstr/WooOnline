package state

import (
	"encoding/binary"
	"log/slog"
	"sync"
	"time"

	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/wire"
)

const (
	gatheringIDOffset                = 0x14
	ownerPIDOffset                   = 0x18
	displayQStringOffset             = 0x34
	settingsBlockSize                = 0x40
	participantCountOffsetInSettings = 0x11
	publicSlotsOffsetInSettings      = 0x0d
	privateSlotsOffsetInSettings     = 0x15
	MaxParticipantsPerGathering      = 4
	strangleholdDerivedSize          = 1 + 13*4 + 1
)

type Participant struct {
	PID         uint32
	DisplayName string
	RemoteKey   string
	State       [16]byte
}

type Gathering struct {
	Mu           sync.Mutex
	ID           uint32
	OwnerPID     uint32
	OwnerRemote  string
	RawParams    []byte
	CreatedAt    time.Time
	HostConn     *prudp.Connection
	Participants []*Participant
	InvitedPIDs  map[uint32]struct{}
	Closed       bool

	ExpectedReportPIDs map[uint32]struct{}
	ReceivedReportPIDs map[uint32]struct{}
	ReportTimeout      *time.Timer
}

func (g *Gathering) StrangleholdObjectEnd() (int, bool) {
	if len(g.RawParams) < displayQStringOffset+2 {
		return 0, false
	}
	classLength := int(binary.LittleEndian.Uint16(g.RawParams[:2]))
	if classLength != 10 || string(g.RawParams[2:12]) != "SparkGame\x00" {
		return 0, false
	}
	displayLength := int(binary.LittleEndian.Uint16(g.RawParams[displayQStringOffset : displayQStringOffset+2]))
	end := displayQStringOffset + 2 + displayLength + strangleholdDerivedSize
	if displayLength < 1 || end > len(g.RawParams) {
		return 0, false
	}
	return end, true
}

func (g *Gathering) IsStrangleholdLayout() bool {
	_, ok := g.StrangleholdObjectEnd()
	return ok
}

func (g *Gathering) wireOffsets() (int, int, int) {
	return gatheringIDOffset, ownerPIDOffset, displayQStringOffset
}

func (g *Gathering) settingsSlice() []byte {
	if len(g.RawParams) < displayQStringOffset+2 {
		return nil
	}
	displayLength := int(binary.LittleEndian.Uint16(g.RawParams[displayQStringOffset : displayQStringOffset+2]))
	start := displayQStringOffset + 2 + displayLength
	end := start + settingsBlockSize
	if len(g.RawParams) < end {
		return nil
	}
	return g.RawParams[start:end]
}

func (g *Gathering) PublicSlots() int {
	settings := g.settingsSlice()
	if settings == nil {
		return MaxParticipantsPerGathering - 1
	}
	return int(binary.LittleEndian.Uint32(settings[publicSlotsOffsetInSettings : publicSlotsOffsetInSettings+4]))
}

func (g *Gathering) PrivateSlots() int {
	settings := g.settingsSlice()
	if settings == nil {
		return 0
	}
	return int(binary.LittleEndian.Uint32(settings[privateSlotsOffsetInSettings : privateSlotsOffsetInSettings+4]))
}

func (g *Gathering) PublicRemaining() int {
	used := 0
	for _, participant := range g.Participants {
		if participant.PID == g.OwnerPID {
			continue
		}
		if _, invited := g.InvitedPIDs[participant.PID]; !invited {
			used++
		}
	}
	return max(0, g.PublicSlots()-used)
}

func (g *Gathering) IsFull() bool {
	return len(g.Participants) >= MaxParticipantsPerGathering
}

func (g *Gathering) HostDisplayName() string {
	_, _, displayOffset := g.wireOffsets()
	if len(g.RawParams) < displayOffset+2 {
		return "pid_" + decimal(g.OwnerPID)
	}
	size := int(binary.LittleEndian.Uint16(g.RawParams[displayOffset : displayOffset+2]))
	start := displayOffset + 2
	if size < 1 || start+size > len(g.RawParams) {
		return "pid_" + decimal(g.OwnerPID)
	}
	raw := g.RawParams[start : start+size]
	if raw[len(raw)-1] == 0 {
		raw = raw[:len(raw)-1]
	}
	for index, value := range raw {
		if value == ' ' {
			raw = raw[:index]
			break
		}
	}
	return string(raw)
}

func (g *Gathering) RenderJoinEmbed(postJoinCount int) []byte {
	if len(g.RawParams) < ownerPIDOffset+4 {
		return append([]byte(nil), g.RawParams...)
	}
	out := append([]byte(nil), g.RawParams...)
	binary.LittleEndian.PutUint32(out[gatheringIDOffset:gatheringIDOffset+4], g.ID)
	binary.LittleEndian.PutUint32(out[ownerPIDOffset:ownerPIDOffset+4], g.OwnerPID)
	if len(out) < displayQStringOffset+2 {
		return out
	}
	displayLength := int(binary.LittleEndian.Uint16(out[displayQStringOffset : displayQStringOffset+2]))
	settingsStart := displayQStringOffset + 2 + displayLength
	settingsEnd := settingsStart + settingsBlockSize
	if len(out) < settingsEnd {
		return out
	}
	countOffset := settingsStart + participantCountOffsetInSettings
	binary.LittleEndian.PutUint32(out[countOffset:countOffset+4], uint32(postJoinCount))
	return out[:settingsEnd-1]
}

func (g *Gathering) RenderSettingsForBrowse() []byte {
	gatheringOffset, ownerOffset, displayOffset := g.wireOffsets()
	if len(g.RawParams) < ownerOffset+4 {
		return append([]byte(nil), g.RawParams...)
	}
	out := append([]byte(nil), g.RawParams...)
	binary.LittleEndian.PutUint32(out[gatheringOffset:gatheringOffset+4], g.ID)
	binary.LittleEndian.PutUint32(out[ownerOffset:ownerOffset+4], g.OwnerPID)
	if end, ok := g.StrangleholdObjectEnd(); ok {
		if len(out) >= 0x20 {
			binary.LittleEndian.PutUint32(out[0x1c:0x20], g.OwnerPID)
		}
		return out[:end]
	}
	if len(out) < displayOffset+2 {
		return out
	}
	displayLength := int(binary.LittleEndian.Uint16(out[displayOffset : displayOffset+2]))
	settingsStart := displayOffset + 2 + displayLength
	settingsEnd := settingsStart + settingsBlockSize
	if len(out) < settingsEnd {
		return out
	}
	countOffset := settingsStart + participantCountOffsetInSettings
	binary.LittleEndian.PutUint32(out[countOffset:countOffset+4], uint32(max(0, len(g.Participants)-1)))
	return out[:settingsEnd-1]
}

func (g *Gathering) RenderURLGroupForBrowse() []byte {
	if objectEnd, ok := g.StrangleholdObjectEnd(); ok && len(g.RawParams) >= objectEnd+4 {
		trailing := g.RawParams[objectEnd:]
		for _, start := range []int{1, 0} {
			if len(trailing) < start+4 {
				continue
			}
			urls := trailing[start:]
			count := int(binary.LittleEndian.Uint32(urls[:4]))
			if count > 16 {
				continue
			}
			offset := 4
			valid := true
			for range count {
				if offset+2 > len(urls) {
					valid = false
					break
				}
				size := int(binary.LittleEndian.Uint16(urls[offset : offset+2]))
				if size < 1 || offset+2+size > len(urls) {
					valid = false
					break
				}
				offset += 2 + size
			}
			if valid && offset == len(urls) {
				out := binary.LittleEndian.AppendUint32(nil, g.ID)
				return append(out, urls...)
			}
		}
	}
	if g.HostConn != nil && len(g.HostConn.StationURLs) > 0 {
		var out wire.Writer
		out.U32(g.ID)
		out.U32(uint32(len(g.HostConn.StationURLs)))
		for _, stationURL := range g.HostConn.StationURLs {
			out.QString(stationURL)
		}
		return out.Data()
	}
	settings := g.settingsSlice()
	if settings == nil {
		return nil
	}
	displayLength := int(binary.LittleEndian.Uint16(g.RawParams[displayQStringOffset : displayQStringOffset+2]))
	settingsEnd := displayQStringOffset + 2 + displayLength + settingsBlockSize
	out := binary.LittleEndian.AppendUint32(nil, g.ID)
	return append(out, g.RawParams[settingsEnd:]...)
}

func (g *Gathering) Settings() []byte {
	return append([]byte(nil), g.settingsSlice()...)
}

type GatheringRegistry struct {
	mu             sync.RWMutex
	nextID         uint32
	byID           map[uint32]*Gathering
	ownerToID      map[uint32]uint32
	keepOldMatches bool
	logger         *slog.Logger
}

func NewGatheringRegistry(keepOldMatches bool, logger *slog.Logger) *GatheringRegistry {
	return &GatheringRegistry{
		nextID: 10000, byID: make(map[uint32]*Gathering),
		ownerToID: make(map[uint32]uint32), keepOldMatches: keepOldMatches,
		logger: logger.With("component", "gatherings"),
	}
}

func (r *GatheringRegistry) Create(ownerPID uint32, ownerRemote string, rawParams []byte, host *prudp.Connection) *Gathering {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.ownerToID[ownerPID]; ok && !r.keepOldMatches {
		delete(r.byID, existingID)
		r.logger.Info("auto-destroying stale gathering", "gathering_id", existingID, "owner_pid", ownerPID)
	}
	gathering := &Gathering{
		ID: r.nextID, OwnerPID: ownerPID, OwnerRemote: ownerRemote,
		RawParams: append([]byte(nil), rawParams...), CreatedAt: time.Now(), HostConn: host,
		InvitedPIDs:        make(map[uint32]struct{}),
		ExpectedReportPIDs: make(map[uint32]struct{}),
		ReceivedReportPIDs: make(map[uint32]struct{}),
	}
	r.nextID++
	gathering.Participants = append(gathering.Participants, &Participant{
		PID: ownerPID, DisplayName: gathering.HostDisplayName(), RemoteKey: ownerRemote,
	})
	r.byID[gathering.ID] = gathering
	r.ownerToID[ownerPID] = gathering.ID
	return gathering
}

func (r *GatheringRegistry) Get(id uint32) *Gathering {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[id]
}

func (r *GatheringRegistry) All() []*Gathering {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Gathering, 0, len(r.byID))
	for _, gathering := range r.byID {
		out = append(out, gathering)
	}
	return out
}

func (r *GatheringRegistry) AddParticipant(id, pid uint32, displayName, remote string) *Participant {
	r.mu.Lock()
	defer r.mu.Unlock()
	gathering := r.byID[id]
	if gathering == nil {
		return nil
	}
	for _, participant := range gathering.Participants {
		if participant.PID == pid {
			return participant
		}
	}
	participant := &Participant{PID: pid, DisplayName: displayName, RemoteKey: remote}
	gathering.Participants = append(gathering.Participants, participant)
	return participant
}

func (r *GatheringRegistry) RemoveParticipant(id, pid uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	gathering := r.byID[id]
	if gathering == nil {
		return false
	}
	for index, participant := range gathering.Participants {
		if participant.PID == pid {
			gathering.Participants = append(gathering.Participants[:index], gathering.Participants[index+1:]...)
			return true
		}
	}
	return false
}

func (r *GatheringRegistry) RemoveParticipantByRemote(remote string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for _, gathering := range r.byID {
		if gathering.OwnerRemote == remote {
			continue
		}
		filtered := gathering.Participants[:0]
		found := false
		for _, participant := range gathering.Participants {
			if participant.RemoteKey == remote {
				found = true
				continue
			}
			filtered = append(filtered, participant)
		}
		gathering.Participants = filtered
		if found {
			removed++
		}
	}
	return removed
}

func (r *GatheringRegistry) SetClosed(id uint32, closed bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	gathering := r.byID[id]
	if gathering == nil || gathering.Closed == closed {
		return false
	}
	gathering.Closed = closed
	return true
}

func (r *GatheringRegistry) Destroy(id uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	gathering := r.byID[id]
	if gathering == nil {
		return false
	}
	if gathering.ReportTimeout != nil {
		gathering.ReportTimeout.Stop()
	}
	delete(r.byID, id)
	if r.ownerToID[gathering.OwnerPID] == id {
		delete(r.ownerToID, gathering.OwnerPID)
	}
	return true
}

func (r *GatheringRegistry) DestroyByOwnerRemote(remote string) int {
	if r.keepOldMatches {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []uint32
	for id, gathering := range r.byID {
		if gathering.OwnerRemote == remote {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		gathering := r.byID[id]
		delete(r.byID, id)
		if r.ownerToID[gathering.OwnerPID] == id {
			delete(r.ownerToID, gathering.OwnerPID)
		}
	}
	return len(ids)
}

func (r *GatheringRegistry) ContainingPID(pid uint32) []*Gathering {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Gathering
	for _, gathering := range r.byID {
		for _, participant := range gathering.Participants {
			if participant.PID == pid {
				out = append(out, gathering)
				break
			}
		}
	}
	return out
}

func decimal(value uint32) string {
	if value == 0 {
		return "0"
	}
	var digits [10]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
