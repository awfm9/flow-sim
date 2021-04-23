package actor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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
	pub     *flow.AccountKey
	priv    crypto.PrivateKey
	signer  crypto.Signer
	nonce   uint64
	mutex   *sync.Mutex
	wg      *sync.WaitGroup
}

func (u *User) Address() string {
	return u.address.Hex()
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
	header, err := u.cli.GetLatestBlockHeader(context.Background(), false)
	if err != nil {
		return fmt.Errorf("could not get latest block header (%w)", err)
	}

	// atomically load and increase the nonce
	nonce := atomic.AddUint64(&u.nonce, 1)

	// set all the signature-related addresses to this user's
	tx.SetPayer(u.address).
		SetProposalKey(u.address, 0, nonce-1).
		AddAuthorizer(u.address).
		SetReferenceBlockID(header.ID)

	// sign the transaction
	err = tx.SignEnvelope(u.address, 0, u.signer)
	if err != nil {
		go u.resetNonce()
		return fmt.Errorf("could not sign envelope (%w)", err)
	}

	// STEP 6: submit the transaction
	err = u.cli.SendTransaction(context.Background(), *tx)
	if err != nil {
		go u.resetNonce()
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
				u.log.Error().Err(err).Msg("could not get transaction result")
				go u.resetNonce()
				return
			}

			// if the transaction has expired, call the expired callback
			if result.Status == flow.TransactionStatusExpired || result.Status == flow.TransactionStatusUnknown {
				go u.resetNonce()
				failed(fmt.Errorf("transaction failed (%s)", result.Status))
				return
			}

			// if the transaction has failed, call the failure callback
			if result.Error != nil {
				go u.resetNonce()
				failed(result.Error)
				return
			}

			// if the transaction result has been sealed, call the callback
			if result.Status == flow.TransactionStatusSealed {
				sealed(result)
				return
			}

		}
	}()

	return nil
}

func (u *User) SendTokens(address flow.Address, amount uint) error {

	// obtain a transaction that sends tokens from our address to theirs
	tx := u.lib.SendTokens(u.address, address, amount)

	// create a function to log successful transfer
	sealed := func(*flow.TransactionResult) {
		u.log.Info().
			Str("from", u.address.Hex()).
			Str("to", address.Hex()).
			Uint("amount", amount).
			Msg("token transfer succeeded")
	}

	// create a function to log failed transfer
	failed := func(err error) {
		u.log.Error().
			Err(err).
			Str("from", u.address.Hex()).
			Str("to", address.Hex()).
			Uint("amount", amount).
			Msg("token transfer failed")
	}

	// submit the transaction
	err := u.Submit(tx, sealed, failed)
	if err != nil {
		return fmt.Errorf("could not submit transaction (%w)", err)
	}

	return nil
}

func (u *User) resetNonce() {

	// locking this mutex will make sure that all submitted transactions are
	// waiting at the sync gate until we had a chance to reset the nonce
	u.mutex.Lock()
	defer u.mutex.Unlock()

	// once the lock is acquired, we wait for all pending transactions to either
	// fail or be sealed
	u.wg.Wait()

	// when we are here, all transactions have failed or have been selead, so
	// we can use the on-chain nonce to resume submitting transactions with the
	// correct nonce
	pub, err := u.cli.GetAccountAtLatestBlock(context.Background(), u.address)
	if err != nil {
		u.log.Fatal().Err(err).Msg("could not get account for nonce reset")
	}

	// set the nonce and then the defer will unlock the submits
	u.nonce = pub.Keys[0].SequenceNumber
}
