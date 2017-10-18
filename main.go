package main

import (
	"math/rand"
	"fmt"
	"strings"

	// "github.com/acomagu/gcf-slack-bot/slackcr"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/foursquare"
	"net/http"
	"encoding/json"
	"github.com/kellydunn/golang-geo"
	"googlemaps.github.io/maps"
	"context"
)

// var port = os.Getenv("PORT")
// var botAPIToken = os.Getenv("SLACK_BOT_API_TOKEN")
// var godBotAPIToken = os.Getenv("SLACK_GOD_BOT_API_TOKEN")

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

func main() {
	// slackClients := slackcr.NewSlackClients(botAPIToken, godBotAPIToken)
	// slackCr := slackcr.New(slackClients, topics(slackClients))
	// slackCr.Listen(port)

	conf := &oauth2.Config{
		ClientID: "CZEB5SHBPO1LRZN5LKWS1C0LKKCW2GEI1T4DJA3WQWNCIX2X",
		ClientSecret: "3VN1HIOIMTW3OFI5CPZSDP30EMJNBL5EJJXOAPKLVRNSWQJ0",
		RedirectURL: "https://0a8d448e.ngrok.io/authenticated",
		Endpoint: foursquare.Endpoint,
	}
	fmt.Println(conf.AuthCodeURL("state", oauth2.AccessTypeOffline))

	http.HandleFunc("/authenticated", func(w http.ResponseWriter, req *http.Request) {
		code := req.URL.Query().Get("code")
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

		mresp, err := mclient.TextSearch(context.Background(), &maps.TextSearchRequest{Query: "会津若松"})
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

		fmt.Println(choose(venues))
	})
	fmt.Println(http.ListenAndServe(":8080", nil))
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
