package main

import (
	"encoding/json"
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
	"github.com/NOVAPokemon/utils/clients"
	"github.com/NOVAPokemon/utils/items"
	"github.com/NOVAPokemon/utils/notifications"
	"github.com/NOVAPokemon/utils/tokens"
	ws "github.com/NOVAPokemon/utils/websockets"
	notificationMessages "github.com/NOVAPokemon/utils/websockets/notifications"
	"github.com/NOVAPokemon/utils/websockets/trades"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"net/http"
	"os"
	"sync"
	"time"
)

type keyType = string
type valueType = *TradeLobby

var WaitingTrades = sync.Map{}
var OngoingTrades = sync.Map{}
var httpClient = &http.Client{}
var serverName string

var notificationsClient = clients.NewNotificationClient(nil)

func init() {
	if aux, exists := os.LookupEnv(utils.LocationServerNameEnvVar); exists {
		serverName = aux
	} else {
		log.Fatal("Could not load server name")
	}
}

func HandleGetLobbies(w http.ResponseWriter, r *http.Request) {
	_, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapGetLobbiesError(err), http.StatusUnauthorized)
		return
	}

	var availableLobbies []utils.Lobby
	WaitingTrades.Range(func(key, value interface{}) bool {
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
		utils.LogAndSendHTTPError(&w, wrapGetLobbiesError(err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	_, err = w.Write(js)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapGetLobbiesError(err), http.StatusInternalServerError)
	}
}

func HandleCreateTradeLobby(w http.ResponseWriter, r *http.Request) {
	var request api.CreateLobbyRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCreateTradeError(err), http.StatusBadRequest)
		return
	}

	authClaims, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCreateTradeError(err), http.StatusUnauthorized)
		return
	}

	lobbyId := primitive.NewObjectID()

	sender := authClaims.Username
	receiver := request.Username
	authToken := r.Header.Get(tokens.AuthTokenHeaderName)

	err = postNotification(sender, receiver, lobbyId.Hex(), authToken)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCreateTradeError(err), http.StatusInternalServerError)
		return
	}

	lobby := TradeLobby{
		expected:       [2]string{authClaims.Username, request.Username},
		wsLobby:        ws.NewLobby(lobbyId),
		availableItems: [2]trades.ItemsMap{},
		initialHashes:  [2][]byte{},
		started:        make(chan struct{}),
	}

	resp := api.CreateLobbyResponse{
		LobbyId:    lobbyId.Hex(),
		ServerName: serverName,
	}
	respBytes, err := json.Marshal(resp)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCreateTradeError(err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(respBytes)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapCreateTradeError(err), http.StatusInternalServerError)
		return
	}

	WaitingTrades.Store(lobbyId.Hex(), &lobby)
	log.Info("created lobby ", lobbyId)

	go cleanLobby(lobbyId.Hex(), lobby.wsLobby.EndConnectionChannels[lobby.wsLobby.TrainersJoined])
}

func HandleJoinTradeLobby(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		err = ws.WrapUpgradeConnectionError(err)
		handleJoinConnError(err, conn)
		return
	}

	claims, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		err = ws.WrapUpgradeConnectionError(err)
		handleJoinConnError(err, conn)
		return
	}

	vars := mux.Vars(r)
	lobbyIdHex, ok := vars[api.TradeIdVar]
	if !ok {
		handleJoinConnError(errorNoTradeId, conn)
		return
	}

	lobbyId, err := primitive.ObjectIDFromHex(lobbyIdHex)
	if err != nil {
		handleJoinConnError(errorInvalidId, conn)
		return
	}

	lobbyInterface, ok := WaitingTrades.Load(lobbyIdHex)
	if !ok {
		err = newTradeLobbyNotFoundError(lobbyIdHex)
		handleJoinConnError(err, conn)
		return
	}

	lobby := lobbyInterface.(valueType)
	username := claims.Username
	if lobby.expected[0] != username && lobby.expected[1] != username {
		err = newPlayerNotExpectedError(username)
		handleJoinConnError(err, conn)
		return
	}

	itemsClaims, err := tokens.ExtractAndVerifyItemsToken(r.Header)
	if err != nil {
		handleJoinConnError(err, conn)
		return
	}

	authToken := r.Header.Get(tokens.AuthTokenHeaderName)

	trainersClient := clients.NewTrainersClient(httpClient)
	valid, err := trainersClient.VerifyItems(username, itemsClaims.ItemsHash, authToken)
	if err != nil {
		handleJoinConnError(err, conn)
		return
	}

	if !*valid {
		err = tokens.ErrorInvalidItemsToken
		handleJoinConnError(err, conn)
		return
	}

	lobby.AddTrainer(claims.Username, itemsClaims.Items, itemsClaims.ItemsHash,
		r.Header.Get(tokens.AuthTokenHeaderName), conn)

	if lobby.wsLobby.TrainersJoined == 2 {
		WaitingTrades.Delete(lobbyId)
		OngoingTrades.Store(lobbyId.Hex(), lobby)

		err = lobby.StartTrade()

		if err != nil {
			handleJoinConnError(err, conn)
		} else if lobby.status.TradeFinished {
			log.Info("Finished trade")

			err = commitChanges(trainersClient, lobby)
			if err != nil {
				handleJoinConnError(err, conn)
				return
			}

			lobby.finish()
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

			updateClients(ws.FinishMessage{}.SerializeToWSMessage(), lobby.wsLobby.TrainerOutChannels[0])

			time.Sleep(2 * time.Second)

			ws.CloseLobby(lobby.wsLobby)
			return
		case <-lobby.started:
			return
		}
	}
}

func handleJoinConnError(err error, conn *websocket.Conn) {
	log.Error(wrapJoinTradeError(err))

	if conn == nil {
		return
	}

	err = conn.Close()
	if err != nil {
		log.Error(wrapJoinTradeError(err))
	}
}

func cleanLobby(lobbyId string, endConnection chan struct{}) {
	for {
		select {
		case <-endConnection:
			WaitingTrades.Delete(lobbyId)
			OngoingTrades.Delete(lobbyId)
			return
		}
	}
}

func commitChanges(trainersClient *clients.TrainersClient, lobby *TradeLobby) error {
	trade := lobby.status

	trainer1Username := lobby.expected[0]
	trainer2Username := lobby.expected[1]

	items1 := trade.Players[0].Items
	items2 := trade.Players[1].Items

	err := tradeItems(trainersClient, trainer1Username, lobby.authTokens[0], items1, items2)
	if err != nil {
		return wrapCommitChangesError(err)
	}

	lobby.sendTokenToUser(trainersClient, 0)

	err = tradeItems(trainersClient, trainer2Username, lobby.authTokens[1], items2, items1)
	if err != nil {
		return wrapCommitChangesError(err)
	}

	lobby.sendTokenToUser(trainersClient, 1)

	log.Info("Changes committed")

	return nil
}

func tradeItems(trainersClient *clients.TrainersClient, username, authToken string,
	toRemove, toAdd []items.Item) error {
	toRemoveIds := make([]string, len(toRemove))
	for i, item := range toRemove {
		toRemoveIds[i] = item.Id.Hex()
	}

	if len(toRemove) > 0 {
		_, err := trainersClient.RemoveItems(username, toRemoveIds, authToken)
		if err != nil {
			return wrapTradeItemsError(err)
		}
	}

	if len(toAdd) > 0 {
		_, err := trainersClient.AddItems(username, toAdd, authToken)
		if err != nil {
			return wrapTradeItemsError(err)
		} else {
			log.Info("items were successfully added")
		}
	}

	if len(toRemove) == 0 && len(toAdd) == 0 {
		if err := trainersClient.GetItemsToken(username, authToken); err != nil {
			return wrapTradeItemsError(err)
		}
	}

	return nil
}

func postNotification(sender, receiver, lobbyId string, authToken string) error {
	toMarshal := notifications.WantsToTradeContent{Username: sender, LobbyId: lobbyId, ServerName: serverName}
	contentBytes, err := json.Marshal(toMarshal)
	if err != nil {
		log.Error(err)
		return err
	}

	notification := utils.Notification{
		Username: receiver,
		Type:     notifications.WantsToTrade,
		Content:  contentBytes,
	}

	notificationMsg := notificationMessages.NewNotificationMessage(notification)
	notificationMsg.Emit(ws.MakeTimestamp())
	notificationMsg.LogEmit(notificationMessages.Notification)

	err = notificationsClient.AddNotification(&notificationMsg, authToken)

	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}
