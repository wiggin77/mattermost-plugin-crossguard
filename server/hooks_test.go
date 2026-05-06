package main

import (
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
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
