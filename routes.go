package main

import (
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
)

const GetTradesName = "GET_TRADES"
const StartTradeName = "START_TRADE"
const JoinTradeName = "JOIN_TRADE"

const GET = "GET"
const POST = "POST"

var routes = utils.Routes{
	api.DefaultRoute,
	utils.Route{
		Name:        GetTradesName,
		Method:      GET,
		Pattern:     api.GetTradesPath,
		HandlerFunc: GetCurrentLobbies,
	},
	utils.Route{
		Name:        StartTradeName,
		Method:      POST,
		Pattern:     api.StartTradePath,
		HandlerFunc: CreateTradeLobby,
	},
	utils.Route{
		Name:        JoinTradeName,
		Method:      GET,
		Pattern:     api.JoinTradeRoute,
		HandlerFunc: JoinTradeLobby,
	},
}
