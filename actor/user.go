package actor

import (
	"context"
	"fmt"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/client"
	"github.com/onflow/flow-go-sdk/crypto"
)

type User struct {
	cli     *client.Client
	lib     Library
	address flow.Address
	pub     *flow.AccountKey
	priv    crypto.PrivateKey
	signer  crypto.Signer
	nonce   uint64
}

func (u *User) Address() string {
	return u.address.Hex()
}

func (u *User) Submit(tx *flow.Transaction) error {

	// set all the signature-related addresses to this user's
	tx.SetPayer(u.address).
		SetProposalKey(u.address, 0, u.nonce).
		AddAuthorizer(u.address)

	// get the reference block ID for expiration management
	header, err := u.cli.GetLatestBlockHeader(context.Background(), false)
	if err != nil {
		return fmt.Errorf("could not get latest block header (%w)", err)
	}
	tx.SetReferenceBlockID(header.ID)

	// sign the transaction
	err = tx.SignEnvelope(u.address, 0, u.signer)
	if err != nil {
		return fmt.Errorf("could not sign envelope (%w)", err)
	}

	// STEP 6: submit the transaction
	err = u.cli.SendTransaction(context.Background(), *tx)
	if err != nil {
		return fmt.Errorf("could not send transaction (%w)", err)
	}

	return nil
}
