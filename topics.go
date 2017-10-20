package main

import (
	"github.com/acomagu/chatroom-go-v2/chatroom"
	"strings"
	"golang.org/x/oauth2"
)

func topics() []chatroom.Topic {
	return []chatroom.Topic{getCodeTopic, meshiTopic}
}

func getCodeTopic(room chatroom.Room) chatroom.DidTalk {
	text := (<-room.In).(string)
	if text != "get" {
		return false
	}
	postToSlack( conf.AuthCodeURL("state", oauth2.AccessTypeOffline))
	return true
}

func meshiTopic(room chatroom.Room) chatroom.DidTalk {
	text := (<-room.In).(string)
	if !strings.HasPrefix(text, "meshi") {
		return false
	}

	res := ""
	if len(text) >= 6 {
		res = sel(text[5:len(text)])
	} else {
		res = sel("")
	}
	postToSlack(res)
	return true
}
