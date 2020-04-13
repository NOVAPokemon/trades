package main

import (
	"encoding/json"
	"fmt"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
	"github.com/NOVAPokemon/utils/clients"
	"github.com/NOVAPokemon/utils/items"
	tradeMessages "github.com/NOVAPokemon/utils/messages/trades"
	"github.com/NOVAPokemon/utils/notifications"
	"github.com/NOVAPokemon/utils/tokens"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
	"sync"
	"time"
)

const TradeName = "TRADES"

type keyType = string
type valueType = *TradeLobby

var Trades = sync.Map{}
var httpClient = &http.Client{}

var notificationsClient = clients.NewNotificationClient(fmt.Sprintf("%s:%d", utils.Host, utils.NotificationsPort), nil)

func HandleGetCurrentLobbies(w http.ResponseWriter, r *http.Request) {
	_, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		log.Error("Unauthenticated clients")
		return
	}

	var availableLobbies []utils.Lobby
	Trades.Range(func(key, value interface{}) bool {
		wsLobby := value.(valueType).wsLobby
		if !wsLobby.Started {
			lobbyId, err := primitive.ObjectIDFromHex(key.(keyType))
			if err != nil {
				return false
			}

			availableLobbies = append(availableLobbies, utils.Lobby{
				Id:       lobbyId,
				Username: wsLobby.TrainerUsernames[0],
			})
		}

		return true
	})

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

func HandleCreateTradeLobby(w http.ResponseWriter, r *http.Request) {
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

	Trades.Store(lobbyId.Hex(), &lobby)
	log.Info("created lobby ", lobbyId)

	go cleanLobby(lobbyId.Hex(), lobby.wsLobby.EndConnectionChannels[lobby.wsLobby.TrainersJoined])
}

func HandleJoinTradeLobby(w http.ResponseWriter, r *http.Request) {
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

	lobbyInterface, ok := Trades.Load(lobbyId.Hex())
	if !ok {
		closeConnAndHandleError(conn, &w, "Trade missing", err)
		return
	}

	lobby := lobbyInterface.(valueType)

	if lobby.expected[0] != claims.Username && lobby.expected[1] != claims.Username {
		closeConnAndHandleError(conn, &w, "player not expected in lobby", nil)
		return
	}

	itemsClaims, err := tokens.ExtractAndVerifyItemsToken(r.Header)
	if err != nil {
		return
	}

	trainersClient := clients.NewTrainersClient(fmt.Sprintf("%s:%d", utils.Host, utils.TrainersPort), httpClient)

	if !checkItemsToken(*trainersClient, claims.Username, itemsClaims.ItemsHash, r.Header.Get(tokens.AuthTokenHeaderName)) {
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
			commitChanges(trainersClient, lobby)
			err := checkChanges(trainersClient, lobby)
			if err != nil {
				log.Error(err)
				return
			}

			if err := lobby.finish(trainersClient); err != nil {
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

			if !lobby.wsLobby.Finished {
				return
			}

			updateClients(tradeMessages.FinishMessage{}.Serialize(), lobby.wsLobby.TrainerOutChannels[0])

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

func cleanLobby(lobbyId string, endConnection chan struct{}) {
	for {
		select {
		case <-endConnection:
			Trades.Delete(lobbyId)
			return
		}
	}
}

func commitChanges(trainersClient *clients.TrainersClient, lobby *TradeLobby) {
	trade := lobby.status

	trainer1Username := lobby.expected[0]
	trainer2Username := lobby.expected[1]

	items1 := trade.Players[0].Items
	items2 := trade.Players[1].Items

	if len(items1) > 0 {
		tradeItems(trainersClient, trainer1Username, trainer2Username, lobby.authTokens[0], lobby.authTokens[1], items1)
	}

	if len(items2) > 0 {
		tradeItems(trainersClient, trainer2Username, trainer1Username, lobby.authTokens[1], lobby.authTokens[0], items2)
	}

	_ = trainersClient.GetItemsToken(trainer1Username, lobby.authTokens[0])
	_ = lobby.sendTokenToUser(trainersClient, 0)

	_ = trainersClient.GetItemsToken(trainer2Username, lobby.authTokens[1])
	_ = lobby.sendTokenToUser(trainersClient, 1)

	log.Warn("Changes committed")
}

func checkChanges(trainersClient *clients.TrainersClient, lobby *TradeLobby) error {
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

func tradeItems(trainersClient *clients.TrainersClient, fromUsername, toUsername, fromAuthToken, toAuthToken string, items []items.Item) {
	itemIds := make([]string, len(items))
	for i, item := range items {
		itemIds[i] = item.Id.Hex()
	}

	_, err := trainersClient.RemoveItemsFromBag(fromUsername, itemIds, fromAuthToken)
	if err != nil {
		log.Error(err)
		return
	}

	_, err = trainersClient.AddItemsToBag(toUsername, items, toAuthToken)
	if err != nil {
		log.Error(err)
	} else {
		log.Info("items were successfully added")
	}

	/*
		if err := clients.CheckItemsAdded(items, result); err != nil {
			log.Error(err)
		} else {
			log.Info("items were successfully added")
		}
	*/
}

func checkItemsToken(trainersClient clients.TrainersClient, username string, itemsHash []byte, authToken string) bool {
	verify, err := trainersClient.VerifyItems(username, itemsHash, authToken)
	if err != nil {
		log.Error(err)
		return false
	}

	log.Warn("hashes are equal: ", *verify)

	return *verify
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
