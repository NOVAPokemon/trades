package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
	"github.com/NOVAPokemon/utils/cookies"
	trainerdb "github.com/NOVAPokemon/utils/database/trainer"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
	"net/http/cookiejar"
	"net/url"
)

const TradeName = "TRADES"

type Hub struct {
	Trades map[primitive.ObjectID]*TradeLobby
}

func HandleGetCurrentLobbies(hub *Hub, w http.ResponseWriter, r *http.Request) {
	var availableLobbies = make([]utils.Lobby, 0)

	_, err := cookies.ExtractAndVerifyAuthToken(&w, r, TradeName)

	if err != nil {
		log.Error("Unauthenticated clients")
		return
	}

	for id, lobby := range hub.Trades {
		if !lobby.wsLobby.Started {
			availableLobbies = append(availableLobbies, utils.Lobby{
				Id:       id,
				Username: lobby.wsLobby.TrainerUsernames[0],
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

	log.Info(hub.Trades)

	if err != nil {
		handleError(&w, "Could not upgrade to websocket", err)
		return
	}

	authClaims, err := cookies.ExtractAndVerifyAuthToken(&w, r, TradeName)
	if err != nil {
		return
	}

	for _, cookie := range r.Cookies() {
		log.Warn(cookie.Name)
		log.Info(cookie.Domain)
		log.Info(cookie.Path)
	}

	itemsClaims, err := cookies.ExtractItemsToken(r)
	if err != nil {
		log.Error(err)
		return
	}

	itemsCookie, err := r.Cookie(cookies.ItemsTokenCookieName)
	if err != nil {
		log.Error(err)
		return
	}

	authCookie, err := r.Cookie(cookies.AuthTokenCookieName)
	if err != nil {
		log.Error(err)
		return
	}

	if !checkItemsToken(authClaims.Username, itemsClaims.ItemsHash, itemsCookie, authCookie) {
		log.Error("items token not valid")
		conn.Close()
		return
	}

	lobbyId := primitive.NewObjectID()
	lobby := TradeLobby{
		wsLobby:        ws.NewLobby(lobbyId),
		availableItems: [2]trades.ItemsMap{},
	}
	lobby.AddTrainer(authClaims.Username, itemsClaims.Items, conn)
	hub.Trades[lobbyId] = &lobby

	log.Info(hub.Trades)

	go cleanLobby(lobby.wsLobby)
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

	vars := mux.Vars(r)
	lobbyIdHex, ok := vars[TradeIdVar]
	if !ok {
		handleError(&w, "No battle id provided", err)
		return
	}

	lobbyId, err := primitive.ObjectIDFromHex(lobbyIdHex)
	if err != nil {
		handleError(&w, "Battle id invalid", err)
		return
	}

	itemsClaims, err := cookies.ExtractItemsToken(r)
	if err != nil {
		return
	}

	itemsCookie, err := r.Cookie(cookies.ItemsTokenCookieName)
	if err != nil {
		log.Error(err)
		return
	}

	authCookie, err := r.Cookie(cookies.AuthTokenCookieName)
	if err != nil {
		log.Error(err)
		return
	}

	if !checkItemsToken(claims.Username, itemsClaims.ItemsHash, itemsCookie, authCookie) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	lobby, ok := hub.Trades[lobbyId]
	if !ok {
		handleError(&w, "Trade missing", err)
		return
	}

	lobby.AddTrainer(claims.Username, itemsClaims.Items, conn2)

	err = lobby.StartTrade()

	if err != nil {
		log.Error(err)
	} else if lobby.status.TradeFinished {
		log.Info("Finished trade")
		commitChanges(lobby.wsLobby, lobby.status)
	} else {
		log.Error("Something went wrong...")
	}

	ws.CloseLobby(lobby.wsLobby)
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
	trainer1Username := lobby.TrainerUsernames[0]
	trainer2Username := lobby.TrainerUsernames[1]

	items1 := trade.Players[0].Items
	items2 := trade.Players[1].Items

	if len(items1) > 0 {
		tradeItems(trainer1Username, trainer2Username, items1)
	}
	if len(items2) > 0 {
		tradeItems(trainer2Username, trainer1Username, items2)
	}
}

func tradeItems(fromUsername, toUsername string, items []*utils.Item) {
	itemIds := make([]primitive.ObjectID, len(items))
	for i, item := range items {
		itemIds[i] = item.Id
	}

	_, err := trainerdb.RemoveItemsFromTrainer(fromUsername, itemIds)
	if err != nil {
		log.Error(err)
		return
	}

	_, err = trainerdb.AddItemsToTrainer(toUsername, items)
	if err != nil {
		log.Error(err)
	}
}

func checkItemsToken(username string, itemsHash []byte, cookies ...*http.Cookie) bool {
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Error(err)
		return false
	}

	host := fmt.Sprintf("%s:%d", utils.Host, utils.TrainersPort)
	trainerUrl := url.URL{
		Scheme: "http",
		Host:   host,
		Path:   fmt.Sprintf(api.VerifyItemsPath, username),
	}

	jar.SetCookies(&url.URL{
		Scheme: "http",
		Host:   utils.Host,
		Path:   "/",
	}, cookies)

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

	for _, cookie := range jar.Cookies(&trainerUrl) {
		log.Info(cookie)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Error(err)
		return false
	}

	if resp.StatusCode != http.StatusOK {
		log.Error("status: ", resp.StatusCode)
		return false
	}
	return true
}
