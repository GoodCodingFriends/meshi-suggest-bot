package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/acomagu/chatroom-go-v2/chatroom"
	"github.com/garyburd/redigo/redis"
	"github.com/kellydunn/golang-geo"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/foursquare"
	"googlemaps.github.io/maps"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
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

type Resp struct {
	Response struct {
		Venues struct {
			Items []struct {
				Venue Venue `json:"venue"`
			} `json:"items"`
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

var conf = &oauth2.Config{
	ClientID:     foursquareClientID,
	ClientSecret: foursquareClientSecret,
	RedirectURL:  endpointURI + "/authenticated",
	Endpoint:     foursquare.Endpoint,
}

func main() {
	rand.Seed(time.Now().Unix())

	rds, err := redis.DialURL(os.Getenv("REDIS_URL"))
	if err != nil {
		fmt.Println(err)
	}
	defer rds.Close()

	cr := chatroom.New(topics(rds))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			fmt.Println(err)
			return
		}
		username, ok, err := getSentUserName(body)
		if err != nil || !ok {
			fmt.Printf("could not get username from response: %s\n", err)
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
			fmt.Println(err)
			return
		}

		client := conf.Client(oauth2.NoContext, tok)

		resp, err := client.Get(fmt.Sprintf("https://api.foursquare.com/v2/users/self?oauth_token=%s&v=20170801", tok.AccessToken))
		if err != nil {
			fmt.Println(err)
			return
		}

		r := UserResp{}
		err = json.NewDecoder(resp.Body).Decode(&r)
		if err != nil {
			fmt.Println(err)
			return
		}

		storeUsersAndTokens(rds, *tok, r.Response.User)

		w.Write([]byte("Close this window."))
	})

	fmt.Println(http.ListenAndServe(":"+port, nil))
}

func fetchUsersAndTokens(rds redis.Conn) ([]oauth2.Token, []User, error) {
	values, err := redis.Values(rds.Do(
		"SORT", "userAndTokens",
		"GET", "userAndToken:*->id",
		"GET", "userAndToken:*->token",
		"GET", "userAndToken:*->firstName",
		"GET", "userAndToken:*->lastName",
	))
	if err != nil {
		return nil, nil, err
	}

	tokens := []oauth2.Token{}
	users := []User{}
	for len(values) > 0 {
		var id, accessToken, firstName, lastName string
		values, err = redis.Scan(values, &id, &accessToken, &firstName, &lastName)
		if err != nil {
			return nil, nil, err
		}

		token := oauth2.Token{
			AccessToken: accessToken,
		}
		tokens = append(tokens, token)

		user := User{
			ID:        id,
			FirstName: firstName,
			LastName:  lastName,
		}
		users = append(users, user)
	}

	return tokens, users, nil
}

func storeUsersAndTokens(rds redis.Conn, token oauth2.Token, user User) error {
	s := errRedisSender{
		rds: rds,
	}

	s.Send(
		"HMSET", fmt.Sprintf("userAndToken:%s", user.ID),
		"id", user.ID,
		"token", token.AccessToken,
		"firstName", user.FirstName,
		"lastName", user.LastName,
	)
	s.Send("LPUSH", "userAndTokens", user.ID)
	return s.err
}

type errRedisSender struct {
	rds redis.Conn
	err error
}

func (s *errRedisSender) Send(cmd string, args ...interface{}) {
	if s.err != nil {
		return
	}
	s.rds.Send(cmd, args...)
}

func postToSlack(text string) {
	jsonStr, err := json.Marshal(Slack{Text: text, Username: "MESHI", IconEmoji: ":just_do_it:"})
	if err != nil {
		fmt.Println(err)
	}
	http.PostForm(slackIncomingWebhookURL, url.Values{"payload": {string(jsonStr)}})
}

func getReceivedMessage(body []byte) string {
	parsed, err := url.ParseQuery(string(body))
	if err != nil {
		fmt.Println(err)
	}
	return parsed["text"][0]
}

func getSentUserName(body []byte) (string, bool, error) {
	parsed, err := url.ParseQuery(string(body))
	if err != nil {
		return "", false, err
	}
	username, ok := parsed["user_name"]
	if !ok || len(username) == 0 {
		return "", false, nil
	}
	return username[0], true, nil
}

func sel(rds redis.Conn, place string) string {
	tokens, users, err := fetchUsersAndTokens(rds)
	if err != nil {
		fmt.Println(err)
	}

	wg := &sync.WaitGroup{}
	candChan := make(chan Cand)
	for i, token := range tokens {
		user := users[i]
		wg.Add(1)

		go func(token oauth2.Token, user User) {
			fclient := conf.Client(oauth2.NoContext, &token)
			fresp, err := fclient.Get(fmt.Sprintf("https://api.foursquare.com/v2/users/self/venuehistory?oauth_token=%s&v=20170801", token.AccessToken))
			if err != nil {
				fmt.Println(err)
			}

			r := Resp{}
			err = json.NewDecoder(fresp.Body).Decode(&r)
			if err != nil {
				fmt.Println(err)
			}

			mclient, err := maps.NewClient(maps.WithAPIKey("AIzaSyCpAcJuShwvg9XhepXzvork-erXf5fcT1w"))
			if err != nil {
				fmt.Println(err)
			}

			p := place
			if p == "" {
				p = "会津若松"
			}

			mresp, err := mclient.TextSearch(context.Background(), &maps.TextSearchRequest{Query: p})
			if err != nil {
				fmt.Println(err)
			}

			loc1 := mresp.Results[0].Geometry.Location
			for _, item := range r.Response.Venues.Items {
				loc2 := item.Venue.Location
				radius := geo.NewPoint(loc1.Lat, loc1.Lng).GreatCircleDistance(geo.NewPoint(loc2.Lat, loc2.Lng))
				if !isRestaurant(item.Venue) || radius > 40 {
					continue
				}

				candChan <- Cand{
					Venue: item.Venue,
					User:  user,
				}
			}
			wg.Done()
		}(token, user)
	}

	go func() {
		wg.Wait()
		close(candChan)
	}()

	ans, err := chooseChan(candChan)
	if err != nil {
		fmt.Println(err)
		return ""
	}
	return fmt.Sprintf("%s (by %s %s)", ans.Venue.Name, ans.User.FirstName, ans.User.LastName)
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
