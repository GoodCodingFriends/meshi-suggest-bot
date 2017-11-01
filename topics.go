package main

import (
	"github.com/acomagu/chatroom-go-v2/chatroom"
	"github.com/garyburd/redigo/redis"
	"golang.org/x/oauth2"
	"regexp"
)

func topics(rds redis.Conn) []chatroom.Topic {
	getCodeTopic := newGetCodeTopic(rds).talk
	meshiTopic := newMeshiTopic(rds).talk
	return []chatroom.Topic{getCodeTopic, meshiTopic}
}

type GetCodeTopic struct {
	rds redis.Conn
}

func newGetCodeTopic(rds redis.Conn) GetCodeTopic {
	return GetCodeTopic{
		rds: rds,
	}
}

func (GetCodeTopic) talk(room chatroom.Room) chatroom.DidTalk {
	text := (<-room.In).(string)
	if text != "get" {
		return false
	}
	postToSlack(conf.AuthCodeURL("state", oauth2.AccessTypeOffline))
	return true
}

type MeshiTopic struct {
	rds redis.Conn
}

func newMeshiTopic(rds redis.Conn) MeshiTopic {
	return MeshiTopic{
		rds: rds,
	}
}

func (t MeshiTopic) talk(room chatroom.Room) chatroom.DidTalk {
	text := (<-room.In).(string)
	matches := regexp.MustCompile(`^ご(?:はん|飯)(?:るーれっと|ルーレット)[ 　]?(.*)?$`).FindStringSubmatch(text)
	if len(matches) < 2 {
		return false
	}

	locName := matches[1]

	res := sel(t.rds, locName)
	postToSlack(res)
	return true
}
