package main

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/pkg/errors"
	xcontext "golang.org/x/net/context"
	"golang.org/x/oauth2"
	"googlemaps.github.io/maps"
)

type MapsClientMock struct{}

func (MapsClientMock) TextSearch(ctx xcontext.Context, r *maps.TextSearchRequest) (maps.PlacesSearchResponse, error) {
	res := maps.PlacesSearchResponse{
		Results: []maps.PlacesSearchResult{
			{
				Geometry: maps.AddressGeometry{
					Location: maps.LatLng{
						Lat: 37.52345,
						Lng: 139.92345,
					},
				},
			},
		},
	}

	return res, nil
}

type UserAndTokenStoreMock struct {
	token *oauth2.Token
}

func (m UserAndTokenStoreMock) fetchUsersAndTokens() ([]oauth2.Token, []User, error) {
	return []oauth2.Token{*m.token}, []User{
		{
			ID:        "id",
			FirstName: "firstname",
			LastName:  "lastname",
		},
	}, nil
}

var tokenMock = oauth2.Token{
	AccessToken: "accesstoken",
}

var doUpdate = flag.Bool("update", false, "update stored response from actual data for test")

func (UserAndTokenStoreMock) storeUsersAndTokens(oauth2.Token, User) error {
	return nil
}

func Test_MeshiTopic(t *testing.T) {
	mc := MapsClientMock{}

	muxAPI := http.NewServeMux()
	muxAPI.HandleFunc("/v2/users/self/venuehistory", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/venuehistory-1.json")
	})

	testAPIServer := httptest.NewServer(muxAPI)
	defer testAPIServer.Close()

	t.Log(testAPIServer.URL)
	client := testAPIServer.Client()

	var f *os.File
	var foursquareBaseURL string
	var foursquareBaseHTTPClient *http.Client
	var token *oauth2.Token
	if *doUpdate {
		var err error
		f, err = os.Create("testdata/venuehistory-1.json")
		if err != nil {
			t.Error(errors.Wrap(err, "could not open file to update test data"))
			return
		}

		token = &oauth2.Token{
			AccessToken: os.Getenv("TEST_FOURSQUARE_ACCESSTOKEN"),
		}
	} else {
		foursquareBaseURL = testAPIServer.URL
		foursquareBaseHTTPClient = client
		token = &tokenMock
	}

	meshi := MeshiTopic{
		MapClient:                mc,
		UserAndTokenStore:        UserAndTokenStoreMock{token: token},
		FoursquareBaseURL:        foursquareBaseURL,
		FoursquareBaseHTTPClient: foursquareBaseHTTPClient,
		FoursquareResponseLog:    f,
	}
	_, err := meshi.sel("")
	if err != nil {
		t.Error(err)
	}
}
