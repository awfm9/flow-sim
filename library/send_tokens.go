package library

import (
	"github.com/onflow/flow-go-sdk"
)

func sendTokens(flowAddress flow.Address) func(flow.Address, flow.Address, uint) *flow.Transaction {
	return func(from flow.Address, to flow.Address, amount uint) *flow.Transaction {
		return nil
	}
}
