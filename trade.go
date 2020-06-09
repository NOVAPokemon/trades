package main

import (
	"errors"
	"fmt"
	"sync"

	"github.com/NOVAPokemon/utils/clients"
	"github.com/NOVAPokemon/utils/items"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type TradeLobby struct {
	joinLock       sync.Mutex
	expected       [2]string
	wsLobby        *ws.Lobby
	status         *trades.TradeStatus
	availableItems [2]trades.ItemsMap
	initialHashes  [2][]byte
	authTokens     [2]string
	started        chan struct{}
	rejected       chan struct{}
}

func (lobby *TradeLobby) AddTrainer(username string, items map[string]items.Item, itemsHash []byte,
	authToken string, trainerConn *websocket.Conn) int64 {
	lobby.joinLock.Lock()
	defer lobby.joinLock.Unlock()

	numJoined := lobby.wsLobby.TrainersJoined
	lobby.availableItems[numJoined] = items
	lobby.authTokens[numJoined] = authToken
	lobby.initialHashes[numJoined] = itemsHash
	return ws.AddTrainer(lobby.wsLobby, username, trainerConn)
}

func (lobby *TradeLobby) StartTrade() error {

	players := [2]trades.Player{
		{Items: []items.Item{}, Accepted: false},
		{Items: []items.Item{}, Accepted: false},
	}

	lobby.status = &trades.TradeStatus{
		Players: players,
	}

	return lobby.tradeMainLoop()
}

func (lobby *TradeLobby) tradeMainLoop() error {
	wsLobby := lobby.wsLobby

	wsLobby.Started = true
	updateClients(ws.StartMessage{}.SerializeToWSMessage(), wsLobby.TrainerOutChannels[0], wsLobby.TrainerOutChannels[1])
	close(lobby.started)

	var (
		trainerNum   int
		tradeMessage *ws.Message
		msgStr       *string
	)

	for {
		select {
		case str, ok := <-*wsLobby.TrainerInChannels[0]:
			if !ok {
				continue
			}
			trainerNum = 0
			msgStr = str
		case str, ok := <-*wsLobby.TrainerInChannels[1]:
			if !ok {
				continue
			}
			trainerNum = 1
			msgStr = str
		case <-wsLobby.EndConnectionChannels[0]:
			return errors.New("error during trade on user 0")
		case <-wsLobby.EndConnectionChannels[1]:
			return errors.New("error during trade on user 1")
		}

		tradeMessage = handleChannelMessage(msgStr, &lobby.availableItems, lobby.status, trainerNum)
		if tradeMessage == nil {
			log.Error("trade message was nil")
			return nil
		}

		switch tradeMessage.MsgType {
		case ws.Error:
			updateClients(tradeMessage, wsLobby.TrainerOutChannels[trainerNum])
		case trades.Update:
			updateClients(tradeMessage, wsLobby.TrainerOutChannels[0], wsLobby.TrainerOutChannels[1])
		}

		if lobby.status.TradeFinished {
			return nil
		}
	}
}

func (lobby *TradeLobby) finish() {
	finishMessage := ws.FinishMessage{Success: true}.SerializeToWSMessage()

	updateClients(finishMessage, lobby.wsLobby.TrainerOutChannels[0], lobby.wsLobby.TrainerOutChannels[1])

	<-lobby.wsLobby.EndConnectionChannels[0]
	<-lobby.wsLobby.EndConnectionChannels[1]
}

func (lobby *TradeLobby) sendTokenToUser(trainersClient *clients.TrainersClient, trainerNum int) {
	updateClients(
		ws.SetTokenMessage{TokensString: []string{trainersClient.ItemsToken}}.SerializeToWSMessage(),
		lobby.wsLobby.TrainerOutChannels[trainerNum])
}

func handleChannelMessage(msgStr *string, availableItems *[2]trades.ItemsMap,
	status *trades.TradeStatus, trainerNum int) *ws.Message {

	msg, err := ws.ParseMessage(msgStr)
	if err != nil {
		return trades.ErrorParsing
	}

	return handleMessage(msg, availableItems, status, trainerNum)
}

func handleMessage(message *ws.Message, availableItems *[2]trades.ItemsMap,
	status *trades.TradeStatus, trainerNum int) *ws.Message {

	switch message.MsgType {
	case trades.Trade:
		return handleTradeMessage(message, availableItems, status, trainerNum)
	case trades.Accept:
		return handleAcceptMessage(message, status, trainerNum)
	default:
		return ws.ErrorMessage{
			Info:  fmt.Sprintf("invalid msg type %s", message.MsgType),
			Fatal: false,
		}.SerializeToWSMessage()
	}
}

func handleTradeMessage(message *ws.Message, availableItems *[2]trades.ItemsMap,
	trade *trades.TradeStatus, trainerNum int) *ws.Message {
	desMsg, err := trades.DeserializeTradeMessage(message)
	if err != nil {
		log.Error(err)
		return nil
	}

	tradeMsg := desMsg.(*trades.TradeMessage)

	itemId := tradeMsg.ItemId
	item, ok := (*availableItems)[trainerNum][itemId]

	if !ok {
		return ws.ErrorMessage{
			Info:  fmt.Sprintf("you dont have %s", itemId),
			Fatal: false,
		}.SerializeToWSMessage()
	} else {
		for _, itemAdded := range trade.Players[trainerNum].Items {
			if itemAdded.Id.Hex() == itemId {
				return ws.ErrorMessage{
					Info:  fmt.Sprintf("you already added %s", itemId),
					Fatal: false,
				}.SerializeToWSMessage()
			}
		}
	}

	trade.Players[trainerNum].Items = append(trade.Players[trainerNum].Items, item)
	return trades.UpdateMessageFromTrade(trade, tradeMsg.TrackedMessage).SerializeToWSMessage()
}

func handleAcceptMessage(message *ws.Message, trade *trades.TradeStatus, trainerNum int) *ws.Message {
	desMsg, err := trades.DeserializeTradeMessage(message)
	if err != nil {
		log.Error(err)
		return nil
	}

	acceptMsg := desMsg.(*trades.AcceptMessage)
	trade.Players[trainerNum].Accepted = true

	if checkIfBattleFinished(trade) {
		trade.TradeFinished = true
	}

	return trades.UpdateMessageFromTrade(trade, acceptMsg.TrackedMessage).SerializeToWSMessage()
}

func updateClients(msg *ws.Message, sendTo ...*ws.SyncChannel) {
	for _, channel := range sendTo {
		_ = channel.Write(ws.GenericMsg{
			MsgType: websocket.TextMessage,
			Data:    []byte(msg.Serialize()),
		})
	}
}

func checkIfBattleFinished(trade *trades.TradeStatus) bool {
	return trade.Players[0].Accepted && trade.Players[1].Accepted
}
