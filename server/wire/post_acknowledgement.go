package wire

import (
	mmModel "github.com/mattermost/mattermost/server/public/model"
)

// PostAcknowledgement is the wire representation of an acknowledgement
// of a post by a user. All upstream fields are carried.
type PostAcknowledgement struct {
	UserId         string `xml:"UserId"`
	PostId         string `xml:"PostId"`
	AcknowledgedAt int64  `xml:"AcknowledgedAt"`
	ChannelId      string `xml:"ChannelId"`
	RemoteId       string `xml:"RemoteId,omitempty"`
}

// PostAcknowledgementFromModel converts an upstream PostAcknowledgement
// to its wire form. Returns nil if the input is nil.
func PostAcknowledgementFromModel(a *mmModel.PostAcknowledgement) *PostAcknowledgement {
	if a == nil {
		return nil
	}
	out := &PostAcknowledgement{
		UserId:         a.UserId,
		PostId:         a.PostId,
		AcknowledgedAt: a.AcknowledgedAt,
		ChannelId:      a.ChannelId,
	}
	if a.RemoteId != nil {
		out.RemoteId = *a.RemoteId
	}
	return out
}

// ToModel converts the wire PostAcknowledgement back to the upstream
// model form. Returns nil if the receiver is nil.
func (a *PostAcknowledgement) ToModel() *mmModel.PostAcknowledgement {
	if a == nil {
		return nil
	}
	out := &mmModel.PostAcknowledgement{
		UserId:         a.UserId,
		PostId:         a.PostId,
		AcknowledgedAt: a.AcknowledgedAt,
		ChannelId:      a.ChannelId,
	}
	if a.RemoteId != "" {
		rid := a.RemoteId
		out.RemoteId = &rid
	}
	return out
}
