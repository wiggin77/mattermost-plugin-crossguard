//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// RepoRoot returns the absolute path of the repository root. The integration
// tests address fixtures (testdata/*) via paths relative to this anchor so
// they work regardless of where `go test` is invoked from.
func RepoRoot(t *testing.T) string {
	t.Helper()
	// This file lives at server/integration/file.go; go up two levels.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

// RepoPath joins one or more path components onto RepoRoot.
func RepoPath(t *testing.T, parts ...string) string {
	t.Helper()
	return filepath.Join(append([]string{RepoRoot(t)}, parts...)...)
}

// UploadFile reads a file relative to the repo root and uploads it to the
// channel as the supplied client. Returns the file id assigned by the
// server. The shell tests used `testdata/sample.pdf` etc.; callers should
// pass the same kind of path.
func UploadFile(t *testing.T, client *model.Client4, channelID, repoRelPath string) string {
	t.Helper()
	absPath := RepoPath(t, repoRelPath)
	// Test fixture path resolved from a controlled prefix (repo root).
	data, err := os.ReadFile(absPath) //nolint:gosec // G304: path anchored to repo root
	if err != nil {
		t.Fatalf("read %s: %v", absPath, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, _, err := client.UploadFile(ctx, data, channelID, filepath.Base(repoRelPath))
	if err != nil {
		t.Fatalf("UploadFile %s: %v", repoRelPath, err)
	}
	if len(resp.FileInfos) == 0 {
		t.Fatalf("UploadFile %s: no file_infos returned", repoRelPath)
	}
	return resp.FileInfos[0].Id
}

// CreatePostWithFile is a convenience wrapper that creates a post with one
// attached file id.
func CreatePostWithFile(t *testing.T, client *model.Client4, channelID, message, fileID string) *model.Post {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, _, err := client.CreatePost(ctx, &model.Post{
		ChannelId: channelID,
		Message:   message,
		FileIds:   []string{fileID},
	})
	if err != nil {
		t.Fatalf("CreatePost(file): %v", err)
	}
	return p
}

// SetProfileImage uploads a new avatar for the user via Client4.
func SetProfileImage(t *testing.T, client *model.Client4, userID string, png []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := client.SetProfileImage(ctx, userID, png); err != nil {
		t.Fatalf("SetProfileImage(%s): %v", userID, err)
	}
}
