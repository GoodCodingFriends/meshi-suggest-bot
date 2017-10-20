package chatroom

import (
	"fmt"
)

// topicChan includes chatroom channel, the channel pass the returned value from topic.
type topicChan struct {
	Room
	Return chan bool
}

// A DidTalk means whether the Topic talks with user. A Topic function must return this type value.
type DidTalk bool

// Topic type is the function express a bunch of flow in chattting. Pass slice of this to New(), and the function called them in order. If one of them returns true, the loop breaks.
type Topic func(Room) DidTalk

func (cr Chatroom) talk(chatroom Room) {
	topicChans := []topicChan{}
	for _, topic := range cr.topics {
		topicChan := loopTopic(topic, chatroom)
		topicChans = append(topicChans, topicChan)
	}
	middleChatroom, clearPool, broadcastPool := poolMessages(chatroom)
	changeDestTopicTo := distributeMessage(middleChatroom)
	go controller(topicChans, changeDestTopicTo, broadcastPool, clearPool)
}

func controller(topicChans []topicChan, changeDestTopicTo chan Room, broadcastPool chan bool, clearPool chan bool) {
	for {
		for i, topicChan := range topicChans {
			changeDestTopicTo <- topicChan.Room
			if i > 0 { // for the start time.
				broadcastPool <- true
			}
			didTalk := <-topicChan.Return
			if didTalk {
				clearPool <- true
				break
			}
		}
		clearPool <- true
	}
}

// This pipe stores messages from user with flowing next Chatroom(middleChatroom). And this provides functions, clearPool and broadcastPool. This is used in controller().
func poolMessages(chatroom Room) (Room, chan bool, chan bool) {
	middleChatroom := Room{
		In:  make(chan interface{}),
		Out: chatroom.Out,
	}
	clearPool := make(chan bool)
	broadcastPool := make(chan bool)

	go func(chatroom Room, middleChatroom Room, clearPool <-chan bool, broadcastPool <-chan bool) {
		var pool []interface{}
		for {
			select {
			case message := <-chatroom.In:
				pool = append(pool, message)
				middleChatroom.In <- message

			case <-clearPool:
				pool = pool[:0]

			case <-broadcastPool:
				for _, message := range pool {
					middleChatroom.In <- message
				}
			}
		}
	}(chatroom, middleChatroom, clearPool, broadcastPool)

	return middleChatroom, clearPool, broadcastPool
}

// distributeMessage pass message from chatroom to chatroom. The chatroom of destination will change as needed, changed by value of channel, changeDestTopicTo.
func distributeMessage(middleChatroom Room) chan Room {
	changeDestTopicTo := make(chan Room)

	go func(middleChatroom Room, changeDestTopicTo <-chan Room) {
		var dest Room
		dest = <-changeDestTopicTo
		for {
			select {
			case message := <-middleChatroom.In:
				if dest == (Room{}) {
					fmt.Println("Error: the destination chatroom is not set.")
					break
				}
				dest.In <- message

			case _dest := <-changeDestTopicTo:
				dest = _dest
			}
		}
	}(middleChatroom, changeDestTopicTo)

	return changeDestTopicTo
}

// loopTopic just loops topic.
func loopTopic(topic Topic, chatroom Room) topicChan {
	tc := topicChan{
		Room: Room{
			In:  make(chan interface{}),
			Out: chatroom.Out,
		},
		Return: make(chan bool),
	}

	go func(topic Topic, tc topicChan) {
		for {
			tc.Return <- bool(topic(tc.Room))
		}
	}(topic, tc)

	return tc
}
