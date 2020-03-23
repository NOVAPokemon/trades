package main

import (
	ws "github.com/NOVAPokemon/utils/websockets"
	log "github.com/sirupsen/logrus"
)

func StartTrade(lobby *ws.Lobby) {
	lobby.Started = true

	log.Infof("Started trade")

	for {
		select {
		case msg := <-*lobby.TrainerInChannels[0]:
			log.Infof("[Lobby %s]: Message from trainer 1 received: %s", lobby.Id.Hex(), *msg)
			*lobby.TrainerOutChannels[1] <- msg

		case msg := <-*lobby.TrainerInChannels[1]:
			log.Infof("[Lobby %s]: Message from trainer 2 received: %s", lobby.Id.Hex(), *msg)
			*lobby.TrainerOutChannels[0] <- msg
		}
	}
	// handleFinishTrade()
}
