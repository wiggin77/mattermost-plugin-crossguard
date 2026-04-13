package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// startEmbeddedNATS starts an in-process NATS server on a random port for testing.
// Returns the server URL and a cleanup function. The server is automatically
// stopped when the test completes.
func startEmbeddedNATS(t *testing.T) string {
	t.Helper()
	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1, // random available port
		NoLog:     true,
		NoSigs:    true,
		JetStream: true,
		StoreDir:  t.TempDir(),
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to create embedded NATS server: %v", err)
	}
	srv.Start()
	if !srv.ReadyForConnections(5 * natsConnectTimeout) {
		t.Fatal("embedded NATS server not ready")
	}
	t.Cleanup(func() { srv.Shutdown() })
	return fmt.Sprintf("nats://127.0.0.1:%d", opts.Port)
}

// connectToEmbeddedNATS creates a natsProvider connected to the embedded test server.
func connectToEmbeddedNATS(t *testing.T, addr, subject string) *natsProvider {
	t.Helper()
	nc, err := nats.Connect(addr, nats.Timeout(natsConnectTimeout))
	if err != nil {
		t.Fatalf("failed to connect to embedded NATS: %v", err)
	}
	t.Cleanup(func() { nc.Close() })
	return &natsProvider{
		nc:      nc,
		subject: subject,
	}
}

// flexibleKVStore extends testKVStore with configurable function overrides.
// When a function pointer is nil, it delegates to the embedded testKVStore.
type flexibleKVStore struct {
	*testKVStore
	getTeamConnectionsFn            func(string) ([]store.TeamConnection, error)
	addTeamConnectionFn             func(string, store.TeamConnection) error
	removeTeamConnectionFn          func(string, store.TeamConnection) error
	getInitializedTeamIDsFn         func() ([]string, error)
	addInitializedTeamIDFn          func(string) error
	removeInitializedTeamIDFn       func(string) error
	getChannelConnectionsFn         func(string) ([]store.TeamConnection, error)
	addChannelConnectionFn          func(string, store.TeamConnection) error
	removeChannelConnectionFn       func(string, store.TeamConnection) error
	deleteChannelConnectionsFn      func(string) error
	setTeamConnectionsFn            func(string, []store.TeamConnection) error
	getConnectionPromptFn           func(string, string) (*store.ConnectionPrompt, error)
	setConnectionPromptFn           func(string, string, *store.ConnectionPrompt) error
	deleteConnectionPromptFn        func(string, string) error
	createConnectionPromptFn        func(string, string, *store.ConnectionPrompt) (bool, error)
	getChannelConnectionPromptFn    func(string, string) (*store.ConnectionPrompt, error)
	setChannelConnectionPromptFn    func(string, string, *store.ConnectionPrompt) error
	deleteChannelConnectionPromptFn func(string, string) error
	createChannelConnectionPromptFn func(string, string, *store.ConnectionPrompt) (bool, error)
	getTeamRewriteIndexFn           func(string, string) (string, error)
	setTeamRewriteIndexFn           func(string, string, string) error
	deleteTeamRewriteIndexFn        func(string, string) error
	isDeletingFlagSetFn             func(string) (bool, error)
	getConnectionRequestFn          func(string, string) (*store.ConnectionRequest, error)
	createConnectionRequestFn       func(string, string, *store.ConnectionRequest) (bool, error)
	deleteConnectionRequestFn       func(string, string) error
}

func (s *flexibleKVStore) GetTeamConnections(teamID string) ([]store.TeamConnection, error) {
	if s.getTeamConnectionsFn != nil {
		return s.getTeamConnectionsFn(teamID)
	}
	return s.testKVStore.GetTeamConnections(teamID)
}

func (s *flexibleKVStore) AddTeamConnection(teamID string, conn store.TeamConnection) error {
	if s.addTeamConnectionFn != nil {
		return s.addTeamConnectionFn(teamID, conn)
	}
	return s.testKVStore.AddTeamConnection(teamID, conn)
}

func (s *flexibleKVStore) RemoveTeamConnection(teamID string, conn store.TeamConnection) error {
	if s.removeTeamConnectionFn != nil {
		return s.removeTeamConnectionFn(teamID, conn)
	}
	return s.testKVStore.RemoveTeamConnection(teamID, conn)
}

func (s *flexibleKVStore) GetInitializedTeamIDs() ([]string, error) {
	if s.getInitializedTeamIDsFn != nil {
		return s.getInitializedTeamIDsFn()
	}
	return s.testKVStore.GetInitializedTeamIDs()
}

func (s *flexibleKVStore) AddInitializedTeamID(teamID string) error {
	if s.addInitializedTeamIDFn != nil {
		return s.addInitializedTeamIDFn(teamID)
	}
	return s.testKVStore.AddInitializedTeamID(teamID)
}

func (s *flexibleKVStore) RemoveInitializedTeamID(teamID string) error {
	if s.removeInitializedTeamIDFn != nil {
		return s.removeInitializedTeamIDFn(teamID)
	}
	return s.testKVStore.RemoveInitializedTeamID(teamID)
}

func (s *flexibleKVStore) GetChannelConnections(channelID string) ([]store.TeamConnection, error) {
	if s.getChannelConnectionsFn != nil {
		return s.getChannelConnectionsFn(channelID)
	}
	return s.testKVStore.GetChannelConnections(channelID)
}

func (s *flexibleKVStore) AddChannelConnection(channelID string, conn store.TeamConnection) error {
	if s.addChannelConnectionFn != nil {
		return s.addChannelConnectionFn(channelID, conn)
	}
	return s.testKVStore.AddChannelConnection(channelID, conn)
}

func (s *flexibleKVStore) RemoveChannelConnection(channelID string, conn store.TeamConnection) error {
	if s.removeChannelConnectionFn != nil {
		return s.removeChannelConnectionFn(channelID, conn)
	}
	return s.testKVStore.RemoveChannelConnection(channelID, conn)
}

func (s *flexibleKVStore) DeleteChannelConnections(channelID string) error {
	if s.deleteChannelConnectionsFn != nil {
		return s.deleteChannelConnectionsFn(channelID)
	}
	return s.testKVStore.DeleteChannelConnections(channelID)
}

func (s *flexibleKVStore) SetTeamConnections(teamID string, conns []store.TeamConnection) error {
	if s.setTeamConnectionsFn != nil {
		return s.setTeamConnectionsFn(teamID, conns)
	}
	return s.testKVStore.SetTeamConnections(teamID, conns)
}

func (s *flexibleKVStore) GetConnectionPrompt(teamID, connName string) (*store.ConnectionPrompt, error) {
	if s.getConnectionPromptFn != nil {
		return s.getConnectionPromptFn(teamID, connName)
	}
	return s.testKVStore.GetConnectionPrompt(teamID, connName)
}

func (s *flexibleKVStore) SetConnectionPrompt(teamID, connName string, prompt *store.ConnectionPrompt) error {
	if s.setConnectionPromptFn != nil {
		return s.setConnectionPromptFn(teamID, connName, prompt)
	}
	return s.testKVStore.SetConnectionPrompt(teamID, connName, prompt)
}

func (s *flexibleKVStore) DeleteConnectionPrompt(teamID, connName string) error {
	if s.deleteConnectionPromptFn != nil {
		return s.deleteConnectionPromptFn(teamID, connName)
	}
	return s.testKVStore.DeleteConnectionPrompt(teamID, connName)
}

func (s *flexibleKVStore) CreateConnectionPrompt(teamID, connName string, prompt *store.ConnectionPrompt) (bool, error) {
	if s.createConnectionPromptFn != nil {
		return s.createConnectionPromptFn(teamID, connName, prompt)
	}
	return s.testKVStore.CreateConnectionPrompt(teamID, connName, prompt)
}

func (s *flexibleKVStore) GetChannelConnectionPrompt(channelID, connName string) (*store.ConnectionPrompt, error) {
	if s.getChannelConnectionPromptFn != nil {
		return s.getChannelConnectionPromptFn(channelID, connName)
	}
	return s.testKVStore.GetChannelConnectionPrompt(channelID, connName)
}

func (s *flexibleKVStore) SetChannelConnectionPrompt(channelID, connName string, prompt *store.ConnectionPrompt) error {
	if s.setChannelConnectionPromptFn != nil {
		return s.setChannelConnectionPromptFn(channelID, connName, prompt)
	}
	return s.testKVStore.SetChannelConnectionPrompt(channelID, connName, prompt)
}

func (s *flexibleKVStore) DeleteChannelConnectionPrompt(channelID, connName string) error {
	if s.deleteChannelConnectionPromptFn != nil {
		return s.deleteChannelConnectionPromptFn(channelID, connName)
	}
	return s.testKVStore.DeleteChannelConnectionPrompt(channelID, connName)
}

func (s *flexibleKVStore) CreateChannelConnectionPrompt(channelID, connName string, prompt *store.ConnectionPrompt) (bool, error) {
	if s.createChannelConnectionPromptFn != nil {
		return s.createChannelConnectionPromptFn(channelID, connName, prompt)
	}
	return s.testKVStore.CreateChannelConnectionPrompt(channelID, connName, prompt)
}

func (s *flexibleKVStore) GetTeamRewriteIndex(connName, remoteTeamName string) (string, error) {
	if s.getTeamRewriteIndexFn != nil {
		return s.getTeamRewriteIndexFn(connName, remoteTeamName)
	}
	return s.testKVStore.GetTeamRewriteIndex(connName, remoteTeamName)
}

func (s *flexibleKVStore) SetTeamRewriteIndex(connName, remoteTeamName, localTeamID string) error {
	if s.setTeamRewriteIndexFn != nil {
		return s.setTeamRewriteIndexFn(connName, remoteTeamName, localTeamID)
	}
	return s.testKVStore.SetTeamRewriteIndex(connName, remoteTeamName, localTeamID)
}

func (s *flexibleKVStore) DeleteTeamRewriteIndex(connName, remoteTeamName string) error {
	if s.deleteTeamRewriteIndexFn != nil {
		return s.deleteTeamRewriteIndexFn(connName, remoteTeamName)
	}
	return s.testKVStore.DeleteTeamRewriteIndex(connName, remoteTeamName)
}

func (s *flexibleKVStore) IsDeletingFlagSet(postID string) (bool, error) {
	if s.isDeletingFlagSetFn != nil {
		return s.isDeletingFlagSetFn(postID)
	}
	return s.testKVStore.IsDeletingFlagSet(postID)
}

func (s *flexibleKVStore) GetConnectionRequest(teamID, connKey string) (*store.ConnectionRequest, error) {
	if s.getConnectionRequestFn != nil {
		return s.getConnectionRequestFn(teamID, connKey)
	}
	return nil, nil
}

func (s *flexibleKVStore) CreateConnectionRequest(teamID, connKey string, req *store.ConnectionRequest) (bool, error) {
	if s.createConnectionRequestFn != nil {
		return s.createConnectionRequestFn(teamID, connKey, req)
	}
	return true, nil
}

func (s *flexibleKVStore) DeleteConnectionRequest(teamID, connKey string) error {
	if s.deleteConnectionRequestFn != nil {
		return s.deleteConnectionRequestFn(teamID, connKey)
	}
	return nil
}

// setupTestPluginWithRouter creates a Plugin with the router initialized and
// a flexibleKVStore. Callers can override KV methods via the returned store.
func setupTestPluginWithRouter(api *plugintest.API) (*Plugin, *flexibleKVStore) {
	p := &Plugin{}
	p.SetAPI(api)
	p.botUserID = "bot-user-id"
	kvs := &flexibleKVStore{testKVStore: newTestKVStore()}
	p.kvstore = kvs
	ctx, cancel := context.WithCancel(context.Background())
	p.ctx = ctx
	p.cancel = cancel
	p.relaySem = make(chan struct{}, 50)
	p.fileSem = make(chan struct{}, 32)
	p.initAPI()
	return p, kvs
}

// makeAuthRequest builds an HTTP request suitable for plugin handler tests.
// If body is non-nil it is JSON-encoded. The Mattermost-User-Id header is set
// when userID is non-empty.
func makeAuthRequest(t *testing.T, method, path string, body any, userID string) *http.Request {
	t.Helper()
	var r *http.Request
	if body != nil {
		data, err := json.Marshal(body)
		assert.NoError(t, err)
		r = httptest.NewRequest(method, path, strings.NewReader(string(data)))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Content-Type", "application/json")
	if userID != "" {
		r.Header.Set("Mattermost-User-Id", userID)
	}
	return r
}

// decodeJSONResponse unmarshals the recorder body into a generic map.
func decodeJSONResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	return result
}

// Compile-time check that mockQueueProvider implements QueueProvider.
var _ QueueProvider = (*mockQueueProvider)(nil)

// mockQueueProvider is a test double for QueueProvider with configurable behavior.
type mockQueueProvider struct {
	publishFn    func(ctx context.Context, data []byte) error
	subscribeFn  func(ctx context.Context, handler func(data []byte) error) error
	uploadFileFn func(ctx context.Context, key string, data []byte, headers map[string]string) error
	watchFilesFn func(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error
	maxMsgSize   int
	closeFn      func() error
}

func (m *mockQueueProvider) Publish(ctx context.Context, data []byte) error {
	if m.publishFn != nil {
		return m.publishFn(ctx, data)
	}
	return nil
}

func (m *mockQueueProvider) Subscribe(ctx context.Context, handler func(data []byte) error) error {
	if m.subscribeFn != nil {
		return m.subscribeFn(ctx, handler)
	}
	return nil
}

func (m *mockQueueProvider) UploadFile(ctx context.Context, key string, data []byte, headers map[string]string) error {
	if m.uploadFileFn != nil {
		return m.uploadFileFn(ctx, key, data, headers)
	}
	return nil
}

func (m *mockQueueProvider) WatchFiles(ctx context.Context, handler func(key string, data []byte, headers map[string]string) error) error {
	if m.watchFilesFn != nil {
		return m.watchFilesFn(ctx, handler)
	}
	return nil
}

func (m *mockQueueProvider) MaxMessageSize() int {
	return m.maxMsgSize
}

func (m *mockQueueProvider) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}
