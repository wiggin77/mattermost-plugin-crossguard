package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

type testKVStore struct {
	store.KVStore
	postMappings  map[string]string
	deletingFlags map[string]bool
}

func newTestKVStore() *testKVStore {
	return &testKVStore{
		postMappings:  make(map[string]string),
		deletingFlags: make(map[string]bool),
	}
}

func (s *testKVStore) GetTeamConnections(string) ([]store.TeamConnection, error) {
	return []store.TeamConnection{
		{Direction: "inbound", Connection: "high"},
		{Direction: "outbound", Connection: "high"},
	}, nil
}
func (s *testKVStore) SetTeamConnections(string, []store.TeamConnection) error { return nil }
func (s *testKVStore) DeleteTeamConnections(string) error                      { return nil }
func (s *testKVStore) IsTeamInitialized(string) (bool, error)                  { return true, nil }
func (s *testKVStore) AddTeamConnection(string, store.TeamConnection) error    { return nil }
func (s *testKVStore) RemoveTeamConnection(string, store.TeamConnection) error { return nil }
func (s *testKVStore) GetInitializedTeamIDs() ([]string, error)                { return nil, nil }
func (s *testKVStore) AddInitializedTeamID(string) error                       { return nil }
func (s *testKVStore) RemoveInitializedTeamID(string) error                    { return nil }
func (s *testKVStore) GetChannelConnections(string) ([]store.TeamConnection, error) {
	return []store.TeamConnection{
		{Direction: "inbound", Connection: "high"},
		{Direction: "outbound", Connection: "high"},
	}, nil
}
func (s *testKVStore) SetChannelConnections(string, []store.TeamConnection) error { return nil }
func (s *testKVStore) DeleteChannelConnections(string) error                      { return nil }
func (s *testKVStore) IsChannelInitialized(string) (bool, error)                  { return true, nil }
func (s *testKVStore) AddChannelConnection(string, store.TeamConnection) error    { return nil }
func (s *testKVStore) RemoveChannelConnection(string, store.TeamConnection) error { return nil }

func (s *testKVStore) SetPostMapping(connName, remotePostID, localPostID string) error {
	s.postMappings[connName+"-"+remotePostID] = localPostID
	return nil
}

func (s *testKVStore) GetPostMapping(connName, remotePostID string) (string, error) {
	return s.postMappings[connName+"-"+remotePostID], nil
}

func (s *testKVStore) DeletePostMapping(connName, remotePostID string) error {
	delete(s.postMappings, connName+"-"+remotePostID)
	return nil
}

func (s *testKVStore) SetDeletingFlag(postID string) error {
	s.deletingFlags[postID] = true
	return nil
}

func (s *testKVStore) IsDeletingFlagSet(postID string) (bool, error) {
	return s.deletingFlags[postID], nil
}

func (s *testKVStore) ClearDeletingFlag(postID string) error {
	delete(s.deletingFlags, postID)
	return nil
}

func (s *testKVStore) GetConnectionPrompt(string, string) (*store.ConnectionPrompt, error) {
	return nil, nil
}

func (s *testKVStore) SetConnectionPrompt(string, string, *store.ConnectionPrompt) error {
	return nil
}

func (s *testKVStore) DeleteConnectionPrompt(string, string) error {
	return nil
}

func (s *testKVStore) CreateConnectionPrompt(string, string, *store.ConnectionPrompt) (bool, error) {
	return true, nil
}

func (s *testKVStore) GetChannelConnectionPrompt(string, string) (*store.ConnectionPrompt, error) {
	return nil, nil
}

func (s *testKVStore) SetChannelConnectionPrompt(string, string, *store.ConnectionPrompt) error {
	return nil
}

func (s *testKVStore) DeleteChannelConnectionPrompt(string, string) error {
	return nil
}

func (s *testKVStore) CreateChannelConnectionPrompt(string, string, *store.ConnectionPrompt) (bool, error) {
	return true, nil
}

func (s *testKVStore) GetTeamRewriteIndex(string, string) (string, error) {
	return "", nil
}

func (s *testKVStore) SetTeamRewriteIndex(string, string, string) error {
	return nil
}

func (s *testKVStore) DeleteTeamRewriteIndex(string, string) error {
	return nil
}

func (s *testKVStore) GetChannelConnectionRequest(string, string) (*store.ConnectionRequest, error) {
	return nil, nil
}

func (s *testKVStore) CreateChannelConnectionRequest(string, string, *store.ConnectionRequest) (bool, error) {
	return true, nil
}

func (s *testKVStore) UpdateChannelConnectionRequest(string, string, *store.ConnectionRequest) error {
	return nil
}

func (s *testKVStore) DeleteChannelConnectionRequest(string, string) error {
	return nil
}

func setupTestPlugin(api *plugintest.API) (*Plugin, *testKVStore) {
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	kvs := newTestKVStore()
	p.kvstore = kvs
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)
	return p, kvs
}

func TestResolveTeamAndChannel(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		team := &mmModel.Team{Id: "team-id", Name: "test-a"}
		channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}

		api.On("GetTeamByName", "test-a").Return(team, nil)
		api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)

		gotTeam, gotChannel, err := p.resolveTeamAndChannel("high", "test-a", "town-square")
		require.NoError(t, err)
		assert.Equal(t, "team-id", gotTeam.Id)
		assert.Equal(t, "chan-id", gotChannel.Id)
	})

	t.Run("team not found", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		api.On("GetTeamByName", "unknown").Return(nil, &mmModel.AppError{Message: "not found"})

		_, _, err := p.resolveTeamAndChannel("high", "unknown", "town-square")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("channel not found", func(t *testing.T) {
		api := &plugintest.API{}
		p, _ := setupTestPlugin(api)

		team := &mmModel.Team{Id: "team-id", Name: "test-a"}
		api.On("GetTeamByName", "test-a").Return(team, nil)
		api.On("GetChannelByName", "team-id", "unknown-chan", false).Return(nil, &mmModel.AppError{Message: "not found"})

		_, _, err := p.resolveTeamAndChannel("high", "test-a", "unknown-chan")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("connection not linked to team", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{}
		p.SetAPI(api)
		p.botUserID = "bot-user-id"
		kvs := newTestKVStore()
		kvs.KVStore = nil
		p.kvstore = &unlinkTestKVStore{testKVStore: kvs, conns: []store.TeamConnection{
			{Direction: "inbound", Connection: "other"},
		}}
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)

		team := &mmModel.Team{Id: "team-id", Name: "test-a", DisplayName: "Test A"}
		channel := &mmModel.Channel{Id: "ts-id", Name: "town-square", TeamId: "team-id"}
		api.On("GetTeamByName", "test-a").Return(team, nil)
		api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
		api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{Id: "prompt-post-id"}, nil)
		registerLogMocks(api, "LogError")

		_, _, err := p.resolveTeamAndChannel("high", "test-a", "town-square")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not linked")
	})

	t.Run("rewrite when team exists but connection not linked", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{}
		p.SetAPI(api)
		p.botUserID = "bot-user-id"
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)

		destTeam := &mmModel.Team{Id: "dest-team-id", Name: "test"}
		channel := &mmModel.Channel{Id: "dest-chan-id", Name: "local-loopback", TeamId: "dest-team-id"}

		api.On("GetTeam", "dest-team-id").Return(destTeam, nil)
		api.On("GetChannelByName", "dest-team-id", "local-loopback", false).Return(channel, nil)

		kvs := &rewriteTestKVStore{
			testKVStore: newTestKVStore(),
			teamConns: map[string][]store.TeamConnection{
				"src-team-id":  {{Direction: "outbound", Connection: "loopback"}},
				"dest-team-id": {{Direction: "inbound", Connection: "loopback"}},
			},
			chanConns: map[string][]store.TeamConnection{
				"dest-chan-id": {{Direction: "inbound", Connection: "loopback"}},
			},
			rewriteIndex: map[string]string{
				"loopback::loop": "dest-team-id",
			},
		}
		p.kvstore = kvs

		gotTeam, gotChannel, err := p.resolveTeamAndChannel("loopback", "loop", "local-loopback")
		require.NoError(t, err)
		assert.Equal(t, "dest-team-id", gotTeam.Id)
		assert.Equal(t, "dest-chan-id", gotChannel.Id)
	})

	t.Run("rewrite index error", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{}
		p.SetAPI(api)
		p.botUserID = "bot-user-id"
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)

		kvs := &rewriteTestKVStore{
			testKVStore: newTestKVStore(),
			indexErr:    errors.New("store unavailable"),
		}
		p.kvstore = kvs

		_, _, err := p.resolveTeamAndChannel("loopback", "loop", "local-loopback")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rewrite index lookup")
		assert.Contains(t, err.Error(), "store unavailable")
	})

	t.Run("rewrite target team not found", func(t *testing.T) {
		api := &plugintest.API{}
		p := &Plugin{}
		p.SetAPI(api)
		p.botUserID = "bot-user-id"
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)

		api.On("GetTeam", "deleted-team-id").Return(nil, &mmModel.AppError{Message: "team not found"})

		kvs := &rewriteTestKVStore{
			testKVStore: newTestKVStore(),
			rewriteIndex: map[string]string{
				"loopback::loop": "deleted-team-id",
			},
		}
		p.kvstore = kvs

		_, _, err := p.resolveTeamAndChannel("loopback", "loop", "local-loopback")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rewrite target team")
	})
}

type rewriteTestKVStore struct {
	*testKVStore
	teamConns    map[string][]store.TeamConnection
	chanConns    map[string][]store.TeamConnection
	rewriteIndex map[string]string
	indexErr     error
}

func (s *rewriteTestKVStore) GetTeamConnections(teamID string) ([]store.TeamConnection, error) {
	if conns, ok := s.teamConns[teamID]; ok {
		return conns, nil
	}
	return nil, nil
}

func (s *rewriteTestKVStore) GetChannelConnections(chanID string) ([]store.TeamConnection, error) {
	if conns, ok := s.chanConns[chanID]; ok {
		return conns, nil
	}
	return nil, nil
}

func (s *rewriteTestKVStore) GetTeamRewriteIndex(connName, remoteTeamName string) (string, error) {
	if s.indexErr != nil {
		return "", s.indexErr
	}
	return s.rewriteIndex[connName+"::"+remoteTeamName], nil
}

type unlinkTestKVStore struct {
	*testKVStore
	conns []store.TeamConnection
}

func (s *unlinkTestKVStore) GetTeamConnections(string) ([]store.TeamConnection, error) {
	return s.conns, nil
}

func TestHandleInboundPost(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("CreatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.UserId == "sync-uid" &&
			post.ChannelId == "chan-id" &&
			post.Message == "hello from remote" &&
			post.GetProp("crossguard_relayed") == true
	})).Return(&mmModel.Post{Id: "local-post-id"}, nil)

	postMsg := model.PostMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		MessageText: "hello from remote",
	}

	p.handleInboundPost("high", &postMsg, true)

	assert.Equal(t, "local-post-id", kvs.postMappings["high-remote-post-id"])
	api.AssertExpectations(t)
}

func TestHandleInboundPost_ResolveFailed(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	notFoundErr := &mmModel.AppError{Message: "team not found"}
	api.On("GetTeamByName", "missing-team").Return(nil, notFoundErr)
	api.On("LogWarn", "Inbound post: resolve failed", "error_code", mock.Anything,
		"conn", "high", "error", mock.Anything).Return()

	postMsg := model.PostMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "missing-team",
		Username:    "alice",
		MessageText: "hello",
	}

	missing := p.handleInboundPost("high", &postMsg, false)
	assert.False(t, missing)
	api.AssertExpectations(t)
}

func TestHandleInboundUpdate(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	existingPost := &mmModel.Post{
		Id:      "local-post-id",
		Message: "original",
	}
	existingPost.AddProp("crossguard_relayed", true)

	api.On("GetPost", "local-post-id").Return(existingPost, nil)
	api.On("UpdatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.Id == "local-post-id" && post.Message == "edited message"
	})).Return(&mmModel.Post{Id: "local-post-id"}, nil)

	postMsg := model.PostMessage{
		PostID:      "remote-post-id",
		MessageText: "edited message",
	}

	p.handleInboundUpdate("high", &postMsg)
	api.AssertExpectations(t)
}

func TestHandleInboundUpdate_NoMapping(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogWarn", "Inbound update: no post mapping found", "error_code", mock.Anything, "conn", "high", "remote_id", "unknown-id").Return()

	postMsg := model.PostMessage{PostID: "unknown-id", MessageText: "edited"}

	p.handleInboundUpdate("high", &postMsg)
}

func TestHandleInboundDelete(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	api.On("DeletePost", "local-post-id").Return(nil)

	deleteMsg := model.DeleteMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
	}
	p.handleInboundDelete("high", &deleteMsg)

	_, exists := kvs.postMappings["high-remote-post-id"]
	assert.False(t, exists, "post mapping should be deleted")

	assert.False(t, kvs.deletingFlags["local-post-id"], "delete flag should be cleaned up")

	api.AssertExpectations(t)
}

func TestHandleInboundReaction_Add(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("AddReaction", mock.MatchedBy(func(r *mmModel.Reaction) bool {
		return r.UserId == "sync-uid" && r.PostId == "local-post-id" && r.EmojiName == "thumbsup"
	})).Return(&mmModel.Reaction{}, nil)

	reactionMsg := model.ReactionMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		EmojiName:   "thumbsup",
	}
	p.handleInboundReaction("high", &reactionMsg, true)
	api.AssertExpectations(t)
}

func TestHandleInboundReaction_Remove(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("RemoveReaction", mock.MatchedBy(func(r *mmModel.Reaction) bool {
		return r.UserId == "sync-uid" && r.PostId == "local-post-id" && r.EmojiName == "thumbsup"
	})).Return(nil)

	reactionMsg := model.ReactionMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		EmojiName:   "thumbsup",
	}
	p.handleInboundReaction("high", &reactionMsg, false)
	api.AssertExpectations(t)
}

func TestHandleInboundPost_WithThread(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-root-id"] = "local-root-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("CreatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.RootId == "local-root-id" && post.Message == "reply"
	})).Return(&mmModel.Post{Id: "local-reply-id"}, nil)

	postMsg := model.PostMessage{
		PostID:      "remote-reply-id",
		RootID:      "remote-root-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		MessageText: "reply",
	}

	p.handleInboundPost("high", &postMsg, true)

	assert.Equal(t, "local-reply-id", kvs.postMappings["high-remote-reply-id"])
	api.AssertExpectations(t)
}

// setPostMappingFailStore overrides SetPostMapping to return an error.
type setPostMappingFailStore struct {
	*testKVStore
	setPostMappingErr error
}

func (s *setPostMappingFailStore) SetPostMapping(_, _, _ string) error {
	return s.setPostMappingErr
}

// getPostMappingFailStore overrides GetPostMapping to return an error.
type getPostMappingFailStore struct {
	*testKVStore
	getPostMappingErr error
}

func (s *getPostMappingFailStore) GetPostMapping(_, _ string) (string, error) {
	return "", s.getPostMappingErr
}

// setDeletingFlagFailStore overrides SetDeletingFlag to return an error.
type setDeletingFlagFailStore struct {
	*testKVStore
	setDeletingFlagErr error
}

func (s *setDeletingFlagFailStore) SetDeletingFlag(_ string) error {
	return s.setDeletingFlagErr
}

func TestHandleInboundPost_DuplicatePost(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)

	// Pre-populate the mapping so the idempotency check finds it.
	kvs.postMappings["high-remote-post-id"] = "existing-local-id"

	postMsg := model.PostMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		MessageText: "hello from remote",
	}

	p.handleInboundPost("high", &postMsg, true)

	api.AssertNotCalled(t, "CreatePost", mock.Anything)
}

func TestHandleInboundPost_SetPostMappingFailure(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)

	kvs := &setPostMappingFailStore{
		testKVStore:       newTestKVStore(),
		setPostMappingErr: errors.New("kv write failed"),
	}
	p.kvstore = kvs

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(&mmModel.Post{Id: "local-post-id"}, nil)
	api.On("LogError", "Inbound post: failed to store post mapping", "error_code", mock.Anything,
		"conn", "high", "remote_id", "remote-post-id", "local_id", "local-post-id", "error", "kv write failed").Return()

	postMsg := model.PostMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		MessageText: "hello from remote",
	}

	p.handleInboundPost("high", &postMsg, true)

	api.AssertCalled(t, "CreatePost", mock.AnythingOfType("*model.Post"))
	api.AssertExpectations(t)
}

func TestHandleInboundPost_CreatePostFailure(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("CreatePost", mock.AnythingOfType("*model.Post")).Return(nil, &mmModel.AppError{Message: "create failed"})
	api.On("LogError", "Inbound post: create failed", "error_code", mock.Anything, "conn", "high", "error", "create failed").Return()

	postMsg := model.PostMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		MessageText: "hello from remote",
	}

	p.handleInboundPost("high", &postMsg, true)

	// SetPostMapping should not be called because CreatePost failed.
	_, exists := kvs.postMappings["high-remote-post-id"]
	assert.False(t, exists, "post mapping should not exist after CreatePost failure")
	api.AssertExpectations(t)
}

func TestHandleInboundUpdate_GetPostMappingError(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)

	kvs := &getPostMappingFailStore{
		testKVStore:       newTestKVStore(),
		getPostMappingErr: errors.New("kv read failed"),
	}
	p.kvstore = kvs

	api.On("LogError", "Inbound update: failed to look up post mapping", "error_code", mock.Anything,
		"conn", "high", "remote_id", "remote-post-id", "error", "kv read failed").Return()

	postMsg := model.PostMessage{PostID: "remote-post-id", MessageText: "edited"}

	p.handleInboundUpdate("high", &postMsg)

	api.AssertNotCalled(t, "GetPost", mock.Anything)
	api.AssertNotCalled(t, "UpdatePost", mock.Anything)
	api.AssertExpectations(t)
}

func TestHandleInboundUpdate_GetPostFailure(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	api.On("GetPost", "local-post-id").Return(nil, &mmModel.AppError{Message: "post not found"})
	api.On("LogError", "Inbound update: failed to get local post", "error_code", mock.Anything,
		"conn", "high", "local_id", "local-post-id", "error", "post not found").Return()

	postMsg := model.PostMessage{PostID: "remote-post-id", MessageText: "edited"}

	p.handleInboundUpdate("high", &postMsg)

	api.AssertNotCalled(t, "UpdatePost", mock.Anything)
	api.AssertExpectations(t)
}

func TestHandleInboundDelete_NoMapping(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	deleteMsg := model.DeleteMessage{
		PostID:      "nonexistent-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
	}

	// Missing mapping returns missing=true so the caller can enqueue for retry.
	assert.True(t, p.handleInboundDelete("high", &deleteMsg))

	api.AssertNotCalled(t, "DeletePost", mock.Anything)
	api.AssertExpectations(t)
}

func TestHandleInboundDelete_SetDeletingFlagFailure(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)

	kvs := &setDeletingFlagFailStore{
		testKVStore:        newTestKVStore(),
		setDeletingFlagErr: errors.New("flag write failed"),
	}
	kvs.postMappings["high-remote-post-id"] = "local-post-id"
	p.kvstore = kvs

	api.On("LogError", "Inbound delete: failed to set delete flag", "error_code", mock.Anything,
		"conn", "high", "local_id", "local-post-id", "error", "flag write failed").Return()
	api.On("DeletePost", "local-post-id").Return(nil)

	deleteMsg := model.DeleteMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
	}

	p.handleInboundDelete("high", &deleteMsg)

	api.AssertCalled(t, "DeletePost", "local-post-id")
	api.AssertExpectations(t)
}

func TestHandleInboundDelete_DeletePostFailure(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	api.On("DeletePost", "local-post-id").Return(&mmModel.AppError{Message: "delete failed"})
	api.On("LogError", "Inbound delete: failed to delete post", "error_code", mock.Anything,
		"conn", "high", "local_id", "local-post-id", "error", "delete failed").Return()

	deleteMsg := model.DeleteMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
	}

	p.handleInboundDelete("high", &deleteMsg)

	// ClearDeletingFlag should still be called (cleanup continues).
	assert.False(t, kvs.deletingFlags["local-post-id"], "delete flag should be cleared even after DeletePost failure")
	api.AssertExpectations(t)
}

func TestHandleInboundReaction_NoMapping(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	reactionMsg := model.ReactionMessage{
		PostID:      "nonexistent-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		EmojiName:   "thumbsup",
	}

	// Missing mapping returns missing=true so the caller can enqueue for retry.
	assert.True(t, p.handleInboundReaction("high", &reactionMsg, true))

	api.AssertNotCalled(t, "AddReaction", mock.Anything)
	api.AssertNotCalled(t, "RemoveReaction", mock.Anything)
	api.AssertExpectations(t)
}

func TestHandleInboundMessage_UnknownType(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogWarn", "Unknown inbound message type", "error_code", mock.Anything, "conn", "high", "type", "unknown_type").Return()

	env := &model.Envelope{Type: "unknown_type"}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	api.AssertExpectations(t)
}

func TestHandleInboundMessage_SemaphoreFull(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	// Create a semaphore of size 1 and fill it.
	p.relaySem = make(chan struct{}, 1)
	p.relaySem <- struct{}{}

	api.On("LogWarn", "Relay semaphore full, dropping inbound message", "error_code", mock.Anything, "conn", "high").Return()

	env := &model.Envelope{Type: model.MessageTypeTest}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	api.AssertExpectations(t)
}

func TestHandleInboundMessage_TestMessageType(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogInfo", "Received inbound test message", "error_code", mock.Anything, "conn", "high", "id", "test-123").Return()

	env := &model.Envelope{
		Type:        model.MessageTypeTest,
		TestMessage: &model.TestMessage{ID: "test-123"},
	}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	api.AssertExpectations(t)
}

func TestGetInboundConn_Found(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	p.inboundConns = []inboundConn{
		{name: "low", provider: &mockQueueProvider{}},
		{name: "high", provider: &mockQueueProvider{}},
	}

	got := p.getInboundConn("high")
	require.NotNil(t, got)
	assert.Equal(t, "high", got.name)
}

func TestGetInboundConn_NotFound(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	p.inboundConns = []inboundConn{
		{name: "low", provider: &mockQueueProvider{}},
		{name: "high", provider: &mockQueueProvider{}},
	}

	got := p.getInboundConn("nonexistent")
	assert.Nil(t, got)
}

func TestGetInboundConn_EmptyPool(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	got := p.getInboundConn("high")
	assert.Nil(t, got)
}

func TestHandleInboundMessage_PostMissingPayload(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogError", "Inbound post: missing payload", "error_code", mock.Anything, "conn", "high").Return()

	env := &model.Envelope{Type: model.MessageTypePost}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	api.AssertExpectations(t)
}

func TestFindTeamByRewrite_NoMapping(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	team, err := p.findTeamByRewrite("high", "remote-team")
	require.NoError(t, err)
	assert.Nil(t, team)
}

func TestFindTeamByRewrite_MappingExists(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)

	expectedTeam := &mmModel.Team{Id: "local-team-id", Name: "local-team"}
	api.On("GetTeam", "local-team-id").Return(expectedTeam, nil)

	kvs := &flexibleKVStore{testKVStore: newTestKVStore()}
	kvs.getTeamRewriteIndexFn = func(connName, remoteTeamName string) (string, error) {
		if connName == "high" && remoteTeamName == "remote-team" {
			return "local-team-id", nil
		}
		return "", nil
	}
	p.kvstore = kvs

	team, err := p.findTeamByRewrite("high", "remote-team")
	require.NoError(t, err)
	require.NotNil(t, team)
	assert.Equal(t, "local-team-id", team.Id)
	api.AssertExpectations(t)
}

func TestFindTeamByRewrite_GetTeamError(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)

	api.On("GetTeam", "bad-team-id").Return(nil, &mmModel.AppError{Message: "team not found"})

	kvs := &flexibleKVStore{testKVStore: newTestKVStore()}
	kvs.getTeamRewriteIndexFn = func(connName, remoteTeamName string) (string, error) {
		return "bad-team-id", nil
	}
	p.kvstore = kvs

	team, err := p.findTeamByRewrite("high", "remote-team")
	require.Error(t, err)
	assert.Nil(t, team)
	assert.Contains(t, err.Error(), "rewrite target team")
	assert.Contains(t, err.Error(), "not found")
	api.AssertExpectations(t)
}

func TestHandleInboundFile_MissingHeaders(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)
	p.fileSem = make(chan struct{}, 32)

	api.On("LogWarn", "Inbound file: missing required headers, skipping", "error_code", mock.Anything,
		"key", "some-key", headerConnName, "", headerPostID, "", headerFilename, "").Return()

	err := p.handleInboundFile(p.ctx, "high", "some-key", []byte("data"), map[string]string{})
	require.NoError(t, err)

	api.AssertExpectations(t)
}

func TestHandleInboundFile_ConnectionMismatch(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)
	p.fileSem = make(chan struct{}, 32)

	headers := map[string]string{
		headerConnName: "other-conn",
		headerPostID:   "remote-post-id",
		headerFilename: "file.txt",
	}

	err := p.handleInboundFile(p.ctx, "high", "some-key", []byte("data"), headers)
	require.NoError(t, err)

	// Should return nil without logging anything (silent skip for mismatched conn).
	api.AssertNotCalled(t, "LogWarn", mock.Anything)
	api.AssertNotCalled(t, "LogError", mock.Anything)
}

func TestHandleInboundFile_ConnectionNotActive(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)
	p.fileSem = make(chan struct{}, 32)

	headers := map[string]string{
		headerConnName: "high",
		headerPostID:   "remote-post-id",
		headerFilename: "file.txt",
	}

	api.On("LogWarn", "Inbound file: connection no longer active, skipping", "error_code", mock.Anything,
		"conn", "high", "filename", "file.txt").Return()

	err := p.handleInboundFile(p.ctx, "high", "some-key", []byte("data"), headers)
	require.NoError(t, err)

	api.AssertExpectations(t)
}

func TestConnectInbound(t *testing.T) {
	t.Run("config parse error logs and returns empty pool", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPlugin(api)
		p.fileSem = make(chan struct{}, 32)

		p.configuration = &configuration{
			InboundConnections: "not valid json",
		}

		p.connectInbound()

		p.inboundMu.RLock()
		defer p.inboundMu.RUnlock()
		assert.Nil(t, p.inboundConns)
	})

	t.Run("provider creation failure skips connection", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPlugin(api)
		p.fileSem = make(chan struct{}, 32)

		p.configuration = &configuration{
			InboundConnections: `[{"name":"bad","provider":"unknown"}]`,
		}

		p.connectInbound()

		p.inboundMu.RLock()
		defer p.inboundMu.RUnlock()
		assert.Nil(t, p.inboundConns)
	})
}

func TestCloseInbound(t *testing.T) {
	t.Run("closes all providers and nils pool", func(t *testing.T) {
		p := &Plugin{}
		ctx, cancel := context.WithCancel(context.Background())
		p.ctx = ctx
		p.cancel = cancel
		p.relaySem = make(chan struct{}, 50)
		p.fileSem = make(chan struct{}, 32)
		p.inboundCancel = func() {}

		closed := make([]string, 0)
		p.inboundConns = []inboundConn{
			{
				provider: &mockQueueProvider{closeFn: func() error {
					closed = append(closed, "conn-a")
					return nil
				}},
				name: "conn-a",
			},
			{
				provider: &mockQueueProvider{closeFn: func() error {
					closed = append(closed, "conn-b")
					return nil
				}},
				name: "conn-b",
			},
		}

		p.closeInbound()

		assert.Nil(t, p.inboundConns)
		assert.ElementsMatch(t, []string{"conn-a", "conn-b"}, closed)
	})

	t.Run("nil inboundCancel does not panic", func(t *testing.T) {
		p := &Plugin{}
		p.inboundCancel = nil

		assert.NotPanics(t, func() {
			p.closeInbound()
		})
	})
}

func TestReconnectInbound(t *testing.T) {
	t.Run("closes old providers and rebuilds pool", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPlugin(api)
		p.fileSem = make(chan struct{}, 32)

		oldClosed := false
		p.inboundCancel = func() {}
		p.inboundConns = []inboundConn{
			{
				provider: &mockQueueProvider{closeFn: func() error {
					oldClosed = true
					return nil
				}},
				name: "old-conn",
			},
		}

		p.configuration = &configuration{
			InboundConnections: "[]",
		}

		p.reconnectInbound()

		assert.True(t, oldClosed)
		p.inboundMu.RLock()
		defer p.inboundMu.RUnlock()
		assert.Nil(t, p.inboundConns)
	})
}

func TestHandleInboundFile_MissingHeaders2(t *testing.T) {
	t.Run("empty required headers logs warning and skips", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPlugin(api)
		p.fileSem = make(chan struct{}, 32)

		err := p.handleInboundFile(p.ctx, "my-conn", "blob-key", []byte("data"), map[string]string{})
		assert.NoError(t, err)
	})

	t.Run("mismatched connection name skips silently", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)
		p, _ := setupTestPlugin(api)
		p.fileSem = make(chan struct{}, 32)

		headers := map[string]string{
			headerConnName: "other-conn",
			headerPostID:   "remote-post-1",
			headerFilename: "file.pdf",
		}
		err := p.handleInboundFile(p.ctx, "my-conn", "blob-key", []byte("data"), headers)
		assert.NoError(t, err)
	})
}

func TestHandleInboundFile_HappyPath(t *testing.T) {
	api := &plugintest.API{}
	mockLog(api)
	p, kvs := setupTestPlugin(api)
	p.fileSem = make(chan struct{}, 32)

	p.inboundConns = []inboundConn{
		{
			provider:            &mockQueueProvider{},
			name:                "my-conn",
			fileTransferEnabled: true,
		},
	}

	kvs.postMappings["my-conn-remote-post-1"] = "local-post-1"

	existingPost := &mmModel.Post{
		Id:        "local-post-1",
		ChannelId: "ch1",
		FileIds:   mmModel.StringArray{},
	}
	api.On("GetPost", "local-post-1").Return(existingPost, nil)
	api.On("UploadFile", []byte("file-data"), "ch1", "report.pdf").Return(&mmModel.FileInfo{
		Id:   "uploaded-file-1",
		Name: "report.pdf",
	}, nil)
	api.On("UpdatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.Id == "local-post-1"
	})).Return(existingPost, nil)

	headers := map[string]string{
		headerConnName: "my-conn",
		headerPostID:   "remote-post-1",
		headerFilename: "report.pdf",
	}
	err := p.handleInboundFile(p.ctx, "my-conn", "blob-key", []byte("file-data"), headers)
	assert.NoError(t, err)

	api.AssertCalled(t, "UploadFile", []byte("file-data"), "ch1", "report.pdf")
	api.AssertCalled(t, "UpdatePost", mock.Anything)
}

// ---------------------------------------------------------------------------
// Additional inbound edge-case tests (new)
// ---------------------------------------------------------------------------

func TestHandleInboundPost_WithRootID_MappingExists(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	// Pre-populate root mapping.
	kvs.postMappings["high-remote-root-id"] = "local-root-id"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("CreatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.RootId == "local-root-id" && post.Message == "threaded reply"
	})).Return(&mmModel.Post{Id: "local-reply-id"}, nil)

	postMsg := model.PostMessage{
		PostID:      "remote-reply-id",
		RootID:      "remote-root-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		MessageText: "threaded reply",
	}

	p.handleInboundPost("high", &postMsg, true)

	assert.Equal(t, "local-reply-id", kvs.postMappings["high-remote-reply-id"])
	api.AssertExpectations(t)
}

func TestHandleInboundPost_WithRootID_MappingMissing(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	// No root mapping exists.
	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-uid", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-uid").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-uid").Return(&mmModel.ChannelMember{}, nil)
	api.On("LogWarn", "Inbound post: failed to look up root mapping", "error_code", mock.Anything,
		"conn", "high", "remote_root_id", "nonexistent-root", "error", mock.Anything).Maybe()
	api.On("LogWarn", "Inbound post: root not found after retries, creating standalone", "error_code", mock.Anything,
		"conn", "high", "remote_root_id", "nonexistent-root").Return()
	api.On("CreatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.RootId == "" && post.Message == "orphaned reply"
	})).Return(&mmModel.Post{Id: "local-post-id"}, nil)

	postMsg := model.PostMessage{
		PostID:      "remote-post-id",
		RootID:      "nonexistent-root",
		ChannelName: "town-square",
		TeamName:    "test-a",
		Username:    "alice",
		MessageText: "orphaned reply",
	}

	p.handleInboundPost("high", &postMsg, true)

	assert.Equal(t, "local-post-id", kvs.postMappings["high-remote-post-id"])
}

func TestHandleInboundUpdate_UpdatePostFails(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	existingPost := &mmModel.Post{Id: "local-post-id", Message: "original"}
	api.On("GetPost", "local-post-id").Return(existingPost, nil)
	api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(nil, &mmModel.AppError{Message: "update failed"})
	api.On("LogError", "Inbound update: failed to update post", "error_code", mock.Anything,
		"conn", "high", "local_id", "local-post-id", "error", "update failed").Return()

	postMsg := model.PostMessage{PostID: "remote-post-id", MessageText: "edited"}
	p.handleInboundUpdate("high", &postMsg)

	api.AssertExpectations(t)
}

func TestHandleInboundDelete_FullLifecycle(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)

	kvs.postMappings["high-remote-post-id"] = "local-post-id"

	api.On("DeletePost", "local-post-id").Return(nil)

	deleteMsg := model.DeleteMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
	}
	p.handleInboundDelete("high", &deleteMsg)

	// Verify deleting flag was set and then cleared.
	assert.False(t, kvs.deletingFlags["local-post-id"], "delete flag should be cleared after delete")
	// Verify post mapping was removed.
	_, exists := kvs.postMappings["high-remote-post-id"]
	assert.False(t, exists, "post mapping should be deleted")

	api.AssertExpectations(t)
}

// clearDeletingFlagFailStore overrides ClearDeletingFlag to return an error.
type clearDeletingFlagFailStore struct {
	*testKVStore
	clearErr error
}

func (s *clearDeletingFlagFailStore) ClearDeletingFlag(_ string) error {
	return s.clearErr
}

func TestHandleInboundDelete_ClearFlagFails(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)

	kvs := &clearDeletingFlagFailStore{
		testKVStore: newTestKVStore(),
		clearErr:    errors.New("clear flag failed"),
	}
	kvs.postMappings["high-remote-post-id"] = "local-post-id"
	p.kvstore = kvs

	api.On("DeletePost", "local-post-id").Return(nil)
	api.On("LogWarn", "Inbound delete: failed to remove delete flag", "error_code", mock.Anything,
		"error_code", mock.Anything,
		"conn", "high", "local_id", "local-post-id", "error", "clear flag failed").Return()
	registerLogMocks(api, "LogWarn")

	deleteMsg := model.DeleteMessage{
		PostID:      "remote-post-id",
		ChannelName: "town-square",
		TeamName:    "test-a",
	}

	// Should not panic even though ClearDeletingFlag fails.
	assert.NotPanics(t, func() {
		p.handleInboundDelete("high", &deleteMsg)
	})
	api.AssertCalled(t, "DeletePost", "local-post-id")
}

func TestHandleInboundFile_PostMappingRetry(t *testing.T) {
	api := &plugintest.API{}
	mockLog(api)
	p, _ := setupTestPlugin(api)
	p.fileSem = make(chan struct{}, 32)

	p.inboundConns = []inboundConn{
		{
			provider:            &mockQueueProvider{},
			name:                "high",
			fileTransferEnabled: true,
		},
	}

	// GetPostMapping returns empty on first 2 calls, then returns value on 3rd.
	callCount := 0
	kvs := &flexibleKVStore{testKVStore: newTestKVStore()}
	kvs.postMappings["high-remote-post-1"] = "local-post-1"
	p.kvstore = kvs

	// Override GetPostMapping to simulate retry behavior.
	retryKvs := &retryPostMappingStore{
		testKVStore: kvs.testKVStore,
		threshold:   3,
	}
	retryKvs.callCount = &callCount
	p.kvstore = retryKvs

	existingPost := &mmModel.Post{
		Id:        "local-post-1",
		ChannelId: "ch1",
		FileIds:   mmModel.StringArray{},
	}
	api.On("GetPost", "local-post-1").Return(existingPost, nil)
	api.On("UploadFile", []byte("file-data"), "ch1", "doc.pdf").Return(&mmModel.FileInfo{
		Id:   "uploaded-file-1",
		Name: "doc.pdf",
	}, nil)
	api.On("UpdatePost", mock.MatchedBy(func(post *mmModel.Post) bool {
		return post.Id == "local-post-1"
	})).Return(existingPost, nil)

	headers := map[string]string{
		headerConnName: "high",
		headerPostID:   "remote-post-1",
		headerFilename: "doc.pdf",
	}
	err := p.handleInboundFile(p.ctx, "high", "blob-key", []byte("file-data"), headers)
	assert.NoError(t, err)
	assert.Equal(t, 3, *retryKvs.callCount)
}

// retryPostMappingStore returns empty mapping until callCount reaches threshold.
type retryPostMappingStore struct {
	*testKVStore
	threshold int
	callCount *int
}

func (s *retryPostMappingStore) GetPostMapping(connName, remotePostID string) (string, error) {
	*s.callCount++
	if *s.callCount < s.threshold {
		return "", nil
	}
	return s.testKVStore.GetPostMapping(connName, remotePostID)
}

func TestHandleInboundFile_ContextCancelledDuringRetry(t *testing.T) {
	api := &plugintest.API{}
	mockLog(api)
	p, _ := setupTestPlugin(api)
	p.fileSem = make(chan struct{}, 32)

	p.inboundConns = []inboundConn{
		{
			provider:            &mockQueueProvider{},
			name:                "high",
			fileTransferEnabled: true,
		},
	}

	// Create a context that we cancel immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// GetPostMapping always returns empty to force retry, but context is cancelled.
	kvs := &flexibleKVStore{testKVStore: newTestKVStore()}
	p.kvstore = kvs

	headers := map[string]string{
		headerConnName: "high",
		headerPostID:   "remote-post-1",
		headerFilename: "doc.pdf",
	}
	err := p.handleInboundFile(ctx, "high", "blob-key", []byte("file-data"), headers)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestHandleInboundFile_UploadFails(t *testing.T) {
	api := &plugintest.API{}
	mockLog(api)
	p, kvs := setupTestPlugin(api)
	p.fileSem = make(chan struct{}, 32)

	p.inboundConns = []inboundConn{
		{
			provider:            &mockQueueProvider{},
			name:                "high",
			fileTransferEnabled: true,
		},
	}

	kvs.postMappings["high-remote-post-1"] = "local-post-1"

	existingPost := &mmModel.Post{
		Id:        "local-post-1",
		ChannelId: "ch1",
		FileIds:   mmModel.StringArray{},
	}
	api.On("GetPost", "local-post-1").Return(existingPost, nil)
	api.On("UploadFile", []byte("file-data"), "ch1", "doc.pdf").Return(nil, &mmModel.AppError{Message: "upload failed"})

	headers := map[string]string{
		headerConnName: "high",
		headerPostID:   "remote-post-1",
		headerFilename: "doc.pdf",
	}
	err := p.handleInboundFile(p.ctx, "high", "blob-key", []byte("file-data"), headers)
	assert.NoError(t, err)
	// UpdatePost should not be called when upload fails.
	api.AssertNotCalled(t, "UpdatePost", mock.Anything)
}

func TestHandleInboundFile_UpdatePostFails(t *testing.T) {
	api := &plugintest.API{}
	mockLog(api)
	p, kvs := setupTestPlugin(api)
	p.fileSem = make(chan struct{}, 32)

	p.inboundConns = []inboundConn{
		{
			provider:            &mockQueueProvider{},
			name:                "high",
			fileTransferEnabled: true,
		},
	}

	kvs.postMappings["high-remote-post-1"] = "local-post-1"

	existingPost := &mmModel.Post{
		Id:        "local-post-1",
		ChannelId: "ch1",
		FileIds:   mmModel.StringArray{},
	}
	api.On("GetPost", "local-post-1").Return(existingPost, nil)
	api.On("UploadFile", []byte("file-data"), "ch1", "doc.pdf").Return(&mmModel.FileInfo{
		Id:   "uploaded-file-1",
		Name: "doc.pdf",
	}, nil)
	api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(nil, &mmModel.AppError{Message: "update failed"})

	headers := map[string]string{
		headerConnName: "high",
		headerPostID:   "remote-post-1",
		headerFilename: "doc.pdf",
	}
	err := p.handleInboundFile(p.ctx, "high", "blob-key", []byte("file-data"), headers)
	assert.NoError(t, err)
}

func TestResolveTeamAndChannel_RewriteIndex(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)

	destTeam := &mmModel.Team{Id: "dest-team-id", Name: "local-team"}
	channel := &mmModel.Channel{Id: "dest-chan-id", Name: "town-square", TeamId: "dest-team-id"}

	api.On("GetTeam", "dest-team-id").Return(destTeam, nil)
	api.On("GetChannelByName", "dest-team-id", "town-square", false).Return(channel, nil)

	kvs := &rewriteTestKVStore{
		testKVStore: newTestKVStore(),
		teamConns: map[string][]store.TeamConnection{
			"dest-team-id": {{Direction: "inbound", Connection: "high"}},
		},
		chanConns: map[string][]store.TeamConnection{
			"dest-chan-id": {{Direction: "inbound", Connection: "high"}},
		},
		rewriteIndex: map[string]string{
			"high::remote-team": "dest-team-id",
		},
	}
	p.kvstore = kvs

	gotTeam, gotChannel, err := p.resolveTeamAndChannel("high", "remote-team", "town-square")
	require.NoError(t, err)
	assert.Equal(t, "dest-team-id", gotTeam.Id)
	assert.Equal(t, "dest-chan-id", gotChannel.Id)
}

func TestWatchFiles_Success(t *testing.T) {
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo")
	p, _ := setupTestPlugin(api)

	provider := &mockQueueProvider{
		watchFilesFn: func(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
			return nil
		},
	}

	p.watchFiles(p.ctx, "high", provider)
	// No error logged, so just LogInfo for "File watcher started"
	api.AssertNotCalled(t, "LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestWatchFiles_Error(t *testing.T) {
	api := &plugintest.API{}
	registerLogMocks(api, "LogInfo", "LogError")
	p, _ := setupTestPlugin(api)

	provider := &mockQueueProvider{
		watchFilesFn: func(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
			return errors.New("watcher failed")
		},
	}

	p.watchFiles(p.ctx, "high", provider)
	api.AssertCalled(t, "LogError", "File watcher exited with error",
		"error_code", mock.Anything,
		"conn", "high", "error", "watcher failed")
}

// ---------------------------------------------------------------------------
// Missing coverage: envelope-level missing-payload branches
// ---------------------------------------------------------------------------

func TestHandleInboundMessage_UpdateMissingPayload(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogError", "Inbound update: missing payload", "error_code", mock.Anything, "conn", "high").Return()

	env := &model.Envelope{Type: model.MessageTypeUpdate}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	api.AssertExpectations(t)
}

func TestHandleInboundMessage_DeleteMissingPayload(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogError", "Inbound delete: missing payload", "error_code", mock.Anything, "conn", "high").Return()

	env := &model.Envelope{Type: model.MessageTypeDelete}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	api.AssertExpectations(t)
}

func TestHandleInboundMessage_ReactionAddMissingPayload(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogError", "Inbound reaction add: missing payload", "error_code", mock.Anything, "conn", "high").Return()

	env := &model.Envelope{Type: model.MessageTypeReactionAdd}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	api.AssertExpectations(t)
}

func TestHandleInboundMessage_ReactionRemoveMissingPayload(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogError", "Inbound reaction remove: missing payload", "error_code", mock.Anything, "conn", "high").Return()

	env := &model.Envelope{Type: model.MessageTypeReactionRemove}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	api.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Missing coverage: context cancelled before dispatch
// ---------------------------------------------------------------------------

func TestHandleInboundMessage_ContextCancelled(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)
	p.kvstore = newTestKVStore()

	// Cancel context before sending message.
	cancel()

	env := &model.Envelope{
		Type: model.MessageTypePost,
		PostMessage: &model.PostMessage{
			PostID:      "p1",
			TeamName:    "team-a",
			ChannelName: "town-square",
			Username:    "alice",
			MessageText: "hello",
		},
	}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	// Should not have attempted to create a post because context was cancelled.
	api.AssertNotCalled(t, "CreatePost", mock.Anything)
}

// ---------------------------------------------------------------------------
// Missing coverage: test message with nil TestMessage (no ID)
// ---------------------------------------------------------------------------

func TestHandleInboundMessage_TestMessageWithoutID(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	api.On("LogInfo", "Received inbound test message", "error_code", mock.Anything, "conn", "high").Return()

	env := &model.Envelope{Type: model.MessageTypeTest}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	api.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Missing coverage: unmarshal error path
// ---------------------------------------------------------------------------

func TestHandleInboundMessage_MissingMappingQueuesRetry(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)
	p.retryQueue = newRetryQueue(0)

	// Update with a PostID that has no mapping triggers missing=true.
	api.On("LogError", "Inbound update: failed to look up post mapping",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Maybe()
	api.On("LogWarn", "Missing message: queuing for retry",
		"error_code", mock.Anything,
		"conn", "high",
		"type", model.MessageTypeUpdate,
		"remote_post_id", "unknown-post",
		"queue_size", mock.Anything).Return()

	env := &model.Envelope{
		Type: model.MessageTypeUpdate,
		PostMessage: &model.PostMessage{
			PostID:      "unknown-post",
			MessageText: "updated text",
		},
	}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	assert.Equal(t, 1, p.retryQueue.Len(), "message should be enqueued for retry")
	api.AssertExpectations(t)
}

func TestHandleInboundMessage_MissingMappingNilQueue(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)
	// retryQueue is nil (default); missing=true should not panic.

	env := &model.Envelope{
		Type: model.MessageTypeUpdate,
		PostMessage: &model.PostMessage{
			PostID:      "unknown-post",
			MessageText: "updated text",
		},
	}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	// No panic, no queue operation, no retry log.
}

func TestHandleInboundMessage_RetryQueueFull(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)
	p.retryQueue = newRetryQueue(0)

	// Fill the retry queue to capacity.
	dummyEnv := &model.Envelope{
		Type: model.MessageTypeUpdate,
		PostMessage: &model.PostMessage{
			PostID:      "filler",
			MessageText: "x",
		},
	}
	dummyData, err := model.Marshal(dummyEnv, model.FormatJSON)
	require.NoError(t, err)
	for i := range retryQueueMaxSize {
		ok := p.retryQueue.Enqueue("high", dummyData, fmt.Sprintf("filler-%d", i), model.MessageTypeUpdate)
		require.True(t, ok)
	}
	assert.Equal(t, retryQueueMaxSize, p.retryQueue.Len())

	api.On("LogError", "Missing message: queue full, dropping message",
		"error_code", mock.Anything,
		"conn", "high",
		"type", model.MessageTypeUpdate,
		"remote_post_id", "unknown-post",
		"queue_size", retryQueueMaxSize).Return()

	env := &model.Envelope{
		Type: model.MessageTypeUpdate,
		PostMessage: &model.PostMessage{
			PostID:      "unknown-post",
			MessageText: "updated text",
		},
	}
	data, err := model.Marshal(env, model.FormatJSON)
	require.NoError(t, err)

	handler := p.handleInboundMessage("high")
	err = handler(data)
	require.NoError(t, err)

	p.wg.Wait()
	assert.Equal(t, retryQueueMaxSize, p.retryQueue.Len(), "queue size unchanged, message was dropped")
	api.AssertExpectations(t)
}

func TestHandleInboundMessage_UnmarshalError(t *testing.T) {
	api := &plugintest.API{}
	p, _ := setupTestPlugin(api)

	registerLogMocks(api, "LogError")

	handler := p.handleInboundMessage("high")
	err := handler([]byte("not valid json or xml"))
	require.NoError(t, err)

	p.wg.Wait()
}

// ---------------------------------------------------------------------------
// Missing coverage: reaction add/remove API failures
// ---------------------------------------------------------------------------

func TestHandleInboundReaction_AddFails(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)
	kvs.postMappings["high-remote-post-1"] = "local-post-1"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-user-id", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-user-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-user-id").Return(&mmModel.ChannelMember{}, nil)

	api.On("AddReaction", mock.AnythingOfType("*model.Reaction")).Return(nil, &mmModel.AppError{Message: "add failed"})
	api.On("LogError", "Inbound reaction: add failed", "error_code", mock.Anything, "conn", "high", "post_id", "local-post-1", "error", "add failed").Return()

	reactionMsg := &model.ReactionMessage{
		PostID:      "remote-post-1",
		TeamName:    "test-a",
		ChannelName: "town-square",
		Username:    "alice",
		EmojiName:   "thumbsup",
	}

	p.handleInboundReaction("high", reactionMsg, true)
	api.AssertExpectations(t)
}

func TestHandleInboundReaction_RemoveFails(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)
	kvs.postMappings["high-remote-post-1"] = "local-post-1"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	syncUser := &mmModel.User{Id: "sync-user-id", Username: "alice.high", Position: syncUserPosition}
	notFoundErr := &mmModel.AppError{Message: "not found"}

	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)
	api.On("GetUserByUsername", "alice").Return(nil, notFoundErr)
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(syncUser, nil)
	api.On("CreateTeamMember", "team-id", "sync-user-id").Return(&mmModel.TeamMember{}, nil)
	api.On("AddChannelMember", "chan-id", "sync-user-id").Return(&mmModel.ChannelMember{}, nil)

	api.On("RemoveReaction", mock.AnythingOfType("*model.Reaction")).Return(&mmModel.AppError{Message: "remove failed"})
	api.On("LogError", "Inbound reaction: remove failed", "error_code", mock.Anything, "conn", "high", "post_id", "local-post-1", "error", "remove failed").Return()

	reactionMsg := &model.ReactionMessage{
		PostID:      "remote-post-1",
		TeamName:    "test-a",
		ChannelName: "town-square",
		Username:    "alice",
		EmojiName:   "thumbsup",
	}

	p.handleInboundReaction("high", reactionMsg, false)
	api.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Missing coverage: reaction resolve user failure
// ---------------------------------------------------------------------------

func TestHandleInboundReaction_ResolveUserFails(t *testing.T) {
	api := &plugintest.API{}
	p, kvs := setupTestPlugin(api)
	kvs.postMappings["high-remote-post-1"] = "local-post-1"

	team := &mmModel.Team{Id: "team-id", Name: "test-a"}
	channel := &mmModel.Channel{Id: "chan-id", Name: "town-square", TeamId: "team-id"}
	api.On("GetTeamByName", "test-a").Return(team, nil)
	api.On("GetChannelByName", "team-id", "town-square", false).Return(channel, nil)

	// Both username lookup and sync user creation fail.
	api.On("GetUserByUsername", "alice").Return(nil, &mmModel.AppError{Message: "not found"})
	api.On("LogDebug", "Username lookup did not find local user, falling back to sync user", "error_code", mock.Anything,
		"username", "alice", "conn", "high").Return()
	api.On("GetUserByUsername", "alice.high").Return(nil, &mmModel.AppError{Message: "user not found"})
	api.On("CreateUser", mock.AnythingOfType("*model.User")).Return(nil, &mmModel.AppError{Message: "create user failed"})
	api.On("LogError", "Inbound reaction: resolve user failed", "error_code", mock.Anything,
		"conn", "high", "username", "alice", "error", mock.Anything).Return()

	reactionMsg := &model.ReactionMessage{
		PostID:      "remote-post-1",
		TeamName:    "test-a",
		ChannelName: "town-square",
		Username:    "alice",
		EmojiName:   "thumbsup",
	}

	p.handleInboundReaction("high", reactionMsg, true)
	api.AssertNotCalled(t, "AddReaction", mock.Anything)
}

// ---------------------------------------------------------------------------
// Missing coverage: reaction GetPostMapping store error
// ---------------------------------------------------------------------------

func TestHandleInboundReaction_GetMappingError(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)

	kvs := &getPostMappingFailStore{
		testKVStore:       newTestKVStore(),
		getPostMappingErr: errors.New("store error"),
	}
	p.kvstore = kvs

	api.On("LogError", "Inbound reaction: failed to look up post mapping", "error_code", mock.Anything,
		"conn", "high", "remote_id", "remote-post-1", "error", "store error").Return()

	reactionMsg := &model.ReactionMessage{
		PostID:      "remote-post-1",
		TeamName:    "test-a",
		ChannelName: "town-square",
		Username:    "alice",
		EmojiName:   "thumbsup",
	}

	p.handleInboundReaction("high", reactionMsg, true)
	api.AssertNotCalled(t, "AddReaction", mock.Anything)
	api.AssertExpectations(t)
}

func TestConnectInbound_Additional(t *testing.T) {
	t.Run("config parse error", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)

		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			InboundConnections: `[{invalid json`,
		}

		p.connectInbound()

		// Should log error and return without setting any inbound connections
		p.inboundMu.RLock()
		assert.Nil(t, p.inboundConns)
		p.inboundMu.RUnlock()
	})

	t.Run("provider creation error continues", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)

		p, _ := setupTestPluginWithRouter(api)
		// Use an unknown provider type so createProvider returns an error
		p.configuration = &configuration{
			InboundConnections: `[{"name":"bad","provider":"unknown_provider"}]`,
		}

		p.connectInbound()

		p.inboundMu.RLock()
		assert.Empty(t, p.inboundConns)
		p.inboundMu.RUnlock()
	})

	t.Run("subscribe error closes provider and continues", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)

		addr := startEmbeddedNATS(t)

		p, _ := setupTestPluginWithRouter(api)
		// Use a NATS subject containing a space, which is invalid for NATS Subscribe.
		// newNATSProvider will connect successfully but Subscribe will fail.
		p.configuration = &configuration{
			InboundConnections: `[{"name":"bad-sub","provider":"nats","nats":{"address":"` + addr + `","subject":"crossguard. invalid subject"}}]`,
		}

		p.connectInbound()

		p.inboundMu.RLock()
		assert.Empty(t, p.inboundConns, "no inbound connections should be established when subscribe fails")
		p.inboundMu.RUnlock()
	})

	t.Run("happy path with file transfer enabled starts watcher", func(t *testing.T) {
		api := &plugintest.API{}
		mockLog(api)

		addr := startEmbeddedNATS(t)

		p, _ := setupTestPluginWithRouter(api)
		p.configuration = &configuration{
			InboundConnections: `[{"name":"good","provider":"nats","file_transfer_enabled":true,"nats":{"address":"` + addr + `","subject":"crossguard.good"}}]`,
		}

		p.connectInbound()

		p.inboundMu.RLock()
		require.Len(t, p.inboundConns, 1)
		assert.Equal(t, "good", p.inboundConns[0].name)
		assert.True(t, p.inboundConns[0].fileTransferEnabled)
		p.inboundMu.RUnlock()

		// Tear down cleanly: cancel context and wait for watcher goroutine.
		p.closeInbound()
	})
}
