package main

import (
	"net/http"
)

var hub = &Hub{
	Trades: make(map[string]*TradeLobby),
}

func GetCurrentLobbies(w http.ResponseWriter, r *http.Request) {
	HandleGetCurrentLobbies(hub, w, r)
}

func CreateTradeLobby(w http.ResponseWriter, r *http.Request) {
	HandleCreateTradeLobby(hub, w, r)
}

func JoinTradeLobby(w http.ResponseWriter, r *http.Request) {
	HandleJoinTradeLobby(hub, w, r)
}
