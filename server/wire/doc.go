// Package wire defines the on-the-wire types used by the Cross Guard
// plugin for cross-domain message relay. They mirror a pruned subset of
// the Mattermost shared-channels content types (SyncMsg, Post, User,
// Reaction, Status, MembershipChange, PostAcknowledgement) but are
// owned by the plugin, not the upstream model.
//
// Why the plugin owns these types:
//
//  1. Compliance review. The XSD that ships to security review enumerates
//     every field that crosses a domain boundary. Tracking upstream
//     directly is a maintenance treadmill that breaks on every model
//     version bump; using xs:any placeholders obscures fields. Owning the
//     types lets us emit a strict, stable schema.
//
//  2. Privilege containment. Fields like User.MfaActive, User.LastPasswordUpdate,
//     User.NotifyProps and Post.Props["force_notification"] carry data
//     that should not cross domains. The receiver's sanitizeUserForSync
//     wipes some of them on arrival, but the bytes still appear on the
//     wire and in inspection logs. Pruning at the wire layer prevents
//     them from being emitted in the first place.
//
//  3. Bandwidth and review surface. Fields the receiver computes from
//     its own database (Post.ReplyCount, Post.LastReplyAt, Post.Participants)
//     are sent and immediately discarded. Dropping them shrinks the wire
//     payload and the surface a reviewer has to reason about.
//
// Each wire type exposes two conversions:
//
//	func XFromModel(*mmModel.X) *X   // outbound: upstream -> wire
//	func (x *X) ToModel() *mmModel.X // inbound: wire -> upstream
//
// Both are explicit field-by-field copies. No reflection, no tag tricks.
// The whole point is that adding or dropping a field requires editing
// both directions by hand, which is exactly when the security review
// should fire.
//
// Dependency direction: package wire imports encoding/xml and
// github.com/mattermost/mattermost/server/public/model (the latter only
// inside *FromModel / *ToModel constructors). It must not import any
// other plugin-internal package.
//
// Upstream model changes silently drop new fields. If Mattermost adds,
// say, User.PhoneNumber, our UserFromModel will not carry it. That is
// the design, not a bug. Do not "fix" it by mirroring upstream blindly;
// add the field to the wire type only after the compliance review
// approves the new XSD revision.
package wire
