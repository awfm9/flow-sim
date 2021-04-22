package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"time"

	"github.com/awfm9/flow-sim/scripts"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/client"
	"github.com/onflow/flow-go-sdk/crypto"
)

func main() {

	// declare the configuration variables
	var (
		flagTPS     uint
		flagAPI     string
		flagKey     string
		flagNet     string
		flagBalance uint
	)

	// bind the configuration variables to command line flags
	pflag.UintVarP(&flagTPS, "tps", "t", 8, "transactions per second throughput")
	pflag.StringVarP(&flagAPI, "api", "a", "localhost:3569", "access node API address")
	pflag.StringVarP(&flagKey, "key", "k", "8ae3d0461cfed6d6f49bfc25fa899351c39d1bd21fdba8c87595b6c49bb4cc43", "service account private key")
	pflag.StringVarP(&flagNet, "net", "n", "flow-testnet", "Flow network to use")
	pflag.UintVarP(&flagBalance, "balance", "b", 1_000_000, "default token balance for new accounts")

	// parse the command line flags into the configuration variables
	pflag.Parse()

	// initialize the logger
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.TimestampFunc = func() time.Time { return time.Now().UTC() }
	log := zerolog.New(os.Stderr).With().Timestamp().Logger().Level(zerolog.DebugLevel)

	// initialize the flow address generator and get the default accounts:
	// 1st: service account (root)
	// 2nd: fungible token contract
	// 3rd: flow token contract
	gen := sdk.NewAddressGenerator(sdk.ChainID(flagNet))
	rootAddress := gen.NextAddress()
	fungibleAddress := gen.NextAddress()
	flowAddress := gen.NextAddress()

	// initialize the SDK client
	cli, err := client.New(flagAPI, grpc.WithInsecure())
	if err != nil {
		log.Fatal().Err(err).Str("api", flagAPI).Msg("could not connect to access node")
	}

	// get the root account public information
	rootAccount, err := cli.GetAccount(context.Background(), rootAddress)
	if err != nil {
		log.Fatal().Err(err).Str("address", rootAddress.String()).Msg("could not get root account")
	}

	// decode the root account private key and create the signer
	rootPub := rootAccount.Keys[0]
	rootPriv, err := crypto.DecodePrivateKeyHex(rootPub.SigAlgo, flagKey)
	if err != nil {
		log.Fatal().Err(err).Str("key", flagKey).Msg("could not decode service account private key")
	}
	rootSigner := crypto.NewInMemorySigner(rootPriv, rootPub.HashAlgo)

	// generate a private key for a new account
	seed := make([]byte, crypto.MinSeedLength)
	_, err = rand.Read(seed)
	if err != nil {
		log.Fatal().Err(err).Msg("could not get random seed")
	}
	privKey, err := crypto.GeneratePrivateKey(crypto.ECDSA_P256, seed)
	if err != nil {
		log.Fatal().Err(err).Msg("could not generate private key")
	}

	// STEP 1: bind closure to immutable addresses & configured amounts
	createAccount := scripts.CreateAccount(fungibleAddress, flowAddress, flagBalance)

	// STEP 2: create a transaction to create a specific account
	accountKey := sdk.NewAccountKey().
		FromPrivateKey(privKey).
		SetHashAlgo(crypto.SHA3_256).
		SetWeight(sdk.AccountKeyWeightThreshold)
	tx := createAccount(accountKey)

	// STEP 3: configure the transaction for the signer
	tx.SetPayer(rootAddress).
		SetProposalKey(rootAddress, 0, rootAccount.Keys[0].SequenceNumber).
		AddAuthorizer(rootAddress)

	// STEP 4: configure the transaction for current blockchain state
	final, err := cli.GetLatestBlockHeader(context.Background(), false)
	if err != nil {
		log.Fatal().Err(err).Msg("could not get block header")
	}
	tx.SetReferenceBlockID(final.ID)

	// STEP 5: sign the transaction
	err = tx.SignEnvelope(rootAddress, 0, rootSigner)
	if err != nil {
		log.Fatal().Err(err).Msg("could not sign transaction envelope")
	}

	// STEP 6: submit the transaction
	err = cli.SendTransaction(context.Background(), *tx)
	if err != nil {
		log.Fatal().Err(err).Msg("could not send transaction")
	}

	// keep polling for the result
	status := sdk.TransactionStatusUnknown
	var result *sdk.TransactionResult
	ticker := time.NewTicker(100 * time.Millisecond)
Loop:
	for range ticker.C {
		result, err = cli.GetTransactionResult(context.Background(), tx.ID())
		if err != nil {
			log.Fatal().Err(err).Msg("could not get result")
		}
		if status == result.Status {
			continue
		}
		status = result.Status
		log.Info().Str("status", status.String()).Msg("transaction status changed")
		if status == sdk.TransactionStatusSealed || status == sdk.TransactionStatusExpired {
			break Loop
		}
	}
	ticker.Stop()

	// log the transaction error
	log.Info().Str("events", fmt.Sprintf("%v", result.Events)).Msg("transaction events")
}
