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

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// inboundFileTestSetup wires a Plugin with a flexibleKVStore and a remoteID
// mapping for "inbound:high". Returns the plugin, the kvstore, and the
// connection name.
func inboundFileTestSetup(t *testing.T, api *plugintest.API) (*Plugin, *flexibleKVStore, string) {
	t.Helper()
	p, kvs := setupTestPluginWithRouter(api)
	connName := "high"
	p.remoteIDs = map[string]string{
		"inbound:" + connName: mmModel.NewId(),
	}
	return p, kvs, connName
}

func encodedFileInfo(t *testing.T, fi *mmModel.FileInfo) string {
	t.Helper()
	b, err := json.Marshal(fi)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(b)
}

func TestHandleInboundFile_UnknownKind(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, conn := inboundFileTestSetup(t, api)

	err := p.handleInboundFile(conn)("k", []byte("x"), map[string]string{
		headerKind: "bogus",
	})
	require.NoError(t, err, "unknown kind should drop, not retry")
}

func TestHandleInboundAttachment_MissingHeader(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, conn := inboundFileTestSetup(t, api)

	// All four required headers must be present.
	headers := map[string]string{
		headerKind: kindAttachment,
		// missing team, channel, post, fileinfo
	}
	require.NoError(t, p.handleInboundAttachment(conn, "k", []byte("x"), headers))
	api.AssertNotCalled(t, "GetTeamByName", mock.Anything)
}

func TestHandleInboundAttachment_BadFileInfo(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, conn := inboundFileTestSetup(t, api)

	headers := map[string]string{
		headerKind:     kindAttachment,
		headerTeamName: "team-a",
		headerChanName: "general",
		headerPostID:   "p1",
		headerFileInfo: "not-base64!!!",
	}
	require.NoError(t, p.handleInboundAttachment(conn, "k", []byte("x"), headers))
	api.AssertNotCalled(t, "GetTeamByName", mock.Anything)
}

func TestHandleInboundAttachment_TeamNotFound_Retries(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, conn := inboundFileTestSetup(t, api)

	api.On("GetTeamByName", "team-a").Return(nil, &mmModel.AppError{Message: "no such team"})

	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf"}
	headers := map[string]string{
		headerKind:     kindAttachment,
		headerTeamName: "team-a",
		headerChanName: "general",
		headerPostID:   "p1",
		headerFileInfo: encodedFileInfo(t, fi),
	}
	err := p.handleInboundAttachment(conn, "k", []byte("x"), headers)
	require.Error(t, err, "missing team should return error so the watcher retries")
}

func TestHandleInboundAttachment_ChannelNotFound_Retries(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, conn := inboundFileTestSetup(t, api)

	team := &mmModel.Team{Id: "t1", Name: "team-a"}
	api.On("GetTeamByName", team.Name).Return(team, (*mmModel.AppError)(nil))
	api.On("GetChannelByName", team.Id, "general", false).
		Return(nil, &mmModel.AppError{Message: "no such channel"})

	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf"}
	headers := map[string]string{
		headerKind:     kindAttachment,
		headerTeamName: team.Name,
		headerChanName: "general",
		headerPostID:   "p1",
		headerFileInfo: encodedFileInfo(t, fi),
	}
	err := p.handleInboundAttachment(conn, "k", []byte("x"), headers)
	require.Error(t, err, "missing channel should return error so the watcher retries")
}

func TestHandleInboundAttachment_ChannelUnlinked_Drops(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs, conn := inboundFileTestSetup(t, api)

	team := &mmModel.Team{Id: "t1", Name: "team-a"}
	channel := &mmModel.Channel{Id: "c1", TeamId: team.Id, Name: "general"}
	api.On("GetTeamByName", team.Name).Return(team, (*mmModel.AppError)(nil))
	api.On("GetChannelByName", team.Id, channel.Name, false).Return(channel, (*mmModel.AppError)(nil))

	// Channel is registered but inbound for "high" is not linked.
	kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{
			{Direction: "outbound", Connection: conn},
		}, nil
	}

	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf"}
	headers := map[string]string{
		headerKind:     kindAttachment,
		headerTeamName: team.Name,
		headerChanName: channel.Name,
		headerPostID:   "p1",
		headerFileInfo: encodedFileInfo(t, fi),
	}
	require.NoError(t, p.handleInboundAttachment(conn, "k", []byte("x"), headers))
	api.AssertNotCalled(t, "ReceiveSharedChannelAttachmentSyncMsg",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleInboundAttachment_HappyPath(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs, conn := inboundFileTestSetup(t, api)

	team := &mmModel.Team{Id: "t1", Name: "team-a"}
	channel := &mmModel.Channel{Id: "c1", TeamId: team.Id, Name: "general"}
	api.On("GetTeamByName", team.Name).Return(team, (*mmModel.AppError)(nil))
	api.On("GetChannelByName", team.Id, channel.Name, false).Return(channel, (*mmModel.AppError)(nil))

	kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{
			{Direction: "inbound", Connection: conn},
		}, nil
	}

	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf"}
	api.On("ReceiveSharedChannelAttachmentSyncMsg",
		p.remoteIDs["inbound:"+conn], channel.Id,
		mock.MatchedBy(func(arg *mmModel.FileInfo) bool { return arg != nil && arg.Id == fi.Id }),
		mock.Anything,
	).Return((*mmModel.FileInfo)(nil), nil)

	headers := map[string]string{
		headerKind:     kindAttachment,
		headerTeamName: team.Name,
		headerChanName: channel.Name,
		headerPostID:   "p1",
		headerFileInfo: encodedFileInfo(t, fi),
	}
	require.NoError(t, p.handleInboundAttachment(conn, "k", []byte("payload"), headers))
	api.AssertExpectations(t)
}

func TestHandleInboundAttachment_ReceiveFails_Retries(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs, conn := inboundFileTestSetup(t, api)

	team := &mmModel.Team{Id: "t1", Name: "team-a"}
	channel := &mmModel.Channel{Id: "c1", TeamId: team.Id, Name: "general"}
	api.On("GetTeamByName", team.Name).Return(team, (*mmModel.AppError)(nil))
	api.On("GetChannelByName", team.Id, channel.Name, false).Return(channel, (*mmModel.AppError)(nil))

	kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{
			{Direction: "inbound", Connection: conn},
		}, nil
	}

	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf"}
	api.On("ReceiveSharedChannelAttachmentSyncMsg",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	).Return((*mmModel.FileInfo)(nil), errors.New("receive boom"))

	headers := map[string]string{
		headerKind:     kindAttachment,
		headerTeamName: team.Name,
		headerChanName: channel.Name,
		headerPostID:   "p1",
		headerFileInfo: encodedFileInfo(t, fi),
	}
	err := p.handleInboundAttachment(conn, "k", []byte("payload"), headers)
	require.Error(t, err, "receive error must propagate so watcher retries")
}

func TestHandleInboundAttachment_NoRemoteForConn(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, kvs, conn := inboundFileTestSetup(t, api)
	p.remoteIDs = map[string]string{} // wipe so lookup fails

	team := &mmModel.Team{Id: "t1", Name: "team-a"}
	channel := &mmModel.Channel{Id: "c1", TeamId: team.Id, Name: "general"}
	api.On("GetTeamByName", team.Name).Return(team, (*mmModel.AppError)(nil))
	api.On("GetChannelByName", team.Id, channel.Name, false).Return(channel, (*mmModel.AppError)(nil))
	kvs.getChannelConnectionsFn = func(string) ([]store.TeamConnection, error) {
		return []store.TeamConnection{
			{Direction: "inbound", Connection: conn},
		}, nil
	}

	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf"}
	headers := map[string]string{
		headerKind:     kindAttachment,
		headerTeamName: team.Name,
		headerChanName: channel.Name,
		headerPostID:   "p1",
		headerFileInfo: encodedFileInfo(t, fi),
	}
	require.NoError(t, p.handleInboundAttachment(conn, "k", []byte("x"), headers))
	api.AssertNotCalled(t, "ReceiveSharedChannelAttachmentSyncMsg",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleInboundProfileImage_MissingHeader(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, conn := inboundFileTestSetup(t, api)

	require.NoError(t, p.handleInboundProfileImage(conn, "k", []byte("x"), map[string]string{
		headerKind: kindProfileImage,
		// missing user_id
	}))
	api.AssertNotCalled(t, "ReceiveSharedChannelProfileImageSyncMsg",
		mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleInboundProfileImage_NoRemoteForConn(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, conn := inboundFileTestSetup(t, api)
	p.remoteIDs = map[string]string{}

	require.NoError(t, p.handleInboundProfileImage(conn, "k", []byte("x"), map[string]string{
		headerKind:   kindProfileImage,
		headerUserID: "u1",
	}))
	api.AssertNotCalled(t, "ReceiveSharedChannelProfileImageSyncMsg",
		mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleInboundProfileImage_HappyPath(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, conn := inboundFileTestSetup(t, api)

	api.On("ReceiveSharedChannelProfileImageSyncMsg",
		p.remoteIDs["inbound:"+conn], "u1", []byte("png"),
	).Return(nil)

	require.NoError(t, p.handleInboundProfileImage(conn, "profile_image/u1", []byte("png"), map[string]string{
		headerKind:   kindProfileImage,
		headerUserID: "u1",
	}))
	api.AssertExpectations(t)
}

func TestHandleInboundProfileImage_ReceiveFails_Retries(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, conn := inboundFileTestSetup(t, api)

	api.On("ReceiveSharedChannelProfileImageSyncMsg",
		mock.Anything, "u1", mock.Anything,
	).Return(errors.New("receive boom"))

	err := p.handleInboundProfileImage(conn, "profile_image/u1", []byte("png"), map[string]string{
		headerKind:   kindProfileImage,
		headerUserID: "u1",
	})
	require.Error(t, err, "receive error must propagate so watcher retries")
}

func TestDecodeFileInfoHeader_RoundTrip(t *testing.T) {
	fi := &mmModel.FileInfo{Id: "f1", Name: "doc.pdf", Size: 42}
	b, err := json.Marshal(fi)
	require.NoError(t, err)
	enc := base64.StdEncoding.EncodeToString(b)

	got, err := decodeFileInfoHeader(enc)
	require.NoError(t, err)
	assert.Equal(t, fi.Id, got.Id)
	assert.Equal(t, fi.Name, got.Name)
	assert.Equal(t, fi.Size, got.Size)
}

func TestDecodeFileInfoHeader_BadBase64(t *testing.T) {
	_, err := decodeFileInfoHeader("not-base64!!!")
	require.Error(t, err)
}

func TestDecodeFileInfoHeader_BadJSON(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString([]byte("not json"))
	_, err := decodeFileInfoHeader(enc)
	require.Error(t, err)
}

func TestStartInboundFileWatchers_CtxCancellation(t *testing.T) {
	api := &plugintest.API{}
	defaultLogMocks(api)
	p, _, _ := inboundFileTestSetup(t, api)

	// WatchFiles blocks until context is cancelled.
	provider := &mockQueueProvider{
		watchFilesFn: func(ctx context.Context, _ func(string, []byte, map[string]string) error) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	ic := inboundConn{provider: provider, name: "high"}
	ctx, cancel := context.WithCancel(context.Background())
	p.startInboundFileWatchers(ctx, ic)
	cancel()
	p.wg.Wait() // returns when goroutine exits
}
