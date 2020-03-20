package main

import (
	"encoding/json"
	"github.com/NOVAPokemon/authentication/auth"
	"github.com/NOVAPokemon/utils"
	trainerdb "github.com/NOVAPokemon/utils/database/trainer"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
	"strings"
)

const TradeName = "TRADES"

type Hub struct {
	Trades map[primitive.ObjectID]*TradeLobby
}

func HandleGetCurrentLobbies(hub *Hub, w http.ResponseWriter, r *http.Request) {
	var availableLobbies = make([]utils.Lobby, 0)

	err, _ := auth.VerifyJWT(&w, r, TradeName)

	if err != nil {
		log.Error("Unauthenticated client")
		return
	}

	for id, lobby := range hub.Trades {
		if !lobby.started {
			availableLobbies = append(availableLobbies, utils.Lobby{
				Id:        id,
				Username: lobby.Trainer1.Username,
			})
		}
	}

	log.Infof("Request for trade lobbies, response %+v", availableLobbies)
	js, err := json.Marshal(availableLobbies)

	if err != nil {
		handleError(&w, "Error marshalling json", err)
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(js)

	if err != nil {
		handleError(&w, "Error writing json to body", err)
	}
}

func HandleCreateTradeLobby(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		handleErrorAndCloseConnection(&w, "Could not upgrade to websocket", conn, err)
		return
	}

	err, claims := auth.VerifyJWT(&w, r, TradeName)

	if err != nil {
		conn.Close()
		return
	}

	err, trainer1 := trainerdb.GetTrainerByUsername(claims.Username)

	if err != nil {
		handleErrorAndCloseConnection(&w, "Error retrieving trainer", conn, err)
		return
	}

	lobbyId := primitive.NewObjectID()
	lobby := NewTrade(lobbyId, trainer1, conn)
	hub.Trades[lobbyId] = lobby
}

func HandleJoinTradeLobby(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn2, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		handleErrorAndCloseConnection(&w, "Connection error", conn2, err)
		return
	}

	err, claims := auth.VerifyJWT(&w, r, TradeName)

	if err != nil {
		conn2.Close()
		return
	}

	splitPath := strings.Split(r.URL.Path, "/")
	lobbyId, err := primitive.ObjectIDFromHex(splitPath[len(splitPath)-1])

	if err != nil {
		handleErrorAndCloseConnection(&w, "Battle id invalid", conn2, err)
		return
	}

	lobby := hub.Trades[lobbyId]

	if lobby == nil {
		handleErrorAndCloseConnection(&w, "Trade missing", conn2, err)
		return
	}

	//TODO error here because of jwt content
	err, trainer2 := trainerdb.GetTrainerByUsername(claims.Username)

	if err != nil {
		handleErrorAndCloseConnection(&w, "Error retrieving trainer with such id", conn2, err)
		return
	}

	JoinTrade(lobby, trainer2, conn2)

	StartTrade(lobby)
}

func handleError(w *http.ResponseWriter, errorString string, err error) {
	log.Error(err)
	http.Error(*w, errorString, http.StatusInternalServerError)
	return
}

func handleErrorAndCloseConnection(w *http.ResponseWriter, errorString string, conn *websocket.Conn, err error) {
	handleError(w, errorString, err)
	(*conn).Close()
}
