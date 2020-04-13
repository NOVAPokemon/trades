package main

import (
	"errors"
	"fmt"
	"github.com/NOVAPokemon/utils/clients"
	"github.com/NOVAPokemon/utils/items"
	tradeMessages "github.com/NOVAPokemon/utils/messages/trades"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

type TradeLobby struct {
	expected       [2]string
	wsLobby        *ws.Lobby
	status         *trades.TradeStatus
	availableItems [2]trades.ItemsMap
	initialHashes  [2][]byte
	authTokens     [2]string
	started        chan struct{}
}

func (lobby *TradeLobby) AddTrainer(username string, items map[string]items.Item, itemsHash []byte,
	authToken string, trainerConn *websocket.Conn) {
	numJoined := lobby.wsLobby.TrainersJoined
	lobby.availableItems[numJoined] = items
	lobby.authTokens[numJoined] = authToken
	lobby.initialHashes[numJoined] = itemsHash
	ws.AddTrainer(lobby.wsLobby, username, trainerConn)
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

	updateClients(tradeMessages.StartMessage{}.Serialize(), wsLobby.TrainerOutChannels[0], wsLobby.TrainerOutChannels[1])
	close(lobby.started)
	wsLobby.Started = true

	var trainerNum int
	var tradeMessage *ws.Message
	var msgStr *string
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
		case trades.ERROR:
			updateClients(tradeMessage, wsLobby.TrainerOutChannels[trainerNum])
		case trades.UPDATE:
			updateClients(tradeMessage, wsLobby.TrainerOutChannels[0], wsLobby.TrainerOutChannels[1])
		}

		if lobby.status.TradeFinished {
			return nil
		}
	}
}

func (lobby *TradeLobby) finish() {
	finishMessage := tradeMessages.FinishMessage{Success: true}.Serialize()

	updateClients(finishMessage, lobby.wsLobby.TrainerOutChannels[0], lobby.wsLobby.TrainerOutChannels[1])

	<-lobby.wsLobby.EndConnectionChannels[0]
	<-lobby.wsLobby.EndConnectionChannels[1]
}

func (lobby *TradeLobby) sendTokenToUser(trainersClient *clients.TrainersClient, trainerNum int) {
	updateClients(
		tradeMessages.SetTokenMessage{TokenString: trainersClient.ItemsToken}.Serialize(),
		lobby.wsLobby.TrainerOutChannels[trainerNum])
}

func handleChannelMessage(msgStr *string, availableItems *[2]trades.ItemsMap,
	status *trades.TradeStatus, trainerNum int) *ws.Message {

	msg, err := ws.ParseMessage(msgStr)
	if err != nil {
		return tradeMessages.ErrorParsing
	}

	return handleMessage(msg, availableItems, status, trainerNum)
}

func handleMessage(message *ws.Message, availableItems *[2]trades.ItemsMap,
	status *trades.TradeStatus, trainerNum int) *ws.Message {

	switch message.MsgType {
	case trades.TRADE:
		return handleTradeMessage(message, availableItems, status, trainerNum)
	case trades.ACCEPT:
		return handleAcceptMessage(status, trainerNum)
	default:
		return tradeMessages.ErrorMessage{
			Info:  fmt.Sprintf("invalid msg type %s", message.MsgType),
			Fatal: false,
		}.Serialize()
	}
}

func handleTradeMessage(message *ws.Message, availableItems *[2]trades.ItemsMap,
	trade *trades.TradeStatus, trainerNum int) *ws.Message {
	if len(message.MsgArgs) > 1 {
		return tradeMessages.ErrorOneItemAtATime
	}

	itemId := message.MsgArgs[0]
	item, ok := (*availableItems)[trainerNum][itemId]
	if !ok {
		return tradeMessages.ErrorMessage{
			Info:  fmt.Sprintf("you dont have %s", itemId),
			Fatal: false,
		}.Serialize()
	} else {
		for _, itemAdded := range trade.Players[trainerNum].Items {
			if itemAdded.Id.Hex() == itemId {
				return tradeMessages.ErrorMessage{
					Info:  fmt.Sprintf("you already added %s", itemId),
					Fatal: false,
				}.Serialize()
			}
		}
	}

	trade.Players[trainerNum].Items = append(trade.Players[trainerNum].Items, item)
	return tradeMessages.UpdateMessageFromTrade(trade).Serialize()
}

func handleAcceptMessage(trade *trades.TradeStatus, trainerNum int) *ws.Message {
	trade.Players[trainerNum].Accepted = true

	if checkIfBattleFinished(trade) {
		trade.TradeFinished = true
	}

	return tradeMessages.UpdateMessageFromTrade(trade).Serialize()
}

func updateClients(msg *ws.Message, sendTo ...*chan *string) {
	for _, channel := range sendTo {
		ws.SendMessage(*msg, *channel)
	}
}

func checkIfBattleFinished(trade *trades.TradeStatus) bool {
	return trade.Players[0].Accepted && trade.Players[1].Accepted
}
