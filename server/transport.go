package main

import (
	"encoding/xml"
	"fmt"

	mmModel "github.com/mattermost/mattermost/server/public/model"
)

// Transport message types for the wire envelope.
const (
	TransportTypeSyncMsg      = "sync_msg"
	TransportTypeAttachment   = "attachment"
	TransportTypeProfileImage = "profile_image"
	TransportTypeTest         = "test"
)

// TransportEnvelope wraps content for XML wire transport between servers.
// Exactly one of SyncMsg or TestID is populated, depending on Type.
type TransportEnvelope struct {
	XMLName     xml.Name         `xml:"CrossGuardEnvelope"`
	Version     int              `xml:"version,attr"`
	Type        string           `xml:"type,attr"`
	ConnName    string           `xml:"ConnName"`
	TeamName    string           `xml:"TeamName"`
	ChannelName string           `xml:"ChannelName"`
	SyncMsg     *mmModel.SyncMsg `xml:"SyncMsg,omitempty"`
	TestID      string           `xml:"TestID,omitempty"`
}

// MarshalEnvelope serializes a TransportEnvelope to XML with the standard header.
func MarshalEnvelope(env *TransportEnvelope) ([]byte, error) {
	if env == nil {
		return nil, fmt.Errorf("envelope is nil")
	}
	if env.Version == 0 {
		env.Version = 1
	}
	data, err := xml.Marshal(env)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}

// UnmarshalEnvelope deserializes XML bytes into a TransportEnvelope.
func UnmarshalEnvelope(data []byte) (*TransportEnvelope, error) {
	var env TransportEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// buildSyncResponse constructs the SyncResponse acknowledging which timestamps
// have been processed for the given SyncMsg.
func buildSyncResponse(msg *mmModel.SyncMsg) mmModel.SyncResponse {
	resp := mmModel.SyncResponse{}
	if msg == nil {
		return resp
	}

	for _, user := range msg.Users {
		if user != nil && user.UpdateAt > resp.UsersLastUpdateAt {
			resp.UsersLastUpdateAt = user.UpdateAt
		}
	}
	for _, post := range msg.Posts {
		if post != nil && post.UpdateAt > resp.PostsLastUpdateAt {
			resp.PostsLastUpdateAt = post.UpdateAt
		}
	}
	for _, reaction := range msg.Reactions {
		if reaction != nil && reaction.UpdateAt > resp.ReactionsLastUpdateAt {
			resp.ReactionsLastUpdateAt = reaction.UpdateAt
		}
	}
	for _, ack := range msg.Acknowledgements {
		if ack != nil && ack.AcknowledgedAt > resp.AcknowledgementsLastUpdateAt {
			resp.AcknowledgementsLastUpdateAt = ack.AcknowledgedAt
		}
	}

	return resp
}

// splitTransportEnvelope splits the SyncMsg portion of an envelope into
// multiple envelopes, each fitting within maxSize bytes when serialized.
//
// When the envelope already fits, it is returned as-is. If maxSize <= 0
// (provider has no limit), it is returned as-is.
//
// The Users map is duplicated into each split envelope to preserve sender
// context for the posts. Reactions and Acknowledgements are placed only in
// the envelope containing their associated post.
//
// If the envelope holds no posts (users-only sync) or a single post is too
// large on its own, it is sent as one envelope. The provider handles
// individual oversize messages.
func splitTransportEnvelope(env *TransportEnvelope, maxSize int) ([]*TransportEnvelope, error) {
	if env == nil {
		return nil, fmt.Errorf("envelope is nil")
	}

	data, err := MarshalEnvelope(env)
	if err != nil {
		return nil, err
	}
	if maxSize <= 0 || len(data) <= maxSize {
		return []*TransportEnvelope{env}, nil
	}

	if env.SyncMsg == nil || len(env.SyncMsg.Posts) <= 1 {
		return []*TransportEnvelope{env}, nil
	}

	posts := env.SyncMsg.Posts
	postIDs := make(map[string]struct{}, len(posts))
	for _, p := range posts {
		if p != nil {
			postIDs[p.Id] = struct{}{}
		}
	}

	// Pre-bucket reactions and acknowledgements by post ID, so we can build
	// each split's slices without rescanning the originals every time.
	reactionsByPost := make(map[string][]*mmModel.Reaction)
	for _, r := range env.SyncMsg.Reactions {
		if r == nil {
			continue
		}
		reactionsByPost[r.PostId] = append(reactionsByPost[r.PostId], r)
	}
	acksByPost := make(map[string][]*mmModel.PostAcknowledgement)
	for _, a := range env.SyncMsg.Acknowledgements {
		if a == nil {
			continue
		}
		acksByPost[a.PostId] = append(acksByPost[a.PostId], a)
	}

	splits := make([]*TransportEnvelope, 0)
	idx := 0
	for idx < len(posts) {
		// Try the largest remaining slice first, halving until it fits or
		// shrinks to a single post (which we then send unconditionally).
		count := len(posts) - idx
		for count > 1 {
			candidate := buildSplitEnvelope(env, posts[idx:idx+count], reactionsByPost, acksByPost)
			candidateData, marshalErr := MarshalEnvelope(candidate)
			if marshalErr != nil {
				return nil, marshalErr
			}
			if len(candidateData) <= maxSize {
				break
			}
			count /= 2
		}
		split := buildSplitEnvelope(env, posts[idx:idx+count], reactionsByPost, acksByPost)
		splits = append(splits, split)
		idx += count
	}

	return splits, nil
}

func buildSplitEnvelope(
	src *TransportEnvelope,
	posts []*mmModel.Post,
	reactionsByPost map[string][]*mmModel.Reaction,
	acksByPost map[string][]*mmModel.PostAcknowledgement,
) *TransportEnvelope {
	subMsg := &mmModel.SyncMsg{
		Id:                src.SyncMsg.Id,
		ChannelId:         src.SyncMsg.ChannelId,
		Users:             src.SyncMsg.Users,
		MentionTransforms: src.SyncMsg.MentionTransforms,
		Posts:             posts,
	}

	for _, p := range posts {
		if p == nil {
			continue
		}
		if rs := reactionsByPost[p.Id]; len(rs) > 0 {
			subMsg.Reactions = append(subMsg.Reactions, rs...)
		}
		if as := acksByPost[p.Id]; len(as) > 0 {
			subMsg.Acknowledgements = append(subMsg.Acknowledgements, as...)
		}
	}

	return &TransportEnvelope{
		Version:     src.Version,
		Type:        src.Type,
		ConnName:    src.ConnName,
		TeamName:    src.TeamName,
		ChannelName: src.ChannelName,
		SyncMsg:     subMsg,
		TestID:      src.TestID,
	}
}
