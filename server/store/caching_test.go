package store

import (
	"fmt"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

type mockKVStore struct {
	getTeamConnectionsFn       func(string) ([]TeamConnection, error)
	setTeamConnectionsFn       func(string, []TeamConnection) error
	deleteTeamConnectionsFn    func(string) error
	isTeamInitializedFn        func(string) (bool, error)
	addTeamConnectionFn        func(string, TeamConnection) error
	removeTeamConnectionFn     func(string, TeamConnection) error
	getInitializedTeamIDsFn    func() ([]string, error)
	addInitializedTeamIDFn     func(string) error
	removeInitializedTeamIDFn  func(string) error
	getChannelConnectionsFn    func(string) ([]TeamConnection, error)
	setChannelConnectionsFn    func(string, []TeamConnection) error
	deleteChannelConnectionsFn func(string) error
	isChannelInitializedFn     func(string) (bool, error)
	addChannelConnectionFn     func(string, TeamConnection) error
	removeChannelConnectionFn  func(string, TeamConnection) error
	setPostMappingFn           func(string, string, string) error
	getPostMappingFn           func(string, string) (string, error)
	deletePostMappingFn        func(string, string) error
	setDeletingFlagFn          func(string) error
	isDeletingFlagSetFn        func(string) (bool, error)
	clearDeletingFlagFn        func(string) error
	getTeamRewriteIndexFn      func(string, string) (string, error)
	setTeamRewriteIndexFn      func(string, string, string) error
	deleteTeamRewriteIndexFn   func(string, string) error
}

func (m *mockKVStore) GetTeamConnections(teamID string) ([]TeamConnection, error) {
	if m.getTeamConnectionsFn != nil {
		return m.getTeamConnectionsFn(teamID)
	}
	return []TeamConnection{}, nil
}

func (m *mockKVStore) SetTeamConnections(teamID string, conns []TeamConnection) error {
	if m.setTeamConnectionsFn != nil {
		return m.setTeamConnectionsFn(teamID, conns)
	}
	return nil
}

func (m *mockKVStore) DeleteTeamConnections(teamID string) error {
	if m.deleteTeamConnectionsFn != nil {
		return m.deleteTeamConnectionsFn(teamID)
	}
	return nil
}

func (m *mockKVStore) IsTeamInitialized(teamID string) (bool, error) {
	if m.isTeamInitializedFn != nil {
		return m.isTeamInitializedFn(teamID)
	}
	return false, nil
}

func (m *mockKVStore) AddTeamConnection(teamID string, conn TeamConnection) error {
	if m.addTeamConnectionFn != nil {
		return m.addTeamConnectionFn(teamID, conn)
	}
	return nil
}

func (m *mockKVStore) RemoveTeamConnection(teamID string, conn TeamConnection) error {
	if m.removeTeamConnectionFn != nil {
		return m.removeTeamConnectionFn(teamID, conn)
	}
	return nil
}

func (m *mockKVStore) GetInitializedTeamIDs() ([]string, error) {
	if m.getInitializedTeamIDsFn != nil {
		return m.getInitializedTeamIDsFn()
	}
	return []string{}, nil
}

func (m *mockKVStore) AddInitializedTeamID(teamID string) error {
	if m.addInitializedTeamIDFn != nil {
		return m.addInitializedTeamIDFn(teamID)
	}
	return nil
}

func (m *mockKVStore) RemoveInitializedTeamID(teamID string) error {
	if m.removeInitializedTeamIDFn != nil {
		return m.removeInitializedTeamIDFn(teamID)
	}
	return nil
}

func (m *mockKVStore) GetChannelConnections(channelID string) ([]TeamConnection, error) {
	if m.getChannelConnectionsFn != nil {
		return m.getChannelConnectionsFn(channelID)
	}
	return []TeamConnection{}, nil
}

func (m *mockKVStore) SetChannelConnections(channelID string, conns []TeamConnection) error {
	if m.setChannelConnectionsFn != nil {
		return m.setChannelConnectionsFn(channelID, conns)
	}
	return nil
}

func (m *mockKVStore) DeleteChannelConnections(channelID string) error {
	if m.deleteChannelConnectionsFn != nil {
		return m.deleteChannelConnectionsFn(channelID)
	}
	return nil
}

func (m *mockKVStore) IsChannelInitialized(channelID string) (bool, error) {
	if m.isChannelInitializedFn != nil {
		return m.isChannelInitializedFn(channelID)
	}
	return false, nil
}

func (m *mockKVStore) AddChannelConnection(channelID string, conn TeamConnection) error {
	if m.addChannelConnectionFn != nil {
		return m.addChannelConnectionFn(channelID, conn)
	}
	return nil
}

func (m *mockKVStore) RemoveChannelConnection(channelID string, conn TeamConnection) error {
	if m.removeChannelConnectionFn != nil {
		return m.removeChannelConnectionFn(channelID, conn)
	}
	return nil
}

func (m *mockKVStore) SetPostMapping(connName, remotePostID, localPostID string) error {
	if m.setPostMappingFn != nil {
		return m.setPostMappingFn(connName, remotePostID, localPostID)
	}
	return nil
}

func (m *mockKVStore) GetPostMapping(connName, remotePostID string) (string, error) {
	if m.getPostMappingFn != nil {
		return m.getPostMappingFn(connName, remotePostID)
	}
	return "", nil
}

func (m *mockKVStore) DeletePostMapping(connName, remotePostID string) error {
	if m.deletePostMappingFn != nil {
		return m.deletePostMappingFn(connName, remotePostID)
	}
	return nil
}

func (m *mockKVStore) SetDeletingFlag(postID string) error {
	if m.setDeletingFlagFn != nil {
		return m.setDeletingFlagFn(postID)
	}
	return nil
}

func (m *mockKVStore) IsDeletingFlagSet(postID string) (bool, error) {
	if m.isDeletingFlagSetFn != nil {
		return m.isDeletingFlagSetFn(postID)
	}
	return false, nil
}

func (m *mockKVStore) ClearDeletingFlag(postID string) error {
	if m.clearDeletingFlagFn != nil {
		return m.clearDeletingFlagFn(postID)
	}
	return nil
}

func (m *mockKVStore) GetConnectionPrompt(teamID, connName string) (*ConnectionPrompt, error) {
	return nil, nil
}

func (m *mockKVStore) SetConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) error {
	return nil
}

func (m *mockKVStore) DeleteConnectionPrompt(teamID, connName string) error {
	return nil
}

func (m *mockKVStore) CreateConnectionPrompt(teamID, connName string, prompt *ConnectionPrompt) (bool, error) {
	return true, nil
}

func (m *mockKVStore) GetChannelConnectionPrompt(channelID, connName string) (*ConnectionPrompt, error) {
	return nil, nil
}

func (m *mockKVStore) SetChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) error {
	return nil
}

func (m *mockKVStore) DeleteChannelConnectionPrompt(channelID, connName string) error {
	return nil
}

func (m *mockKVStore) CreateChannelConnectionPrompt(channelID, connName string, prompt *ConnectionPrompt) (bool, error) {
	return true, nil
}

func (m *mockKVStore) GetTeamRewriteIndex(connName, remoteTeamName string) (string, error) {
	if m.getTeamRewriteIndexFn != nil {
		return m.getTeamRewriteIndexFn(connName, remoteTeamName)
	}
	return "", nil
}

func (m *mockKVStore) SetTeamRewriteIndex(connName, remoteTeamName, localTeamID string) error {
	if m.setTeamRewriteIndexFn != nil {
		return m.setTeamRewriteIndexFn(connName, remoteTeamName, localTeamID)
	}
	return nil
}

func (m *mockKVStore) DeleteTeamRewriteIndex(connName, remoteTeamName string) error {
	if m.deleteTeamRewriteIndexFn != nil {
		return m.deleteTeamRewriteIndexFn(connName, remoteTeamName)
	}
	return nil
}

func (m *mockKVStore) GetConnectionRequest(_, _ string) (*ConnectionRequest, error) {
	return nil, nil
}

func (m *mockKVStore) CreateConnectionRequest(_, _ string, _ *ConnectionRequest) (bool, error) {
	return true, nil
}

func (m *mockKVStore) DeleteConnectionRequest(_, _ string) error {
	return nil
}

func (m *mockKVStore) GetChannelConnectionRequest(_, _ string) (*ConnectionRequest, error) {
	return nil, nil
}

func (m *mockKVStore) CreateChannelConnectionRequest(_, _ string, _ *ConnectionRequest) (bool, error) {
	return true, nil
}

func (m *mockKVStore) UpdateChannelConnectionRequest(_, _ string, _ *ConnectionRequest) error {
	return nil
}

func (m *mockKVStore) DeleteChannelConnectionRequest(_, _ string) error {
	return nil
}

func (m *mockKVStore) BumpSequenceCounter(_, _ string) (uint64, error) {
	return 0, nil
}

func newTestCaching(inner *mockKVStore) (*CachingKVStore, *plugintest.API) {
	api := &plugintest.API{}
	api.On("PublishPluginClusterEvent", mock.Anything, mock.Anything).Return(nil)
	for n := 1; n <= 16; n++ {
		args := make([]any, n)
		for i := range args {
			args[i] = mock.Anything
		}
		api.On("LogWarn", args...).Maybe()
	}
	c := NewCachingKVStore(inner, api)
	return c, api
}

func TestGetTeamConnections_CacheHitMiss(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			calls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, []TeamConnection{{Direction: "outbound", Connection: "a"}}, val)
	assert.Equal(t, 1, calls)

	val2, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, []TeamConnection{{Direction: "outbound", Connection: "a"}}, val2)
	assert.Equal(t, 1, calls)
}

func TestGetTeamConnections_CachesEmpty(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			calls++
			return []TeamConnection{}, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Empty(t, val)

	val, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Empty(t, val)
	assert.Equal(t, 1, calls)
}

func TestGetTeamConnections_ErrorNotCached(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			calls++
			return nil, fmt.Errorf("kv store unavailable")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.Error(t, err)

	_, err = c.GetTeamConnections("team1")
	require.Error(t, err)
	assert.Equal(t, 2, calls)
}

func TestAddTeamConnection_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
		addTeamConnectionFn: func(teamID string, conn TeamConnection) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddTeamConnection("team1", TeamConnection{Direction: "inbound", Connection: "a"})
	require.NoError(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestAddTeamConnection_ErrorDoesNotInvalidate(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{}, nil
		},
		addTeamConnectionFn: func(teamID string, conn TeamConnection) error {
			return fmt.Errorf("write failed")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddTeamConnection("team1", TeamConnection{Direction: "outbound", Connection: "a"})
	require.Error(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)
}

func TestAddTeamConnection_PublishesClusterEvent(t *testing.T) {
	inner := &mockKVStore{
		addTeamConnectionFn: func(teamID string, conn TeamConnection) error { return nil },
	}
	api := &plugintest.API{}
	api.On("PublishPluginClusterEvent", model.PluginClusterEvent{
		Id:   ClusterEventInvalidateTeamInit,
		Data: []byte("team1"),
	}, model.PluginClusterEventSendOptions{
		SendType: model.PluginClusterEventSendTypeBestEffort,
	}).Return(nil).Once()

	c := NewCachingKVStore(inner, api)

	err := c.AddTeamConnection("team1", TeamConnection{Direction: "outbound", Connection: "a"})
	require.NoError(t, err)

	api.AssertExpectations(t)
}

func TestHandleClusterEvent_InvalidatesTeamInit(t *testing.T) {
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
	}
	c, _ := newTestCaching(inner)

	_, _ = c.GetTeamConnections("team1")
	assert.Equal(t, 1, c.teamInitCache.Len())

	c.HandleClusterEvent(model.PluginClusterEvent{
		Id:   ClusterEventInvalidateTeamInit,
		Data: []byte("team1"),
	})
	assert.Equal(t, 0, c.teamInitCache.Len())
}

func TestGetInitializedTeamIDs_CacheHitMiss(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			calls++
			return []string{"team1", "team2"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	ids, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, []string{"team1", "team2"}, ids)
	assert.Equal(t, 1, calls)

	ids2, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, []string{"team1", "team2"}, ids2)
	assert.Equal(t, 1, calls)
}

func TestGetInitializedTeamIDs_ReturnsDeepCopy(t *testing.T) {
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			return []string{"team1"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	ids1, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	_ = append(ids1, "team2")

	ids2, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, []string{"team1"}, ids2)
}

func TestGetInitializedTeamIDs_ErrorNotCached(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			calls++
			return nil, fmt.Errorf("kv store unavailable")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetInitializedTeamIDs()
	require.Error(t, err)

	_, err = c.GetInitializedTeamIDs()
	require.Error(t, err)
	assert.Equal(t, 2, calls)
}

func TestAddInitializedTeamID_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			getCalls++
			return []string{"team1"}, nil
		},
		addInitializedTeamIDFn: func(teamID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddInitializedTeamID("team2")
	require.NoError(t, err)

	_, err = c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestAddInitializedTeamID_ErrorDoesNotInvalidate(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			getCalls++
			return []string{"team1"}, nil
		},
		addInitializedTeamIDFn: func(teamID string) error {
			return fmt.Errorf("write failed")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddInitializedTeamID("team2")
	require.Error(t, err)

	_, err = c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)
}

func TestHandleClusterEvent_InvalidatesInitTeams(t *testing.T) {
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			return []string{"team1"}, nil
		},
	}
	c, _ := newTestCaching(inner)

	_, _ = c.GetInitializedTeamIDs()
	assert.Equal(t, 1, c.initTeamsCache.Len())

	c.HandleClusterEvent(model.PluginClusterEvent{
		Id:   ClusterEventInvalidateInitTeams,
		Data: []byte(initTeamsCacheKey),
	})
	assert.Equal(t, 0, c.initTeamsCache.Len())
}

func TestDeleteTeamConnections_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
		deleteTeamConnectionsFn: func(teamID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.DeleteTeamConnections("team1")
	require.NoError(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestRemoveTeamConnection_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
		removeTeamConnectionFn: func(teamID string, conn TeamConnection) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.RemoveTeamConnection("team1", TeamConnection{Direction: "outbound", Connection: "a"})
	require.NoError(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestRemoveInitializedTeamID_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getInitializedTeamIDsFn: func() ([]string, error) {
			getCalls++
			return []string{"team1"}, nil
		},
		removeInitializedTeamIDFn: func(teamID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.RemoveInitializedTeamID("team1")
	require.NoError(t, err)

	_, err = c.GetInitializedTeamIDs()
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestGetChannelConnections_CacheHitMiss(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			calls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, []TeamConnection{{Direction: "outbound", Connection: "a"}}, val)
	assert.Equal(t, 1, calls)

	val2, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, []TeamConnection{{Direction: "outbound", Connection: "a"}}, val2)
	assert.Equal(t, 1, calls)
}

func TestGetChannelConnections_CachesEmpty(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			calls++
			return []TeamConnection{}, nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Empty(t, val)

	val, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Empty(t, val)
	assert.Equal(t, 1, calls)
}

func TestGetChannelConnections_ErrorNotCached(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			calls++
			return nil, fmt.Errorf("kv store unavailable")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.Error(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.Error(t, err)
	assert.Equal(t, 2, calls)
}

func TestAddChannelConnection_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
		addChannelConnectionFn: func(channelID string, conn TeamConnection) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddChannelConnection("chan1", TeamConnection{Direction: "inbound", Connection: "a"})
	require.NoError(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestAddChannelConnection_ErrorDoesNotInvalidate(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{}, nil
		},
		addChannelConnectionFn: func(channelID string, conn TeamConnection) error {
			return fmt.Errorf("write failed")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.AddChannelConnection("chan1", TeamConnection{Direction: "outbound", Connection: "a"})
	require.Error(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)
}

func TestAddChannelConnection_PublishesClusterEvent(t *testing.T) {
	inner := &mockKVStore{
		addChannelConnectionFn: func(channelID string, conn TeamConnection) error { return nil },
	}
	api := &plugintest.API{}
	api.On("PublishPluginClusterEvent", model.PluginClusterEvent{
		Id:   ClusterEventInvalidateChannelInit,
		Data: []byte("chan1"),
	}, model.PluginClusterEventSendOptions{
		SendType: model.PluginClusterEventSendTypeBestEffort,
	}).Return(nil).Once()

	c := NewCachingKVStore(inner, api)

	err := c.AddChannelConnection("chan1", TeamConnection{Direction: "outbound", Connection: "a"})
	require.NoError(t, err)

	api.AssertExpectations(t)
}

func TestSetChannelConnections_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
		setChannelConnectionsFn: func(channelID string, conns []TeamConnection) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.SetChannelConnections("chan1", []TeamConnection{
		{Direction: "outbound", Connection: "a"},
		{Direction: "inbound", Connection: "a"},
	})
	require.NoError(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestDeleteChannelConnections_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
		deleteChannelConnectionsFn: func(channelID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.DeleteChannelConnections("chan1")
	require.NoError(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestRemoveChannelConnection_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
		removeChannelConnectionFn: func(channelID string, conn TeamConnection) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.RemoveChannelConnection("chan1", TeamConnection{Direction: "outbound", Connection: "a"})
	require.NoError(t, err)

	_, err = c.GetChannelConnections("chan1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestHandleClusterEvent_InvalidatesChannelInit(t *testing.T) {
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
	}
	c, _ := newTestCaching(inner)

	_, _ = c.GetChannelConnections("chan1")
	assert.Equal(t, 1, c.channelInitCache.Len())

	c.HandleClusterEvent(model.PluginClusterEvent{
		Id:   ClusterEventInvalidateChannelInit,
		Data: []byte("chan1"),
	})
	assert.Equal(t, 0, c.channelInitCache.Len())
}

func TestGetTeamRewriteIndex_CachesNegativeLookup(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamRewriteIndexFn: func(connName, remoteTeamName string) (string, error) {
			calls++
			return "", nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamRewriteIndex("loopback", "unknown-team")
	require.NoError(t, err)
	assert.Equal(t, "", val)
	assert.Equal(t, 1, calls)

	val, err = c.GetTeamRewriteIndex("loopback", "unknown-team")
	require.NoError(t, err)
	assert.Equal(t, "", val)
	assert.Equal(t, 1, calls, "second call should hit cache, not inner store")
}

func TestGetTeamRewriteIndex_CacheHitMiss(t *testing.T) {
	calls := 0
	inner := &mockKVStore{
		getTeamRewriteIndexFn: func(connName, remoteTeamName string) (string, error) {
			calls++
			return "team-123", nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamRewriteIndex("loopback", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "team-123", val)
	assert.Equal(t, 1, calls)

	val, err = c.GetTeamRewriteIndex("loopback", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "team-123", val)
	assert.Equal(t, 1, calls)
}

func TestSetTeamRewriteIndex_InvalidatesNegativeCache(t *testing.T) {
	lookupCalls := 0
	inner := &mockKVStore{
		getTeamRewriteIndexFn: func(connName, remoteTeamName string) (string, error) {
			lookupCalls++
			if lookupCalls == 1 {
				return "", nil
			}
			return "team-123", nil
		},
		setTeamRewriteIndexFn: func(connName, remoteTeamName, localTeamID string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamRewriteIndex("loopback", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "", val)
	assert.Equal(t, 1, lookupCalls)

	err = c.SetTeamRewriteIndex("loopback", "remote-team", "team-123")
	require.NoError(t, err)

	val, err = c.GetTeamRewriteIndex("loopback", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "team-123", val)
	assert.Equal(t, 2, lookupCalls, "should re-fetch after invalidation")
}

func TestSetTeamConnections_InvalidatesCache(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
		setTeamConnectionsFn: func(teamID string, conns []TeamConnection) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.SetTeamConnections("team1", []TeamConnection{
		{Direction: "outbound", Connection: "a"},
		{Direction: "inbound", Connection: "a"},
	})
	require.NoError(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 2, getCalls)
}

func TestSetTeamConnections_ErrorDoesNotInvalidate(t *testing.T) {
	getCalls := 0
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			getCalls++
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
		setTeamConnectionsFn: func(teamID string, conns []TeamConnection) error {
			return fmt.Errorf("write failed")
		},
	}
	c, _ := newTestCaching(inner)

	_, err := c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)

	err = c.SetTeamConnections("team1", []TeamConnection{
		{Direction: "outbound", Connection: "a"},
		{Direction: "inbound", Connection: "a"},
	})
	require.Error(t, err)

	_, err = c.GetTeamConnections("team1")
	require.NoError(t, err)
	assert.Equal(t, 1, getCalls)
}

func TestIsTeamInitialized_WithConnections(t *testing.T) {
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
	}
	c, _ := newTestCaching(inner)

	ok, err := c.IsTeamInitialized("team1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestIsTeamInitialized_Empty(t *testing.T) {
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			return []TeamConnection{}, nil
		},
	}
	c, _ := newTestCaching(inner)

	ok, err := c.IsTeamInitialized("team1")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsTeamInitialized_Error(t *testing.T) {
	inner := &mockKVStore{
		getTeamConnectionsFn: func(teamID string) ([]TeamConnection, error) {
			return nil, fmt.Errorf("kv store unavailable")
		},
	}
	c, _ := newTestCaching(inner)

	ok, err := c.IsTeamInitialized("team1")
	require.Error(t, err)
	assert.False(t, ok)
}

func TestIsChannelInitialized_WithConnections(t *testing.T) {
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			return []TeamConnection{{Direction: "outbound", Connection: "a"}}, nil
		},
	}
	c, _ := newTestCaching(inner)

	ok, err := c.IsChannelInitialized("chan1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestIsChannelInitialized_Empty(t *testing.T) {
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			return []TeamConnection{}, nil
		},
	}
	c, _ := newTestCaching(inner)

	ok, err := c.IsChannelInitialized("chan1")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsChannelInitialized_Error(t *testing.T) {
	inner := &mockKVStore{
		getChannelConnectionsFn: func(channelID string) ([]TeamConnection, error) {
			return nil, fmt.Errorf("kv store unavailable")
		},
	}
	c, _ := newTestCaching(inner)

	ok, err := c.IsChannelInitialized("chan1")
	require.Error(t, err)
	assert.False(t, ok)
}

func TestDeleteTeamRewriteIndex_InvalidatesCache(t *testing.T) {
	lookupCalls := 0
	inner := &mockKVStore{
		getTeamRewriteIndexFn: func(connName, remoteTeamName string) (string, error) {
			lookupCalls++
			return "team-123", nil
		},
		deleteTeamRewriteIndexFn: func(connName, remoteTeamName string) error {
			return nil
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamRewriteIndex("loopback", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "team-123", val)
	assert.Equal(t, 1, lookupCalls)

	err = c.DeleteTeamRewriteIndex("loopback", "remote-team")
	require.NoError(t, err)

	val, err = c.GetTeamRewriteIndex("loopback", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "team-123", val)
	assert.Equal(t, 2, lookupCalls, "should re-fetch after invalidation")
}

func TestDeleteTeamRewriteIndex_ErrorDoesNotInvalidate(t *testing.T) {
	lookupCalls := 0
	inner := &mockKVStore{
		getTeamRewriteIndexFn: func(connName, remoteTeamName string) (string, error) {
			lookupCalls++
			return "team-123", nil
		},
		deleteTeamRewriteIndexFn: func(connName, remoteTeamName string) error {
			return fmt.Errorf("delete failed")
		},
	}
	c, _ := newTestCaching(inner)

	val, err := c.GetTeamRewriteIndex("loopback", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "team-123", val)
	assert.Equal(t, 1, lookupCalls)

	err = c.DeleteTeamRewriteIndex("loopback", "remote-team")
	require.Error(t, err)

	val, err = c.GetTeamRewriteIndex("loopback", "remote-team")
	require.NoError(t, err)
	assert.Equal(t, "team-123", val)
	assert.Equal(t, 1, lookupCalls, "should still hit cache after failed delete")
}

func TestInvalidate_PublishFailure(t *testing.T) {
	inner := &mockKVStore{
		setTeamConnectionsFn: func(teamID string, conns []TeamConnection) error {
			return nil
		},
	}
	api := &plugintest.API{}
	api.On("PublishPluginClusterEvent", mock.Anything, mock.Anything).Return(fmt.Errorf("cluster event publish failed"))
	api.On("LogWarn", "Failed to publish cache invalidation event",
		"error_code", errcode.StoreCachePublishInvalidationFailed,
		"event", ClusterEventInvalidateTeamInit,
		"key", "team1",
		"error", "cluster event publish failed").Return()

	c := NewCachingKVStore(inner, api)

	err := c.SetTeamConnections("team1", []TeamConnection{{Direction: "outbound", Connection: "a"}})
	require.NoError(t, err, "mutation should succeed even when publish fails")

	api.AssertCalled(t, "LogWarn", "Failed to publish cache invalidation event",
		"error_code", errcode.StoreCachePublishInvalidationFailed,
		"event", ClusterEventInvalidateTeamInit,
		"key", "team1",
		"error", "cluster event publish failed")
}

// ---------------------------------------------------------------------------
// Caching write error branches (raise Delete*/Set*/Remove* from 75% to 100%)
// ---------------------------------------------------------------------------

func TestCaching_DeleteTeamConnections_InnerError(t *testing.T) {
	inner := &mockKVStore{
		deleteTeamConnectionsFn: func(string) error { return fmt.Errorf("inner fail") },
	}
	c, _ := newTestCaching(inner)
	err := c.DeleteTeamConnections("team1")
	require.Error(t, err)
}

func TestCaching_RemoveTeamConnection_InnerError(t *testing.T) {
	inner := &mockKVStore{
		removeTeamConnectionFn: func(string, TeamConnection) error { return fmt.Errorf("inner fail") },
	}
	c, _ := newTestCaching(inner)
	err := c.RemoveTeamConnection("team1", TeamConnection{Direction: "outbound", Connection: "a"})
	require.Error(t, err)
}

func TestCaching_RemoveInitializedTeamID_InnerError(t *testing.T) {
	inner := &mockKVStore{
		removeInitializedTeamIDFn: func(string) error { return fmt.Errorf("inner fail") },
	}
	c, _ := newTestCaching(inner)
	err := c.RemoveInitializedTeamID("team1")
	require.Error(t, err)
}

func TestCaching_SetChannelConnections_InnerError(t *testing.T) {
	inner := &mockKVStore{
		setChannelConnectionsFn: func(string, []TeamConnection) error { return fmt.Errorf("inner fail") },
	}
	c, _ := newTestCaching(inner)
	err := c.SetChannelConnections("ch1", nil)
	require.Error(t, err)
}

func TestCaching_DeleteChannelConnections_InnerError(t *testing.T) {
	inner := &mockKVStore{
		deleteChannelConnectionsFn: func(string) error { return fmt.Errorf("inner fail") },
	}
	c, _ := newTestCaching(inner)
	err := c.DeleteChannelConnections("ch1")
	require.Error(t, err)
}

func TestCaching_RemoveChannelConnection_InnerError(t *testing.T) {
	inner := &mockKVStore{
		removeChannelConnectionFn: func(string, TeamConnection) error { return fmt.Errorf("inner fail") },
	}
	c, _ := newTestCaching(inner)
	err := c.RemoveChannelConnection("ch1", TeamConnection{Direction: "inbound", Connection: "a"})
	require.Error(t, err)
}
