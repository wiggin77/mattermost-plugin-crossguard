package store

const (
	PromptStatePending = "pending"
	PromptStateBlocked = "blocked"
)

// ConnectionRequest represents a pending team admin request for a system admin
// to approve linking a connection to a team.
type ConnectionRequest struct {
	RequesterID string   `json:"requester_id"`
	TeamID      string   `json:"team_id"`
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
	SetPostMapping(connName, remotePostID, localPostID string) error
	GetPostMapping(connName, remotePostID string) (string, error)
	DeletePostMapping(connName, remotePostID string) error
	SetDeletingFlag(postID string) error
	IsDeletingFlagSet(postID string) (bool, error)
	ClearDeletingFlag(postID string) error
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
}
