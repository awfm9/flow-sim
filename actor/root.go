package actor

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/client"
	"github.com/onflow/flow-go-sdk/crypto"
)

type Root struct {
	*User
}

func NewRoot(cli *client.Client, lib Library, address flow.Address, key string) (*Root, error) {

	account, err := cli.GetAccount(context.Background(), address)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve account (%w)", err)
	}

	pub := account.Keys[0]
	priv, err := crypto.DecodePrivateKeyHex(pub.SigAlgo, key)
	if err != nil {
		return nil, fmt.Errorf("could not decode private key hex (%w)", err)
	}

	root := &Root{
		User: &User{
			cli:     cli,
			lib:     lib,
			address: address,
			pub:     pub,
			priv:    priv,
			signer:  crypto.NewInMemorySigner(priv, pub.HashAlgo),
			nonce:   pub.SequenceNumber,
		},
	}

	return root, nil
}

func (r *Root) CreateUser() (*User, error) {

	// read seed from cryptographically secure random function
	seed := make([]byte, crypto.MinSeedLength)
	_, err := rand.Read(seed)
	if err != nil {
		return nil, fmt.Errorf("could not read random seed (%w)", err)
	}

	// generate the private key using the obtained seed
	priv, err := crypto.GeneratePrivateKey(crypto.ECDSA_P256, seed)
	if err != nil {
		return nil, fmt.Errorf("could not generate private key (%w)", err)
	}

	// create the account public key for the private key
	pub := flow.NewAccountKey().
		FromPrivateKey(priv).
		SetHashAlgo(crypto.SHA3_256).
		SetWeight(flow.AccountKeyWeightThreshold)

	// obtain the transaction that creates an account with the given key
	tx := r.lib.CreateAccount(pub)

	// submit the transaction and wait for result
	err = r.Submit(tx)
	if err != nil {
		return nil, fmt.Errorf("could not submit transaction (%w)", err)
	}

	// wait for the transaction to be sealed so we have full result available
	var address flow.Address
Outer:
	for {
		time.Sleep(100 * time.Millisecond)
		result, err := r.cli.GetTransactionResult(context.Background(), tx.ID())
		if err != nil {
			return nil, fmt.Errorf("could not get transaction result (%w)", err)
		}
		if result.Status != flow.TransactionStatusSealed {
			continue
		}
		if result.Error != nil {
			return nil, fmt.Errorf("transaction failed (%w)", err)
		}
		for _, event := range result.Events {
			if event.Type != flow.EventAccountCreated {
				continue
			}
			address = flow.AccountCreatedEvent(event).Address()
			break Outer
		}
	}

	user := &User{
		lib:     r.lib,
		cli:     r.cli,
		address: address,
		pub:     pub,
		priv:    priv,
		signer:  crypto.NewInMemorySigner(priv, pub.HashAlgo),
		nonce:   pub.SequenceNumber,
	}

	return user, nil
}
