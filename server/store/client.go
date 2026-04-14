package store

import (
	"fmt"
	"slices"

	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"
)

// Client wraps the Mattermost pluginapi KV store.
type Client struct {
	client                *pluginapi.Client
	teamInitPrefix        string
	channelInitPrefix     string
	initializedTeamsKey   string
	connPromptPrefix      string
	chanConnPromptPrefix  string
	rewriteIndexPrefix    string
	connRequestPrefix     string
	chanConnRequestPrefix string
}

// NewKVStore creates a new KV store client.
func NewKVStore(client *pluginapi.Client, pluginID string) KVStore {
	return Client{
		client:                client,
		teamInitPrefix:        pluginID + "-teaminit-",
		channelInitPrefix:     pluginID + "-channelinit-",
		initializedTeamsKey:   pluginID + "-initialized-teams",
		connPromptPrefix:      pluginID + "-connprompt-",
		chanConnPromptPrefix:  pluginID + "-chanprompt-",
		rewriteIndexPrefix:    pluginID + "-rwi-",
		connRequestPrefix:     pluginID + "-connreq-",
		chanConnRequestPrefix: pluginID + "-chanconnreq-",
	}
}

// GetTeamConnections returns the list of connections linked to a team.
func (kv Client) GetTeamConnections(teamID string) ([]TeamConnection, error) {
	var conns []TeamConnection
	err := kv.client.KV.Get(kv.teamInitPrefix+teamID, &conns)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get team connections")
	}
	if conns == nil {
		return []TeamConnection{}, nil
	}
	return conns, nil
}

// SetTeamConnections stores the full list of connections for a team.
func (kv Client) SetTeamConnections(teamID string, conns []TeamConnection) error {
	_, err := kv.client.KV.Set(kv.teamInitPrefix+teamID, conns)
	if err != nil {
		return errors.Wrap(err, "failed to set team connections")
	}
	return nil
}

// DeleteTeamConnections removes all connection links for a team.
func (kv Client) DeleteTeamConnections(teamID string) error {
	if err := kv.client.KV.Delete(kv.teamInitPrefix + teamID); err != nil {
		return errors.Wrap(err, "failed to delete team connections")
	}
	return nil
}

// IsTeamInitialized returns true if the team has at least one connection linked.
func (kv Client) IsTeamInitialized(teamID string) (bool, error) {
	conns, err := kv.GetTeamConnections(teamID)
	if err != nil {
		return false, err
	}
	return len(conns) > 0, nil
}

// AddTeamConnection atomically adds a connection to a team's connection list.
func (kv Client) AddTeamConnection(teamID string, conn TeamConnection) error {
	return kv.casModifyConnectionList(kv.teamInitPrefix+teamID, func(conns []TeamConnection) ([]TeamConnection, bool) {
		for _, existing := range conns {
			if existing.Matches(conn) {
				return conns, false
			}
		}
		return append(conns, conn), true
	})
}

// RemoveTeamConnection atomically removes a connection from a team's connection list.
// It also cleans up the reverse index if the removed connection had a RemoteTeamName.
func (kv Client) RemoveTeamConnection(teamID string, conn TeamConnection) error {
	var removedConn *TeamConnection
	err := kv.casModifyConnectionList(kv.teamInitPrefix+teamID, func(conns []TeamConnection) ([]TeamConnection, bool) {
		idx := -1
		for i, existing := range conns {
			if existing.Matches(conn) {
				idx = i
				removedConn = &TeamConnection{
					Direction:      existing.Direction,
					Connection:     existing.Connection,
					RemoteTeamName: existing.RemoteTeamName,
				}
				break
			}
		}
		if idx < 0 {
			return conns, false
		}
		result := make([]TeamConnection, 0, len(conns)-1)
		result = append(result, conns[:idx]...)
		result = append(result, conns[idx+1:]...)
		return result, true
	})
	if err != nil {
		return err
	}
	if removedConn != nil && removedConn.RemoteTeamName != "" {
		_ = kv.DeleteTeamRewriteIndex(removedConn.Connection, removedConn.RemoteTeamName)
	}
	return nil
}

// GetInitializedTeamIDs returns the list of team IDs that have been initialized.
func (kv Client) GetInitializedTeamIDs() ([]string, error) {
	var teamIDs []string
	if err := kv.client.KV.Get(kv.initializedTeamsKey, &teamIDs); err != nil {
		return nil, errors.Wrap(err, "failed to get initialized team IDs")
	}
	if teamIDs == nil {
		return []string{}, nil
	}
	return teamIDs, nil
}

// AddInitializedTeamID atomically adds a team ID to the initialized teams list
// using compare-and-set to prevent overwrites from concurrent writes.
func (kv Client) AddInitializedTeamID(teamID string) error {
	return kv.casModifyStringList(kv.initializedTeamsKey, func(ids []string) ([]string, bool) {
		if slices.Contains(ids, teamID) {
			return ids, false
		}
		return append(ids, teamID), true
	})
}

// RemoveInitializedTeamID atomically removes a team ID from the initialized teams list.
func (kv Client) RemoveInitializedTeamID(teamID string) error {
	return kv.casModifyStringList(kv.initializedTeamsKey, func(ids []string) ([]string, bool) {
		idx := slices.Index(ids, teamID)
		if idx < 0 {
			return ids, false
		}
		result := make([]string, 0, len(ids)-1)
		result = append(result, ids[:idx]...)
		result = append(result, ids[idx+1:]...)
		return result, true
	})
}

// GetChannelConnections returns the list of connections linked to a channel.
func (kv Client) GetChannelConnections(channelID string) ([]TeamConnection, error) {
	var conns []TeamConnection
	err := kv.client.KV.Get(kv.channelInitPrefix+channelID, &conns)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get channel connections")
	}
	if conns == nil {
		return []TeamConnection{}, nil
	}
	return conns, nil
}

// SetChannelConnections stores the full list of connections for a channel.
func (kv Client) SetChannelConnections(channelID string, conns []TeamConnection) error {
	_, err := kv.client.KV.Set(kv.channelInitPrefix+channelID, conns)
	if err != nil {
		return errors.Wrap(err, "failed to set channel connections")
	}
	return nil
}

// DeleteChannelConnections removes all connection links for a channel.
func (kv Client) DeleteChannelConnections(channelID string) error {
	if err := kv.client.KV.Delete(kv.channelInitPrefix + channelID); err != nil {
		return errors.Wrap(err, "failed to delete channel connections")
	}
	return nil
}

// IsChannelInitialized returns true if the channel has at least one connection linked.
func (kv Client) IsChannelInitialized(channelID string) (bool, error) {
	conns, err := kv.GetChannelConnections(channelID)
	if err != nil {
		return false, err
	}
	return len(conns) > 0, nil
}

// AddChannelConnection atomically adds a connection to a channel's connection list.
func (kv Client) AddChannelConnection(channelID string, conn TeamConnection) error {
	return kv.casModifyConnectionList(kv.channelInitPrefix+channelID, func(conns []TeamConnection) ([]TeamConnection, bool) {
		for _, existing := range conns {
			if existing.Matches(conn) {
				return conns, false
			}
		}
		return append(conns, conn), true
	})
}

// RemoveChannelConnection atomically removes a connection from a channel's connection list.
func (kv Client) RemoveChannelConnection(channelID string, conn TeamConnection) error {
	return kv.casModifyConnectionList(kv.channelInitPrefix+channelID, func(conns []TeamConnection) ([]TeamConnection, bool) {
		idx := -1
		for i, existing := range conns {
			if existing.Matches(conn) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return conns, false
		}
		result := make([]TeamConnection, 0, len(conns)-1)
		result = append(result, conns[:idx]...)
		result = append(result, conns[idx+1:]...)
		return result, true
	})
}

// postMappingKey returns the KV key for a remote-to-local post ID mapping.
func postMappingKey(connName, remotePostID string) string {
	return "pm-" + connName + "-" + remotePostID
}

// SetPostMapping stores a remote-to-local post ID mapping for a connection.
func (kv Client) SetPostMapping(connName, remotePostID, localPostID string) error {
	_, err := kv.client.KV.Set(postMappingKey(connName, remotePostID), localPostID)
	if err != nil {
		return errors.Wrap(err, "failed to set post mapping")
	}
	return nil
}

// GetPostMapping retrieves the local post ID for a remote post on a connection.
func (kv Client) GetPostMapping(connName, remotePostID string) (string, error) {
	var localPostID string
	err := kv.client.KV.Get(postMappingKey(connName, remotePostID), &localPostID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get post mapping")
	}
	return localPostID, nil
}

// DeletePostMapping removes a remote-to-local post ID mapping.
func (kv Client) DeletePostMapping(connName, remotePostID string) error {
	if err := kv.client.KV.Delete(postMappingKey(connName, remotePostID)); err != nil {
		return errors.Wrap(err, "failed to delete post mapping")
	}
	return nil
}

func deletingFlagKey(postID string) string {
	return "crossguard-deleting-" + postID
}

// SetDeletingFlag marks a post as being deleted by the inbound handler.
func (kv Client) SetDeletingFlag(postID string) error {
	_, err := kv.client.KV.Set(deletingFlagKey(postID), true)
	if err != nil {
		return errors.Wrap(err, "failed to set deleting flag")
	}
	return nil
}

// IsDeletingFlagSet returns true if the post is being deleted by the inbound handler.
func (kv Client) IsDeletingFlagSet(postID string) (bool, error) {
	var set bool
	err := kv.client.KV.Get(deletingFlagKey(postID), &set)
	if err != nil {
		return false, errors.Wrap(err, "failed to get deleting flag")
	}
	return set, nil
}

// ClearDeletingFlag removes the deleting flag for a post.
func (kv Client) ClearDeletingFlag(postID string) error {
	if err := kv.client.KV.Delete(deletingFlagKey(postID)); err != nil {
		return errors.Wrap(err, "failed to clear deleting flag")
	}
	return nil
}

// GetConnectionPrompt retrieves the connection prompt state for a team+connection.
func (kv Client) GetConnectionPrompt(teamID, connName string) (*ConnectionPrompt, error) {
	var prompt ConnectionPrompt
	err := kv.client.KV.Get(kv.connPromptPrefix+teamID+"-"+connName, &prompt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get connection prompt")
	}
	if prompt.State == "" {
		return nil, nil
	}
	return &prompt, nil
}

// SetConnectionPrompt stores the connection prompt state for a team+connection.
func (kv Client) SetConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) error {
	_, err := kv.client.KV.Set(kv.connPromptPrefix+teamID+"-"+connName, prompt)
	if err != nil {
		return errors.Wrap(err, "failed to set connection prompt")
	}
	return nil
}

// DeleteConnectionPrompt removes the connection prompt state for a team+connection.
func (kv Client) DeleteConnectionPrompt(teamID, connName string) error {
	if err := kv.client.KV.Delete(kv.connPromptPrefix + teamID + "-" + connName); err != nil {
		return errors.Wrap(err, "failed to delete connection prompt")
	}
	return nil
}

// CreateConnectionPrompt atomically creates a connection prompt only if none exists.
// Returns true if created, false if a prompt already exists for this team+connection.
func (kv Client) CreateConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) (bool, error) {
	saved, err := kv.client.KV.Set(kv.connPromptPrefix+teamID+"-"+connName, prompt, pluginapi.SetAtomic(nil))
	if err != nil {
		return false, errors.Wrap(err, "failed to create connection prompt")
	}
	return saved, nil
}

// GetChannelConnectionPrompt retrieves the connection prompt state for a channel+connection.
func (kv Client) GetChannelConnectionPrompt(channelID, connName string) (*ConnectionPrompt, error) {
	var prompt ConnectionPrompt
	err := kv.client.KV.Get(kv.chanConnPromptPrefix+channelID+"-"+connName, &prompt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get channel connection prompt")
	}
	if prompt.State == "" {
		return nil, nil
	}
	return &prompt, nil
}

// SetChannelConnectionPrompt stores the connection prompt state for a channel+connection.
func (kv Client) SetChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) error {
	_, err := kv.client.KV.Set(kv.chanConnPromptPrefix+channelID+"-"+connName, prompt)
	if err != nil {
		return errors.Wrap(err, "failed to set channel connection prompt")
	}
	return nil
}

// DeleteChannelConnectionPrompt removes the connection prompt state for a channel+connection.
func (kv Client) DeleteChannelConnectionPrompt(channelID, connName string) error {
	if err := kv.client.KV.Delete(kv.chanConnPromptPrefix + channelID + "-" + connName); err != nil {
		return errors.Wrap(err, "failed to delete channel connection prompt")
	}
	return nil
}

// CreateChannelConnectionPrompt atomically creates a channel connection prompt only if none exists.
// Returns true if created, false if a prompt already exists for this channel+connection.
func (kv Client) CreateChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) (bool, error) {
	saved, err := kv.client.KV.Set(kv.chanConnPromptPrefix+channelID+"-"+connName, prompt, pluginapi.SetAtomic(nil))
	if err != nil {
		return false, errors.Wrap(err, "failed to create channel connection prompt")
	}
	return saved, nil
}

// GetConnectionRequest retrieves a pending connection request for a team+connKey.
func (kv Client) GetConnectionRequest(teamID, connKey string) (*ConnectionRequest, error) {
	var req ConnectionRequest
	err := kv.client.KV.Get(kv.connRequestPrefix+teamID+"-"+connKey, &req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get connection request")
	}
	if req.RequesterID == "" {
		return nil, nil
	}
	return &req, nil
}

// CreateConnectionRequest atomically creates a connection request only if none exists.
// Returns true if created, false if a request already exists.
func (kv Client) CreateConnectionRequest(teamID, connKey string, req *ConnectionRequest) (bool, error) {
	saved, err := kv.client.KV.Set(kv.connRequestPrefix+teamID+"-"+connKey, req, pluginapi.SetAtomic(nil))
	if err != nil {
		return false, errors.Wrap(err, "failed to create connection request")
	}
	return saved, nil
}

// DeleteConnectionRequest removes a pending connection request.
func (kv Client) DeleteConnectionRequest(teamID, connKey string) error {
	if err := kv.client.KV.Delete(kv.connRequestPrefix + teamID + "-" + connKey); err != nil {
		return errors.Wrap(err, "failed to delete connection request")
	}
	return nil
}

// GetChannelConnectionRequest retrieves a pending channel connection request.
func (kv Client) GetChannelConnectionRequest(channelID, connKey string) (*ConnectionRequest, error) {
	var req ConnectionRequest
	err := kv.client.KV.Get(kv.chanConnRequestPrefix+channelID+"-"+connKey, &req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get channel connection request")
	}
	if req.RequesterID == "" {
		return nil, nil
	}
	return &req, nil
}

// CreateChannelConnectionRequest atomically creates a channel connection request only if none exists.
// Returns true if created, false if a request already exists.
func (kv Client) CreateChannelConnectionRequest(channelID, connKey string, req *ConnectionRequest) (bool, error) {
	saved, err := kv.client.KV.Set(kv.chanConnRequestPrefix+channelID+"-"+connKey, req, pluginapi.SetAtomic(nil))
	if err != nil {
		return false, errors.Wrap(err, "failed to create channel connection request")
	}
	return saved, nil
}

// UpdateChannelConnectionRequest overwrites an existing channel connection request.
func (kv Client) UpdateChannelConnectionRequest(channelID, connKey string, req *ConnectionRequest) error {
	_, err := kv.client.KV.Set(kv.chanConnRequestPrefix+channelID+"-"+connKey, req)
	if err != nil {
		return errors.Wrap(err, "failed to update channel connection request")
	}
	return nil
}

// DeleteChannelConnectionRequest removes a pending channel connection request.
func (kv Client) DeleteChannelConnectionRequest(channelID, connKey string) error {
	if err := kv.client.KV.Delete(kv.chanConnRequestPrefix + channelID + "-" + connKey); err != nil {
		return errors.Wrap(err, "failed to delete channel connection request")
	}
	return nil
}

func rewriteIndexKey(prefix, connName, remoteTeamName string) string {
	return prefix + connName + "::" + remoteTeamName
}

// GetTeamRewriteIndex returns the local team ID mapped to a (connName, remoteTeamName) pair.
func (kv Client) GetTeamRewriteIndex(connName, remoteTeamName string) (string, error) {
	var localTeamID string
	err := kv.client.KV.Get(rewriteIndexKey(kv.rewriteIndexPrefix, connName, remoteTeamName), &localTeamID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get rewrite index")
	}
	return localTeamID, nil
}

// SetTeamRewriteIndex maps a (connName, remoteTeamName) pair to a local team ID.
// Returns an error if the mapping already exists for a different team.
func (kv Client) SetTeamRewriteIndex(connName, remoteTeamName, localTeamID string) error {
	existing, err := kv.GetTeamRewriteIndex(connName, remoteTeamName)
	if err != nil {
		return err
	}
	if existing != "" && existing != localTeamID {
		return fmt.Errorf("remote team name %q on connection %q is already mapped to team %s; clear that rewrite first", remoteTeamName, connName, existing)
	}

	_, err = kv.client.KV.Set(rewriteIndexKey(kv.rewriteIndexPrefix, connName, remoteTeamName), localTeamID)
	if err != nil {
		return errors.Wrap(err, "failed to set rewrite index")
	}
	return nil
}

// DeleteTeamRewriteIndex removes a (connName, remoteTeamName) reverse index entry.
func (kv Client) DeleteTeamRewriteIndex(connName, remoteTeamName string) error {
	if err := kv.client.KV.Delete(rewriteIndexKey(kv.rewriteIndexPrefix, connName, remoteTeamName)); err != nil {
		return errors.Wrap(err, "failed to delete rewrite index")
	}
	return nil
}

// casModifyStringList atomically modifies a string list stored in KV.
// The modify function receives the current list and returns the new list and
// whether a change was made. If no change, the function returns nil immediately.
func (kv Client) casModifyStringList(key string, modify func([]string) ([]string, bool)) error {
	const maxRetries = 5
	for range maxRetries {
		var items []string
		if err := kv.client.KV.Get(key, &items); err != nil {
			return errors.Wrap(err, "failed to read list for CAS")
		}

		newItems, changed := modify(items)
		if !changed {
			return nil
		}

		var saved bool
		var err error
		if items == nil {
			saved, err = kv.client.KV.Set(key, newItems, pluginapi.SetAtomic(nil))
		} else {
			saved, err = kv.client.KV.Set(key, newItems, pluginapi.SetAtomic(items))
		}
		if err != nil {
			return errors.Wrap(err, "failed to CAS list")
		}
		if saved {
			return nil
		}
	}
	return errors.New("failed to modify list after max retries")
}

// casModifyConnectionList atomically modifies a TeamConnection list stored in KV.
func (kv Client) casModifyConnectionList(key string, modify func([]TeamConnection) ([]TeamConnection, bool)) error {
	const maxRetries = 5
	for range maxRetries {
		var items []TeamConnection
		if err := kv.client.KV.Get(key, &items); err != nil {
			return errors.Wrap(err, "failed to read connection list for CAS")
		}

		newItems, changed := modify(items)
		if !changed {
			return nil
		}

		var saved bool
		var err error
		if items == nil {
			saved, err = kv.client.KV.Set(key, newItems, pluginapi.SetAtomic(nil))
		} else {
			saved, err = kv.client.KV.Set(key, newItems, pluginapi.SetAtomic(items))
		}
		if err != nil {
			return errors.Wrap(err, "failed to CAS connection list")
		}
		if saved {
			return nil
		}
	}
	return errors.New("failed to modify connection list after max retries")
}
