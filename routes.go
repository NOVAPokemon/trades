package main

import (
	"github.com/NOVAPokemon/trades/exported"
	"github.com/NOVAPokemon/utils"
)

const GetTradesName = "GET_TRADES"
const StartTradeName = "START_TRADE"
const JoinTradeName = "JOIN_TRADE"

const GET = "GET"

var routes = utils.Routes{
	utils.Route{
		Name:        GetTradesName,
		Method:      GET,
		Pattern:     exported.GetTradesPath,
		HandlerFunc: GetCurrentLobbies,
	},
	utils.Route{
		Name:        StartTradeName,
		Method:      GET,
		Pattern:     exported.StartTradePath,
		HandlerFunc: CreateTradeLobby,
	},
	utils.Route{
		Name:        JoinTradeName,
		Method:      GET,
		Pattern:     exported.JoinTradeRoute,
		HandlerFunc: JoinTradeLobby,
	},
}
