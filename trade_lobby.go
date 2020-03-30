package main

import (
	"encoding/json"
	"errors"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	log "github.com/sirupsen/logrus"
)

func StartTrade(lobby *ws.Lobby) (error, *trades.TradeStatus) {
	players := [2]trades.Players{
		{Items: []string{}, Accepted: false},
		{Items: []string{}, Accepted: false},
	}

	tradeStatus := &trades.TradeStatus{
		Players: players,
	}

	return tradeMainLoop(lobby, tradeStatus), tradeStatus
}

func tradeMainLoop(lobby *ws.Lobby, trade *trades.TradeStatus) error {
	for {
		select {
		case msgStr, ok := <-*lobby.TrainerInChannels[0]:
			if handleChannelMessage(msgStr, ok, 0, trade, lobby) {
				return nil
			}
		case msgStr, ok := <-*lobby.TrainerInChannels[1]:
			if handleChannelMessage(msgStr, ok, 1, trade, lobby) {
				return nil
			}
		case <-lobby.EndConnectionChannel:
			return errors.New("error during trade")
		}
	}
}

func handleChannelMessage(msgStr *string, ok bool, trainerNum int, trade *trades.TradeStatus, lobby *ws.Lobby) bool {
	if !ok {
		return false
	}

	trainerChanOut := *lobby.TrainerOutChannels[trainerNum]

	log.Infof(*msgStr)
	err, msg := trades.ParseMessage(msgStr)
	if err != nil {
		handleMessageError(err, trainerChanOut)
		return false
	}

	err, finished := handleMessage(msg, trade, trainerNum)

	if err != nil {
		handleMessageError(err, trainerChanOut)
		return false
	}

	updateClients(trade, lobby)

	if finished {
		finish(lobby)
		return true
	}

	return false
}

func handleMessage(message *trades.TradeMessage, trade *trades.TradeStatus, trainerNum int) (err error, finished bool) {
	log.Info(message.MsgType)

	switch message.MsgType {
	case trades.TRADE:
		err := handleTradeMessage(message, trade, trainerNum)
		if err != nil {
			return err, false
		}
	case trades.ACCEPT:
		err := handleAcceptMessage(message, trade, trainerNum)
		if err != nil {
			return err, false
		}
	}

	return nil, trade.TradeFinished
}

func handleTradeMessage(message *trades.TradeMessage, trade *trades.TradeStatus, trainerNum int) error {
	if len(message.MsgArgs) > 1 {
		return errors.New("can only add one item to trade at a time")
	}

	item := message.MsgArgs[0]

	trade.Players[trainerNum].Items = append(trade.Players[trainerNum].Items, item)

	return nil
}

func handleAcceptMessage(message *trades.TradeMessage, trade *trades.TradeStatus, trainerNum int) error {
	if len(message.MsgArgs) != 0 {
		return errors.New("accept should not take any args")
	}

	trade.Players[trainerNum].Accepted = true

	if checkIfBattleFinished(trade) {
		trade.TradeFinished = true
	}

	return nil
}

func finish(lobby *ws.Lobby) {
	lobby.Finished = true

	finishMessage := &trades.TradeMessage{
		MsgType: trades.FINISH,
		MsgArgs: nil,
	}

	trades.SendMessage(finishMessage, *lobby.TrainerOutChannels[0])
	trades.SendMessage(finishMessage, *lobby.TrainerOutChannels[1])
}

func updateClients(trade *trades.TradeStatus, lobby *ws.Lobby) {
	tradeJSON, err := json.Marshal((*trade).Players)

	log.Info(string(tradeJSON))

	if err != nil {
		log.Error(err)
		return
	}

	msg := &trades.TradeMessage{
		MsgType: trades.UPDATE,
		MsgArgs: []string{string(tradeJSON)},
	}

	trades.SendMessage(msg, *lobby.TrainerOutChannels[0])
	trades.SendMessage(msg, *lobby.TrainerOutChannels[1])
}

func checkIfBattleFinished(trade *trades.TradeStatus) bool {
	if trade.Players[0].Accepted && trade.Players[1].Accepted {
		return true
	}
	return false
}

func handleMessageError(err error, trainerChanOut chan *string) {
	log.Error(err)
	trades.SendMessage(
		&trades.TradeMessage{
			MsgType: trades.ERROR,
			MsgArgs: nil,
		},
		trainerChanOut)
}
