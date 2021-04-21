package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	"github.com/onflow/cadence"
	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/client"
	"github.com/onflow/flow-go-sdk/crypto"
	"github.com/onflow/flow-go/model/flow"
)

func main() {

	// declare the configuration variables
	var (
		flagTPS uint
		flagAPI string
		flagKey string
		flagNet string
	)

	// bind the configuration variables to command line flags
	pflag.UintVarP(&flagTPS, "tps", "t", 1, "transactions per second throughput")
	pflag.StringVarP(&flagAPI, "api", "a", "localhost:3569", "access node API address")
	pflag.StringVarP(&flagKey, "key", "k", "8ae3d0461cfed6d6f49bfc25fa899351c39d1bd21fdba8c87595b6c49bb4cc43", "service account private key")
	pflag.StringVarP(&flagNet, "net", "n", string(flow.Testnet), "Flow network to use")

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
	tokenAddress := gen.NextAddress()
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

	// create the SDK version of the account public key
	accountKey := sdk.NewAccountKey().
		FromPrivateKey(privKey).
		SetHashAlgo(crypto.SHA3_256).
		SetWeight(sdk.AccountKeyWeightThreshold)

	// get the latest block to use as reference block
	final, err := cli.GetLatestBlockHeader(context.Background(), false)
	if err != nil {
		log.Fatal().Err(err).Msg("could not get block header")
	}

	// make the script
	script := fmt.Sprintf(`
import FungibleToken from 0x%s
import FlowToken from 0x%s

transaction(publicKey: [UInt8], tokens: UFix64) {
  prepare(signer: AuthAccount) {
	let vault = signer.borrow<&FlowToken.Vault>(from: /storage/flowTokenVault)
      ?? panic("Could not borrow reference to the owner's Vault")

	let account = AuthAccount(payer: signer)
	account.addPublicKey(publicKey)

	let receiver = account.getCapability(/public/flowTokenReceiver)
	.borrow<&{FungibleToken.Receiver}>()
	?? panic("Could not borrow receiver reference to the recipient's Vault")

	receiver.deposit(from: <-vault.withdraw(amount: tokens))
  }
}
`, tokenAddress, flowAddress)

	// generate the account creation transaction
	tx := sdk.NewTransaction().
		SetScript([]byte(script)).
		SetReferenceBlockID(final.ID).
		SetProposalKey(rootAddress, 0, rootAccount.Keys[0].SequenceNumber).
		AddAuthorizer(rootAddress).
		SetPayer(rootAddress)

	// convert the account key to a cadence byte array
	keyHash := accountKey.Encode()
	bytes := make([]cadence.Value, 0, len(keyHash))
	for _, b := range keyHash {
		bytes = append(bytes, cadence.NewUInt8(b))
	}
	byteArray := cadence.NewArray(bytes)

	// define the initial token amount of the account
	amount, err := cadence.NewUFix64FromParts(1_000_000, 0)
	if err != nil {
		log.Fatal().Err(err).Msg("could not create token amount")
	}

	// add the arguments to the transaction
	err = tx.AddArgument(byteArray)
	if err != nil {
		log.Fatal().Err(err).Msg("could not add public key bytes to transaction")
	}
	err = tx.AddArgument(amount)
	if err != nil {
		log.Fatal().Err(err).Msg("could not add token amount to transaction")
	}

	// sign the transaction with the root key
	err = tx.SignEnvelope(rootAddress, 0, rootSigner)
	if err != nil {
		log.Fatal().Err(err).Msg("could not sign transaction envelope")
	}

	// submit the transaction through the SDK client
	err = cli.SendTransaction(context.Background(), *tx)
	if err != nil {
		log.Fatal().Err(err).Msg("could not send transaction")
	}

	// keep polling for the result
	ticker := time.NewTicker(100 * time.Millisecond)
	for range ticker.C {
		result, err := cli.GetTransactionResult(context.Background(), tx.ID())
		if err != nil {
			log.Fatal().Err(err).Msg("could not get result")
		}
		switch result.Status {
		case sdk.TransactionStatusExpired:
			log.Info().Msg("transaction expired")
		case sdk.TransactionStatusUnknown:
			log.Info().Msg("transaction unknown")
		case sdk.TransactionStatusPending:
			log.Info().Msg("transaction pending")
		case sdk.TransactionStatusExecuted:
			log.Info().Msg("transaction executed")
		case sdk.TransactionStatusFinalized:
			log.Info().Msg("transaction finalized")
		case sdk.TransactionStatusSealed:
			log.Info().Msg("transaction sealed")
		}
	}
	ticker.Stop()
}
