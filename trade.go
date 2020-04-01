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
}

func (lobby *TradeLobby) AddTrainer(username string, items map[string]utils.Item, trainerConn *websocket.Conn) {
	lobby.availableItems[lobby.wsLobby.TrainersJoined] = items
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
		case <-wsLobby.EndConnectionChannel:
			return errors.New("error during trade")
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
			log.Info("finishing...")
			lobby.finish()
			return nil
		}
	}
}

func (lobby *TradeLobby) finish() {
	wsLobby := lobby.wsLobby

	finishMessage := &ws.Message{
		MsgType: trades.FINISH,
		MsgArgs: nil,
	}

	updateClients(finishMessage, wsLobby.TrainerOutChannels[0], wsLobby.TrainerOutChannels[1])
}

func handleChannelMessage(msgStr *string, availableItems *[2]trades.ItemsMap,
	status *trades.TradeStatus, trainerNum int) *ws.Message {

	log.Infof(*msgStr)
	err, msg := ws.ParseMessage(msgStr)
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
	log.Info(message.MsgType)

	switch message.MsgType {
	case trades.TRADE:
		return handleTradeMessage(message, availableItems, status, trainerNum)
	case trades.ACCEPT:
		return handleAcceptMessage(message, status, trainerNum)
	default:
		return &ws.Message{MsgType: trades.ERROR, MsgArgs: []string{"invalid msg type"}}
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
		return &ws.Message{MsgType: trades.ERROR, MsgArgs: []string{"you dont have that item"}}
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
