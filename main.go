package main

import (
	"math/rand"
	"os"
	"fmt"
	"strings"
	"io/ioutil"
	"net/url"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/foursquare"
	"net/http"
	"encoding/json"
	"github.com/kellydunn/golang-geo"
	"googlemaps.github.io/maps"
	"github.com/acomagu/chatroom-go-v2/chatroom"
	"context"
)

type Slack struct {
	Text string `json:"text"`
}

type Location struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type Category struct {
	Name string `json:"name"`
}

type Venue struct {
	Name string `json:"name"`
	Location Location `json:"location"`
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

var foursquareClientID = os.Getenv("FOURSQUARE_CLIENT_ID")
var foursquareClientSecret = os.Getenv("FOURSQUARE_CLIENT_SECRET")
var endpointURI = os.Getenv("ENDPOINT_URI")
var slackIncomingWebhookURL = os.Getenv("SLACK_INCOMING_WEBHOOK_URL")
var port = os.Getenv("PORT")

var code string
var conf = &oauth2.Config{
	ClientID: foursquareClientID,
	ClientSecret: foursquareClientSecret,
	RedirectURL: endpointURI + "/authenticated",
	Endpoint: foursquare.Endpoint,
}

func main() {
	cr := chatroom.New(topics())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		if getSentUserName(body) == "slackbot" {
			return
		}
		// Pass the received message to Chatroom.
		cr.In <- getReceivedMessage(body)
	})
	http.HandleFunc("/authenticated", func(w http.ResponseWriter, req *http.Request) {
		code = req.URL.Query().Get("code")
	})

	fmt.Println(http.ListenAndServe(":"+port, nil))
}

func postToSlack(text string) {
	jsonStr, _ := json.Marshal(Slack{Text: text})
	http.PostForm(slackIncomingWebhookURL, url.Values{"payload": {string(jsonStr)}})
}

func getReceivedMessage(body []byte) string {
	parsed, _ := url.ParseQuery(string(body))
	return parsed["text"][0]
}

func getSentUserName(body []byte) string {
	parsed, _ := url.ParseQuery(string(body))
	return parsed["user_name"][0]
}

func sel(place string) string {
	tok, err := conf.Exchange(oauth2.NoContext, code)
	if err != nil {
		fmt.Println(err)
	}

	fclient := conf.Client(oauth2.NoContext, tok)
	fresp, err := fclient.Get(fmt.Sprintf("https://api.foursquare.com/v2/users/self/venuehistory?oauth_token=%s&v=20170801", tok.AccessToken))
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

	venues := []string{}
	loc1 := mresp.Results[0].Geometry.Location
	for _, item := range r.Response.Venues.Items {
		loc2 := item.Venue.Location
		radius := geo.NewPoint(loc1.Lat, loc1.Lng).GreatCircleDistance(geo.NewPoint(loc2.Lat, loc2.Lng))
		if !isRestaurant(item.Venue) || radius > 40 {
			continue
		}

		venues = append(venues, item.Venue.Name)
	}

	return choose(venues)
}

func choose(arr []string) string {
	return arr[rand.Intn(len(arr))]
}

func isRestaurant(v Venue) bool {
	for _, c := range v.Categories {
		if strings.HasSuffix(c.Name, "Restaurant") {
			return true
		}
	}
	return false
}
