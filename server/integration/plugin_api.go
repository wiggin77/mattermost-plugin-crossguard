//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// PluginAPIPath builds an absolute URL to the crossguard plugin's HTTP API.
// Client4 routes are scoped to /api/v4/*, so plugin endpoints need to be
// hit directly. Path should start with "/".
func PluginAPIPath(s Server, path string) string {
	return fmt.Sprintf("%s/plugins/%s/api/v1%s", s.URL(), PluginID, path)
}

// PluginPOST sends a JSON-encoded POST to the plugin's HTTP API using the
// given authenticated client's session token. The response body is decoded
// into out when non-nil. A non-2xx status fails the test.
func PluginPOST(t *testing.T, client *model.Client4, s Server, path string, body, out any) {
	t.Helper()
	pluginRequest(t, client, "POST", s, path, body, out)
}

// PluginGET sends a GET to the plugin's HTTP API. See PluginPOST.
func PluginGET(t *testing.T, client *model.Client4, s Server, path string, out any) {
	t.Helper()
	pluginRequest(t, client, "GET", s, path, nil, out)
}

func pluginRequest(t *testing.T, client *model.Client4, method string, s Server, path string, body, out any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s body: %v", path, err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, PluginAPIPath(s, path), reqBody)
	if err != nil {
		t.Fatalf("new %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if client.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+client.AuthToken)
	}

	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s on %s: status %d: %s", method, path, s.Name, resp.StatusCode, string(raw))
	}

	if out == nil {
		return
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s %s response: %v", method, path, err)
	}
}
