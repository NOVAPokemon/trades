package main

import (
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"math/rand"
	"net/http"
	"time"
)

const host = "localhost"
const Port = 8003

var url = ":8002"
var Addr = flag.String("addr", url, "http service address")

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func main() {
	fmt.Print()
	rand.Seed(time.Now().Unix())

	flag.Parse()

	hub := &Hub{
		Trades: make(map[primitive.ObjectID]*TradeLobby),
	}

	r := mux.NewRouter()
	r.HandleFunc("/trades", func(w http.ResponseWriter, r *http.Request) {
		HandleGetCurrentLobbies(hub, w, r)
	})
	r.HandleFunc("/trades/join", func(w http.ResponseWriter, r *http.Request) {
		HandleCreateTradeLobby(hub, w, r)
	})
	r.HandleFunc("/trades/join/{tradeId}", func(w http.ResponseWriter, r *http.Request) {
		HandleJoinTradeLobby(hub, w, r)
	})

	log.Infof("Starting TRADES server in port %d...\n", Port)
	log.Fatal(http.ListenAndServe(*Addr, r))
}
