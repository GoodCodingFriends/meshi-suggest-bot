package main

import (
	"encoding/json"
	"fmt"
	"github.com/acomagu/chatroom-go-v2/chatroom"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/foursquare"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Slack struct {
	Text      string `json:"text"`
	Username  string `json:"username"`
	IconEmoji string `json:"icon_emoji"`
}

type Location struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type Category struct {
	Name string `json:"name"`
}

type Venue struct {
	Name       string     `json:"name"`
	Location   Location   `json:"location"`
	Categories []Category `json:"categories"`
}

type VenueItem struct {
	Venue Venue `json:"venue"`
}

type Resp struct {
	Response struct {
		Venues struct {
			Items []VenueItem `json:"items"`
		} `json:"venues"`
	} `json:"response"`
}

type UserResp struct {
	Response struct {
		User User `json:"user"`
	} `json:"response"`
}

type User struct {
	ID        string `json:"id"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

type Cand struct {
	Venue Venue
	User  User
}

var foursquareClientID = os.Getenv("FOURSQUARE_CLIENT_ID")
var foursquareClientSecret = os.Getenv("FOURSQUARE_CLIENT_SECRET")
var endpointURI = os.Getenv("ENDPOINT_URI")
var slackIncomingWebhookURL = os.Getenv("SLACK_INCOMING_WEBHOOK_URL")
var port = os.Getenv("PORT")
var slackBotAPIToken = os.Getenv("SLACK_BOT_API_TOKEN")
var redisURL = os.Getenv("REDIS_URL")

var conf = &oauth2.Config{
	ClientID:     foursquareClientID,
	ClientSecret: foursquareClientSecret,
	RedirectURL:  endpointURI + "/authenticated",
	Endpoint:     foursquare.Endpoint,
}

func main() {
	rand.Seed(time.Now().Unix())

	topics, err := topics(redisURL)
	if err != nil {
		log.Print(errors.Wrap(err, "could not initialize topics"))
		return
	}
	cr := chatroom.New(topics)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Print(errors.Wrap(err, "fatal to read from reqeust body"))
			return
		}
		username, ok, err := getSentUserName(body)
		if err != nil || !ok {
			log.Print(errors.Wrap(err, "could not get username from response"))
			return
		}
		if username == "slackbot" {
			return
		}
		// Pass the received message to Chatroom.
		fmt.Printf("-> %s\n", getReceivedMessage(body))
		cr.In <- getReceivedMessage(body)
	})

	http.HandleFunc("/authenticated", func(w http.ResponseWriter, req *http.Request) {
		code := req.URL.Query().Get("code")

		tok, err := conf.Exchange(oauth2.NoContext, code)
		if err != nil {
			log.Print(errors.Wrap(err, "fatal to get access token"))
			return
		}

		client := conf.Client(oauth2.NoContext, tok)

		resp, err := client.Get(fmt.Sprintf("https://api.foursquare.com/v2/users/self?oauth_token=%s&v=20170801", tok.AccessToken))
		if err != nil {
			log.Print(errors.Wrap(err, "fatal to get user info"))
			return
		}

		r := UserResp{}
		err = json.NewDecoder(resp.Body).Decode(&r)
		if err != nil {
			log.Print(errors.Wrap(err, "fatal to decode user info as JSON"))
			return
		}

		s := newRedisUserAndTokenStore(redisURL)
		err = s.storeUsersAndTokens(*tok, r.Response.User)
		if err != nil {
			log.Print(errors.Wrap(err, "could not store users and tokens to Redis"))
			return
		}

		w.Write([]byte("Close this window."))
	})

	fmt.Println(http.ListenAndServe(":"+port, nil))
}

func postToSlack(text string) {
	fmt.Printf("<- %s\n", text)

	jsonStr, err := json.Marshal(Slack{Text: text, Username: "MESHI", IconEmoji: ":just_do_it:"})
	if err != nil {
		log.Print(errors.Wrap(err, "could not marshal Slack message struct as JSON"))
		return
	}

	_, err = http.PostForm(slackIncomingWebhookURL, url.Values{"payload": {string(jsonStr)}})
	if err != nil {
		log.Print(errors.Wrap(err, "could not send message"))
	}
}

func getReceivedMessage(body []byte) string {
	parsed, err := url.ParseQuery(string(body))
	if err != nil {
		log.Print(errors.Wrap(err, "could not parse URL Query"))
	}
	return parsed["text"][0]
}

func getSentUserName(body []byte) (string, bool, error) {
	parsed, err := url.ParseQuery(string(body))
	if err != nil {
		return "", false, errors.Wrap(err, "could not parse URL Query")
	}
	username, ok := parsed["user_name"]
	if !ok || len(username) == 0 {
		return "", false, nil
	}
	return username[0], true, nil
}

func chooseChan(ch <-chan Cand) (*Cand, error) {
	cands := []Cand{}
	for str := range ch {
		cands = append(cands, str)
	}

	return choose(cands)
}

func choose(arr []Cand) (*Cand, error) {
	if len(arr) == 0 {
		return nil, fmt.Errorf("empty array")
	}
	return &arr[rand.Intn(len(arr))], nil
}

func isRestaurant(v Venue) bool {
	for _, c := range v.Categories {
		if strings.HasSuffix(c.Name, "Restaurant") {
			return true
		}
	}
	return false
}
