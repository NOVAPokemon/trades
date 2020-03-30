package exported

import "fmt"

const TradeIdVar = "tradeId"

const GetTradesPath = "/trades"
const StartTradePath = "/trades/join"
var JoinTradePath = fmt.Sprintf("/trades/join/{%s}", TradeIdVar)
