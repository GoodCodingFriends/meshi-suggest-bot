package chatroom

// A Chatroom has all functions and channels to be exported from this package.
type Chatroom struct {
	topics []Topic
	Room
}

// A Room has functions to wait or send messages with user. This is passed to Topic function as argument.
type Room struct {
	In  chan interface{}
	Out chan interface{}
}

// New creates and initialize a Chatroom. This also starts a go-routine to pass messages to Topics.
func New(topics []Topic) Chatroom {
	room := Room{
		In:  make(chan interface{}),
		Out: make(chan interface{}),
	}
	chatroom := Chatroom{
		topics: topics,
		Room:   room,
	}
	go chatroom.talk(room)
	return chatroom
}
