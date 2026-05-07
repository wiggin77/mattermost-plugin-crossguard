package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAugmentSyncMsgUsers_NoReferencedUsers(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _ := setupTestPlugin(api)

	msg := &mmModel.SyncMsg{
		Id:        mmModel.NewId(),
		ChannelId: "chan-id",
	}

	out := p.augmentSyncMsgUsers(msg)
	assert.Same(t, msg, out, "no references should return original SyncMsg")
	api.AssertNotCalled(t, "GetUser")
}

func TestAugmentSyncMsgUsers_AuthorAlreadyIncluded(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _ := setupTestPlugin(api)

	user := &mmModel.User{Id: "user-1"}
	msg := &mmModel.SyncMsg{
		ChannelId: "chan-id",
		Users:     map[string]*mmModel.User{user.Id: user},
		Posts:     []*mmModel.Post{{Id: "post-1", UserId: user.Id}},
	}

	out := p.augmentSyncMsgUsers(msg)
	assert.Same(t, msg, out, "author already in Users: no augmentation")
	api.AssertNotCalled(t, "GetUser")
}

func TestAugmentSyncMsgUsers_AuthorMissing(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _ := setupTestPlugin(api)

	author := &mmModel.User{Id: "user-1"}
	api.On("GetUser", "user-1").Return(author, nil).Once()

	msg := &mmModel.SyncMsg{
		ChannelId: "chan-id",
		Posts:     []*mmModel.Post{{Id: "post-1", UserId: author.Id}},
	}

	out := p.augmentSyncMsgUsers(msg)
	require.NotSame(t, msg, out, "augmentation should return a new SyncMsg")
	assert.Len(t, out.Users, 1)
	assert.Same(t, author, out.Users[author.Id])
	api.AssertExpectations(t)
}

func TestAugmentSyncMsgUsers_DedupAcrossSources(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _ := setupTestPlugin(api)

	already := &mmModel.User{Id: "user-already"}
	a := &mmModel.User{Id: "user-a"}
	b := &mmModel.User{Id: "user-b"}
	c := &mmModel.User{Id: "user-c"}

	api.On("GetUser", "user-a").Return(a, nil).Once()
	api.On("GetUser", "user-b").Return(b, nil).Once()
	api.On("GetUser", "user-c").Return(c, nil).Once()

	msg := &mmModel.SyncMsg{
		ChannelId: "chan-id",
		Users:     map[string]*mmModel.User{already.Id: already},
		Posts: []*mmModel.Post{
			{Id: "post-1", UserId: a.Id},
			{Id: "post-2", UserId: a.Id}, // duplicate of a
			{Id: "post-3", UserId: already.Id},
		},
		Reactions: []*mmModel.Reaction{
			{PostId: "post-1", UserId: b.Id},
			{PostId: "post-1", UserId: a.Id}, // duplicate again
		},
		Acknowledgements: []*mmModel.PostAcknowledgement{
			{PostId: "post-1", UserId: c.Id},
		},
		MembershipChanges: []*mmModel.MembershipChangeMsg{
			{ChannelId: "chan-id", UserId: b.Id}, // duplicate of b
		},
		Statuses: []*mmModel.Status{
			{UserId: c.Id}, // duplicate of c
		},
	}

	out := p.augmentSyncMsgUsers(msg)
	require.NotSame(t, msg, out)
	assert.Len(t, out.Users, 4)
	assert.Same(t, already, out.Users[already.Id])
	assert.Same(t, a, out.Users[a.Id])
	assert.Same(t, b, out.Users[b.Id])
	assert.Same(t, c, out.Users[c.Id])
	api.AssertExpectations(t)
}

func TestAugmentSyncMsgUsers_SkipsRemoteOriginUsers(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _ := setupTestPlugin(api)

	remoteID := "remote-cluster-id"
	syntheticUser := &mmModel.User{Id: "user-remote", RemoteId: &remoteID}
	api.On("GetUser", "user-remote").Return(syntheticUser, nil).Once()

	msg := &mmModel.SyncMsg{
		ChannelId: "chan-id",
		Posts:     []*mmModel.Post{{Id: "post-1", UserId: syntheticUser.Id}},
	}

	out := p.augmentSyncMsgUsers(msg)
	assert.Same(t, msg, out, "remote-origin users should not trigger a new SyncMsg")
	api.AssertExpectations(t)
}

func TestAugmentSyncMsgUsers_GetUserError(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _ := setupTestPlugin(api)

	good := &mmModel.User{Id: "user-good"}
	api.On("GetUser", "user-good").Return(good, nil).Once()
	api.On("GetUser", "user-missing").Return(nil, &mmModel.AppError{Message: "not found"}).Once()

	msg := &mmModel.SyncMsg{
		ChannelId: "chan-id",
		Posts: []*mmModel.Post{
			{Id: "post-1", UserId: good.Id},
			{Id: "post-2", UserId: "user-missing"},
		},
	}

	out := p.augmentSyncMsgUsers(msg)
	require.NotSame(t, msg, out)
	assert.Len(t, out.Users, 1)
	assert.Same(t, good, out.Users[good.Id])
	_, present := out.Users["user-missing"]
	assert.False(t, present, "missing user should be skipped")
	api.AssertExpectations(t)
}

func TestAugmentSyncMsgUsers_DefensiveCopy(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _ := setupTestPlugin(api)

	original := &mmModel.User{Id: "user-original"}
	added := &mmModel.User{Id: "user-added"}
	api.On("GetUser", "user-added").Return(added, nil).Once()

	msg := &mmModel.SyncMsg{
		ChannelId: "chan-id",
		Users:     map[string]*mmModel.User{original.Id: original},
		Posts:     []*mmModel.Post{{Id: "post-1", UserId: added.Id}},
	}

	out := p.augmentSyncMsgUsers(msg)
	require.NotSame(t, msg, out)

	// Mutate the augmented Users map; the original SyncMsg's Users must not change.
	delete(out.Users, original.Id)
	out.Users["injected"] = &mmModel.User{Id: "injected"}

	assert.Len(t, msg.Users, 1, "original SyncMsg.Users should be unchanged")
	assert.Same(t, original, msg.Users[original.Id])
	_, injectedInOriginal := msg.Users["injected"]
	assert.False(t, injectedInOriginal, "mutating augmented map must not leak into original")
	api.AssertExpectations(t)
}

func TestAugmentSyncMsgUsers_NilMsg(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _ := setupTestPlugin(api)

	out := p.augmentSyncMsgUsers(nil)
	assert.Nil(t, out)
}

// outboundFileTestSetup wires a Plugin with one outbound connection ("high")
// configured for file transfer, a mock provider, and a remoteID mapping so
// connNameForRemote can resolve. Returns the plugin, the mock provider for
// assertions on UploadFile, and the remote cluster the hooks expect.
func outboundFileTestSetup(t *testing.T, api *plugintest.API, conn ConnectionConfig) (*Plugin, *mockQueueProvider, *mmModel.RemoteCluster) {
	t.Helper()
	p, _ := setupTestPlugin(api)

	connsJSON, err := json.Marshal([]ConnectionConfig{conn})
	require.NoError(t, err)
	p.configuration = &configuration{OutboundConnections: string(connsJSON)}

	provider := &mockQueueProvider{}
	p.outboundConns = []outboundConn{
		{
			provider: provider,
			name:     conn.Name,
			healthy:  true,
		},
	}

	remoteID := mmModel.NewId()
	p.remoteIDs = map[string]string{
		"outbound:" + conn.Name: remoteID,
	}
	rc := &mmModel.RemoteCluster{RemoteId: remoteID, Name: conn.Name}

	// Default config: no MaxFileSize cap unless test overrides.
	api.On("GetConfig").Return(&mmModel.Config{}).Maybe()

	return p, provider, rc
}

func TestOnSharedChannelsAttachmentSyncMsg_HappyPath(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: true,
	}
	p, provider, rc := outboundFileTestSetup(t, api, conn)

	team := &mmModel.Team{Id: mmModel.NewId(), Name: "team-a"}
	channel := &mmModel.Channel{Id: mmModel.NewId(), TeamId: team.Id, Name: "general"}
	post := &mmModel.Post{Id: mmModel.NewId(), ChannelId: channel.Id}
	fi := &mmModel.FileInfo{Id: mmModel.NewId(), Name: "doc.pdf", Size: 1024}
	fileBytes := []byte("file-content")

	api.On("GetFile", fi.Id).Return(fileBytes, (*mmModel.AppError)(nil))
	api.On("GetChannel", channel.Id).Return(channel, (*mmModel.AppError)(nil))
	api.On("GetTeam", team.Id).Return(team, (*mmModel.AppError)(nil))

	var gotKey string
	var gotData []byte
	var gotHeaders map[string]string
	provider.uploadFileFn = func(_ context.Context, key string, data []byte, headers map[string]string) error {
		gotKey = key
		gotData = data
		gotHeaders = headers
		return nil
	}

	require.NoError(t, p.OnSharedChannelsAttachmentSyncMsg(fi, post, rc))

	assert.Equal(t, "attachment/"+post.Id+"/"+fi.Id, gotKey)
	assert.Equal(t, fileBytes, gotData)
	assert.Equal(t, kindAttachment, gotHeaders[headerKind])
	assert.Equal(t, conn.Name, gotHeaders[headerConnName])
	assert.Equal(t, team.Name, gotHeaders[headerTeamName])
	assert.Equal(t, channel.Name, gotHeaders[headerChanName])
	assert.Equal(t, post.Id, gotHeaders[headerPostID])
	assert.Equal(t, fi.Name, gotHeaders[headerFilename])

	// Decode the FileInfo header and verify it round-trips.
	rawJSON, err := base64.StdEncoding.DecodeString(gotHeaders[headerFileInfo])
	require.NoError(t, err)
	var decoded mmModel.FileInfo
	require.NoError(t, json.Unmarshal(rawJSON, &decoded))
	assert.Equal(t, fi.Id, decoded.Id)
	assert.Equal(t, fi.Name, decoded.Name)
}

func TestOnSharedChannelsAttachmentSyncMsg_DisabledConnection(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: false,
	}
	p, provider, rc := outboundFileTestSetup(t, api, conn)

	uploadCalled := false
	provider.uploadFileFn = func(context.Context, string, []byte, map[string]string) error {
		uploadCalled = true
		return nil
	}

	post := &mmModel.Post{Id: "p1", ChannelId: "c1"}
	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf"}
	require.NoError(t, p.OnSharedChannelsAttachmentSyncMsg(fi, post, rc))
	assert.False(t, uploadCalled, "upload must not run when file transfer disabled")
	api.AssertNotCalled(t, "GetFile", mock.Anything)
}

func TestOnSharedChannelsAttachmentSyncMsg_FilteredOut(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: true,
		FileFilterMode:      fileFilterModeDeny,
		FileFilterTypes:     ".exe",
	}
	p, provider, rc := outboundFileTestSetup(t, api, conn)

	uploadCalled := false
	provider.uploadFileFn = func(context.Context, string, []byte, map[string]string) error {
		uploadCalled = true
		return nil
	}

	post := &mmModel.Post{Id: "p1", ChannelId: "c1"}
	fi := &mmModel.FileInfo{Id: "f1", Name: "tool.exe"}
	require.NoError(t, p.OnSharedChannelsAttachmentSyncMsg(fi, post, rc))
	assert.False(t, uploadCalled, "filtered file must not upload")
	api.AssertNotCalled(t, "GetFile", mock.Anything)
}

func TestOnSharedChannelsAttachmentSyncMsg_GetFileError(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: true,
	}
	p, _, rc := outboundFileTestSetup(t, api, conn)

	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf"}
	post := &mmModel.Post{Id: "p1", ChannelId: "c1"}
	api.On("GetFile", fi.Id).Return([]byte(nil), &mmModel.AppError{Message: "boom"})

	err := p.OnSharedChannelsAttachmentSyncMsg(fi, post, rc)
	require.Error(t, err)
}

func TestOnSharedChannelsAttachmentSyncMsg_UploadError(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: true,
	}
	p, provider, rc := outboundFileTestSetup(t, api, conn)

	team := &mmModel.Team{Id: "t1", Name: "team-a"}
	channel := &mmModel.Channel{Id: "c1", TeamId: team.Id, Name: "general"}
	post := &mmModel.Post{Id: "p1", ChannelId: channel.Id}
	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf"}

	api.On("GetFile", fi.Id).Return([]byte("x"), (*mmModel.AppError)(nil))
	api.On("GetChannel", channel.Id).Return(channel, (*mmModel.AppError)(nil))
	api.On("GetTeam", team.Id).Return(team, (*mmModel.AppError)(nil))

	provider.uploadFileFn = func(context.Context, string, []byte, map[string]string) error {
		return errors.New("upload boom")
	}

	err := p.OnSharedChannelsAttachmentSyncMsg(fi, post, rc)
	require.Error(t, err, "upload error must propagate so framework retries")

	// Message publish (publishToOutboundConn) and file upload run on
	// independent transports (e.g., NATS core pub/sub vs. JetStream Object
	// Store) and failures in one must not block the other.
	p.outboundMu.RLock()
	defer p.outboundMu.RUnlock()
	require.Len(t, p.outboundConns, 1)
	assert.True(t, p.outboundConns[0].healthy,
		"upload error must not poison message-publish health state")
}

func TestOnSharedChannelsAttachmentSyncMsg_SizeExceeded(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: true,
	}
	p, provider, rc := outboundFileTestSetup(t, api, conn)

	maxSize := int64(1024)
	api.ExpectedCalls = nil // drop the default permissive GetConfig stub
	api.On("GetConfig").Return(&mmModel.Config{
		FileSettings: mmModel.FileSettings{MaxFileSize: &maxSize},
	})
	defaultLogMocks(api)

	post := &mmModel.Post{Id: "p1", ChannelId: "c1"}
	fi := &mmModel.FileInfo{Id: "f1", Name: "huge.bin", Size: 2048}

	uploadCalled := false
	provider.uploadFileFn = func(context.Context, string, []byte, map[string]string) error {
		uploadCalled = true
		return nil
	}

	require.NoError(t, p.OnSharedChannelsAttachmentSyncMsg(fi, post, rc))
	assert.False(t, uploadCalled)
	api.AssertNotCalled(t, "GetFile", mock.Anything)
}

func TestOnSharedChannelsAttachmentSyncMsg_NoOutboundProvider(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: true,
	}
	p, _, rc := outboundFileTestSetup(t, api, conn)
	p.outboundConns = nil // simulate inbound-only config

	require.NoError(t, p.OnSharedChannelsAttachmentSyncMsg(
		&mmModel.FileInfo{Id: "f1", Name: "doc.pdf"},
		&mmModel.Post{Id: "p1", ChannelId: "c1"},
		rc,
	))
	api.AssertNotCalled(t, "GetFile", mock.Anything)
}

func TestOnSharedChannelsProfileImageSyncMsg_HappyPath(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: true,
	}
	p, provider, rc := outboundFileTestSetup(t, api, conn)

	user := &mmModel.User{Id: mmModel.NewId(), Username: "alice"}
	imgBytes := []byte("png-bytes")
	api.On("GetProfileImage", user.Id).Return(imgBytes, (*mmModel.AppError)(nil))

	var gotKey string
	var gotData []byte
	var gotHeaders map[string]string
	provider.uploadFileFn = func(_ context.Context, key string, data []byte, headers map[string]string) error {
		gotKey = key
		gotData = data
		gotHeaders = headers
		return nil
	}

	require.NoError(t, p.OnSharedChannelsProfileImageSyncMsg(user, rc))
	assert.Equal(t, "profile_image/"+user.Id, gotKey)
	assert.Equal(t, imgBytes, gotData)
	assert.Equal(t, kindProfileImage, gotHeaders[headerKind])
	assert.Equal(t, user.Id, gotHeaders[headerUserID])
	assert.Equal(t, conn.Name, gotHeaders[headerConnName])
}

func TestOnSharedChannelsProfileImageSyncMsg_Disabled(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: false,
	}
	p, provider, rc := outboundFileTestSetup(t, api, conn)

	called := false
	provider.uploadFileFn = func(context.Context, string, []byte, map[string]string) error {
		called = true
		return nil
	}

	user := &mmModel.User{Id: "u1"}
	require.NoError(t, p.OnSharedChannelsProfileImageSyncMsg(user, rc))
	assert.False(t, called)
	api.AssertNotCalled(t, "GetProfileImage", mock.Anything)
}

func TestOnSharedChannelsProfileImageSyncMsg_UploadError(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: true,
	}
	p, provider, rc := outboundFileTestSetup(t, api, conn)

	user := &mmModel.User{Id: "u1"}
	api.On("GetProfileImage", user.Id).Return([]byte("x"), (*mmModel.AppError)(nil))
	provider.uploadFileFn = func(context.Context, string, []byte, map[string]string) error {
		return errors.New("upload boom")
	}

	err := p.OnSharedChannelsProfileImageSyncMsg(user, rc)
	require.Error(t, err, "diagnostic error returned even though framework will not retry")
}

func TestOnSharedChannelsProfileImageSyncMsg_NoRemoteMatch(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)

	conn := ConnectionConfig{
		Name:                "high",
		Provider:            ProviderNATS,
		FileTransferEnabled: true,
	}
	p, _, _ := outboundFileTestSetup(t, api, conn)

	rc := &mmModel.RemoteCluster{RemoteId: "unknown-remote"}
	user := &mmModel.User{Id: "u1"}
	require.NoError(t, p.OnSharedChannelsProfileImageSyncMsg(user, rc))
	api.AssertNotCalled(t, "GetProfileImage", mock.Anything)
}
