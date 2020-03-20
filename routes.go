package main

import (
	"github.com/NOVAPokemon/utils"
)

const GetTradesName = "GET_TRADES"
const StartTradeName = "START_TRADE"
const JoinTradeName = "JOIN_TRADE"

const GET = "GET"
const POST = "POST"

const GetTradesPath = "/trades"
const StartTradePath = "/trades/join"
const JoinTradePath = "/trades/join/{tradeId}"

var routes = utils.Routes{
	utils.Route{
		Name:        GetTradesName,
		Method:      GET,
		Pattern:     GetTradesPath,
		HandlerFunc: GetCurrentLobbies,
	},
	utils.Route{
		Name:        StartTradeName,
		Method:      GET,
		Pattern:     StartTradePath,
		HandlerFunc: CreateTradeLobby,
	},
	utils.Route{
		Name:        JoinTradeName,
		Method:      GET,
		Pattern:     JoinTradePath,
		HandlerFunc: JoinTradeLobby,
	},
}
