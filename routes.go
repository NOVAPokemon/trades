package main

import (
	"github.com/NOVAPokemon/utils"
	"github.com/NOVAPokemon/utils/api"
	"strings"
)

const GetLobbiesName = "GET_TRADE_LOBBIES"
const CreateTradeName = "START_TRADE"
const JoinTradeName = "JOIN_TRADE"

const GET = "GET"
const POST = "POST"

var routes = utils.Routes{
	api.GenStatusRoute(strings.ToLower(serviceName)),
	utils.Route{
		Name:        GetLobbiesName,
		Method:      GET,
		Pattern:     api.GetTradesPath,
		HandlerFunc: HandleGetLobbies,
	},
	utils.Route{
		Name:        CreateTradeName,
		Method:      POST,
		Pattern:     api.StartTradePath,
		HandlerFunc: HandleCreateTradeLobby,
	},
	utils.Route{
		Name:        JoinTradeName,
		Method:      GET,
		Pattern:     api.JoinTradeRoute,
		HandlerFunc: HandleJoinTradeLobby,
	},
}
