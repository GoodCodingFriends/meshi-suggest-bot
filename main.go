package main

import (
	"net/http"
)

func main() {
	http.HandleFunc("/authentificated", handleAuthentificatedRequest)
	fmt.Println(http.ListenAndServe(":"+port, nil))
}

func handleAuthentificatedRequest(w http.ResponseWriter, req *http.Request) {
	
}
