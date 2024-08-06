// Package main is the entry point of the program. It uses libp2p to start connecting to other peers to sync the current state of the blockchain and receive/work on new problem definitions and listen to other events.
package main

import (
	"os/exec"
	"runtime"

	"github.com/felix314159/gophy/database"
	"github.com/felix314159/gophy/logger"

	//		/*  this entire block is commented out when you want to use pseudo.go for getting large pseudo blockchain for testing
	"context"
	"crypto/ed25519"
	"flag"
	"fmt"
	"path/filepath"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	
	"github.com/felix314159/gophy/block/simsol"
	"github.com/felix314159/gophy/httpapi"
	"github.com/felix314159/gophy/monitoring"

	// libp2p stuff
	//connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	//		*/
)

const version = "v0.9.16-alpha"

// RA Node ID:	 12D3KooWEYSb69dzeojEH1PygPWef9V1qQJqrGKUEMMsbA4keAyZ

// EXAMPLE USAGE:
//		RA: 			go run . -syncMode=SyncMode_Continuous_Full -httpPort=8097 -localKeyFile=true -raMode=true -raReset=true -pw=supersecretrapw -dockerAlias=ra
//  	Miner: 			go run . -syncMode=SyncMode_Initial_Mine -httpPort=8098 -dockerAlias=someminer
//  	Full Node: 		go run . -syncMode=SyncMode_Initial_Full -httpPort=8099 -dockerAlias=somefullnode

// Note: This is a development version where RA has 30k tokens pre-allocated in genesis block. Needs to be removed before production usage.

func main() {
	//		/*

	// ---- Create database folder if it does not exist already ----
	simsol.CreateFolder(filepath.Join(".", "database"))

	//  ---- Define flags (flagName, defaultValue, description) ----
	
	// flags for networking / keeps running
	topicNamesFlag := flag.String("topicNames", "pouw_chaindb,pouw_newProblem,pouw_newBlock,pouw_minerCommitments,pouw_transactions,pouw_raSecretReveal", "Choose which topics to subscribe to. Must be one string that is comma-separated, contains no spaces and no : chars.")
	syncModeFlag := flag.String("syncMode", "SyncMode_Initial_Full", "Sets the SyncMode which affects which blockchain data is stored locally, how the node responds to other nodes and generally how it behaves in database. Valid choices are SyncMode_Initial_Full, SyncMode_Initial_Light, SyncMode_Initial_Mine, SyncMode_Continuous_Full, SyncMode_Continuous_Light, SyncMode_Continuous_Mine, SyncMode_Passive_Full and SyncMode_Passive_Light.")
	raFlag :=  flag.Bool("raMode", false, "Run as RA and sends them simulation task block problems over time. Only possible if you know private key of RA.")
	raResetFlag := flag.Bool("raReset", false, "Lets RA start new blockchain from scratch (it can't use Initial mode to perform this).")
	localKeyFileFlag :=  flag.Bool("localKeyFile", false, "Re-uses locally available priv and pubkey instead of generating new keys.")
	httpPortFlag := flag.Int("httpPort", 8087, "Sets the port that the two local websites for sending simtasks and transactions runs on.")
	pwFlag := flag.String("pw", "insecurepassword", "Defines password used to encrypt private key in OpenSSH format. Never use this default password in production (it is just useful for automated Docker tests).")
	dockerAliasFlag := flag.String("dockerAlias", "mynode", "Sets the name of the docker instance that runs this node. Useful for having performance stats recording where Sender of a stat can easily by identified via its docker alias.")
	// flags that do something locally and exit
	dumpFlag := flag.String("dump", "nil", "Takes provided blockHash or 'latest' string and retrieves the block from local chaindb. Then prints its header if the block was available.")

	// ---- Parse flags ----
	flag.Parse()
	
	if *dumpFlag == "nil" {
		// separate different executions in the log, also list used flags
		logger.L.Printf("---- STARTING EXECUTION (version %v) ----\nFlag values used:\n\ttopicNames: %v\n\tsyncMode: %v\n\traMode: %v\n\traReset: %v\n\tlocalKeyFile: %v\n\thttpPort: %v\n\tdump: %v", version, *topicNamesFlag, *syncModeFlag, *raFlag, *raResetFlag, *localKeyFileFlag, *httpPortFlag, *dumpFlag) // never log the password
	}
	
	// ---- Handle flags ----
	
	// only allow user ports to avoid port conflicts
	database.HttpPort = *httpPortFlag
	if (database.HttpPort < 1024) || (database.HttpPort > 49151) || (database.HttpPort == 12345) {
		logger.L.Printf("Please set the httpPort flag to a value in [1024, 49151]. Also avoid port 12345 (already used for performance stat server). Aborting program execution.")
		return
	}

	// warn user if default password 'insecurepassword' (just useful for running many docker nodes at once) [do not warn if user is just dumping some data]
	if *pwFlag == "insecurepassword" && *dumpFlag == "nil" {
		logger.L.Printf("WARNING - You are using the default password, this is allowed but not recommended.") // even if you re-use your key and that key uses the default password, you will get reminded every time. this is intended
	}

	// remember your docker instance alias
	database.MyDockerAlias = *dockerAliasFlag

	// handle syncMode flag
	// 		check whether given syncMode is valid
	curNodeMode, err := database.StringToMode(*syncModeFlag)
	if err != nil {
		logger.L.Printf("Invalid syncModeFlag value provided: %v\nTerminating..", err)
		return
	}

	// set IAmRA value (defaults to false)
	if *raFlag {
		database.IAmRA = true
	}
	
	// a full node always stays a full node while running, a light node always stays a light node while running. so its useful to store this seperately for easy access
	//		also: if continuous or passive was chosen, ensure that it's the correct one (check if local blockchain is full or light and if that is compatible with chosen continuous mode)
	switch curNodeMode {
	case database.SyncMode_Initial_Full, database.SyncMode_Initial_Mine:
		database.IAmFullNode = true
	case database.SyncMode_Initial_Light:
		database.IAmFullNode = false
	case database.SyncMode_Continuous_Full, database.SyncMode_Continuous_Mine, database.SyncMode_Passive_Full:
		database.IAmFullNode = true
		// ensure that mode is possible
		localDbIsFullNode := database.BoltBucketExists("statedb")
		if !localDbIsFullNode { // if you are a light node or the RA but without reset flag, then exit with warning
			if !(database.IAmRA) {
				logger.L.Printf("You do not seem to be a full node.. You either are a new node that does not have a database yet (in this case: use -syncMode=SyncMode_Initial_Mine) or you are a light node but you tried to run with %v but this is not allowed! \n", curNodeMode)
				return
			} else { // you are the RA, arriving here is only allowed when you have set the raReset flag to true
				if !(*raResetFlag) {
					logger.L.Printf("You are the RA but you have no statedb and you forgot to set the raReset flag to true.. Not allowed, exiting..")
					return
				}
			}
		}
	case database.SyncMode_Continuous_Light, database.SyncMode_Passive_Light:
		database.IAmFullNode = false
		// ensure that mode is allowed (it should not be allowed to suddenly run a full node as a light node to keep things simple)
		localDbIsFullNode := database.BoltBucketExists("statedb")
		if localDbIsFullNode {
			logger.L.Printf("You are a full node but you tried to run with %v! This is not allowed, run it with _Full or _Mine (does not exist for Passive) instead! \n", curNodeMode)
			return
		}
	}

	// ensure prerequisites for being a miner are met
	if (curNodeMode == database.SyncMode_Continuous_Mine) || (curNodeMode == database.SyncMode_Initial_Mine) {
		// 		1. ensure user is running linux or macOS (fairrot does not support windows)
		if (runtime.GOOS != "linux") && (runtime.GOOS != "darwin") {
			logger.L.Panicf("You try to be a miner but you are not running an OS that supports FairSoft. Please try running this code on linux or macOS. Terminating.")
			return
		}
		//		2. if miner: check if PATH env variable contains substring 'fair' or 'sim' (at some point find more robust solution)
		pathEnvContent := os.Getenv("PATH")
		pathEnvContent = strings.ToLower(pathEnvContent)
		if  !(strings.Contains(pathEnvContent, "sim") || strings.Contains(pathEnvContent, "fair")) {
			logger.L.Panicf("You try to be a miner but querying PATH does not show any indication of fairsoft. Terminating.\nNote: If this is a false positive comment out this check or ensure that you have followed the fairroot tutorial I provided.")
			return
		}
		
		logger.L.Printf("Presence of fairsoft env has been verified.")

	}

	// remember initial nodemode
	database.OriginalMode = curNodeMode

	// print for user whether you are full node or light (but only if this is not just a dump of a block)
	if database.IAmFullNode && (*dumpFlag == "nil") {
		logger.L.Printf("I am a full node.")
	} else if !database.IAmFullNode && (*dumpFlag == "nil") {
		logger.L.Printf("I am a light node.")
	}

	// handle dump flag (must be handled first to avoid db reset in case the default syncmode is initial)
	dumpBlockKey := *dumpFlag
	if dumpBlockKey != "nil" {
		// check if provided flag possibly can be valid ( either it is "latest" or "genesis" (with "" or without "" both work) or its just some hash (without "") of length 64 )
		if (dumpBlockKey != "latest" && dumpBlockKey != "genesis") && len(dumpBlockKey) != 64 {
			logger.L.Printf("Invalid dump flag. Expected 'latest' or 'genesis' or hash hex string of length 64 without leading 0x.")
			return
		}

		// determine whether you are a full or a light node by checking if the statedb bucket exists or not
		localDbIsFullNodeDump := database.BoltBucketExists("statedb")

		// replace genesis string with actual hash of genesis block
		if dumpBlockKey == "genesis" {
			dumpBlockKey = database.ChainDbGetGenesisHash(localDbIsFullNodeDump)
		}

		// try to retrieve block
		retrievedBlockBytes, err := database.BlockGetBytesFromDb(dumpBlockKey)
		if err != nil {
			logger.L.Printf("This block does not seem to exist: %v", err)
			return
		}

		if localDbIsFullNodeDump {
			// deserialize block
			retrievedBlockObject := database.FullBlockBytesToBlock(retrievedBlockBytes)
			// print block, then terminate
			database.PrintBlock(retrievedBlockObject)
			return
		} else {
			// deserialize header
			retrievedHeaderObject, _ := database.HeaderBytesToBlockLight(retrievedBlockBytes)
			// print header, then terminate
			database.PrintBlockHeader(retrievedHeaderObject)
			return
		}

	}

	// handle topicNames flag
	//		first split user-given value by , (topic seperation symbol)
	topics := strings.Split(*topicNamesFlag, ",")		// returns []string
	//  	then append values to TopicSlice
	for _, tt := range topics {
		if strings.Contains(tt, ":") || strings.Contains(tt, " ") {
			logger.L.Panicf("Topics are not allowed to contain ':' or ' '. Terminating..")
		}
		database.TopicSlice = append(database.TopicSlice, tt)
	}

	// start performance stats server if it is not running already
	go monitoring.StartPerformanceStatsServer()
	
	// ---- Initialize SyncHelper ----

	// configure SyncHelper
	database.SyncHelper = database.SyncHelperStruct {
		NodeMode:			curNodeMode,
		ConfirmationsReq:	1, // during development this should be 1, but in production (when new nodes try to join an existing network) this value should be increased. it defines how many response confirmations from different nodes have to be received during inital sync until the response is accepted. a higher value should be more secure
		Data:				make(map[string]struct {
								ConfirmationsCur	int
								Data				[]byte
							}),
		Senders:			[]string{},
	}
	logger.L.Printf("SyncHelper configured to mode: %v", database.SyncHelper.NodeMode)

	// local db will be deleted when sync mode is initial_full/light/mine OR if raResetFlag is set by an RA that sends out problems
	if curNodeMode == database.SyncMode_Initial_Full || curNodeMode == database.SyncMode_Initial_Light || curNodeMode == database.SyncMode_Initial_Mine || (database.IAmRA && *raResetFlag){
		logger.L.Printf("Resetting database...")
		database.ResetAndInitializeGenesis(database.IAmFullNode)
	}

	// ---- Set-up node ----

	// Linux only fix: Set correct buffer size to prevent the following warning:
	//	Failed to sufficiently increase receive buffer size (was: 208 kiB, wanted: 2048 kiB, got: 416 kiB). 
	//	See https://github.com/quic-go/quic-go/wiki/UDP-Buffer-Sizes for details.
	//
	//FixLinuxBuffer()

	// let user cancel execution by sending SIGINT or SIGTERM (prevents issue where ctrl+C or ctrl+. don't kill running program)
	ctx, cancel := context.WithCancel(context.Background())
    sigs := make(chan os.Signal, 1) // avoid unbuffered channel here (https://github.com/golang/lint/issues/175)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    go func() {
    	<-sigs
        cancel()
    }()

	// key is stored in ed25519 format so that encrypted encoding works, but at startup is converted to libp2p ed25519 which slightly differs (avoids 'missing method Equal()' issues with priv)
	var ed25519Priv ed25519.PrivateKey
	var priv crypto.PrivKey
	var pub crypto.PubKey

	privKeyLocation := "privkey.key"
	raPrivKeyLocation := "raprivkey.key"

	// check whether user has existing keyfile or wants to generate new keys
	if *localKeyFileFlag {
		// depending on whether you are RA or not, read priv key from different files (useful to have access to both while testing)	
		if database.IAmRA {
			ed25519Priv = database.ReadKeyFromFileAndDecrypt(*pwFlag, raPrivKeyLocation)
		} else {
			ed25519Priv = database.ReadKeyFromFileAndDecrypt(*pwFlag, privKeyLocation)
		}
		logger.L.Printf("Successfully read and decrypted private key.")

	} else {
		// default: you are a new miner and want to create new priv/pub keypair at this startup
		var err error
		_, ed25519Priv, err = ed25519.GenerateKey(nil) // nil will use crypto/rand.Reader
		if err != nil {
			logger.L.Panic(err)
		}
	}

	// convert ed25519 priv to libp2p priv
	priv, err = crypto.UnmarshalEd25519PrivateKey(ed25519Priv)
    if err != nil {
        logger.L.Panicf("Failed to convert ed25519 priv to libp2p priv: %v\n", err)
    }

    // derive libp2p pub
    pub = priv.GetPublic()

	// derive libp2p nodeID from public key
	database.MyNodeIDString, err = database.PubKeyToNodeID(pub)
	if err != nil {
		logger.L.Panic(err)
	}

	// report your libp2p nodeID to performance stats server
	perfLibp2pID := monitoring.NewPerformanceData(time.Now().UnixNano(), database.MyDockerAlias, monitoring.Event_Libp2pNodeIDKnown, fmt.Sprintf("My libp2p node ID: %v", database.MyNodeIDString))
	err = monitoring.SendPerformanceStatWithRetries(perfLibp2pID)
	if err != nil {
		logger.L.Printf("Failed to submit my node ID to the performance stats server: %v", err)
	}

	// if you are RA, ensure that the derived nodeID is equal to the RAnodeID constant defined in constants.go
	if database.IAmRA && (database.MyNodeIDString != database.RANodeID) {
		logger.L.Panicf("The RA private key you used results in node ID %v but this version of the code sets the const RA node ID to %v. If you intend to use a different RA priv key ensure to also adjust the derived const RA node ID string in constants.go!", database.MyNodeIDString, database.RANodeID)
	}

	// set PrivateKey (view note about this in constants.go)
	database.PrivateKey = priv

	// ---- If you are not the RA: Switch into initial sync mode during peer discovery (to avoid distractions from other nodes) ----
	if !database.IAmRA {
		switch database.OriginalMode {
		case database.SyncMode_Continuous_Full:
			database.SyncHelper.NodeModeWrite(database.SyncMode_Initial_Full)
			logger.L.Printf("Temporarily setting you to %v for your sync.", database.SyncMode_Initial_Full)
		case database.SyncMode_Continuous_Light:
			database.SyncHelper.NodeModeWrite(database.SyncMode_Initial_Light)
			logger.L.Printf("Temporarily setting you to %v for your sync.", database.SyncMode_Initial_Light)
		case database.SyncMode_Continuous_Mine:
			database.SyncHelper.NodeModeWrite(database.SyncMode_Initial_Mine)
			logger.L.Printf("Temporarily setting you to %v for your sync.", database.SyncMode_Initial_Mine)
		}
	}


	// ---- Setup libp2p node ----


	// create connection manager
    // connMgr, err := connmgr.NewConnManager(
    //     200,         	// low watermark (keep attempting to open connections until you are connected to at least this many peers, it helps to stay well connected)
    //     2000,        	// high watermark (start closing less frequently used active connection as soon as you reach this many active connections)
    // )
    // if err != nil {
    //     panic(err)
    // }


    // use ifps team hosted bootstrap servers (https://github.com/ipfs/kubo/blob/master/config/bootstrap_peers.go#L17)
    var bootstrapPeers []peer.AddrInfo
	for _, multiAddr := range dht.DefaultBootstrapPeers {
		// convert each multiaddr.MultiAddr to a peer.AddrInfo
		addrInfo, err := peer.AddrInfoFromP2pAddr(multiAddr)
		if err != nil {
			logger.L.Panicf("failed to convert MultiAddr to AddrInfo: %v", err)
		}
		bootstrapPeers = append(bootstrapPeers, *addrInfo)
	}

    // configure node
	h, err := libp2p.New(
		libp2p.Identity(priv),							   // use generated private key as identity
		libp2p.UserAgent(fmt.Sprintf("pouw/%v", version)), // set agent version so that other nodes know which version of this software this node is running
		
		// options below make node publicly available if possible
		// 		rust tool to verify e.g.: libp2p-lookup dht --network ipfs --peer-id <nodeid> , then it shows the agent version among other listen addresses and supported protocols
		libp2p.EnableAutoRelayWithStaticRelays(bootstrapPeers), // become publicly available via ipfs hosted bootstrap nodes that act as relay servers
		libp2p.EnableHolePunching(), 	// enable holepunching
        libp2p.NATPortMap(), 			// try to use upnp to open port in firewall

        // ---- this will be used implicitely ----

		// libp2p.ListenAddrStrings( // as specified in defaults.go
		// 	"/ip4/0.0.0.0/tcp/0",
		// 	"/ip4/0.0.0.0/udp/0/quic-v1",
		// 	"/ip4/0.0.0.0/udp/0/quic-v1/webtransport",
		// 	"/ip6/::/tcp/0",
		// 	"/ip6/::/udp/0/quic-v1",
		// 	"/ip6/::/udp/0/quic-v1/webtransport",
		// ),

        //libp2p.DefaultSecurity,						// support TLS and Noise
		//libp2p.DefaultTransports,						// support any default transports (tcp, quic, websocket and webtransport)
		//libp2p.DefaultMuxers, 						// support yamux
		//libp2p.DefaultPeerstore, 						// use default peerstore (probably not necessary)

		// ---- tried these but not necessary afaik ----

        //libp2p.EnableRelayService(), 			// if you are publicly available act as a relay and help others behind NAT
        //libp2p.EnableNATService(), 			// if you are publicly available help other to figure out whether they are publicly available or not

        //libp2p.EnableRelay(), 				// makes no difference (because it is enabled by default)
        //libp2p.ForceReachabilityPublic(), 	// do NOT use this when you are behind NAT, i can't even ping my node with this enabled
	)
	if err != nil {
		logger.L.Panic(err)
	}

	// ---- Set stream handler for direct communication via chat protocol ----
	h.SetStreamHandler("/chat/1.0", func(stream network.Stream) {
        database.HandleIncomingChatMessage(stream, h, ctx)
    })

    // ensure that the nodeID you got is equal to the nodeID that should have been derived from the priv key you used
	if database.MyNodeIDString != h.ID().String() {
		logger.L.Panicf("The libp2p node ID you got is %v but from your private key it was expected that you would get %v. Aborting..", h.ID().String(), database.MyNodeIDString)
	}
	logger.L.Printf("My node ID: %v", database.MyNodeIDString)

	// print node addresses after 3 seconds
	// go func() {
	// 	time.Sleep(3 * time.Second)

	// 	logger.L.Printf("I am reachable via these addresses:")
	// 	for _, addr := range h.Addrs() {
	// 		logger.L.Printf("\t%v/p2p/%v", addr, database.MyNodeIDString)
	// 	}
	// }()


	// derive peer ID from node ID string
	myPeerID, err := peer.Decode(database.MyNodeIDString)
	if err != nil {
		logger.L.Panicf("Failed to derive peer ID from node ID string: %v", err)
	}
	if myPeerID.String() != database.MyNodeIDString {
		logger.L.Panicf("Failed to cast node ID string to peer ID. Calculated node ID %v and actual node ID %v should be equal!", myPeerID.String(), database.MyNodeIDString)
	}

	// add private and public key to keystore so that they later can be safely accessed when needed
	h.Peerstore().AddPrivKey(myPeerID, priv)
	h.Peerstore().AddPubKey(myPeerID, pub)

	// if you need to retrieve your private key you can do it like below
	//		h.Peerstore().PrivKey(h.ID())
	// or 
	//		h.Peerstore().PrivKey(database.MyNodeIDString)
	// or just access
	// 		database.PrivateKey

	// if you are not RA: store your private key in an encrypted key file (for RA it is assumed that the keyfile already exists and that everyone knows the nodeID derived from it), unless you have re-used your node identity
	if !(database.IAmRA) && !(*localKeyFileFlag) {
		comment := "gophy key"
		database.EncryptKeyAndWriteToFile(ed25519Priv, *pwFlag, privKeyLocation, comment)
		logger.L.Printf("Encrypted private key has been stored as: %v", privKeyLocation)
	}

	// ---- PubSub topic subscriptions ----

	// use gossipsub for message distribution over topics, returns *pubsub.PubSub (using default GossipSubRouter as the router)
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		logger.L.Panic(err)
	}
	// iterate over topics slice
	for _, t := range database.TopicSlice {
		topic, err := ps.Join(t)		// convert string to *pubsub.Topic (line 1228 of https://github.com/libp2p/go-libp2p-pubsub/blob/master/pubsub.go)
		if err != nil {
			logger.L.Panic(err)
		}

		// add topic to database.PubsubTopicSlice
		database.PubsubTopicSlice = append(database.PubsubTopicSlice, topic)

		// subscribe to topic of interest
		sub, err := topic.Subscribe()	// returns chan *pubsub.Subscription (line 143 of https://github.com/libp2p/go-libp2p-pubsub/blob/master/topic.go)
		if err != nil {
			logger.L.Panic(err)
		}

		logger.L.Printf("Subscribed to topic: %v", t)

		// listen for incoming messages on every topic that was subscribed to
		go database.TopicReceiveMessage(ctx, sub, h)	

	}

	// ---- Peer Discovery ----

	//  A lot happens in the background here:
	//	AFAIK it triggers DHT queries and asks a boostrap node or an early discovered neighbor node whether they know a node who used the same rendezvous string,
	//	then these nodes ask other nodes [close to them] whether they know and so on.. Out of all the ca. 30k DHT servers only 20 store the
	//	[hash(rendezvousString), peerID] list we need to know so log_20(30000) ~ 3.44 so this process is expected to be repeated multiple times
	//	until one of the 20 relevant DHT servers has been found and then they give you their nodeID list

	// its very important the these lines below are placed after you subscribed to all topics, otherwise topic messages get ignored (i think when you discover a peer you ask it 'what topics do you care about', so when the subscribing-to-topics would happen after peer discovery no one would receive topic messages because it would not be known that these nodes are subscribed to these topics so no one would try to propagate topic messages to them)
	ch := make(chan bool)					// unbuffered channel for bool used so that node waits until peers have been discovered before continuing
	go database.DiscoverPeers(ctx, h, ch)	// find peers using DHT bootstrap servers + Rendezvous
	<-ch	// wait until you read sth from DiscoverPeers
	logger.L.Printf("Initial peer discovery completed!")	// continue with program flow (but the discovery keeps running in background and is still looking for new peers)


	// ---- Nodes: Start initial sync / RA: Start sending sim tasks ----

	// RA uses this function
	if database.IAmRA {
		// start http server for send-transaction and send-simtask as goroutine
		go httpapi.RunServer()

		// keep sending new block problems
		go database.RALoop(ctx)

	} else {
		// All other nodes run networking entry function SyncNode
		logger.L.Printf("Starting initial sync..")
		database.SyncNode(ctx, h)

		// After completing initial sync you are connected to enough peers and able to receive transactions or simtasks via the locally hosted websites, so start the http server as goroutine (you should not be allowed to send transactions before you are in sync with the others)
		go httpapi.RunServer()	
	}

	// keep node running (otherwise if main terminates all goroutines are killed)
	select {}

	//		*/


	// err := database.CreatePseudoBlockchainLight(10)
	// if err != nil {
	// 	logger.L.Panic(err)
	// }
	// logger.L.Printf("Created light blockchain.")


	// err := database.CreatePseudoBlockchainFull(10, "supersecretrapw", 20)								
	// if err != nil {
	// 	logger.L.Panic(err)
	// }
	// logger.L.Printf("Created full blockchain.")

}

// FixLinuxBuffer silences a linux warnung that can occur due to mismatch between available and expected quic buffer size.
// It's not a necessity to perform this fix (the warning does not impact functionality and only occurs on some linux+libp2p combinations) and it requires root to fix, so decide yourself.
func FixLinuxBuffer() {
	if (runtime.GOOS != "linux") {
		return
	}

	// define the three commands needed to adjust buffer to recommended size
	cmd1 := exec.Command("sudo", "sysctl", "-w", "net.core.rmem_max=2097152") // 2048*1024=2097152
	cmd2 := exec.Command("sudo", "sysctl", "-w", "net.core.wmem_max=2097152")
	cmd3 := exec.Command("sysctl", "-p")

	// tell user to enter password (first two commands must be run as root, but remember: this fix is optional and not even used by default. afaik everything works normally even without it so running as root not worth it IMO)
	logger.L.Printf("You are running Linux. Enter root password to silence buffer warning:")

	// run commands
	if err := cmd1.Run(); err != nil {
		logger.L.Printf("Failed to adjust buffer size: %v\n", err)
		return
	}
	if err := cmd2.Run(); err != nil {
		logger.L.Printf("Failed to adjust buffer size: %v\n", err)
		return
	}
	if err := cmd3.Run(); err != nil {
		logger.L.Printf("Failed to adjust buffer size: %v\n", err)
		return
	}

	logger.L.Printf("Thanks, QUIC-go UDP buffer size has been set to recommended value of 2048 kiB.")
}
