package wire

import (
	mmModel "github.com/mattermost/mattermost/server/public/model"
)

// Status is the wire representation of a user's presence status.
// ActiveChannel is intentionally dropped: the channel ID is local to
// the source server and has no meaning on the receiver.
type Status struct {
	UserId         string `xml:"UserId"`
	Status         string `xml:"Status"`
	Manual         bool   `xml:"Manual"`
	LastActivityAt int64  `xml:"LastActivityAt"`
	DNDEndTime     int64  `xml:"DNDEndTime"`
}

// StatusFromModel converts an upstream Status to its wire form.
// ActiveChannel is dropped. Returns nil if the input is nil.
func StatusFromModel(s *mmModel.Status) *Status {
	if s == nil {
		return nil
	}
	return &Status{
		UserId:         s.UserId,
		Status:         s.Status,
		Manual:         s.Manual,
		LastActivityAt: s.LastActivityAt,
		DNDEndTime:     s.DNDEndTime,
	}
}

// ToModel converts the wire Status back to the upstream form.
// ActiveChannel is left empty. Returns nil if the receiver is nil.
func (s *Status) ToModel() *mmModel.Status {
	if s == nil {
		return nil
	}
	return &mmModel.Status{
		UserId:         s.UserId,
		Status:         s.Status,
		Manual:         s.Manual,
		LastActivityAt: s.LastActivityAt,
		DNDEndTime:     s.DNDEndTime,
	}
}
