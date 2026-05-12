package wire

import (
	mmModel "github.com/mattermost/mattermost/server/public/model"
)

// Reaction is the wire representation of an emoji reaction on a post.
// All upstream fields are carried; the type is already minimal.
type Reaction struct {
	UserId    string `xml:"UserId"`
	PostId    string `xml:"PostId"`
	EmojiName string `xml:"EmojiName"`
	CreateAt  int64  `xml:"CreateAt"`
	UpdateAt  int64  `xml:"UpdateAt"`
	DeleteAt  int64  `xml:"DeleteAt"`
	RemoteId  string `xml:"RemoteId,omitempty"`
	ChannelId string `xml:"ChannelId"`
}

// ReactionFromModel converts an upstream Reaction to its wire form.
// Returns nil if the input is nil.
func ReactionFromModel(r *mmModel.Reaction) *Reaction {
	if r == nil {
		return nil
	}
	out := &Reaction{
		UserId:    r.UserId,
		PostId:    r.PostId,
		EmojiName: r.EmojiName,
		CreateAt:  r.CreateAt,
		UpdateAt:  r.UpdateAt,
		DeleteAt:  r.DeleteAt,
		ChannelId: r.ChannelId,
	}
	if r.RemoteId != nil {
		out.RemoteId = *r.RemoteId
	}
	return out
}

// ToModel converts the wire Reaction back to the upstream form.
// Returns nil if the receiver is nil.
func (r *Reaction) ToModel() *mmModel.Reaction {
	if r == nil {
		return nil
	}
	out := &mmModel.Reaction{
		UserId:    r.UserId,
		PostId:    r.PostId,
		EmojiName: r.EmojiName,
		CreateAt:  r.CreateAt,
		UpdateAt:  r.UpdateAt,
		DeleteAt:  r.DeleteAt,
		ChannelId: r.ChannelId,
	}
	if r.RemoteId != "" {
		rid := r.RemoteId
		out.RemoteId = &rid
	}
	return out
}
