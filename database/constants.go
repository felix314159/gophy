package database

// constants.go not only includes constants but also other vars

import (
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// dbFileName is the name of the bbolt database file that stores blockchain data
	dbFileName = "pouw_blockchain.db"

	// RANodeID holds the known nodeID of root authority (before production use, if you want to change RA private key ensure that this address is correctly derived from your new RA public key)
	RANodeID = "12D3KooWEYSb69dzeojEH1PygPWef9V1qQJqrGKUEMMsbA4keAyZ"

	// RendezvousString is the rendezvous string for initial node discovery
	RendezvousString = "gophypouwblockchain"

	// BlockTime is the interval at which RA broadcasts new problems and ideally creates new blocks. 86400 sec is around one day, for running tests i will use 1200 (20 min block time which is much faster than production target)
	BlockTime = 1200

	// TransactionsPerBlockCap is the Transactions per Block Upper Cap (each block can contain up to this many transactions)
	TransactionsPerBlockCap = 4757 // rough estimate rationale can be found in transaction.go

	// directChatRetryAmountUpperCap defines how often a node will try to send a message to another node directly via chat protocol when it keeps failing (other node might be offline or not reachable for some other reason so it would not be wise to not have an upper retry cap here)
	directChatRetryAmountUpperCap = 50

	// genesisHash is the hash of the genesis block that every node agrees to start with (TODO: if you remove 30k RA prealloc from gen also update this hash)
	genesisHash = "4d5e17d3bd7a33d83e31927ea33ffad143f8d95d0063a5cc80e0dbc1e77e5d51"

	// DebugLogging affects how much data is being logged, setting it to true results in a huge log file because all state and state merkle related info is logged
	DebugLogging = false

	// SyncMode is an alias for int which is used as a kind of enum that does not exist in Go (remember to add new stuff also to stringer interface in transport.go)
	SyncMode_Initial_Full	 	Mode = iota + 1 // ensure to NOT start at 0 because that is the nil value of int which could lead to confusing problems
	SyncMode_Initial_Light
	SyncMode_Initial_Mine
	SyncMode_Continuous_Full
	SyncMode_Continuous_Light
	SyncMode_Continuous_Mine
	SyncMode_Passive_Full // Miners should be in Continuous_Mine, that's why there is no Passive_Mine
	SyncMode_Passive_Light
	

	// TSData also is an alias for int which is used as enum to describe data types during transport (remember to add new stuff also to stringer interface in transport.go)
	TSData_Block       TSData = iota + 21 	// iota ensures auto-increment for each element
	TSData_Header                         	//
	TSData_StringSlice                    	//
	TSData_SimulationTask                   // SimulationTask is defined in simpar.SimulationTask
	TSData_SimSol 							// this means you are sending/receiving a simsol.BlockProblemSolution
	TSData_MinerCommitment					// defined in simsol.MinerCommitment
	TSData_RAcommitSecret  					// string that is revealed as soon as block problem expires
	TSData_ChainDBRequest   				// you want to request chaindb data from any node
	TSData_Transaction 						// transaction.Transaction
	TSData_LiveData 						// Struct that holds BlockProblemHelper and slice of pending transactions
)

// ---- Mutexes used for safe concurrent access ----

// dbMutex is a mutex used to prevent simultaneous database accesses (must be used locked and defer unlocked in every function that accesses database)
var dbMutex sync.Mutex

// networkingMutex is used so that concurrent reads are allowed, but not concurrent writes and also not concurrent read and write
var networkingMutex sync.RWMutex

// nodeModeMutex is used to you can check which node mode you are in without having to block all other functions that might be called on SyncHelper. This was introduced to reduce mutex locking times (tradeoff: one more mutex = one more source of errors / confusion / complexity)
var nodeModeMutex sync.RWMutex

// currentWinnerMutex for safely accessing CurrentWinner
var currentWinnerMutex sync.Mutex

// HttpapiSimparMutex is a mutex used to allow concurrency-safe access to pending simtasks (use this for writing)
var HttpapiSimparMutex sync.Mutex

// HttpapiSimparRMutex is a mutex used to allow concurrency-safe access to pending simtasks (use this for read-only)
var HttpapiSimparRMutex sync.RWMutex

// HttpapiTransactionMutex is a mutex used to allow concurrency-safe access to pending transactions (use this for writing)
var HttpapiTransactionMutex sync.Mutex

// HttpapiTransactionRMutex is a mutex used to allow concurrency-safe access to pending transactions (use this for read-only)
var HttpapiTransactionRMutex sync.RWMutex

// ---- End of Mutexes ----

// ---- RA ----

// RApub is the known public key of the RA (will be dynamically set by converting RANodeID constant to PubKey)
var RApub crypto.PubKey

// IAmRA describes whether a node is the RA or not
var IAmRA bool

// raPeerID holds the peer ID of the root authority
var raPeerID peer.ID

// ---- Pseudo Simulation ----

// RAnonce is a global nonce for testing/simulation purposes
var RAnonce int

// ---- End of Simulation ----

// databasePath holds the path to the database file pouw_blockchain.db (will be dynamically set depending on the OS)
var databasePath string

// MyNodeIDString holds your own nodeID as 12D3Koo... string
var MyNodeIDString string

// SyncHelper is used to coordinate the current sync
var SyncHelper SyncHelperStruct

// BlockProblemHelper is used to keep track of other miner's commitments for the current block problem
var BlockProblemHelper BlockProblemHelperStruct

// PrivateKey holds your own private key. TODO: it would be better if this is only temporarily retrieved from libp2p keystore whenever its needed and then asap safely removed from memory. but for PoC demonstration this is fine for now
var PrivateKey crypto.PrivKey

// IAmFullNode describes whether a node is a full node or not
var IAmFullNode bool

// TopicSlice holds topics (as strings) that user wants to subscribe to
var TopicSlice = []string{}

// PubsubTopicSlice holds all *pubsub.Topic
var PubsubTopicSlice = []*pubsub.Topic{}

// didReceiveLiveData is used to keep track of whether live data was correctly received from RA (default: false)
var didReceiveLiveData bool

// initialSyncChaindbDataWasReceivedAlready is used so that when a new block is received while you are still waiting for live data from RA, it can be decided whether you are able to handle the new block or not (you need to know the previous block to be able to check the validity)
var initialSyncChaindbDataWasReceivedAlready bool

// OriginalMode stores the initial mode a node was in at startup. It's used to determine the final sync mode after the initial sync is completed.
var OriginalMode Mode 

// HttpPort defines on which port the local websites for sending transactions and simtasks are run on
var HttpPort int

// MyDockerAlias specifies the name of the docker instance that runs this node. Useful for connecting the performance stat reported by a node with its docker identity (if it would report its libp2p node ID it would be a lot of manual work to see whether the given string maps to 1f or 2f or whatever)
var MyDockerAlias string