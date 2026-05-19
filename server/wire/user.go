package wire

import (
	"strings"

	mmModel "github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
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
//
// RemoteUsername and RemoteEmail are validated against the wire schema's
// patterns. A non-conforming value drops the prop key only (the user
// record itself stays) and emits a Warn-level audit log via the
// supplied WireLogger.
func UserPropsFromModel(m mmModel.StringMap, log WireLogger, userID string) *UserProps {
	if len(m) == 0 {
		return nil
	}
	if log == nil {
		log = nopLogger{}
	}
	out := &UserProps{
		CustomStatus:     m[userPropCustomStatus],
		OriginalRemoteId: m[userPropOriginalRemoteId],
	}
	if v := m[userPropRemoteUsername]; v != "" {
		if usernamePattern.MatchString(v) {
			out.RemoteUsername = v
		} else {
			log.Warn(errcode.WireUserPropDroppedNonConforming,
				"user_id", userID, "prop", userPropRemoteUsername)
		}
	}
	if v := m[userPropRemoteEmail]; v != "" {
		if emailPattern.MatchString(v) {
			out.RemoteEmail = v
		} else {
			log.Warn(errcode.WireUserPropDroppedNonConforming,
				"user_id", userID, "prop", userPropRemoteEmail)
		}
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
//
// Username and Email are validated against the constrained wire
// schema's patterns. Username runs the resolve-then-truncate ladder
// (see resolveWireUsername); if it cannot yield a conforming value,
// or Email fails the email pattern, the whole user record is dropped
// (returns nil) with an Error-level audit log via the supplied
// WireLogger. Callers (buildOutboundEnvelopes) already tolerate nil
// users; the post still ships and the receiver resolves identity
// through its own flow.
func UserFromModel(u *mmModel.User, log WireLogger) *User {
	if u == nil {
		return nil
	}
	if log == nil {
		log = nopLogger{}
	}

	wireUsername, ok := resolveWireUsername(u, log)
	if !ok {
		log.Error(errcode.WireUserDroppedNonConformingUsername, "user_id", u.Id)
		return nil
	}
	if !emailPattern.MatchString(u.Email) {
		log.Error(errcode.WireUserDroppedNonConformingEmail, "user_id", u.Id)
		return nil
	}

	out := &User{
		Id:                u.Id,
		CreateAt:          u.CreateAt,
		UpdateAt:          u.UpdateAt,
		DeleteAt:          u.DeleteAt,
		Username:          wireUsername,
		Email:             u.Email,
		Nickname:          u.Nickname,
		FirstName:         u.FirstName,
		LastName:          u.LastName,
		Position:          u.Position,
		Roles:             u.Roles,
		Locale:            u.Locale,
		Timezone:          StringMap(u.Timezone),
		Props:             UserPropsFromModel(u.Props, log, u.Id),
		IsBot:             u.IsBot,
		BotDescription:    u.BotDescription,
		BotLastIconUpdate: u.BotLastIconUpdate,
	}
	if u.RemoteId != nil {
		out.RemoteId = *u.RemoteId
	}
	return out
}

// resolveWireUsername implements the Username resolve-then-truncate
// ladder from implementation-plans/26-05-17-01-constrained-xsd-fixes.md:
//
//  1. As-is. If u.Username matches UsernameType, use it.
//  2. Resolve via RemoteUsername prop. If u.Username doesn't match
//     and u.Props["RemoteUsername"] is non-empty and conforming, use
//     that. Normal path for users that originated on another server;
//     no log.
//  3. Truncate at the first colon. If u.Username contains ':',
//     take everything before the first colon. If the candidate
//     conforms, use it. Warn-level log (indicates RemoteUsername
//     prop was missing or itself non-conforming).
//  4. Otherwise the ladder fails (ok=false); the caller drops the
//     user record with an Error-level log.
func resolveWireUsername(u *mmModel.User, log WireLogger) (string, bool) {
	if usernamePattern.MatchString(u.Username) {
		return u.Username, true
	}
	if v := u.Props[userPropRemoteUsername]; v != "" && usernamePattern.MatchString(v) {
		return v, true
	}
	if i := strings.IndexByte(u.Username, ':'); i > 0 {
		candidate := u.Username[:i]
		if usernamePattern.MatchString(candidate) {
			log.Warn(errcode.WireUsernameTruncatedAtColon, "user_id", u.Id)
			return candidate, true
		}
	}
	return "", false
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
