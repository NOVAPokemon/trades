package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	trainers "github.com/NOVAPokemon/trainers/exported"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/cookies"
	trainerdb "github.com/NOVAPokemon/utils/database/trainer"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

const TradeName = "TRADES"

type Hub struct {
	Trades map[primitive.ObjectID]*ws.Lobby
}

func HandleGetCurrentLobbies(hub *Hub, w http.ResponseWriter, r *http.Request) {
	var availableLobbies = make([]utils.Lobby, 0)

	_, err := cookies.ExtractAndVerifyAuthToken(&w, r, TradeName)

	if err != nil {
		log.Error("Unauthenticated client")
		return
	}

	for id, lobby := range hub.Trades {
		if !lobby.Started {
			availableLobbies = append(availableLobbies, utils.Lobby{
				Id:       id,
				Username: lobby.TrainerUsernames[0],
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

	authClaims, err := cookies.ExtractAndVerifyAuthToken(&w, r, TradeName)
	if err != nil {
		return
	}

	itemsClaims, err := cookies.ExtractItemsToken(r)
	if err != nil {
		return
	}

	lobbyId := primitive.NewObjectID()
	lobby := ws.NewLobby(lobbyId)
	ws.AddTrainer(lobby, authClaims.Username, conn)
	hub.Trades[lobbyId] = lobby

	go cleanLobby(lobby)
}

func HandleJoinTradeLobby(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn2, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		handleError(&w, "Connection error", err)
		return
	}

	claims, err := cookies.ExtractAndVerifyAuthToken(&w, r, TradeName)

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

	trainer2, err := trainerdb.GetTrainerByUsername(claims.Username)

	if err != nil {
		handleError(&w, "Error retrieving trainer with such id", err)
		return
	}

	ws.AddTrainer(lobby, *trainer2, conn2)
	err, trade := StartTrade(lobby)

	if err != nil {
		log.Error(err)
	} else if lobby.Finished {
		log.Info("Finished trade")
		commitChanges(lobby, trade)
	} else {
		log.Error("Something went wrong...")
	}

	ws.CloseLobby(lobby)
}

func handleError(w *http.ResponseWriter, errorString string, err error) {
	log.Error(err)
	http.Error(*w, errorString, http.StatusInternalServerError)
	return
}

func cleanLobby(lobby *ws.Lobby) {
	for {
		select {
		case <-lobby.EndConnectionChannel:
			delete(hub.Trades, lobby.Id)
			return
		}
	}
}

func commitChanges(lobby *ws.Lobby, trade *trades.TradeStatus) {
	trainer1 := lobby.Trainers[0]
	trainer2 := lobby.Trainers[1]

	items1 := trade.Players[0].Items
	items2 := trade.Players[1].Items

	if len(items1) > 0 {
		tradeItems(trainer1.Username, trainer2.Username, items1)
	}
	if len(items2) > 0 {
		tradeItems(trainer2.Username, trainer1.Username, items2)
	}
}

func tradeItems(fromUsername, toUsername string, itemsHex []string) {
	itemObjects := make([]primitive.ObjectID, len(itemsHex))
	for i, item := range itemsHex {
		itemObjects[i], _ = primitive.ObjectIDFromHex(item)
	}

	items, err := trainerdb.RemoveItemsFromTrainer(fromUsername, itemObjects)
	if err != nil {
		log.Error(err)
		return
	}

	_, err = trainerdb.AddItemsToTrainer(toUsername, items)
	if err != nil {
		log.Error(err)
	}
}

func checkItemsToken(itemsHash []byte, cookie *http.Cookie) bool {
	jar, err := cookiejar.New(nil)
	host := fmt.Sprintf("%s:%d", utils.Host, utils.TrainersPort)
	trainerUrl := url.URL{
		Scheme: "http",
		Host:   host,
		Path:   trainers.VerifyItemsPath,
	}

	jar.SetCookies(&trainerUrl, []*http.Cookie{cookie})

	httpClient := &http.Client{
		Jar: jar,
	}

	jsonStr, err := json.Marshal(itemsHash)
	if err != nil {
		log.Error(err)
		return false
	}

	req, err := http.NewRequest("POST", trainerUrl.String(), bytes.NewBuffer(jsonStr))

	if err != nil {
		log.Error(err)
		return false
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)

	if resp.StatusCode != http.StatusOK {
		return false
	}
	return true
}
