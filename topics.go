package main

import (
	"context"
	"time"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sync"

	"github.com/acomagu/chatroom-go-v2/chatroom"
	"github.com/garyburd/redigo/redis"
	"github.com/kellydunn/golang-geo"
	"github.com/pkg/errors"
	xcontext "golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
	"googlemaps.github.io/maps"
)

type MapsClientLike interface {
	TextSearch(xcontext.Context, *maps.TextSearchRequest) (maps.PlacesSearchResponse, error)
}

type UserAndTokenStore interface {
	fetchUsersAndTokens() ([]oauth2.Token, []User, error)
	storeUsersAndTokens(oauth2.Token, User) error
}

func topics(addr string) ([]chatroom.Topic, error) {
	getCodeTopic := newGetCodeTopic().talk

	mc, err := maps.NewClient(maps.WithAPIKey("AIzaSyCpAcJuShwvg9XhepXzvork-erXf5fcT1w"))
	if err != nil {
		return nil, errors.Wrap(err, "could not create Google Maps client")
	}
	meshiTopic := newMeshiTopic(addr, mc).talk
	return []chatroom.Topic{getCodeTopic, meshiTopic}, nil
}

type GetCodeTopic struct {}

func newGetCodeTopic() GetCodeTopic {
	return GetCodeTopic{}
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
	UserAndTokenStore        UserAndTokenStore
	MapClient                MapsClientLike
	FoursquareBaseURL        string
	FoursquareBaseHTTPClient *http.Client
	FoursquareResponseLog    *os.File
}

func newMeshiTopic(addr string, mc MapsClientLike) MeshiTopic {
	s := newRedisUserAndTokenStore(addr)
	return MeshiTopic{
		UserAndTokenStore: s,
		MapClient:         mc,
	}
}

func (t MeshiTopic) talk(room chatroom.Room) chatroom.DidTalk {
	text := (<-room.In).(string)
	matches := regexp.MustCompile(`^ご(?:はん|飯)(?:るーれっと|ルーレット)[ 　]?(.*)?$`).FindStringSubmatch(text)
	if len(matches) < 2 {
		return false
	}

	locName := matches[1]

	res, err := t.sel(locName)
	if err != nil {
		log.Print(errors.Wrap(err, "could not create response"))
		return true
	}

	postToSlack(res)
	return true
}

func (t MeshiTopic) sel(place string) (string, error) {
	tokens, users, err := t.UserAndTokenStore.fetchUsersAndTokens()
	if err != nil {
		return "", errors.Wrap(err, "could not fetch data from Redis")
	}
	if len(tokens) == 0 || len(users) == 0 {
		return "だれもいません。", nil
	}

	g := errgroup.Group{}
	candChan := make(chan Cand)
	for i, token := range tokens {
		token, user := token, users[i]

		g.Go(func() error {
			foursquareClientConfig := FoursquareClientConfig{
				Token:          &token,
				BaseURL:        t.FoursquareBaseURL,
				BaseHTTPClient: t.FoursquareBaseHTTPClient,
				ResponseLog:    t.FoursquareResponseLog,
			}

			fc, err := newFoursquareClient(foursquareClientConfig)
			if err != nil {
				return errors.Wrap(err, "could not create foursquare API client")
			}

			venueC, err := t.fetchRestaurantVenuesForUser(fc, place)
			if err != nil {
				return errors.Wrapf(err, "could not fetch venue histories for %s", user.ID)
			}

			for venue := range venueC {
				candChan <- Cand{
					Venue: venue,
					User:  user,
				}
			}

			return nil
		})
	}

	go func() {
		g.Wait()
		close(candChan)
	}()

	ans, err := chooseChan(candChan)
	if err != nil {
		return "", errors.Wrap(err, "could not select one from candidates")
	}

	if err := g.Wait(); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s (by %s %s)", ans.Venue.Name, ans.User.FirstName, ans.User.LastName), nil
}

func (t *MeshiTopic) fetchRestaurantVenuesForUser(fc *FoursquareClient, place string) (chan Venue, error) {
	r, err := fc.getVenueHistory()
	if err != nil {
		return nil, errors.Wrap(err, "could not get venue history")
	}

	p := place
	if p == "" {
		p = "会津若松"
	}

	mresp, err := t.MapClient.TextSearch(context.Background(), &maps.TextSearchRequest{Query: p})
	if err != nil {
		return nil, errors.Wrap(err, "could not search on Google Maps")
	}

	var wg sync.WaitGroup
	venue := make(chan Venue)
	loc1 := mresp.Results[0].Geometry.Location
	for _, item := range r.Response.Venues.Items {
		wg.Add(1)
		go func(item VenueItem) {
			defer wg.Done()

			loc2 := item.Venue.Location
			radius := geo.NewPoint(loc1.Lat, loc1.Lng).GreatCircleDistance(geo.NewPoint(loc2.Lat, loc2.Lng))
			if !isRestaurant(item.Venue) || radius > 10 {
				return
			}

			venue <- item.Venue
		}(item)
	}

	go func() {
		wg.Wait()
		close(venue)
	}()

	return venue, nil
}

type FoursquareClientConfig struct {
	BaseURL        string
	BaseHTTPClient *http.Client
	Token          *oauth2.Token
	ResponseLog    *os.File
}

type FoursquareClient struct {
	baseURL     string
	client      *http.Client
	token       *oauth2.Token
	responseLog *os.File
}

func newFoursquareClient(config FoursquareClientConfig) (*FoursquareClient, error) {
	var baseURL string
	if config.BaseURL != "" {
		baseURL = config.BaseURL
	} else {
		baseURL = "https://api.foursquare.com"
	}

	var ctx xcontext.Context
	if config.BaseHTTPClient != nil {
		ctx = context.WithValue(context.Background(), oauth2.HTTPClient, config.BaseHTTPClient)
	} else {
		ctx = oauth2.NoContext
	}

	client := conf.Client(ctx, config.Token)

	return &FoursquareClient{
		baseURL:     baseURL,
		client:      client,
		token:       config.Token,
		responseLog: config.ResponseLog,
	}, nil
}

func (c FoursquareClient) getVenueHistory() (Resp, error) {
	resp, err := c.client.Get(fmt.Sprintf("%s/v2/users/self/venuehistory?oauth_token=%s&v=20170801", c.baseURL, c.token.AccessToken))
	if err != nil {
		return Resp{}, errors.Wrap(err, "could not get venuehistory of Foursquare")
	}
	// resp.Body = ioutil.NopCloser(io.TeeReader(resp.Body, os.Stdout))

	r := Resp{}
	err = decodeBody(resp, &r, c.responseLog)
	if err != nil {
		return Resp{}, errors.Wrap(err, "could not decode venuehistory body as JSON")
	}

	return r, nil
}

// decodeBody decodes resp.Body as JSON and extract to out, and write to f if
// it's not nil.
func decodeBody(resp *http.Response, out interface{}, f *os.File) error {
	defer resp.Body.Close()

	// For API symmetric testing.
	if f != nil {
		resp.Body = ioutil.NopCloser(io.TeeReader(resp.Body, f))
		defer f.Close()
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

type RedisUserAndTokenStore struct {
	pool *redis.Pool
}

func newRedisUserAndTokenStore(addr string) *RedisUserAndTokenStore {
	pool := &redis.Pool{
		MaxIdle: 3,
		IdleTimeout: 240 * time.Second,
		Dial: func () (redis.Conn, error) {
			return redis.DialURL(addr)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
	return &RedisUserAndTokenStore{
		pool: pool,
	}
}

func (s RedisUserAndTokenStore) fetchUsersAndTokens() ([]oauth2.Token, []User, error) {
	rds := s.pool.Get()
	defer s.pool.Close()

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
			return nil, nil, errors.Wrap(err, "could not scan values from Redis")
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

func (s RedisUserAndTokenStore) storeUsersAndTokens(token oauth2.Token, user User) error {
	rds := s.pool.Get()
	defer s.pool.Close()

	sender := errRedisSender{
		rds: rds,
	}

	sender.Send(
		"HMSET", fmt.Sprintf("userAndToken:%s", user.ID),
		"id", user.ID,
		"token", token.AccessToken,
		"firstName", user.FirstName,
		"lastName", user.LastName,
	)
	sender.Send("LPUSH", "userAndTokens", user.ID)
	return errors.Wrap(sender.err, "could not execute Redis commands correctly")
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
