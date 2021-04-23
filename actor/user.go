package actor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/client"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/rs/zerolog"
)

type User struct {
	log     zerolog.Logger
	cli     *client.Client
	lib     Library
	address flow.Address
	priv    crypto.PrivateKey
	signer  crypto.Signer
	mutex   *sync.Mutex
	wg      *sync.WaitGroup
}

func (u *User) Address() flow.Address {
	return u.address
}

func (u *User) Submit(
	tx *flow.Transaction,
	sealed func(*flow.TransactionResult),
	failed func(error),
) error {

	// if the nonce is being reset, this lock is held and we can't acquire it
	// until all pending transactions are done; this is just a sync gate, we
	// don't need to do anything while holding the lock
	// the wg keeps track of all pending transaction submissions, so we should
	// only add after we have passed the sync gate
	u.mutex.Lock()
	u.wg.Add(1)
	u.mutex.Unlock()

	// get the reference block ID for expiration management
	header, err := u.cli.GetLatestBlockHeader(context.Background(), true)
	if err != nil {
		return fmt.Errorf("could not get latest block header (%w)", err)
	}

	// get the public account for the nonce
	pub, err := u.cli.GetAccountAtLatestBlock(context.Background(), u.address)
	if err != nil {
		return fmt.Errorf("could not get account for nonce (%w)", err)
	}

	// set all the signature-related addresses to this user's
	tx.SetPayer(u.address).
		SetProposalKey(u.address, 0, pub.Keys[0].SequenceNumber).
		AddAuthorizer(u.address).
		SetReferenceBlockID(header.ID)

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

	// we will keep polling the transaction result inside this closure, where
	// we will call the callback once the transaction has succeeded or failed
	go func() {

		// once we exit the closure, we are done handling this transaction
		defer u.wg.Done()

		// check for a result every 100 milliseconds
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {

			// get the execution result
			result, err := u.cli.GetTransactionResult(context.Background(), tx.ID())
			if err != nil {
				u.log.Fatal().Err(err).Msg("could not get transaction result")
			}

			// if the transaction has expired, report it as error
			if result.Status == flow.TransactionStatusExpired {
				failed(fmt.Errorf("transaction expired"))
				return
			}

			// if the transaction is unknown, report it as error
			if result.Status == flow.TransactionStatusUnknown {
				failed(fmt.Errorf("transaction unknown"))
				return
			}

			// if the transaction is not sealed yet, continue checking
			if result.Status != flow.TransactionStatusSealed {
				continue
			}

			// if the transaction has an error, report failure
			if result.Error != nil {
				failed(result.Error)
				return
			}

			// otherwise, report the result
			sealed(result)
			return
		}
	}()

	return nil
}

func (u *User) SendTokens(address flow.Address, amount uint) error {

	// obtain a transaction that sends tokens from our address to theirs
	tx := u.lib.SendTokens(address, amount)

	// create a function to log successful transfer
	success := make(chan struct{})
	sealed := func(*flow.TransactionResult) {
		close(success)
	}

	// create a function to log failed transfer
	failure := make(chan error)
	failed := func(err error) {
		failure <- err
	}

	// submit the transaction
	err := u.Submit(tx, sealed, failed)
	if err != nil {
		return fmt.Errorf("could not submit transaction (%w)", err)
	}

	select {
	case <-success:
		return nil
	case err := <-failure:
		return fmt.Errorf("could not send tokens (%w)", err)
	}
}
