package main

import (
	"encoding/json"
	"errors"
	ws "github.com/NOVAPokemon/utils/websockets"
	"github.com/NOVAPokemon/utils/websockets/trades"
	log "github.com/sirupsen/logrus"
)

func StartTrade(lobby *ws.Lobby) {
	players := [2]trades.Players{
		{Items: []string{}, Accepted: false},
		{Items: []string{}, Accepted: false},
	}

	tradeStatus := &trades.TradeStatus{
		Players:       players,
	}

	err := tradeMainLoop(lobby, tradeStatus)
	if err != nil {
		log.Error(err)
	}
}

func tradeMainLoop(lobby *ws.Lobby, trade *trades.TradeStatus) error {
	trainer0ChanIn := *lobby.TrainerInChannels[0]
	trainer0ChanOut := *lobby.TrainerOutChannels[0]
	trainer1ChanIn := *lobby.TrainerInChannels[1]
	trainer1ChanOut := *lobby.TrainerOutChannels[1]

	// trade main loop
	for {
		select {
		case msgStr := <-trainer0ChanIn:
			log.Infof(*msgStr)
			err, msg := trades.ParseMessage(msgStr)
			if err != nil {
				handleMessageError(err, trainer0ChanOut)
				continue
			}

			err, finished := handleMessage(msg, trade, 0)

			if err != nil {
				handleMessageError(err, trainer0ChanOut)
				continue
			}

			if finished {
				finish(trainer0ChanOut, trainer1ChanOut)
				return nil
			} else {
				updateClients(trade, trainer0ChanOut, trainer1ChanOut)
			}
		case msgStr := <-trainer1ChanIn:
			log.Infof(*msgStr)
			err, msg := trades.ParseMessage(msgStr)
			if err != nil {
				handleMessageError(err, trainer1ChanOut)
				continue
			}

			err, finished := handleMessage(msg, trade, 1)

			if err != nil {
				handleMessageError(err, trainer1ChanOut)
				continue
			}

			if finished {
				finish(trainer0ChanOut, trainer1ChanOut)
				return nil
			} else {
				updateClients(trade, trainer0ChanOut, trainer1ChanOut)
			}
		}
	}
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

	//TODO verify that item is in jwt

	item := message.MsgArgs[0]

	//if err != nil {
	//	log.Error("error getting id from hex")
	//	log.Error(err)
	//	return err
	//}

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

func finish(trainer0ChanOut, trainer1ChanOut chan *string) {
	finishMessage := &trades.TradeMessage{
		MsgType: trades.FINISH,
		MsgArgs: nil,
	}

	trades.SendMessage(finishMessage, trainer0ChanOut)
	trades.SendMessage(finishMessage, trainer1ChanOut)
}

func updateClients(trade *trades.TradeStatus, trainer0ChanOut, trainer1ChanOut chan *string) {
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

	trades.SendMessage(msg, trainer0ChanOut)
	trades.SendMessage(msg, trainer1ChanOut)
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
