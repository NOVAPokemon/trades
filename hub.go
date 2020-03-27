package main

import (
	"encoding/json"
	"github.com/NOVAPokemon/authentication/auth"
	"github.com/NOVAPokemon/utils"
	trainerdb "github.com/NOVAPokemon/utils/database/trainer"
	ws "github.com/NOVAPokemon/utils/websockets"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
	"strings"
)

const TradeName = "TRADES"

type Hub struct {
	Trades map[primitive.ObjectID]*ws.Lobby
}

func HandleGetCurrentLobbies(hub *Hub, w http.ResponseWriter, r *http.Request) {
	var availableLobbies = make([]utils.Lobby, 0)

	err, _ := auth.VerifyJWT(&w, r, TradeName)

	if err != nil {
		log.Error("Unauthenticated client")
		return
	}

	for id, lobby := range hub.Trades {
		if !lobby.Started {
			availableLobbies = append(availableLobbies, utils.Lobby{
				Id:       id,
				Username: lobby.Trainers[0].Username,
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
		handleError(&w, "Could not upgrade to websocket", err)
		return
	}

	err, claims := auth.VerifyJWT(&w, r, TradeName)

	if err != nil {
		return
	}

	err, trainer1 := trainerdb.GetTrainerByUsername(claims.Username)

	if err != nil {
		handleError(&w, "Error retrieving trainer", err)
		return
	}

	lobbyId := primitive.NewObjectID()
	lobby := ws.NewLobby(lobbyId)
	ws.AddTrainer(lobby, trainer1, conn)
	hub.Trades[lobbyId] = lobby
}

func HandleJoinTradeLobby(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn2, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		handleError(&w, "Connection error", err)
		return
	}

	err, claims := auth.VerifyJWT(&w, r, TradeName)

	if err != nil {
		return
	}

	splitPath := strings.Split(r.URL.Path, "/")
	lobbyId, err := primitive.ObjectIDFromHex(splitPath[len(splitPath)-1])

	if err != nil {
		handleError(&w, "Battle id invalid", err)
		return
	}

	lobby := hub.Trades[lobbyId]

	if lobby == nil {
		handleError(&w, "Trade missing", err)
		return
	}

	err, trainer2 := trainerdb.GetTrainerByUsername(claims.Username)

	if err != nil {
		handleError(&w, "Error retrieving trainer with such id", err)
		return
	}

	ws.AddTrainer(lobby, trainer2, conn2)
	StartTrade(lobby)
	log.Info("Finished trade")

	ws.CloseLobby(lobby)


	commitChanges(lobby)

	delete(hub.Trades, lobbyId)
}

func handleError(w *http.ResponseWriter, errorString string, err error) {
	log.Error(err)
	http.Error(*w, errorString, http.StatusInternalServerError)
	return
}

