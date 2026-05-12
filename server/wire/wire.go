package wire

import (
	"encoding/xml"
	"sort"
)

// StringMap is a string-to-string map serialized as a list of
// <Entry key="..." value="..."/> elements, sorted by key for
// deterministic output. It is used for fields like User.Timezone
// where the keys are not fixed by the schema.
type StringMap map[string]string

// MarshalXML implements xml.Marshaler. Empty maps are omitted by the
// encoder via the omitempty tag before this method is called; this
// method emits a wrapper element containing one <Entry> per key.
func (m StringMap) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
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
			Name: xml.Name{Local: "Entry"},
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

// UnmarshalXML implements xml.Unmarshaler.
func (m *StringMap) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
	if *m == nil {
		*m = make(StringMap)
	}
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "Entry" {
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
