//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// CreatePost posts a message in the given channel using the supplied client.
// Most callers want CreatePostAs which logs in a user implicitly.
func CreatePost(t *testing.T, client *model.Client4, channelID, message string) *model.Post {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, _, err := client.CreatePost(ctx, &model.Post{
		ChannelId: channelID,
		Message:   message,
	})
	if err != nil {
		t.Fatalf("CreatePost(channel=%s): %v", channelID, err)
	}
	return p
}

// EditPostMessage rewrites the message text of postID using PatchPost.
func EditPostMessage(t *testing.T, client *model.Client4, postID, newMessage string) *model.Post {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, _, err := client.PatchPost(ctx, postID, &model.PostPatch{Message: &newMessage})
	if err != nil {
		t.Fatalf("PatchPost(%s): %v", postID, err)
	}
	return p
}

// DeletePostID removes postID. Fails the test on any non-204 error.
func DeletePostID(t *testing.T, client *model.Client4, postID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.DeletePost(ctx, postID); err != nil {
		t.Fatalf("DeletePost(%s): %v", postID, err)
	}
}

// React adds a reaction.
func React(t *testing.T, client *model.Client4, userID, postID, emoji string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := client.SaveReaction(ctx, &model.Reaction{
		UserId:    userID,
		PostId:    postID,
		EmojiName: emoji,
	}); err != nil {
		t.Fatalf("SaveReaction(%s on %s by %s): %v", emoji, postID, userID, err)
	}
}

// Unreact removes a reaction.
func Unreact(t *testing.T, client *model.Client4, userID, postID, emoji string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.DeleteReaction(ctx, &model.Reaction{
		UserId:    userID,
		PostId:    postID,
		EmojiName: emoji,
	}); err != nil {
		t.Fatalf("DeleteReaction(%s on %s by %s): %v", emoji, postID, userID, err)
	}
}

// FindRelayedPost polls the destination channel for a post whose message
// contains the marker. Returns the matched post.
func (h *Harness) FindRelayedPost(t *testing.T, dest Server, channelID, marker string, timeout time.Duration) *model.Post {
	t.Helper()
	client := h.AdminFor(dest)
	return Eventually(t, timeout, "relayed post containing "+marker, func() (*model.Post, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		posts, _, err := client.GetPostsForChannel(ctx, channelID, 0, 20, "", false, false)
		if err != nil {
			return nil, false
		}
		for _, p := range posts.Posts {
			if strings.Contains(p.Message, marker) {
				return p, true
			}
		}
		return nil, false
	})
}

// HasReaction reports whether postID on the given server currently has any
// reaction with the given emoji name.
func (h *Harness) HasReaction(t *testing.T, s Server, postID, emoji string) bool {
	t.Helper()
	client := h.AdminFor(s)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rs, _, err := client.GetReactions(ctx, postID)
	if err != nil {
		return false
	}
	for _, r := range rs {
		if r.EmojiName == emoji {
			return true
		}
	}
	return false
}

// GetPostIncludingDeleted fetches a post by id, treating delete_at>0 posts as
// returnable instead of 404. Useful for verifying soft-deletes propagated.
func GetPostIncludingDeleted(t *testing.T, client *model.Client4, postID string) *model.Post {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, _, err := client.GetPostIncludeDeleted(ctx, postID, "")
	if err != nil {
		t.Fatalf("GetPostIncludeDeleted(%s): %v", postID, err)
	}
	return p
}
