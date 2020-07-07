package main

import (
	"fmt"

	"github.com/NOVAPokemon/utils"
	"github.com/pkg/errors"
)

const (
	errorTradeItems    = "error trading items"
	errorCommitChanges = "error commiting changes"

	errorTradeLobbyNotFoundFormat = "trade lobby %s not found"
	errorPlayerNotExpectedFormat  = "player %s not expected in lobby"
)

var (
	errorNoTradeId = errors.New("no trade id provided")
	errorInvalidId = errors.New("invalid trade id provided")
)

// Handler wrappers
func wrapGetLobbiesError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, getLobbiesName))
}

func wrapCreateTradeError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, createTradeName))
}

func wrapJoinTradeError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, joinTradeName))
}

func wrapRejectTradeError(err error) error {
	return errors.Wrap(err, fmt.Sprintf(utils.ErrorInHandlerFormat, rejectTradeName))
}

// Other wrappers
func wrapTradeItemsError(err error) error {
	return errors.Wrap(err, errorTradeItems)
}

func wrapCommitChangesError(err error) error {
	return errors.Wrap(err, errorCommitChanges)
}

// Error builders
func newTradeLobbyNotFoundError(lobbyId string) error {
	return errors.New(fmt.Sprintf(errorTradeLobbyNotFoundFormat, lobbyId))
}

func newPlayerNotExpectedError(username string) error {
	return errors.New(fmt.Sprintf(errorPlayerNotExpectedFormat, username))
}
