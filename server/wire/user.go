package wire

import (
	mmModel "github.com/mattermost/mattermost/server/public/model"
)

// User.Props upstream keys carried on the wire. Defined as constants
// so the FromModel/ToModel mappings have a single source of truth.
const (
	userPropCustomStatus     = "customStatus"
	userPropRemoteUsername   = "RemoteUsername"
	userPropRemoteEmail      = "RemoteEmail"
	userPropOriginalRemoteId = "OriginalRemoteId"
)

// UserProps is the typed, pruned replacement for the upstream
// User.Props StringMap. Only keys that the framework or compliance
// review have approved cross domain boundaries are represented.
type UserProps struct {
	CustomStatus     string `xml:"CustomStatus,omitempty"`
	RemoteUsername   string `xml:"RemoteUsername,omitempty"`
	RemoteEmail      string `xml:"RemoteEmail,omitempty"`
	OriginalRemoteId string `xml:"OriginalRemoteId,omitempty"`
}

// UserPropsFromModel extracts the whitelisted keys from an upstream
// StringMap. Returns nil if no whitelisted key is populated, so an
// empty Props produces an omitted XML element rather than a self-closer.
func UserPropsFromModel(m mmModel.StringMap) *UserProps {
	if len(m) == 0 {
		return nil
	}
	out := &UserProps{
		CustomStatus:     m[userPropCustomStatus],
		RemoteUsername:   m[userPropRemoteUsername],
		RemoteEmail:      m[userPropRemoteEmail],
		OriginalRemoteId: m[userPropOriginalRemoteId],
	}
	if out.CustomStatus == "" && out.RemoteUsername == "" && out.RemoteEmail == "" && out.OriginalRemoteId == "" {
		return nil
	}
	return out
}

// ToModel converts the typed Props back to an upstream StringMap.
// Returns nil if the receiver is nil.
func (p *UserProps) ToModel() mmModel.StringMap {
	if p == nil {
		return nil
	}
	out := mmModel.StringMap{}
	if p.CustomStatus != "" {
		out[userPropCustomStatus] = p.CustomStatus
	}
	if p.RemoteUsername != "" {
		out[userPropRemoteUsername] = p.RemoteUsername
	}
	if p.RemoteEmail != "" {
		out[userPropRemoteEmail] = p.RemoteEmail
	}
	if p.OriginalRemoteId != "" {
		out[userPropOriginalRemoteId] = p.OriginalRemoteId
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// User is the wire representation of a Mattermost user. Fields the
// receiver wipes via sanitizeUserForSync (AuthService, AllowMarketing,
// NotifyProps, LastPasswordUpdate, LastPictureUpdate, FailedAttempts,
// MfaActive) and local-only fields (TermsOfServiceId,
// DisableWelcomeEmail, LastLogin) are dropped.
type User struct {
	Id                string     `xml:"Id"`
	CreateAt          int64      `xml:"CreateAt,omitempty"`
	UpdateAt          int64      `xml:"UpdateAt,omitempty"`
	DeleteAt          int64      `xml:"DeleteAt"`
	Username          string     `xml:"Username"`
	Email             string     `xml:"Email"`
	Nickname          string     `xml:"Nickname,omitempty"`
	FirstName         string     `xml:"FirstName,omitempty"`
	LastName          string     `xml:"LastName,omitempty"`
	Position          string     `xml:"Position,omitempty"`
	Roles             string     `xml:"Roles"`
	Locale            string     `xml:"Locale,omitempty"`
	Timezone          StringMap  `xml:"Timezone,omitempty"`
	Props             *UserProps `xml:"Props,omitempty"`
	RemoteId          string     `xml:"RemoteId,omitempty"`
	IsBot             bool       `xml:"IsBot,omitempty"`
	BotDescription    string     `xml:"BotDescription,omitempty"`
	BotLastIconUpdate int64      `xml:"BotLastIconUpdate,omitempty"`
}

// UserFromModel converts an upstream User to its wire form. Dropped
// fields (AuthService, EmailVerified, AllowMarketing, NotifyProps,
// LastPasswordUpdate, LastPictureUpdate, FailedAttempts, MfaActive,
// LastActivityAt, TermsOfServiceId, TermsOfServiceCreateAt,
// DisableWelcomeEmail, LastLogin, plus credential fields already
// xml:"-" upstream) do not cross.
func UserFromModel(u *mmModel.User) *User {
	if u == nil {
		return nil
	}
	out := &User{
		Id:                u.Id,
		CreateAt:          u.CreateAt,
		UpdateAt:          u.UpdateAt,
		DeleteAt:          u.DeleteAt,
		Username:          u.Username,
		Email:             u.Email,
		Nickname:          u.Nickname,
		FirstName:         u.FirstName,
		LastName:          u.LastName,
		Position:          u.Position,
		Roles:             u.Roles,
		Locale:            u.Locale,
		Timezone:          StringMap(u.Timezone),
		Props:             UserPropsFromModel(u.Props),
		IsBot:             u.IsBot,
		BotDescription:    u.BotDescription,
		BotLastIconUpdate: u.BotLastIconUpdate,
	}
	if u.RemoteId != nil {
		out.RemoteId = *u.RemoteId
	}
	return out
}

// ToModel converts the wire User back to the upstream form. Dropped
// fields are left as zero values. Returns nil if the receiver is nil.
func (u *User) ToModel() *mmModel.User {
	if u == nil {
		return nil
	}
	out := &mmModel.User{
		Id:                u.Id,
		CreateAt:          u.CreateAt,
		UpdateAt:          u.UpdateAt,
		DeleteAt:          u.DeleteAt,
		Username:          u.Username,
		Email:             u.Email,
		Nickname:          u.Nickname,
		FirstName:         u.FirstName,
		LastName:          u.LastName,
		Position:          u.Position,
		Roles:             u.Roles,
		Locale:            u.Locale,
		Timezone:          mmModel.StringMap(u.Timezone),
		Props:             u.Props.ToModel(),
		IsBot:             u.IsBot,
		BotDescription:    u.BotDescription,
		BotLastIconUpdate: u.BotLastIconUpdate,
	}
	if u.RemoteId != "" {
		rid := u.RemoteId
		out.RemoteId = &rid
	}
	return out
}
