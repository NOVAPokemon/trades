package main

import (
	"encoding/json"
	"fmt"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
	"github.com/NOVAPokemon/utils/clients"
	"github.com/NOVAPokemon/utils/notifications"
	"github.com/NOVAPokemon/utils/tokens"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
	"reflect"
	"time"
)

const TradeName = "TRADES"

type Hub struct {
	Trades map[string]*TradeLobby
}

var trainersClient = clients.NewTrainersClient(fmt.Sprintf("%s:%d", utils.Host, utils.TrainersPort))
var notificationsClient = clients.NewNotificationClient(fmt.Sprintf("%s:%d", utils.Host, utils.NotificationsPort), nil)

func HandleGetCurrentLobbies(hub *Hub, w http.ResponseWriter, r *http.Request) {
	var availableLobbies = make([]utils.Lobby, 0)

	_, err := tokens.ExtractAndVerifyAuthToken(r.Header)

	if err != nil {
		log.Error("Unauthenticated clients")
		return
	}

	for id, lobby := range hub.Trades {
		if !lobby.wsLobby.Started {
			lobbyId, err := primitive.ObjectIDFromHex(id)
			if err != nil {
				return
			}

			availableLobbies = append(availableLobbies, utils.Lobby{
				Id:       lobbyId,
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
	var request api.CreateLobbyRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		utils.HandleJSONDecodeError(&w, TradeName, err)
		return
	}

	authClaims, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		return
	}

	lobbyId := primitive.NewObjectID()

	if postNotification(authClaims.Username, request.Username, lobbyId.Hex(), r.Header.Get(tokens.AuthTokenHeaderName)) != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	lobby := TradeLobby{
		expected:       [2]string{authClaims.Username, request.Username},
		wsLobby:        ws.NewLobby(lobbyId),
		availableItems: [2]trades.ItemsMap{},
		initialHashes:  [2][]byte{},
		started:        make(chan struct{}),
	}

	lobbyBytes, err := json.Marshal(lobbyId)
	if err != nil {
		log.Error(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(lobbyBytes)
	if err != nil {
		handleError(&w, "Error writing json to body", err)
	}

	hub.Trades[lobbyId.Hex()] = &lobby
	log.Info("created lobby ", lobbyId)

	go cleanLobby(lobby.wsLobby)
}

func HandleJoinTradeLobby(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)

	if err != nil {
		closeConnAndHandleError(conn, &w, "Connection error", err)
		return
	}

	claims, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		closeConnAndHandleError(conn, &w, "could not extract auth token", err)
		return
	}

	vars := mux.Vars(r)
	lobbyIdHex, ok := vars[api.TradeIdVar]
	if !ok {
		closeConnAndHandleError(conn, &w, "No trade id provided", err)
		return
	}

	lobbyId, err := primitive.ObjectIDFromHex(lobbyIdHex)
	if err != nil {
		closeConnAndHandleError(conn, &w, "Trade id invalid", err)
		return
	}

	lobby := hub.Trades[lobbyId.Hex()]

	if !ok {
		closeConnAndHandleError(conn, &w, "Trade missing", err)
		return
	}

	if lobby.expected[0] != claims.Username && lobby.expected[1] != claims.Username {
		closeConnAndHandleError(conn, &w, "player not expected in lobby", nil)
		return
	}

	itemsClaims, err := tokens.ExtractAndVerifyItemsToken(r.Header)
	if err != nil {
		return
	}

	if !checkItemsToken(claims.Username, itemsClaims.ItemsHash, r.Header.Get(tokens.AuthTokenHeaderName)) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	lobby.AddTrainer(claims.Username, itemsClaims.Items, itemsClaims.ItemsHash, r.Header.Get(tokens.AuthTokenHeaderName), conn)

	if lobby.wsLobby.TrainersJoined == 2 {
		err = lobby.StartTrade()
		if err != nil {
			log.Error(err)
		} else if lobby.status.TradeFinished {
			log.Info("Finished trade")
			commitChanges(lobby)
			err := checkChanges(lobby)
			if err != nil {
				log.Error(err)
				return
			}

			if err := lobby.finish(); err != nil {
				log.Error(err)
				return
			}
		} else {
			log.Error("Something went wrong...")
		}

		log.Info("closing lobby as expected")
		ws.CloseLobby(lobby.wsLobby)
	} else {
		timer := time.NewTimer(10 * time.Second)
		select {
		case <-timer.C:
			log.Error("closing lobby since time expired")
			finishMessage := &ws.Message{
				MsgType: trades.FINISH,
				MsgArgs: nil,
			}
			updateClients(finishMessage, lobby.wsLobby.TrainerOutChannels[0])

			time.Sleep(2 * time.Second)

			ws.CloseLobby(lobby.wsLobby)
			return
		case <-lobby.started:
			return
		}
	}

}

func closeConnAndHandleError(conn *websocket.Conn, w *http.ResponseWriter, errorString string, err error) {
	log.Error("closing connection")
	conn.Close()
	handleError(w, errorString, err)
}

func handleError(w *http.ResponseWriter, errorString string, err error) {
	log.Error(errorString)
	log.Error(err)
	http.Error(*w, errorString, http.StatusInternalServerError)
	return
}

func cleanLobby(lobby *ws.Lobby) {
	for {
		select {
		case <-lobby.EndConnectionChannel:
			delete(hub.Trades, lobby.Id.Hex())
			return
		}
	}
}

func commitChanges(lobby *TradeLobby) {
	trade := lobby.status

	trainer1Username := lobby.expected[0]
	trainer2Username := lobby.expected[1]

	items1 := trade.Players[0].Items
	items2 := trade.Players[1].Items

	if len(items1) > 0 {
		tradeItems(trainer1Username, trainer2Username, lobby.authTokens[0], lobby.authTokens[1], items1)
	}
	if len(items2) > 0 {
		tradeItems(trainer2Username, trainer1Username, lobby.authTokens[1], lobby.authTokens[0], items2)
	}

	log.Warn("Changes committed")
}

func checkChanges(lobby *TradeLobby) error {
	trade := lobby.status
	verified := 0

	for verified < 2 {
		if len(trade.Players[verified].Items) == 0 {
			verified++
			continue
		} else {
			correct, err := trainersClient.VerifyItems(lobby.expected[verified],
				lobby.initialHashes[verified], lobby.authTokens[verified])
			if err != nil {
				return err
			}

			if !*correct {
				verified++
			}
		}
	}

	log.Info("users had their items changed")

	return nil
}

func tradeItems(fromUsername, toUsername, fromAuthToken, toAuthToken string, items []*utils.Item) {
	itemIds := make([]string, len(items))
	for i, item := range items {
		itemIds[i] = item.Id.Hex()
	}

	err := trainersClient.RemoveItemsFromBag(fromUsername, itemIds, fromAuthToken)
	if err != nil {
		log.Error(err)
		return
	}

	result, err := trainersClient.AddItemsToBag(toUsername, items, toAuthToken)
	if err != nil {
		log.Error(err)
	}

	if !checkItemsAdded(items, result) {
		log.Error("items to add were not successfully added")
	} else {
		log.Info("items were successfully added")
	}
}

func checkItemsToken(username string, itemsHash []byte, authToken string) bool {
	verify, err := trainersClient.VerifyItems(username, itemsHash, authToken)
	if err != nil {
		log.Error(err)
		return false
	}

	log.Warn("hashes are equal: ", *verify)

	return *verify
}

func checkItemsAdded(toAdd, added []*utils.Item) bool {
	for i, item := range toAdd {
		item.Id = added[i].Id
	}

	return reflect.DeepEqual(toAdd, added)
}

func postNotification(sender, receiver, lobbyId string, authToken string) error {
	contentBytes, err := json.Marshal(notifications.WantsToTradeContent{Username: sender, LobbyId: lobbyId})
	if err != nil {
		log.Error(err)
		return err
	}

	err = notificationsClient.AddNotification(utils.Notification{
		Username: receiver,
		Type:     notifications.WantsToTrade,
		Content:  contentBytes,
	}, authToken)

	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}
