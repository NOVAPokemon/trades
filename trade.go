package main

import (
	"errors"
	"fmt"
	"github.com/NOVAPokemon/utils"
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

func (lobby *TradeLobby) AddTrainer(username string, items map[string]utils.Item, itemsHash []byte,
	authToken string, trainerConn *websocket.Conn) {
	numJoined := lobby.wsLobby.TrainersJoined
	lobby.availableItems[numJoined] = items
	lobby.authTokens[numJoined] = authToken
	lobby.initialHashes[numJoined] = itemsHash
	ws.AddTrainer(lobby.wsLobby, username, trainerConn)
}

func (lobby *TradeLobby) StartTrade() error {
	players := [2]trades.Players{
		{Items: []*utils.Item{}, Accepted: false},
		{Items: []*utils.Item{}, Accepted: false},
	}
	lobby.status = &trades.TradeStatus{
		Players: players,
	}

	return lobby.tradeMainLoop()
}

func (lobby *TradeLobby) tradeMainLoop() error {
	wsLobby := lobby.wsLobby

	updateClients(&ws.Message{
		MsgType: trades.START,
		MsgArgs: nil,
	}, wsLobby.TrainerOutChannels[0], wsLobby.TrainerOutChannels[1])
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

func (lobby *TradeLobby) finish() error {
	wsLobby := lobby.wsLobby

	if err := lobby.sendTokenToUser(0); err != nil {
		return err
	}

	if err := lobby.sendTokenToUser(1); err != nil {
		return err
	}

	finishMessage := &ws.Message{
		MsgType: trades.FINISH,
		MsgArgs: nil,
	}
	updateClients(finishMessage, wsLobby.TrainerOutChannels[0], wsLobby.TrainerOutChannels[1])

	<-lobby.wsLobby.EndConnectionChannels[0]
	<-lobby.wsLobby.EndConnectionChannels[1]

	return nil
}

func (lobby *TradeLobby) sendTokenToUser(trainerNum int) error {
	err := trainersClient.GetItemsToken(lobby.expected[trainerNum], lobby.authTokens[trainerNum])
	log.Info("got ", trainersClient.ItemsClaims.ItemsHash)
	if err != nil {
		return err
	}

	setTokensMessage := &ws.Message{
		MsgType: trades.SET_TOKEN,
		MsgArgs: []string{trainersClient.ItemsToken},
	}

	updateClients(setTokensMessage, lobby.wsLobby.TrainerOutChannels[trainerNum])

	return nil
}

func handleChannelMessage(msgStr *string, availableItems *[2]trades.ItemsMap,
	status *trades.TradeStatus, trainerNum int) *ws.Message {

	msg, err := ws.ParseMessage(msgStr)
	if err != nil {
		return &ws.Message{
			MsgType: trades.ERROR,
			MsgArgs: []string{"error parsing message"},
		}
	}

	return handleMessage(msg, availableItems, status, trainerNum)
}

func handleMessage(message *ws.Message, availableItems *[2]trades.ItemsMap,
	status *trades.TradeStatus, trainerNum int) *ws.Message {

	switch message.MsgType {
	case trades.TRADE:
		return handleTradeMessage(message, availableItems, status, trainerNum)
	case trades.ACCEPT:
		return handleAcceptMessage(message, status, trainerNum)
	default:
		return &ws.Message{MsgType: trades.ERROR, MsgArgs: []string{fmt.Sprintf("invalid msg type %s", message.MsgType)}}
	}
}

func handleTradeMessage(message *ws.Message, availableItems *[2]trades.ItemsMap,
	trade *trades.TradeStatus, trainerNum int) *ws.Message {
	if len(message.MsgArgs) > 1 {
		return &ws.Message{MsgType: trades.ERROR, MsgArgs: []string{"can only add one item to trade at a time"}}
	}

	itemId := message.MsgArgs[0]
	item, ok := (*availableItems)[trainerNum][itemId]
	if !ok {
		return &ws.Message{MsgType: trades.ERROR, MsgArgs: []string{fmt.Sprintf("you dont have %s", itemId)}}
	} else {
		for _, itemAdded := range trade.Players[trainerNum].Items {
			if itemAdded.Id.Hex() == itemId {
				return &ws.Message{MsgType: trades.ERROR, MsgArgs: []string{fmt.Sprintf("you already added %s", itemId)}}
			}
		}
	}

	trade.Players[trainerNum].Items = append(trade.Players[trainerNum].Items, &item)
	return &ws.Message{MsgType: trades.UPDATE, MsgArgs: []string{fmt.Sprintf("%+v", *trade)}}

}

func handleAcceptMessage(message *ws.Message, trade *trades.TradeStatus, trainerNum int) *ws.Message {
	if len(message.MsgArgs) != 0 {
		return &ws.Message{
			MsgType: trades.ERROR,
			MsgArgs: []string{"accept should not take any args"},
		}
	}

	trade.Players[trainerNum].Accepted = true

	if checkIfBattleFinished(trade) {
		trade.TradeFinished = true
	}

	return &ws.Message{MsgType: trades.UPDATE, MsgArgs: []string{fmt.Sprintf("%+v", *trade)}}
}

func updateClients(msg *ws.Message, sendTo ...*chan *string) {
	for _, channel := range sendTo {
		ws.SendMessage(*msg, *channel)
	}
}

func checkIfBattleFinished(trade *trades.TradeStatus) bool {
	return trade.Players[0].Accepted && trade.Players[1].Accepted
}
