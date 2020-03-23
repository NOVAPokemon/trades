package main

import (
	ws "github.com/NOVAPokemon/utils/websockets"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
)

var hub = &Hub{
	Trades: make(map[primitive.ObjectID]*ws.Lobby),
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