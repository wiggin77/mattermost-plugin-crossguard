// Package wirearchive provides JSON-to-XML conversion for archived
// Cross Guard envelopes. It is the runtime enforcement seam for the
// JSON wire format: any JSON envelope that fails the XSD after a
// decode-then-re-encode round trip through this package is a
// compliance violation by construction, because the wire types in
// server/wire/ are the only path from JSON bytes to XML bytes.
//
// The package is imported by the integration test harness when
// CROSSGUARD_WIRE_VALIDATE=1, and the same conversion is invoked by
// schema/examples_roundtrip tests at unit-test time.
package wirearchive

import (
	"encoding/json"
	"encoding/xml"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
)

// envelope mirrors server/transport.go's TransportEnvelope on the
// field set that crosses the wire. The XML and JSON struct tags are
// the same as the production type's; field order is preserved so
// re-encoded XML passes schema validation. SyncMsg is the wire
// package's SyncMsg, which has both XML and JSON marshalers and so
// supports the round trip directly.
//
// Defined locally (not imported from package main) because the main
// package is not importable across the `integration` build-tag
// boundary, and adding a helper to package main just for this
// scenario would pollute its export surface.
type envelope struct {
	XMLName     xml.Name      `xml:"CrossGuardEnvelope" json:"-"`
	Version     int           `xml:"version,attr"       json:"version"`
	Type        string        `xml:"type,attr"          json:"type"`
	ConnName    string        `xml:"ConnName"           json:"ConnName"`
	Timestamp   string        `xml:"Timestamp"          json:"Timestamp"`
	Epoch       string        `xml:"Epoch,omitempty"    json:"Epoch,omitempty"`
	Sequence    uint64        `xml:"Sequence,omitempty" json:"Sequence,omitempty"`
	TeamName    string        `xml:"TeamName"           json:"TeamName"`
	ChannelName string        `xml:"ChannelName"        json:"ChannelName"`
	SyncMsg     *wire.SyncMsg `xml:"SyncMsg,omitempty"  json:"SyncMsg,omitempty"`
	TestID      string        `xml:"TestID,omitempty"   json:"TestID,omitempty"`
}

// JSONToXML decodes the given JSON envelope bytes and re-encodes them
// as XML. Returns the XML bytes prefixed with the standard XML header,
// matching what server/transport.go's MarshalEnvelope emits when
// FormatXML is selected. Any unmarshal or marshal error is returned
// verbatim so the caller can surface it in test output alongside the
// failing input.
func JSONToXML(data []byte) ([]byte, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	out, err := xml.Marshal(&env)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}
