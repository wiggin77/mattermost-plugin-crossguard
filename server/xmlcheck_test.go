package main

import (
	"encoding/xml"
	"fmt"
	"testing"

	mmModel "github.com/mattermost/mattermost/server/public/model"
)

type CheckEnv struct {
	XMLName     xml.Name         `xml:"CrossGuardEnvelope"`
	Version     int              `xml:"version,attr"`
	Type        string           `xml:"type,attr"`
	ConnName    string           `xml:"ConnName"`
	TeamName    string           `xml:"TeamName"`
	ChannelName string           `xml:"ChannelName"`
	SyncMsg     *mmModel.SyncMsg `xml:"SyncMsg,omitempty"`
}

func TestCheckActualXML(t *testing.T) {
	env := &CheckEnv{
		Version:     1,
		Type:        "sync_msg",
		ConnName:    "nats-low-to-high",
		TeamName:    "test-a",
		ChannelName: "town-square",
		SyncMsg: &mmModel.SyncMsg{
			Id:        "sm1",
			ChannelId: "ch001",
			Users: map[string]*mmModel.User{
				"u1": {Id: "u1", Username: "alice", UpdateAt: 100, Roles: "system_user"},
			},
			Posts: []*mmModel.Post{
				{Id: "p1", ChannelId: "ch001", UserId: "u1", Message: "Hello", UpdateAt: 200, CreateAt: 200},
			},
		},
	}
	data, _ := xml.MarshalIndent(env, "", "  ")
	fmt.Println(xml.Header + string(data))
}
