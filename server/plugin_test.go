package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func TestOnDeactivate_NilCancel(t *testing.T) {
	api := &plugintest.API{}
	api.On("UnregisterPluginForSharedChannels", mock.Anything).Return(nil).Maybe()
	p := &Plugin{}
	p.SetAPI(api)
	// cancel is nil, should not panic
	err := p.OnDeactivate()
	assert.NoError(t, err)
}

func TestOnDeactivate_CancelsContext(t *testing.T) {
	api := &plugintest.API{}
	api.On("UnregisterPluginForSharedChannels", mock.Anything).Return(nil).Maybe()
	ctx, cancel := context.WithCancel(context.Background())
	p := &Plugin{}
	p.SetAPI(api)
	p.ctx = ctx
	p.cancel = cancel

	err := p.OnDeactivate()
	assert.NoError(t, err)
	// Verify context was cancelled
	assert.Error(t, ctx.Err())
}

func TestOnPluginClusterEvent_CachingStore(t *testing.T) {
	api := &plugintest.API{}
	api.On("LogWarn", "Unexpected cluster event", "id", "unknown-event").Maybe()
	p := &Plugin{}
	p.SetAPI(api)

	inner := &store.Client{}
	caching := store.NewCachingKVStore(inner, api)
	p.kvstore = caching

	// Should not panic, and should delegate to CachingKVStore.HandleClusterEvent
	p.OnPluginClusterEvent(context.Background(), model.PluginClusterEvent{
		Id:   "unknown-event",
		Data: []byte("test"),
	})
}

func TestOnPluginClusterEvent_NonCachingStore(t *testing.T) {
	api := &plugintest.API{}
	p := &Plugin{}
	p.SetAPI(api)

	// Use a non-caching store (mockKVStore via flexibleKVStore wrapper)
	kvs := &flexibleKVStore{testKVStore: newTestKVStore()}
	p.kvstore = kvs

	// Should not panic when kvstore is not *CachingKVStore
	p.OnPluginClusterEvent(context.Background(), model.PluginClusterEvent{
		Id:   "test-event",
		Data: []byte("data"),
	})
}

func TestOnDeactivate_WithConnections(t *testing.T) {
	api := &plugintest.API{}
	api.On("UnregisterPluginForSharedChannels", mock.Anything).Return(nil).Maybe()
	p := &Plugin{}
	p.SetAPI(api)
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel

	outboundClosed := false
	inboundClosed := false
	p.outboundConns = []outboundConn{
		{
			provider: &mockQueueProvider{closeFn: func() error {
				outboundClosed = true
				return nil
			}},
			name: "out-conn",
		},
	}
	p.inboundCancel = func() {}
	p.inboundConns = []inboundConn{
		{
			provider: &mockQueueProvider{closeFn: func() error {
				inboundClosed = true
				return nil
			}},
			name: "in-conn",
		},
	}

	err := p.OnDeactivate()
	assert.NoError(t, err)
	assert.True(t, outboundClosed, "outbound provider should be closed")
	assert.True(t, inboundClosed, "inbound provider should be closed")
}

// TestOnActivate covers the error paths of Plugin.OnActivate. The happy path
// runs connectOutbound/connectInbound which start goroutines and touch real
// providers, so we only exercise the early-return branches.
func TestOnActivate(t *testing.T) {
	botMatcher := mock.MatchedBy(func(b *model.Bot) bool {
		return b != nil && b.Username == "crossguard"
	})

	// mockEnsureBotSuccess wires the minimal set of calls that make
	// pluginapi's EnsureBot return a bot id successfully.
	mockEnsureBotSuccess := func(api *plugintest.API, botID string) {
		api.On("GetServerVersion").Return("5.10.0")
		// cluster.NewMutex.Lock -> KVSetWithOptions(atomic set). Unlock also
		// calls KVSetWithOptions with nil value. Accept any arity with Maybe.
		api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).
			Return(true, (*model.AppError)(nil)).Maybe()
		api.On("EnsureBotUser", botMatcher).Return(botID, nil)
	}

	t.Run("ensure_bot_fails", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		// Force ensureServerVersion to fail inside pluginapi.EnsureBot. This
		// causes an early return before the mutex or EnsureBotUser are called.
		api.On("GetServerVersion").Return("5.9.0")
		defer api.AssertExpectations(t)

		p := &Plugin{}
		p.SetAPI(api)

		err := p.OnActivate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to ensure crossguard bot")
	})

	t.Run("get_bundle_path_fails", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		mockEnsureBotSuccess(api, model.NewId())
		api.On("GetBundlePath").Return("", errors.New("no bundle"))
		defer api.AssertExpectations(t)

		p := &Plugin{}
		p.SetAPI(api)

		err := p.OnActivate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get bundle path")
	})

	t.Run("read_profile_image_fails", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		mockEnsureBotSuccess(api, model.NewId())
		// Bundle path exists but has no assets/crossguard.png, so os.ReadFile fails.
		tmpDir := t.TempDir()
		api.On("GetBundlePath").Return(tmpDir, nil)
		defer api.AssertExpectations(t)

		p := &Plugin{}
		p.SetAPI(api)

		err := p.OnActivate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read bot profile image")
	})

	t.Run("register_command_fails", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		botID := model.NewId()
		mockEnsureBotSuccess(api, botID)

		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "assets"), 0o750))
		require.NoError(t, os.WriteFile(
			filepath.Join(tmpDir, "assets", "crossguard.png"),
			[]byte("fake-png-bytes"),
			0o600,
		))

		api.On("GetBundlePath").Return(tmpDir, nil)
		api.On("SetProfileImage", botID, mock.Anything).Return((*model.AppError)(nil))
		// NewCachingKVStore calls RegisterPluginForClusterEvents.
		api.On("RegisterPluginForClusterEvents").Return((*model.AppError)(nil)).Maybe()
		// Force registerCommand to fail.
		api.On("RegisterCommand", mock.Anything).Return(&model.AppError{Message: "cmd failed"})
		defer api.AssertExpectations(t)

		p := &Plugin{}
		p.SetAPI(api)

		err := p.OnActivate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cmd failed")
		assert.Equal(t, botID, p.botUserID, "botUserID should be set before registerCommand runs")
	})

	t.Run("happy_path_no_connections", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		botID := model.NewId()
		mockEnsureBotSuccess(api, botID)

		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "assets"), 0o750))
		require.NoError(t, os.WriteFile(
			filepath.Join(tmpDir, "assets", "crossguard.png"),
			[]byte("fake-png-bytes"),
			0o600,
		))

		api.On("GetBundlePath").Return(tmpDir, nil)
		api.On("SetProfileImage", botID, mock.Anything).Return((*model.AppError)(nil))
		api.On("RegisterPluginForClusterEvents").Return((*model.AppError)(nil)).Maybe()
		api.On("RegisterCommand", mock.Anything).Return(nil)
		api.On("UnregisterPluginForSharedChannels", mock.Anything).Return(nil).Maybe()

		// Upgrade migration KV calls.
		api.On("KVGet", mock.Anything).Return(nil, (*model.AppError)(nil)).Maybe()
		api.On("KVList", mock.Anything, mock.Anything).Return([]string{}, (*model.AppError)(nil)).Maybe()
		api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(true, (*model.AppError)(nil)).Maybe()

		p := &Plugin{}
		p.SetAPI(api)
		// Set empty configuration so connectOutbound/connectInbound do nothing.
		p.configuration = &configuration{}

		err := p.OnActivate()
		require.NoError(t, err)
		assert.Equal(t, botID, p.botUserID)
		assert.NotNil(t, p.kvstore)
		assert.NotNil(t, p.ctx)
		assert.NotEmpty(t, p.nodeID)

		// Clean up goroutines started by OnActivate.
		err = p.OnDeactivate()
		require.NoError(t, err)
	})

	t.Run("set_profile_image_fails", func(t *testing.T) {
		api := &plugintest.API{}
		defaultLogMocks(api)
		botID := model.NewId()
		mockEnsureBotSuccess(api, botID)

		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "assets"), 0o750))
		require.NoError(t, os.WriteFile(
			filepath.Join(tmpDir, "assets", "crossguard.png"),
			[]byte("fake-png-bytes"),
			0o600,
		))

		api.On("GetBundlePath").Return(tmpDir, nil)
		api.On("SetProfileImage", botID, mock.Anything).
			Return(&model.AppError{Message: "bad profile image"})
		defer api.AssertExpectations(t)

		p := &Plugin{}
		p.SetAPI(api)

		err := p.OnActivate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set bot profile image")
	})
}
