package exported

import "fmt"

const TradeIdVar = "tradeId"

const GetTradesPath = "/trades"
const StartTradePath = "/trades/join"
const JoinTradePath = "/trades/join/%s"

var JoinTradeRoute = fmt.Sprintf("/trades/join/{%s}", TradeIdVar)
