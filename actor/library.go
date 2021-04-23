package actor

import (
	"github.com/onflow/flow-go-sdk"
)

type Library interface {
	CreateAccount(key *flow.AccountKey) *flow.Transaction
	SendTokens(to flow.Address, amount uint) *flow.Transaction
}
