package library

import (
	"github.com/onflow/flow-go-sdk"
)

type Library struct {
	createAccount func(*flow.AccountKey) *flow.Transaction
}

func New(fungibleToken flow.Address, flowToken flow.Address, defaultBalance uint) *Library {
	lib := &Library{
		createAccount: createAccount(fungibleToken, flowToken, defaultBalance),
	}
	return lib
}

func (l *Library) CreateAccount(pub *flow.AccountKey) *flow.Transaction {
	return l.createAccount(pub)
}
