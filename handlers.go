package main

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

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
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type (
	valueType = *tradeLobby
)

const (
	tradeLobbyTimeout = 30
)

var (
	waitingTrades = sync.Map{}
	ongoingTrades = sync.Map{}
	httpClient    = &http.Client{}
	basicClient   = clients.NewBasicClient(false, "")

	serverName          string
	serviceNameHeadless string
	commsManager        ws.CommunicationManager

	notificationsClient *clients.NotificationClient
)

func init() {
	if aux, exists := os.LookupEnv(utils.HostnameEnvVar); exists {
		serverName = aux
	} else {
		log.Fatal("Could not load server name")
	}

	if aux, exists := os.LookupEnv(utils.HeadlessServiceNameEnvVar); exists {
		serviceNameHeadless = aux
	} else {
		log.Fatal("Could not load headless service name")
	}
}

func handleGetLobbies(w http.ResponseWriter, r *http.Request) {
	_, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapGetLobbiesError(err), http.StatusUnauthorized)
		return
	}

	var availableLobbies []utils.Lobby
	waitingTrades.Range(func(key, value interface{}) bool {
		wsLobby := value.(valueType).wsLobby
		select {
		case <-wsLobby.Started:
		default:
			availableLobbies = append(availableLobbies, utils.Lobby{
				Id:       wsLobby.Id,
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

func handleCreateTradeLobby(w http.ResponseWriter, r *http.Request) {
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

	trackedInfo := ws.GetTrackInfoFromHeader(&r.Header)

	lobbyId := primitive.NewObjectID()

	lobby := tradeLobby{
		expected:       [2]string{authClaims.Username, request.Username},
		wsLobby:        ws.NewLobby(lobbyId.Hex(), 2, &trackedInfo),
		availableItems: [2]trades.ItemsMap{},
		initialHashes:  [2]string{},
		rejected:       make(chan struct{}),
		itemsLock:      sync.Mutex{},
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

	waitingTrades.Store(lobbyId.Hex(), &lobby)
	log.Info("created lobby ", lobbyId)

	go cleanLobby(trackedInfo, &lobby)
}

func handleJoinTradeLobby(w http.ResponseWriter, r *http.Request) {
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

	lobbyInterface, ok := waitingTrades.Load(lobbyIdHex)
	if !ok {
		err = newTradeLobbyNotFoundError(lobbyIdHex)
		handleJoinWarning(err, conn)
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

	trainersClient := clients.NewTrainersClient(httpClient, commsManager, basicClient)
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

	trainerNr, err := lobby.addTrainer(claims.Username, itemsClaims.Items, itemsClaims.ItemsHash,
		r.Header.Get(tokens.AuthTokenHeaderName), conn, commsManager)
	if err != nil {
		handleJoinConnError(err, conn)
		return
	}

	if trainerNr == 2 {
		if !atomic.CompareAndSwapInt32(&lobby.initialized, 0, 1) {
			return
		}

		waitingTrades.Delete(lobbyId.Hex())
		ongoingTrades.Store(lobbyId.Hex(), lobby)

		err = lobby.startTrade()
		if err != nil {
			ws.FinishLobby(lobby.wsLobby) // abort lobby on error
		} else { // lobby finished properly
			err = commitChanges(trainersClient, lobby)
			if err != nil {
				ws.FinishLobby(lobby.wsLobby) // abort if commit fails
			} else {
				lobby.finish() // finish gracefully
				log.Infof("closing lobby %s as expected", lobbyIdHex)
			}
		}
		emitTradeFinish()
		ongoingTrades.Delete(lobby.wsLobby.Id)
	} else {
		trackedInfo := ws.GetTrackInfoFromHeader(&r.Header)
		lobby.wsLobby.StartTrackInfo = &trackedInfo
		err = postNotification(lobby.expected[0], lobby.expected[1], lobbyId.Hex(), authToken, trackedInfo)
		if err != nil {
			utils.LogAndSendHTTPError(&w, wrapCreateTradeError(err), http.StatusInternalServerError)
			return
		}
	}
}

func handleRejectTradeLobby(w http.ResponseWriter, r *http.Request) {
	authClaims, err := tokens.ExtractAndVerifyAuthToken(r.Header)
	if err != nil {
		utils.LogAndSendHTTPError(&w, wrapRejectTradeError(err), http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	lobbyIdHex, ok := vars[api.TradeIdVar]
	if !ok {
		utils.LogAndSendHTTPError(&w, wrapRejectTradeError(errorInvalidId), http.StatusBadRequest)
		return
	}

	var lobbyInterface interface{}
	lobbyInterface, ok = ongoingTrades.Load(lobbyIdHex)
	if !ok {
		lobbyInterface, ok = waitingTrades.Load(lobbyIdHex)
		if !ok {
			err = newTradeLobbyNotFoundError(lobbyIdHex)
			utils.LogWarnAndSendHTTPError(&w, wrapRejectTradeError(err), http.StatusNotFound)
			return
		}
	}

	lobby := lobbyInterface.(valueType)
	for _, trainer := range lobby.expected {
		if trainer == authClaims.Username {
			log.Infof("%s rejected invite for lobby %s", trainer, lobbyIdHex)
			close(lobby.rejected)
			return
		}
	}

	err = newPlayerNotExpectedError(authClaims.Username)
	utils.LogAndSendHTTPError(&w, wrapRejectTradeError(err), http.StatusUnauthorized)
}

func handleJoinConnError(err error, conn *websocket.Conn) {
	if errors.Cause(err) == ws.ErrorLobbyAlreadyFinished {
		log.Warn(wrapJoinTradeError(err))
	} else {
		log.Error(wrapJoinTradeError(err))
	}

	if conn == nil {
		return
	}

	err = conn.Close()
	if err != nil {
		log.Error(wrapJoinTradeError(err))
	}
}

func handleJoinWarning(err error, conn *websocket.Conn) {
	log.Warn(wrapJoinTradeError(err))

	if conn == nil {
		return
	}

	err = conn.Close()
	if err != nil {
		log.Error(wrapJoinTradeError(err))
	}
}

func cleanLobby(createdTrackInfo ws.TrackedInfo, lobby *tradeLobby) {
	timer := time.NewTimer(tradeLobbyTimeout * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		if !atomic.CompareAndSwapInt32(&lobby.initialized, 0, 1) {
			return
		}

		if ws.GetTrainersJoined(lobby.wsLobby) > 0 {
			log.Warnf("closing lobby %s since time expired", lobby.wsLobby.Id)
			select {
			case lobby.wsLobby.TrainerOutChannels[0] <- ws.FinishMessage{Success: false}.ConvertToWSMessage():
				select { // wait for proper finish of routine
				case <-lobby.wsLobby.DoneListeningFromConn[0]:
				case <-time.After(5 * time.Second):
				}
			}
		}
		ws.FinishLobby(lobby.wsLobby)
		waitingTrades.Delete(lobby.wsLobby.Id)
	case <-lobby.rejected:
		if ws.GetTrainersJoined(lobby.wsLobby) > 0 {
			select {
			case <-lobby.wsLobby.DoneListeningFromConn[0]:
			default:
				select {
				case lobby.wsLobby.TrainerOutChannels[0] <- trades.RejectTradeMessage{}.
					ConvertToWSMessage(createdTrackInfo):
					select { // wait for proper finish of routine
					case <-lobby.wsLobby.DoneListeningFromConn[0]:
					case <-time.After(5 * time.Second):
					}
				}
			}
		}
		ws.FinishLobby(lobby.wsLobby)
		waitingTrades.Delete(lobby.wsLobby.Id)
	case <-lobby.wsLobby.Started:
	}
}

func commitChanges(trainersClient *clients.TrainersClient, lobby *tradeLobby) error {
	trade := lobby.status

	trainer1Username := lobby.expected[0]
	trainer2Username := lobby.expected[1]

	items1 := trade.Players[0].Items
	items2 := trade.Players[1].Items

	lobby.tokensLock.Lock()
	err := tradeItems(trainersClient, trainer1Username, lobby.authTokens[0], items1, items2)
	if err != nil {
		log.Panicln(wrapCommitChangesError(err))
	}

	lobby.sendTokenToUser(trainersClient, 0)
	err = tradeItems(trainersClient, trainer2Username, lobby.authTokens[1], items2, items1)
	if err != nil {
		log.Panicln(wrapCommitChangesError(err))
	}
	lobby.tokensLock.Unlock()
	lobby.sendTokenToUser(trainersClient, 1)
	log.Info("Changes committed")
	return nil
}

func tradeItems(trainersClient *clients.TrainersClient, username, authToken string,
	toRemove, toAdd []items.Item) error {
	toRemoveIds := make([]string, len(toRemove))
	for i, item := range toRemove {
		toRemoveIds[i] = item.Id
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

func postNotification(sender, receiver, lobbyId, authToken string, info ws.TrackedInfo) error {
	toMarshal := notifications.WantsToTradeContent{
		Username:       sender,
		LobbyId:        lobbyId,
		ServerHostname: serverName,
	}

	contentBytes, err := json.Marshal(toMarshal)
	if err != nil {
		log.Error(err)
		return err
	}

	notification := utils.Notification{
		Id:       primitive.NewObjectID().Hex(),
		Username: receiver,
		Type:     notifications.WantsToTrade,
		Content:  string(contentBytes),
	}

	notificationMsg := notificationMessages.NotificationMessage{
		Notification: notification,
		Info:         info,
	}

	err = notificationsClient.AddNotification(&notificationMsg, authToken)

	if err != nil {
		log.Error(err)
		return err
	}

	return nil
}
