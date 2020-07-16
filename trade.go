package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/NOVAPokemon/utils/clients"
	errors2 "github.com/NOVAPokemon/utils/clients/errors"
	"github.com/NOVAPokemon/utils/items"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type tradeLobby struct {
	expected [2]string
	wsLobby  *ws.Lobby
	status   *trades.TradeStatus

	availableItems [2]trades.ItemsMap
	itemsLock      sync.Mutex

	initialHashes [2][]byte

	authTokens [2]string
	tokensLock sync.Mutex

	rejected chan struct{}
}

func (lobby *tradeLobby) addTrainer(username string, items map[string]items.Item, itemsHash []byte,
	authToken string, trainerConn *websocket.Conn, manager ws.CommunicationManager) (int, error) {
	trainersJoined, err := ws.AddTrainer(lobby.wsLobby, username, trainerConn, manager)
	if err != nil {
		return -1, errors2.WrapAddTrainerError(err)
	}

	lobby.itemsLock.Lock()
	lobby.availableItems[trainersJoined-1] = items
	lobby.itemsLock.Unlock()

	lobby.tokensLock.Lock()
	lobby.authTokens[trainersJoined-1] = authToken
	lobby.tokensLock.Unlock()

	lobby.initialHashes[trainersJoined-1] = itemsHash
	return trainersJoined, nil
}

func (lobby *tradeLobby) startTrade() error {
	players := [2]trades.Player{
		{Items: []items.Item{}, Accepted: false},
		{Items: []items.Item{}, Accepted: false},
	}

	lobby.status = &trades.TradeStatus{
		Players: players,
	}
	return lobby.tradeMainLoop()
}

func (lobby *tradeLobby) tradeMainLoop() error {
	wsLobby := lobby.wsLobby
	updateClients(ws.StartMessage{}, wsLobby.TrainerOutChannels[0],
		wsLobby.TrainerOutChannels[1])
	ws.StartLobby(wsLobby)
	emitTradeStart()

	var (
		trainerNum int
		msg        string
		ok         bool
	)

	for {
		select {
		case msg, ok = <-wsLobby.TrainerInChannels[0]:
			if !ok {
				continue
			}
			trainerNum = 0
		case msg, ok = <-wsLobby.TrainerInChannels[1]:
			if !ok {
				continue
			}
			trainerNum = 1
		case <-wsLobby.DoneListeningFromConn[0]:
			return errors.New("error during trade on user 0")
		case <-wsLobby.DoneListeningFromConn[1]:
			return errors.New("error during trade on user 1")
		case <-wsLobby.DoneWritingToConn[0]:
			return errors.New("error during trade on user 0")
		case <-wsLobby.DoneWritingToConn[1]:
			return errors.New("error during trade on user 1")
		}

		lobby.handleChannelMessage(msg, lobby.status, trainerNum)

		if lobby.status.TradeFinished {
			return nil
		}
	}
}

func (lobby *tradeLobby) finish() {
	finishMessage := ws.FinishMessage{Success: true}
	updateClients(finishMessage, lobby.wsLobby.TrainerOutChannels[0], lobby.wsLobby.TrainerOutChannels[1])

	wg := sync.WaitGroup{}
	for i := 0; i < ws.GetTrainersJoined(lobby.wsLobby); i++ {
		wg.Add(1)
		trainerNr := i
		go func() {
			defer wg.Done()
			select {
			case <-lobby.wsLobby.DoneListeningFromConn[trainerNr]:
			case <-time.After(3 * time.Second):
			}
		}()
	}
	wg.Wait()

	ws.FinishLobby(lobby.wsLobby)
}

func (lobby *tradeLobby) sendTokenToUser(trainersClient *clients.TrainersClient, trainerNum int) {
	updateClients(
		ws.SetTokenMessage{TokensString: []string{trainersClient.ItemsToken}},
		lobby.wsLobby.TrainerOutChannels[trainerNum])
}

func (lobby *tradeLobby) handleChannelMessage(msgStr string, status *trades.TradeStatus, trainerNum int) {
	msg, err := ws.ParseMessage(msgStr)
	var answerMsg ws.Serializable
	if err != nil {
		answerMsg = trades.ErrorParsing
	} else {
		answerMsg = lobby.handleMessage(msg, status, trainerNum)
	}

	if answerMsg == nil {
		return
	}

	switch answerMsg.SerializeToWSMessage().MsgType {
	case ws.Error:
		updateClients(answerMsg, lobby.wsLobby.TrainerOutChannels[trainerNum])
	case trades.Update:
		updateClients(answerMsg, lobby.wsLobby.TrainerOutChannels[0], lobby.wsLobby.TrainerOutChannels[1])
	}
}

func (lobby *tradeLobby) handleMessage(message *ws.Message, status *trades.TradeStatus,
	trainerNum int) ws.Serializable {

	switch message.MsgType {
	case trades.Trade:
		return lobby.handleTradeMessage(message, status, trainerNum)
	case trades.Accept:
		return lobby.handleAcceptMessage(message, status, trainerNum)
	default:
		return ws.ErrorMessage{
			Info:  fmt.Sprintf("invalid msg type %s", message.MsgType),
			Fatal: false,
		}
	}
}

func (lobby *tradeLobby) handleTradeMessage(message *ws.Message, trade *trades.TradeStatus,
	trainerNum int) ws.Serializable {
	desMsg, err := trades.DeserializeTradeMessage(message)
	if err != nil {
		log.Error(err)
		return nil
	}

	tradeMsg := desMsg.(*trades.TradeMessage)

	itemId := tradeMsg.ItemId

	lobby.itemsLock.Lock()
	item, ok := lobby.availableItems[trainerNum][itemId]
	lobby.itemsLock.Unlock()

	if !ok {
		return ws.ErrorMessage{
			Info:  fmt.Sprintf("you dont have %s", itemId),
			Fatal: false,
		}
	} else {
		for _, itemAdded := range trade.Players[trainerNum].Items {
			if itemAdded.Id.Hex() == itemId {
				return ws.ErrorMessage{
					Info:  fmt.Sprintf("you already added %s", itemId),
					Fatal: false,
				}
			}
		}
	}

	trade.Players[trainerNum].Items = append(trade.Players[trainerNum].Items, item)
	return trades.UpdateMessageFromTrade(trade, tradeMsg.TrackedMessage)
}

func (lobby *tradeLobby) handleAcceptMessage(message *ws.Message, trade *trades.TradeStatus,
	trainerNum int) ws.Serializable {
	desMsg, err := trades.DeserializeTradeMessage(message)
	if err != nil {
		log.Error(err)
		return nil
	}

	acceptMsg := desMsg.(*trades.AcceptMessage)
	trade.Players[trainerNum].Accepted = true

	if checkIfTradeFinished(trade) {
		trade.TradeFinished = true
	}

	return trades.UpdateMessageFromTrade(trade, acceptMsg.TrackedMessage)
}

func updateClients(msg ws.Serializable, sendTo ...chan ws.Serializable) {
	for _, channel := range sendTo {
		channel <- msg
	}
}

func checkIfTradeFinished(trade *trades.TradeStatus) bool {
	return trade.Players[0].Accepted && trade.Players[1].Accepted
}
