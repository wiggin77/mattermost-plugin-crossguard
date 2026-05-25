package wire

import (
	"encoding/xml"
	"strings"

	mmModel "github.com/mattermost/mattermost/server/public/model"
)

// FileIDs is the file-attachment ID list carried on a Post. It is a
// named slice type so its custom MarshalXML can suppress the wrapper
// when empty (Go's xml.Encoder emits an empty <FileIds></FileIds>
// wrapper for the `xml:"FileIds>Id"` shorthand even when the slice
// is nil, which we do not want on the wire).
//
// On JSON the wire shape is a flat array of strings, which is what
// encoding/json produces from []string by default; no custom JSON
// marshaler is required. The parent Post field's omitempty tag
// suppresses an empty or nil slice.
type FileIDs []string

// MarshalXML implements xml.Marshaler.
func (f FileIDs) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if len(f) == 0 {
		return nil
	}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	for _, id := range f {
		idStart := xml.StartElement{Name: xml.Name{Local: "Id"}}
		if err := e.EncodeElement(id, idStart); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// UnmarshalXML implements xml.Unmarshaler.
func (f *FileIDs) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "Id" {
				if err := d.Skip(); err != nil {
					return err
				}
				continue
			}
			var s string
			if err := d.DecodeElement(&s, &t); err != nil {
				return err
			}
			*f = append(*f, s)
		case xml.EndElement:
			return nil
		}
	}
}

// Post.Props upstream keys carried on the wire. Constants kept in sync
// with the upstream mmModel.PostPropsX constants by hand; do not pull
// them via import so that an upstream rename is caught by the
// compliance review rather than silently changing the wire format.
const (
	postPropFromWebhook              = "from_webhook"
	postPropFromBot                  = "from_bot"
	postPropFromPlugin               = "from_plugin"
	postPropFromOAuthApp             = "from_oauth_app"
	postPropOverrideUsername         = "override_username"
	postPropOverrideIconURL          = "override_icon_url"
	postPropOverrideIconEmoji        = "override_icon_emoji"
	postPropWebhookDisplayName       = "webhook_display_name"
	postPropAddedUserId              = "addedUserId"
	postPropDeleteBy                 = "deleteBy"
	postPropAddChannelMember         = "add_channel_member"
	postPropMentionHighlightDisabled = "mentionHighlightDisabled"
	postPropDisableGroupHighlight    = "disable_group_highlight"
	postPropAIGeneratedByUserId      = "ai_generated_by"
	postPropAIGeneratedByUsername    = "ai_generated_by_username"
)

// PostProps is the typed, pruned replacement for upstream Post.Props
// (StringInterface). Only the fifteen whitelisted keys cross the wire;
// see implementation-plans/26-05-11-02-plugin-owned-wire-types.md for
// the per-key rationale.
type PostProps struct {
	FromWebhook              bool   `xml:"FromWebhook,omitempty"              json:"FromWebhook,omitempty"`
	FromBot                  bool   `xml:"FromBot,omitempty"                  json:"FromBot,omitempty"`
	FromPlugin               bool   `xml:"FromPlugin,omitempty"               json:"FromPlugin,omitempty"`
	FromOAuthApp             bool   `xml:"FromOAuthApp,omitempty"             json:"FromOAuthApp,omitempty"`
	OverrideUsername         string `xml:"OverrideUsername,omitempty"         json:"OverrideUsername,omitempty"`
	OverrideIconURL          string `xml:"OverrideIconURL,omitempty"          json:"OverrideIconURL,omitempty"`
	OverrideIconEmoji        string `xml:"OverrideIconEmoji,omitempty"        json:"OverrideIconEmoji,omitempty"`
	WebhookDisplayName       string `xml:"WebhookDisplayName,omitempty"       json:"WebhookDisplayName,omitempty"`
	AddedUserId              string `xml:"AddedUserId,omitempty"              json:"AddedUserId,omitempty"`
	DeleteBy                 string `xml:"DeleteBy,omitempty"                 json:"DeleteBy,omitempty"`
	AddChannelMember         string `xml:"AddChannelMember,omitempty"         json:"AddChannelMember,omitempty"`
	MentionHighlightDisabled bool   `xml:"MentionHighlightDisabled,omitempty" json:"MentionHighlightDisabled,omitempty"`
	DisableGroupHighlight    bool   `xml:"DisableGroupHighlight,omitempty"    json:"DisableGroupHighlight,omitempty"`
	AIGeneratedByUserId      string `xml:"AIGeneratedByUserId,omitempty"      json:"AIGeneratedByUserId,omitempty"`
	AIGeneratedByUsername    string `xml:"AIGeneratedByUsername,omitempty"    json:"AIGeneratedByUsername,omitempty"`
}

// PostPropsFromModel extracts the whitelisted keys from an upstream
// StringInterface. Returns nil if no whitelisted key is populated.
func PostPropsFromModel(m mmModel.StringInterface) *PostProps {
	if len(m) == 0 {
		return nil
	}
	out := &PostProps{
		FromWebhook:              propBool(m, postPropFromWebhook),
		FromBot:                  propBool(m, postPropFromBot),
		FromPlugin:               propBool(m, postPropFromPlugin),
		FromOAuthApp:             propBool(m, postPropFromOAuthApp),
		OverrideUsername:         propString(m, postPropOverrideUsername),
		OverrideIconURL:          propString(m, postPropOverrideIconURL),
		OverrideIconEmoji:        stripEmojiColons(propString(m, postPropOverrideIconEmoji)),
		WebhookDisplayName:       propString(m, postPropWebhookDisplayName),
		AddedUserId:              propString(m, postPropAddedUserId),
		DeleteBy:                 propString(m, postPropDeleteBy),
		AddChannelMember:         propString(m, postPropAddChannelMember),
		MentionHighlightDisabled: propBool(m, postPropMentionHighlightDisabled),
		DisableGroupHighlight:    propBool(m, postPropDisableGroupHighlight),
		AIGeneratedByUserId:      propString(m, postPropAIGeneratedByUserId),
		AIGeneratedByUsername:    propString(m, postPropAIGeneratedByUsername),
	}
	if out.isZero() {
		return nil
	}
	return out
}

// ToModel converts the typed PostProps back to an upstream
// StringInterface. Returns nil if the receiver is nil.
func (p *PostProps) ToModel() mmModel.StringInterface {
	if p == nil {
		return nil
	}
	out := mmModel.StringInterface{}
	if p.FromWebhook {
		out[postPropFromWebhook] = true
	}
	if p.FromBot {
		out[postPropFromBot] = true
	}
	if p.FromPlugin {
		out[postPropFromPlugin] = true
	}
	if p.FromOAuthApp {
		out[postPropFromOAuthApp] = true
	}
	if p.OverrideUsername != "" {
		out[postPropOverrideUsername] = p.OverrideUsername
	}
	if p.OverrideIconURL != "" {
		out[postPropOverrideIconURL] = p.OverrideIconURL
	}
	if p.OverrideIconEmoji != "" {
		out[postPropOverrideIconEmoji] = ":" + p.OverrideIconEmoji + ":"
	}
	if p.WebhookDisplayName != "" {
		out[postPropWebhookDisplayName] = p.WebhookDisplayName
	}
	if p.AddedUserId != "" {
		out[postPropAddedUserId] = p.AddedUserId
	}
	if p.DeleteBy != "" {
		out[postPropDeleteBy] = p.DeleteBy
	}
	if p.AddChannelMember != "" {
		out[postPropAddChannelMember] = p.AddChannelMember
	}
	if p.MentionHighlightDisabled {
		out[postPropMentionHighlightDisabled] = true
	}
	if p.DisableGroupHighlight {
		out[postPropDisableGroupHighlight] = true
	}
	if p.AIGeneratedByUserId != "" {
		out[postPropAIGeneratedByUserId] = p.AIGeneratedByUserId
	}
	if p.AIGeneratedByUsername != "" {
		out[postPropAIGeneratedByUsername] = p.AIGeneratedByUsername
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (p *PostProps) isZero() bool {
	return !p.FromWebhook && !p.FromBot && !p.FromPlugin && !p.FromOAuthApp &&
		p.OverrideUsername == "" && p.OverrideIconURL == "" &&
		p.OverrideIconEmoji == "" && p.WebhookDisplayName == "" &&
		p.AddedUserId == "" && p.DeleteBy == "" && p.AddChannelMember == "" &&
		!p.MentionHighlightDisabled && !p.DisableGroupHighlight &&
		p.AIGeneratedByUserId == "" && p.AIGeneratedByUsername == ""
}

// propBool returns a bool from a StringInterface, tolerating the upstream
// pattern where some bool-like flags are stored as the string "true".
func propBool(m mmModel.StringInterface, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true"
	}
	return false
}

func propString(m mmModel.StringInterface, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// stripEmojiColons removes a single leading and trailing ':' from an
// emoji-name value. Mattermost stores override_icon_emoji in the
// ':emoji_name:' form, but the wire schema's EmojiNameType pattern
// admits only the bare name. ToModel() re-adds the colons.
func stripEmojiColons(s string) string {
	return strings.Trim(s, ":")
}

// Post is the wire representation of a Mattermost post. Fields the
// receiver computes from its own state (ReplyCount, LastReplyAt,
// Participants, IsFollowing) and pre-server-processing artifacts
// (MessageSource, PendingPostId) are dropped.
type Post struct {
	Id           string     `xml:"Id"                    json:"Id"`
	CreateAt     int64      `xml:"CreateAt"              json:"CreateAt"`
	UpdateAt     int64      `xml:"UpdateAt"              json:"UpdateAt"`
	EditAt       int64      `xml:"EditAt,omitempty"      json:"EditAt,omitempty"`
	DeleteAt     int64      `xml:"DeleteAt"              json:"DeleteAt"`
	UserId       string     `xml:"UserId"                json:"UserId"`
	ChannelId    string     `xml:"ChannelId"             json:"ChannelId"`
	RootId       string     `xml:"RootId,omitempty"      json:"RootId,omitempty"`
	OriginalId   string     `xml:"OriginalId,omitempty"  json:"OriginalId,omitempty"`
	Message      string     `xml:"Message"               json:"Message"`
	Type         string     `xml:"Type,omitempty"        json:"Type,omitempty"`
	Props        *PostProps `xml:"Props,omitempty"       json:"Props,omitempty"`
	Hashtags     string     `xml:"Hashtags,omitempty"    json:"Hashtags,omitempty"`
	FileIds      FileIDs    `xml:"FileIds,omitempty"     json:"FileIds,omitempty"`
	HasReactions bool       `xml:"HasReactions,omitempty" json:"HasReactions,omitempty"`
	RemoteId     string     `xml:"RemoteId,omitempty"    json:"RemoteId,omitempty"`
	IsPinned     bool       `xml:"IsPinned,omitempty"    json:"IsPinned,omitempty"`
}

// PostFromModel converts an upstream Post to its wire form. Dropped
// fields (MessageSource, PendingPostId, ReplyCount, LastReplyAt,
// Participants, IsFollowing, Metadata, Filenames) do not cross.
func PostFromModel(p *mmModel.Post) *Post {
	if p == nil {
		return nil
	}
	out := &Post{
		Id:           p.Id,
		CreateAt:     p.CreateAt,
		UpdateAt:     p.UpdateAt,
		EditAt:       p.EditAt,
		DeleteAt:     p.DeleteAt,
		UserId:       p.UserId,
		ChannelId:    p.ChannelId,
		RootId:       p.RootId,
		OriginalId:   p.OriginalId,
		Message:      p.Message,
		Type:         p.Type,
		Props:        PostPropsFromModel(p.GetProps()),
		Hashtags:     p.Hashtags,
		FileIds:      FileIDs(append([]string(nil), p.FileIds...)),
		HasReactions: p.HasReactions,
		IsPinned:     p.IsPinned,
	}
	if p.RemoteId != nil {
		out.RemoteId = *p.RemoteId
	}
	return out
}

// ToModel converts the wire Post back to the upstream form. Dropped
// fields are left as zero values; the receiver populates derived
// fields (ReplyCount, LastReplyAt, Participants) from its own database.
func (p *Post) ToModel() *mmModel.Post {
	if p == nil {
		return nil
	}
	out := &mmModel.Post{
		Id:           p.Id,
		CreateAt:     p.CreateAt,
		UpdateAt:     p.UpdateAt,
		EditAt:       p.EditAt,
		DeleteAt:     p.DeleteAt,
		UserId:       p.UserId,
		ChannelId:    p.ChannelId,
		RootId:       p.RootId,
		OriginalId:   p.OriginalId,
		Message:      p.Message,
		Type:         p.Type,
		Hashtags:     p.Hashtags,
		FileIds:      mmModel.StringArray(p.FileIds),
		HasReactions: p.HasReactions,
		IsPinned:     p.IsPinned,
	}
	if props := p.Props.ToModel(); props != nil {
		out.SetProps(props)
	}
	if p.RemoteId != "" {
		rid := p.RemoteId
		out.RemoteId = &rid
	}
	return out
}
