package wire

import (
	mmModel "github.com/mattermost/mattermost/server/public/model"
)

// MembershipChange is the wire representation of a channel membership
// add or remove event. All upstream fields are carried; the Go and XML
// element name drops the redundant "Msg" suffix from upstream's
// MembershipChangeMsg.
type MembershipChange struct {
	ChannelId  string `xml:"ChannelId"`
	UserId     string `xml:"UserId"`
	IsAdd      bool   `xml:"IsAdd"`
	RemoteId   string `xml:"RemoteId,omitempty"`
	ChangeTime int64  `xml:"ChangeTime"`
}

// MembershipChangeFromModel converts an upstream MembershipChangeMsg
// to its wire form. Returns nil if the input is nil.
func MembershipChangeFromModel(m *mmModel.MembershipChangeMsg) *MembershipChange {
	if m == nil {
		return nil
	}
	return &MembershipChange{
		ChannelId:  m.ChannelId,
		UserId:     m.UserId,
		IsAdd:      m.IsAdd,
		RemoteId:   m.RemoteId,
		ChangeTime: m.ChangeTime,
	}
}

// ToModel converts the wire MembershipChange back to the upstream
// MembershipChangeMsg form. Returns nil if the receiver is nil.
func (m *MembershipChange) ToModel() *mmModel.MembershipChangeMsg {
	if m == nil {
		return nil
	}
	return &mmModel.MembershipChangeMsg{
		ChannelId:  m.ChannelId,
		UserId:     m.UserId,
		IsAdd:      m.IsAdd,
		RemoteId:   m.RemoteId,
		ChangeTime: m.ChangeTime,
	}
}
