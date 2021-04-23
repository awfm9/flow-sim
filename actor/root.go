package actor

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/rs/zerolog"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/client"
	"github.com/onflow/flow-go-sdk/crypto"
)

type Root struct {
	*User
}

func NewRoot(log zerolog.Logger, cli *client.Client, lib Library, address flow.Address, key string) (*Root, error) {

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
			log:     log,
			cli:     cli,
			lib:     lib,
			address: address,
			priv:    priv,
			signer:  crypto.NewInMemorySigner(priv, pub.HashAlgo),
			mutex:   &sync.Mutex{},
			wg:      &sync.WaitGroup{},
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

	// create different channels to handle the transaction results
	failure := make(chan error)        // in case the transaction failed
	success := make(chan flow.Address) // to receive the address
	missing := make(chan struct{})     // in case the correct event is missing

	// create a closure to handle a successful account creation
	sealed := func(result *flow.TransactionResult) {
		for _, event := range result.Events {
			if event.Type != flow.EventAccountCreated {
				continue
			}
			success <- flow.AccountCreatedEvent(event).Address()
		}
		close(missing)
	}

	// create a closure to handle failed account creation
	failed := func(err error) {
		failure <- err
	}

	// submit the transaction and wait for result
	err = r.Submit(tx, sealed, failed)
	if err != nil {
		return nil, fmt.Errorf("could not submit transaction (%w)", err)
	}

	// wait on failure or success
	var address flow.Address
	select {
	case address = <-success:
		// continue in function body
	case err := <-failure:
		return nil, fmt.Errorf("account creation transaction failed (%w)", err)
	}

	user := &User{
		log:     r.log,
		lib:     r.lib,
		cli:     r.cli,
		address: address,
		priv:    priv,
		signer:  crypto.NewInMemorySigner(priv, pub.HashAlgo),
		mutex:   &sync.Mutex{},
		wg:      &sync.WaitGroup{},
	}

	return user, nil
}
