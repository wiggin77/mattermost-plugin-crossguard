package wire

import (
	"encoding/xml"
	"maps"
	"sort"

	mmModel "github.com/mattermost/mattermost/server/public/model"
)

// UserMap is a userID-to-user map. It serializes as
// <Users><User id="...">...</User></Users>, sorted by key for
// deterministic output. The id attribute carries the map key, which
// is the same value as User.Id by convention.
type UserMap map[string]*User

// MentionTransforms is a map of source-server mention strings to their
// receiver-server replacements. It serializes as
// <MentionTransforms><Transform key="..." value="..."/></MentionTransforms>,
// sorted by key.
type MentionTransforms map[string]string

// SyncMsg is the wire representation of a Mattermost shared-channels
// sync message. It composes the seven inner content types and carries
// the channel-level identifiers (Id, ChannelId).
//
// SyncMsg has custom MarshalXML/UnmarshalXML so empty containers (no
// posts, no reactions, etc.) are not emitted as empty wrappers. The
// resulting wire format omits any container with no children, which
// the strict XSD declares with minOccurs=0.
type SyncMsg struct {
	Id        string
	ChannelId string
	Users     UserMap
	// Post is at most one per envelope. The sender's split policy emits
	// one envelope per post so compliance content-inspection tools can
	// reject the specific post that triggered classification without
	// dropping unrelated content as collateral. Non-post content
	// (reactions, acks, memberships, statuses) rides with the relevant
	// post or in a separate metadata envelope that has no Post field.
	Post              *Post
	Reactions         []*Reaction
	Statuses          []*Status
	MembershipChanges []*MembershipChange
	Acknowledgements  []*PostAcknowledgement
	MentionTransforms MentionTransforms
}

// MarshalXML implements xml.Marshaler. Element order matches the order
// of fields in SyncMsg; empty containers are suppressed.
func (m *SyncMsg) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	if err := encodeString(e, "Id", m.Id); err != nil {
		return err
	}
	if err := encodeString(e, "ChannelId", m.ChannelId); err != nil {
		return err
	}
	if len(m.Users) > 0 {
		if err := e.EncodeElement(m.Users, xml.StartElement{Name: xml.Name{Local: "Users"}}); err != nil {
			return err
		}
	}
	if m.Post != nil {
		if err := e.EncodeElement(m.Post, xml.StartElement{Name: xml.Name{Local: "Post"}}); err != nil {
			return err
		}
	}
	if err := encodeWrappedSlice(e, "Reactions", "Reaction", m.Reactions); err != nil {
		return err
	}
	if err := encodeWrappedSlice(e, "Statuses", "Status", m.Statuses); err != nil {
		return err
	}
	if err := encodeWrappedSlice(e, "MembershipChanges", "MembershipChange", m.MembershipChanges); err != nil {
		return err
	}
	if err := encodeWrappedSlice(e, "Acknowledgements", "PostAcknowledgement", m.Acknowledgements); err != nil {
		return err
	}
	if len(m.MentionTransforms) > 0 {
		if err := e.EncodeElement(m.MentionTransforms, xml.StartElement{Name: xml.Name{Local: "MentionTransforms"}}); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// UnmarshalXML implements xml.Unmarshaler.
func (m *SyncMsg) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "Id":
				if err := d.DecodeElement(&m.Id, &t); err != nil {
					return err
				}
			case "ChannelId":
				if err := d.DecodeElement(&m.ChannelId, &t); err != nil {
					return err
				}
			case "Users":
				m.Users = UserMap{}
				if err := d.DecodeElement(&m.Users, &t); err != nil {
					return err
				}
			case "Post":
				var p Post
				if err := d.DecodeElement(&p, &t); err != nil {
					return err
				}
				m.Post = &p
			case "Reactions":
				if err := decodeWrappedSlice(d, "Reaction", &m.Reactions); err != nil {
					return err
				}
			case "Statuses":
				if err := decodeWrappedSlice(d, "Status", &m.Statuses); err != nil {
					return err
				}
			case "MembershipChanges":
				if err := decodeWrappedSlice(d, "MembershipChange", &m.MembershipChanges); err != nil {
					return err
				}
			case "Acknowledgements":
				if err := decodeWrappedSlice(d, "PostAcknowledgement", &m.Acknowledgements); err != nil {
					return err
				}
			case "MentionTransforms":
				m.MentionTransforms = MentionTransforms{}
				if err := d.DecodeElement(&m.MentionTransforms, &t); err != nil {
					return err
				}
			default:
				if err := d.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			return nil
		}
	}
}

// encodeString writes a single child element with the given local name
// and string body. The receiver always emits the element so that
// required fields (Id, ChannelId) are present even when zero.
func encodeString(e *xml.Encoder, name, value string) error {
	start := xml.StartElement{Name: xml.Name{Local: name}}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	if value != "" {
		if err := e.EncodeToken(xml.CharData(value)); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// encodeWrappedSlice emits a wrapper <wrapper> ... </wrapper> with one
// <child> element per slice item. Empty/nil slices produce no output.
func encodeWrappedSlice[T any](e *xml.Encoder, wrapper, child string, items []*T) error {
	if len(items) == 0 {
		return nil
	}
	start := xml.StartElement{Name: xml.Name{Local: wrapper}}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	for _, item := range items {
		cs := xml.StartElement{Name: xml.Name{Local: child}}
		if err := e.EncodeElement(item, cs); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// decodeWrappedSlice reads a <wrapper> element, decoding each <child>
// into a new instance of T appended to out.
func decodeWrappedSlice[T any](d *xml.Decoder, child string, out *[]*T) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != child {
				if err := d.Skip(); err != nil {
					return err
				}
				continue
			}
			var v T
			if err := d.DecodeElement(&v, &t); err != nil {
				return err
			}
			*out = append(*out, &v)
		case xml.EndElement:
			return nil
		}
	}
}

// SyncMsgFromModel converts an upstream SyncMsg to its wire form,
// pruning fields per the wire-type policy. Returns nil if the input
// is nil.
func SyncMsgFromModel(m *mmModel.SyncMsg) *SyncMsg {
	if m == nil {
		return nil
	}
	out := &SyncMsg{
		Id:        m.Id,
		ChannelId: m.ChannelId,
	}
	if len(m.Users) > 0 {
		out.Users = make(UserMap, len(m.Users))
		for id, u := range m.Users {
			out.Users[id] = UserFromModel(u)
		}
	}
	// At most one post per envelope by the sender's split policy. If the
	// upstream slice has more than one entry it is a sender-side bug;
	// take the first and let the caller log if it cares. We do not
	// silently coalesce because that would obscure the split-policy
	// invariant.
	if len(m.Posts) > 0 {
		out.Post = PostFromModel(m.Posts[0])
	}
	if len(m.Reactions) > 0 {
		out.Reactions = make([]*Reaction, 0, len(m.Reactions))
		for _, r := range m.Reactions {
			out.Reactions = append(out.Reactions, ReactionFromModel(r))
		}
	}
	if len(m.Statuses) > 0 {
		out.Statuses = make([]*Status, 0, len(m.Statuses))
		for _, s := range m.Statuses {
			out.Statuses = append(out.Statuses, StatusFromModel(s))
		}
	}
	if len(m.MembershipChanges) > 0 {
		out.MembershipChanges = make([]*MembershipChange, 0, len(m.MembershipChanges))
		for _, c := range m.MembershipChanges {
			out.MembershipChanges = append(out.MembershipChanges, MembershipChangeFromModel(c))
		}
	}
	if len(m.Acknowledgements) > 0 {
		out.Acknowledgements = make([]*PostAcknowledgement, 0, len(m.Acknowledgements))
		for _, a := range m.Acknowledgements {
			out.Acknowledgements = append(out.Acknowledgements, PostAcknowledgementFromModel(a))
		}
	}
	if len(m.MentionTransforms) > 0 {
		out.MentionTransforms = make(MentionTransforms, len(m.MentionTransforms))
		maps.Copy(out.MentionTransforms, m.MentionTransforms)
	}
	return out
}

// ToModel converts the wire SyncMsg back to the upstream form.
// Returns nil if the receiver is nil.
func (m *SyncMsg) ToModel() *mmModel.SyncMsg {
	if m == nil {
		return nil
	}
	out := &mmModel.SyncMsg{
		Id:        m.Id,
		ChannelId: m.ChannelId,
	}
	if len(m.Users) > 0 {
		out.Users = make(map[string]*mmModel.User, len(m.Users))
		for id, u := range m.Users {
			out.Users[id] = u.ToModel()
		}
	}
	if m.Post != nil {
		out.Posts = []*mmModel.Post{m.Post.ToModel()}
	}
	if len(m.Reactions) > 0 {
		out.Reactions = make([]*mmModel.Reaction, 0, len(m.Reactions))
		for _, r := range m.Reactions {
			out.Reactions = append(out.Reactions, r.ToModel())
		}
	}
	if len(m.Statuses) > 0 {
		out.Statuses = make([]*mmModel.Status, 0, len(m.Statuses))
		for _, s := range m.Statuses {
			out.Statuses = append(out.Statuses, s.ToModel())
		}
	}
	if len(m.MembershipChanges) > 0 {
		out.MembershipChanges = make([]*mmModel.MembershipChangeMsg, 0, len(m.MembershipChanges))
		for _, c := range m.MembershipChanges {
			out.MembershipChanges = append(out.MembershipChanges, c.ToModel())
		}
	}
	if len(m.Acknowledgements) > 0 {
		out.Acknowledgements = make([]*mmModel.PostAcknowledgement, 0, len(m.Acknowledgements))
		for _, a := range m.Acknowledgements {
			out.Acknowledgements = append(out.Acknowledgements, a.ToModel())
		}
	}
	if len(m.MentionTransforms) > 0 {
		out.MentionTransforms = make(map[string]string, len(m.MentionTransforms))
		maps.Copy(out.MentionTransforms, m.MentionTransforms)
	}
	return out
}

// MarshalXML implements xml.Marshaler for UserMap.
func (m UserMap) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if len(m) == 0 {
		return nil
	}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		userStart := xml.StartElement{
			Name: xml.Name{Local: "User"},
			Attr: []xml.Attr{{Name: xml.Name{Local: "id"}, Value: k}},
		}
		if err := e.EncodeElement(m[k], userStart); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// UnmarshalXML implements xml.Unmarshaler for UserMap. The id attribute
// on each <User> element supplies the map key.
func (m *UserMap) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
	if *m == nil {
		*m = make(UserMap)
	}
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "User" {
				if err := d.Skip(); err != nil {
					return err
				}
				continue
			}
			var id string
			for _, a := range t.Attr {
				if a.Name.Local == "id" {
					id = a.Value
					break
				}
			}
			var u User
			if err := d.DecodeElement(&u, &t); err != nil {
				return err
			}
			(*m)[id] = &u
		case xml.EndElement:
			return nil
		}
	}
}

// MarshalXML implements xml.Marshaler for MentionTransforms.
func (m MentionTransforms) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if len(m) == 0 {
		return nil
	}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		entry := xml.StartElement{
			Name: xml.Name{Local: "Transform"},
			Attr: []xml.Attr{
				{Name: xml.Name{Local: "key"}, Value: k},
				{Name: xml.Name{Local: "value"}, Value: m[k]},
			},
		}
		if err := e.EncodeToken(entry); err != nil {
			return err
		}
		if err := e.EncodeToken(entry.End()); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// UnmarshalXML implements xml.Unmarshaler for MentionTransforms.
func (m *MentionTransforms) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
	if *m == nil {
		*m = make(MentionTransforms)
	}
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "Transform" {
				if err := d.Skip(); err != nil {
					return err
				}
				continue
			}
			var k, v string
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "key":
					k = a.Value
				case "value":
					v = a.Value
				}
			}
			(*m)[k] = v
			if err := d.Skip(); err != nil {
				return err
			}
		case xml.EndElement:
			return nil
		}
	}
}
