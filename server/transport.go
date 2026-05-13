package main

import (
	"encoding/xml"
	"fmt"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
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
// The envelope-level fields (Version, Type, ConnName, Timestamp, Epoch,
// Sequence, TeamName, ChannelName) are used by compliance content-inspection
// systems to route and audit messages without parsing the SyncMsg payload,
// so they are retained as documented in the wire-format plan even when the
// inner SyncMsg encoding follows the upstream Mattermost model layout.
//
// Epoch identifies the sender process generation (26-char Mattermost ID
// generated at OnActivate). Sequence is a per-(ConnName, channel) monotonic
// counter that lets receivers detect out-of-order delivery. Epoch is stamped
// on every sender-originated envelope (sync_msg and test). Sequence is
// stamped only on sync_msg envelopes since test envelopes have no channel
// scope.
type TransportEnvelope struct {
	XMLName     xml.Name      `xml:"CrossGuardEnvelope"`
	Version     int           `xml:"version,attr"`
	Type        string        `xml:"type,attr"`
	ConnName    string        `xml:"ConnName"`
	Timestamp   string        `xml:"Timestamp"`
	Epoch       string        `xml:"Epoch,omitempty"`
	Sequence    uint64        `xml:"Sequence,omitempty"`
	TeamName    string        `xml:"TeamName"`
	ChannelName string        `xml:"ChannelName"`
	SyncMsg     *wire.SyncMsg `xml:"SyncMsg,omitempty"`
	TestID      string        `xml:"TestID,omitempty"`
}

// MarshalEnvelope serializes a TransportEnvelope to XML with the standard header.
func MarshalEnvelope(env *TransportEnvelope) ([]byte, error) {
	if env == nil {
		return nil, fmt.Errorf("envelope is nil")
	}
	if env.Version == 0 {
		env.Version = 1
	}
	if env.Timestamp == "" {
		env.Timestamp = time.Now().UTC().Format(time.RFC3339)
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

// buildOutboundEnvelopes fans out an upstream SyncMsg into one envelope
// per post plus an optional metadata envelope. The unit of publication
// is one logical message so compliance content inspection rejects with
// minimum blast radius: a rejection drops only the offending post, not
// the entire sync cycle's worth of content.
//
// The fanout walks msg:
//
//   - Each post in msg.Posts produces one post envelope carrying the
//     post, its author (filtered out of msg.Users by UserId), reactions
//     on this post (filtered by PostId), acknowledgements on this post,
//     and the full MentionTransforms map.
//
//   - Remaining content (orphan reactions, orphan acks, all memberships,
//     all statuses, and the users referenced by any of those) produces
//     one metadata envelope. The metadata envelope has no <Post>
//     element and is therefore not subject to content-rejection at the
//     compliance tool. Emitted only when at least one non-post item
//     remains after the post fanout.
//
// When msg has no posts and no metadata-eligible content, returns a
// single envelope carrying just the SyncMsg identifiers so the
// upstream cursor still advances.
//
// templateEnv supplies the routing/audit fields (Version, Type,
// ConnName, Timestamp, TeamName, ChannelName). Its own SyncMsg field
// is ignored.
func buildOutboundEnvelopes(templateEnv *TransportEnvelope, msg *mmModel.SyncMsg) []*TransportEnvelope {
	if templateEnv == nil || msg == nil {
		return nil
	}

	// Pre-bucket reactions and acks by their referenced PostId.
	reactionsByPost := make(map[string][]*mmModel.Reaction)
	for _, r := range msg.Reactions {
		if r != nil {
			reactionsByPost[r.PostId] = append(reactionsByPost[r.PostId], r)
		}
	}
	acksByPost := make(map[string][]*mmModel.PostAcknowledgement)
	for _, a := range msg.Acknowledgements {
		if a != nil {
			acksByPost[a.PostId] = append(acksByPost[a.PostId], a)
		}
	}

	localPostIDs := make(map[string]struct{})
	out := make([]*TransportEnvelope, 0, len(msg.Posts)+1)
	for _, post := range msg.Posts {
		if post == nil {
			continue
		}
		localPostIDs[post.Id] = struct{}{}
		out = append(out, buildPostEnvelope(templateEnv, msg, post, reactionsByPost, acksByPost))
	}

	if meta := buildMetadataEnvelope(templateEnv, msg, localPostIDs); meta != nil {
		out = append(out, meta)
	}

	if len(out) == 0 {
		// No posts, no metadata-eligible content; emit a single bare
		// envelope so the framework's per-remote cursor still
		// advances on success.
		out = append(out, cloneEnvelopeHeader(templateEnv, &wire.SyncMsg{
			Id:        msg.Id,
			ChannelId: msg.ChannelId,
		}))
	}
	return out
}

// buildPostEnvelope constructs a single post envelope carrying the
// given post plus its causally-related metadata. The post's author is
// the only user inlined; other users that may have been in msg.Users
// (e.g., referenced by membership changes) belong on the metadata
// envelope.
func buildPostEnvelope(
	templateEnv *TransportEnvelope,
	msg *mmModel.SyncMsg,
	post *mmModel.Post,
	reactionsByPost map[string][]*mmModel.Reaction,
	acksByPost map[string][]*mmModel.PostAcknowledgement,
) *TransportEnvelope {
	subModel := &mmModel.SyncMsg{
		Id:                msg.Id,
		ChannelId:         msg.ChannelId,
		Posts:             []*mmModel.Post{post},
		Reactions:         reactionsByPost[post.Id],
		Acknowledgements:  acksByPost[post.Id],
		MentionTransforms: msg.MentionTransforms,
	}
	if author, ok := msg.Users[post.UserId]; ok {
		subModel.Users = map[string]*mmModel.User{post.UserId: author}
	}
	return cloneEnvelopeHeader(templateEnv, wire.SyncMsgFromModel(subModel))
}

// buildMetadataEnvelope constructs the optional metadata envelope for
// content that does not ride with a post: orphan reactions and acks
// (whose post is not in this sync cycle), all memberships, all
// statuses, and the users referenced by any of those. Returns nil
// when nothing in this category is present.
func buildMetadataEnvelope(templateEnv *TransportEnvelope, msg *mmModel.SyncMsg, localPostIDs map[string]struct{}) *TransportEnvelope {
	var orphanReactions []*mmModel.Reaction
	for _, r := range msg.Reactions {
		if r == nil {
			continue
		}
		if _, in := localPostIDs[r.PostId]; !in {
			orphanReactions = append(orphanReactions, r)
		}
	}
	var orphanAcks []*mmModel.PostAcknowledgement
	for _, a := range msg.Acknowledgements {
		if a == nil {
			continue
		}
		if _, in := localPostIDs[a.PostId]; !in {
			orphanAcks = append(orphanAcks, a)
		}
	}

	hasContent := len(orphanReactions) > 0 ||
		len(orphanAcks) > 0 ||
		len(msg.MembershipChanges) > 0 ||
		len(msg.Statuses) > 0
	if !hasContent {
		return nil
	}

	usersRef := make(map[string]struct{})
	for _, r := range orphanReactions {
		usersRef[r.UserId] = struct{}{}
	}
	for _, a := range orphanAcks {
		usersRef[a.UserId] = struct{}{}
	}
	for _, m := range msg.MembershipChanges {
		if m != nil {
			usersRef[m.UserId] = struct{}{}
		}
	}
	for _, s := range msg.Statuses {
		if s != nil {
			usersRef[s.UserId] = struct{}{}
		}
	}

	var users map[string]*mmModel.User
	for id := range usersRef {
		if u, ok := msg.Users[id]; ok {
			if users == nil {
				users = make(map[string]*mmModel.User)
			}
			users[id] = u
		}
	}

	subModel := &mmModel.SyncMsg{
		Id:                msg.Id,
		ChannelId:         msg.ChannelId,
		Users:             users,
		Reactions:         orphanReactions,
		Statuses:          msg.Statuses,
		MembershipChanges: msg.MembershipChanges,
		Acknowledgements:  orphanAcks,
	}
	return cloneEnvelopeHeader(templateEnv, wire.SyncMsgFromModel(subModel))
}

// cloneEnvelopeHeader copies the routing/audit fields from templateEnv
// into a new envelope and attaches the given SyncMsg. Sequence is left
// zero; publishToOutboundConn stamps a fresh per-channel Sequence on
// each envelope it publishes.
func cloneEnvelopeHeader(templateEnv *TransportEnvelope, subMsg *wire.SyncMsg) *TransportEnvelope {
	return &TransportEnvelope{
		Version:     templateEnv.Version,
		Type:        templateEnv.Type,
		ConnName:    templateEnv.ConnName,
		Timestamp:   templateEnv.Timestamp,
		Epoch:       templateEnv.Epoch,
		TeamName:    templateEnv.TeamName,
		ChannelName: templateEnv.ChannelName,
		SyncMsg:     subMsg,
		TestID:      templateEnv.TestID,
	}
}
