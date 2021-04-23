package library

import (
	"github.com/onflow/flow-go-sdk"
)

type Library struct {
	createAccount func(*flow.AccountKey) *flow.Transaction
	sendTokens    func(flow.Address, uint) *flow.Transaction
}

func New(fungibleToken flow.Address, flowToken flow.Address, defaultBalance uint) *Library {
	lib := &Library{
		createAccount: createAccount(fungibleToken, flowToken, defaultBalance),
		sendTokens:    sendTokens(fungibleToken, flowToken),
	}
	return lib
}

func (l *Library) CreateAccount(pub *flow.AccountKey) *flow.Transaction {
	return l.createAccount(pub)
}

func (l *Library) SendTokens(to flow.Address, amount uint) *flow.Transaction {
	return l.sendTokens(to, amount)
}
