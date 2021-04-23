package library

import (
	"strings"

	"github.com/onflow/cadence"
	"github.com/onflow/cadence/encoding/json"
	"github.com/onflow/flow-go-sdk"
)

const sendTokensTemplate = `
import FungibleToken from 0xFUNGIBLE_TOKEN
import FlowToken from 0xFLOW_TOKEN

transaction(amount: UFix64, to: Address) {

    // The Vault resource that holds the tokens that are being transferred
    let sentVault: @FungibleToken.Vault

    prepare(signer: AuthAccount) {

        // Get a reference to the signer's stored vault
        let vaultRef = signer.borrow<&FlowToken.Vault>(from: /storage/flowTokenVault)
			?? panic("Could not borrow reference to the owner's Vault!")

        // Withdraw tokens from the signer's stored vault
        self.sentVault <- vaultRef.withdraw(amount: amount)
    }

    execute {

        // Get a reference to the recipient's Receiver
        let receiverRef =  getAccount(to)
            .getCapability(/public/flowTokenReceiver)
            .borrow<&{FungibleToken.Receiver}>()
			?? panic("Could not borrow receiver reference to the recipient's Vault")

        // Deposit the withdrawn tokens in the recipient's receiver
        receiverRef.deposit(from: <-self.sentVault)
    }
}
`

func sendTokens(fungibleAddress flow.Address, flowAddress flow.Address) func(flow.Address, uint) *flow.Transaction {
	script := sendTokensTemplate
	script = strings.ReplaceAll(script, FUNGIBLE_TOKEN, fungibleAddress.Hex())
	script = strings.ReplaceAll(script, FLOW_TOKEN, flowAddress.Hex())
	return func(toAddress flow.Address, tokenAmount uint) *flow.Transaction {
		amount, _ := cadence.NewUFix64FromParts(0, tokenAmount)
		tx := flow.NewTransaction().
			SetScript([]byte(script)).
			AddRawArgument(json.MustEncode(amount)).
			AddRawArgument(json.MustEncode(cadence.NewAddress(toAddress)))
		return tx
	}
}
