package main

import (
	"os"

	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/clients"
	http "github.com/bruno-anjos/archimedesHTTPClient"
	"github.com/golang/geo/s2"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	host        = utils.ServeHost
	port        = utils.TradesPort
	serviceName = "TRADES"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func main() {
	flags := utils.ParseFlags(serverName)

	if !*flags.LogToStdout {
		utils.SetLogFile(serverName)
	}

	if !*flags.DelayedComms {
		commsManager = utils.CreateDefaultCommunicationManager()
	} else {
		locationTag := utils.GetLocationTag(utils.DefaultLocationTagsFilename, serverName)
		commsManager = utils.CreateDefaultDelayedManager(locationTag, false)
	}

	location, exists := os.LookupEnv("LOCATION")
	if !exists {
		log.Fatalf("no location in environment")
	}

	httpClient.InitArchimedesClient("localhost", http.DefaultArchimedesPort, s2.CellIDFromToken(location).LatLng())

	notificationsClient = clients.NewNotificationClient(nil, commsManager, httpClient)

	utils.StartServer(serviceName, host, port, routes, commsManager)
}
