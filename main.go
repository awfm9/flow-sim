package main

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	sdk "github.com/onflow/flow-go-sdk"
	"github.com/onflow/flow-go-sdk/client"

	"github.com/awfm9/flow-sim/actor"
	"github.com/awfm9/flow-sim/library"
)

func main() {

	// declare the configuration variables
	var (
		flagAPI     string
		flagKey     string
		flagNet     string
		flagBalance uint
	)

	// bind the configuration variables to command line flags
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

	// initialize the transaction library
	lib := library.New(fungibleAddress, flowAddress, flagBalance)

	// initialize the root account
	root, err := actor.NewRoot(log, cli, lib, rootAddress, flagKey)
	if err != nil {
		log.Fatal().Err(err).Msg("could not initialize root")
	}

	log.Info().Str("address", root.Address()).Msg("root account initialized")

	// initialize a user account
	user, err := root.CreateUser()
	if err != nil {
		log.Fatal().Err(err).Msg("could not create user")
	}

	log.Info().Str("address", user.Address()).Msg("user account created")

	_ = user
}
