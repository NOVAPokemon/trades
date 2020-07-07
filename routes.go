package main

import (
	"fmt"
	"strings"

	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
)

const (
	getLobbiesName  = "GET_TRADE_LOBBIES"
	createTradeName = "START_TRADE"
	joinTradeName   = "JOIN_TRADE"
	rejectTradeName = "REJECT_TRADE"
)

const (
	get  = "GET"
	post = "POST"
)

var routes = utils.Routes{
	api.GenStatusRoute(strings.ToLower(fmt.Sprintf("%s", serviceName))),
	utils.Route{
		Name:        getLobbiesName,
		Method:      get,
		Pattern:     api.GetTradesPath,
		HandlerFunc: handleGetLobbies,
	},
	utils.Route{
		Name:        createTradeName,
		Method:      post,
		Pattern:     api.StartTradePath,
		HandlerFunc: handleCreateTradeLobby,
	},
	utils.Route{
		Name:        joinTradeName,
		Method:      get,
		Pattern:     api.JoinTradeRoute,
		HandlerFunc: handleJoinTradeLobby,
	},
	utils.Route{
		Name:        rejectTradeName,
		Method:      post,
		Pattern:     api.RejectTradeRoute,
		HandlerFunc: handleRejectTradeLobby,
	},
}
