package main

import (
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
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

	// start catching interrupt signals right away
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	// seed the random generator
	rand.Seed(time.Now().UnixNano())

	// declare the configuration variables
	var (
		flagAPI     string
		flagKey     string
		flagNet     string
		flagBalance uint
		flagUsers   uint
		flagLimit   uint64
		flagLevel   string
	)

	// bind the configuration variables to command line flags
	pflag.StringVarP(&flagAPI, "api", "a", "localhost:3569", "access node API address")
	pflag.StringVarP(&flagKey, "key", "k", "8ae3d0461cfed6d6f49bfc25fa899351c39d1bd21fdba8c87595b6c49bb4cc43", "service account private key")
	pflag.StringVarP(&flagNet, "net", "n", "flow-testnet", "Flow network to use")
	pflag.UintVarP(&flagBalance, "balance", "b", 1, "default token balance for new accounts")
	pflag.UintVarP(&flagUsers, "users", "u", 1_000, "number of users to create")
	pflag.Uint64VarP(&flagLimit, "limit", "l", 1_000_000, "number of total transactions to execute before stopping")
	pflag.StringVarP(&flagLevel, "level", "g", zerolog.InfoLevel.String(), "log level to use for log output")

	// parse the command line flags into the configuration variables
	pflag.Parse()

	// initialize the logger
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.TimestampFunc = func() time.Time { return time.Now().UTC() }
	log := zerolog.New(os.Stderr).With().Timestamp().Logger().Level(zerolog.DebugLevel)
	level, err := zerolog.ParseLevel(flagLevel)
	if err != nil {
		log.Fatal().Err(err).Str("level", flagLevel).Msg("could not parse log level")
	}
	log = log.Level(level)

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

	log.Debug().Str("address", root.Address().Hex()).Msg("root account initialized")

	// keeping track of total number of transactions
	var transactions uint64

	// on first signal, we just close the channel; on second one, we force
	// the shut down
	wg := &sync.WaitGroup{}
	done := make(chan struct{})
	go func() {
		<-sig
		close(done)
		<-sig
		os.Exit(1)
	}()

	// create the configured amount of user accounts
	var accounts uint
	creation := make(chan *actor.User)
	wg.Add(1)
	go func() {
		defer wg.Done()
	UserLoop:
		for i := uint(0); i < flagUsers; i++ {

			// if a signal was triggered, quit
			select {
			case <-done:
				break UserLoop
			default:
			}

			// check if we have reached maximum number of transactions
			total := atomic.AddUint64(&transactions, 1)
			if total > flagLimit {
				close(done)
				continue
			}

			log.Debug().Msg("creating user account")

			// create the user
			user, err := root.CreateUser()
			if err != nil {
				log.Fatal().Err(err).Msg("could not create user")
			}

			log.Debug().Str("address", user.Address().Hex()).Msg("user account created")

			accounts++

			log.Info().Uint("accounts", accounts).Msg("user account added")

			// submit user to channel to add to managed users
			creation <- user
		}
		close(creation)
	}()

	// create the number of transactions per second that are configured
	wg.Add(1)
	go func() {
		defer wg.Done()

		// the initial interval is one time-to-sealing
		interval := 15 * time.Second

		// keep list of all users
		var users []*actor.User

	TxLoop:
		for {
			select {

			// if a signal was triggered, quit
			case <-done:
				break TxLoop

			// if we have a user to add to our list
			case user, ok := <-creation:
				if !ok {
					users = nil
				}

				users = append(users, user)

			// otherwise, use ticker to decide how many
			case <-time.After(interval):

				// make sure we have at least two accounts created
				if accounts < 2 {
					continue
				}

				// make sure each account sends one transaction for every
				// time-to-sealing period
				interval = 15 * time.Second / time.Duration(accounts)

				// if we have less than two users, skip for now
				if len(users) < 2 {
					continue
				}

				// check if we have reached maximum number of transactions
				total := atomic.AddUint64(&transactions, 1)
				if total > flagLimit {
					close(done)
					continue
				}

				// shuffle the user pool and pick first two
				rand.Shuffle(len(users), func(i int, j int) {
					users[i], users[j] = users[j], users[i]
				})
				sender := users[0]
				receiver := users[1]

				// remove the sender for the pool for now
				last := len(users) - 1
				users[0], users[last] = users[last], users[0]
				users = users[:last]

				log.Debug().
					Str("sender", sender.Address().Hex()).
					Str("receiver", receiver.Address().Hex()).
					Msg("executing token transfer")

				// execute the send in its own goroutine and add user back to pool when done
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() {
						users = append(users, sender)
					}()
					err := sender.SendTokens(receiver.Address(), 1)
					if err != nil {
						log.Error().Err(err).Msg("token transfer failed")
						return
					}
					log.Debug().
						Str("sender", sender.Address().Hex()).
						Str("receiver", receiver.Address().Hex()).
						Msg("token transfer executed")
				}()

			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		ticker := time.NewTicker(time.Second)
		var previous uint64
	LoggingLoop:
		for {
			select {
			case <-done:
				break LoggingLoop
			case <-ticker.C:
				total := atomic.LoadUint64(&transactions)
				if total == previous {
					continue
				}
				previous = total
				log.Info().Uint64("transactions", total).Msg("transaction(s) added")
			}
		}

	}()

	wg.Wait()
}
