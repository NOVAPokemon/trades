package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	nrTradesStarted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "trades_trades_started",
		Help: "The total number of started raids",
	})
	nrTradesFinished = promauto.NewCounter(prometheus.CounterOpts{
		Name: "trades_trades_finished",
		Help: "The total number of finished raids",
	})
)

func emitTradeStart() {
	nrTradesStarted.Inc()
}

func emitTradeFinish() {
	nrTradesFinished.Inc()
}
