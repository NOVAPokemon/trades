package main

import (
	"encoding/json"
	"fmt"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
	"github.com/NOVAPokemon/utils/clients"
	trainerdb "github.com/NOVAPokemon/utils/database/trainer"
	"github.com/NOVAPokemon/utils/notifications"
	"github.com/NOVAPokemon/utils/tokens"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
)

const TradeName = "TRADES"

type Hub struct {
	Trades map[primitive.ObjectID]*TradeLobby
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

	hub.Trades[lobbyId] = &lobby
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

	lobby := hub.Trades[lobbyId]

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

	if !checkItemsToken(claims.Username, itemsClaims.ItemsHash) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	lobby.AddTrainer(claims.Username, itemsClaims.Items, conn)

	if lobby.wsLobby.TrainersJoined == 2 {
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
	} else {
		return
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

func checkItemsToken(username string, itemsHash []byte) bool {
	verify, err := trainersClient.VerifyItems(username, itemsHash)
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
