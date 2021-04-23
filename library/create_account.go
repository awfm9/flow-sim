package library

import (
	"encoding/hex"
	"strings"

	"github.com/onflow/cadence"
	"github.com/onflow/cadence/encoding/json"
	"github.com/onflow/flow-go-sdk"
)

const (
	FUNGIBLE_TOKEN = "FUNGIBLE_TOKEN"
	FLOW_TOKEN     = "FLOW_TOKEN"
)

const createAccountTemplate = `
import FungibleToken from 0xFUNGIBLE_TOKEN
import FlowToken from 0xFLOW_TOKEN

transaction(key: String, amount: UFix64) {
	prepare(signer: AuthAccount) {
		let account = AuthAccount(payer: signer)
		account.addPublicKey(key.decodeHex())
		let sender = signer.borrow<&FlowToken.Vault>(from: /storage/flowTokenVault)
			?? panic("could not borrow reference for sending vault")
		let receiver = account.getCapability(/public/flowTokenReceiver)
			.borrow<&{FungibleToken.Receiver}>()
			?? panic("could not borrow reference for receiving vault")
		receiver.deposit(from: <-sender.withdraw(amount: amount))
	}
}
`

func createAccount(fungibleAddress flow.Address, flowAddress flow.Address, defaultBalance uint) func(accountKey *flow.AccountKey) *flow.Transaction {
	script := createAccountTemplate
	script = strings.ReplaceAll(script, FUNGIBLE_TOKEN, fungibleAddress.Hex())
	script = strings.ReplaceAll(script, FLOW_TOKEN, flowAddress.Hex())
	amount, _ := cadence.NewUFix64FromParts(int(defaultBalance), 0)
	return func(accountKey *flow.AccountKey) *flow.Transaction {
		key := cadence.NewString(hex.EncodeToString(accountKey.Encode()))
		tx := flow.NewTransaction().
			SetScript([]byte(script)).
			AddRawArgument(json.MustEncode(key)).
			AddRawArgument(json.MustEncode(amount))
		return tx
	}
}
