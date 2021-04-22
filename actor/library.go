package actor

import (
	"github.com/onflow/flow-go-sdk"
)

type Library interface {
	CreateAccount(key *flow.AccountKey) *flow.Transaction
}
