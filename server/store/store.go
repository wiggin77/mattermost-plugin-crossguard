package store

import "time"

const (
	PromptStatePending = "pending"
	PromptStateBlocked = "blocked"
)

// ConnectionRequest represents a pending team admin request for a system admin
// to approve linking a connection to a team.
type ConnectionRequest struct {
	RequesterID string   `json:"requester_id"`
	TeamID      string   `json:"team_id"`
	ChannelID   string   `json:"channel_id,omitempty"`
	ConnKey     string   `json:"conn_key"`
	PostIDs     []string `json:"post_ids"`
	CreatedAt   int64    `json:"created_at"`
}

// ConnectionPrompt represents a pending or blocked inbound connection prompt.
type ConnectionPrompt struct {
	State  string `json:"state"`
	PostID string `json:"post_id"`
}

// TeamConnection represents a single connection linked to a team or channel.
type TeamConnection struct {
	Direction      string `json:"direction"`
	Connection     string `json:"connection"`
	RemoteTeamName string `json:"remote_team_name,omitempty"`
}

// Matches returns true if both connections share the same Direction and Connection.
// RemoteTeamName is metadata and is not considered for identity matching.
func (tc TeamConnection) Matches(other TeamConnection) bool {
	return tc.Direction == other.Direction && tc.Connection == other.Connection
}

// KVStore defines the key-value operations used by the plugin.
type KVStore interface {
	GetTeamConnections(teamID string) ([]TeamConnection, error)
	SetTeamConnections(teamID string, conns []TeamConnection) error
	DeleteTeamConnections(teamID string) error
	IsTeamInitialized(teamID string) (bool, error)
	AddTeamConnection(teamID string, conn TeamConnection) error
	RemoveTeamConnection(teamID string, conn TeamConnection) error
	GetInitializedTeamIDs() ([]string, error)
	AddInitializedTeamID(teamID string) error
	RemoveInitializedTeamID(teamID string) error
	GetChannelConnections(channelID string) ([]TeamConnection, error)
	SetChannelConnections(channelID string, conns []TeamConnection) error
	DeleteChannelConnections(channelID string) error
	IsChannelInitialized(channelID string) (bool, error)
	AddChannelConnection(channelID string, conn TeamConnection) error
	RemoveChannelConnection(channelID string, conn TeamConnection) error
	GetConnectionPrompt(teamID, connName string) (*ConnectionPrompt, error)
	SetConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) error
	DeleteConnectionPrompt(teamID, connName string) error
	CreateConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) (bool, error)
	GetChannelConnectionPrompt(channelID, connName string) (*ConnectionPrompt, error)
	SetChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) error
	DeleteChannelConnectionPrompt(channelID, connName string) error
	CreateChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) (bool, error)
	GetTeamRewriteIndex(connName, remoteTeamName string) (string, error)
	SetTeamRewriteIndex(connName, remoteTeamName, localTeamID string) error
	DeleteTeamRewriteIndex(connName, remoteTeamName string) error
	GetConnectionRequest(teamID, connKey string) (*ConnectionRequest, error)
	CreateConnectionRequest(teamID, connKey string, req *ConnectionRequest) (bool, error)
	DeleteConnectionRequest(teamID, connKey string) error
	GetChannelConnectionRequest(channelID, connKey string) (*ConnectionRequest, error)
	CreateChannelConnectionRequest(channelID, connKey string, req *ConnectionRequest) (bool, error)
	UpdateChannelConnectionRequest(channelID, connKey string, req *ConnectionRequest) error
	DeleteChannelConnectionRequest(channelID, connKey string) error

	// AcquireOrRenewInboundLease writes nodeID as the lease holder for the
	// named inbound connection with a TTL, using CAS so only one cluster node
	// holds the lease at a time. Returns:
	//   - acquired=true, renewed=false: lease was unheld or expired; we now hold it.
	//   - acquired=false, renewed=true: we already held the lease; TTL extended.
	//   - acquired=false, renewed=false: someone else holds it; currentHolder is set.
	// Used by the single-active-receiver election loop.
	AcquireOrRenewInboundLease(connName, nodeID string, ttl time.Duration) (acquired, renewed bool, currentHolder string, err error)

	// ReleaseInboundLease deletes the lease key if and only if the current
	// holder is nodeID. Used for graceful step-down (OnDeactivate, config
	// reload). Safe to call when not the holder; returns nil in that case.
	ReleaseInboundLease(connName, nodeID string) error

	// GetSequencerCursor returns the persisted (epoch, nextExpected) for
	// the inbound (connName, channelID) sequencer cursor, or empty/zero
	// when no checkpoint has been written. Used on lease acquisition to
	// resume the cursor where the previous active node left off.
	GetSequencerCursor(connName, channelID string) (epoch string, nextExpected uint64, err error)

	// SetSequencerCursor writes the (epoch, nextExpected) checkpoint for
	// the named cursor. Called periodically by the sequencer to bound
	// the duplicate-redelivery window after leadership handoff.
	SetSequencerCursor(connName, channelID, epoch string, nextExpected uint64) error
}
