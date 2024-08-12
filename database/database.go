// Package database defines how to write and read data to filesystem using bolt db. It also imports stuff like block so that it knows what data it is handling. It also includes all networking-related code (because this is where lots of data comes from).
package database

// Overview:
//		constants.go holds a few constants and also a few variables
// 		conversions.go takes care of conversions between various data types and structs
//		database.go mainly holds code related to storing or retrieving data in/from bbolt databases
//		httpapiqueues.go is handling data entered via the locally hosted websites localhost:8087/send-transaction and localhost:8087/send-simtask. It not only checks user input for validity and passes it to go code, but also displays results back on the website for the user to see.
// 		keys.go contains private key related functions that make it possible to safely store encrypted private keys in the OpenSSH format.
//		networking.go contains functions and structs helpful in the context of performing syncs between nodes. It features the SyncHelper and the BlockProblemHelper.
//		peerdiscovery.go is mainly responsible for peer discovery which is powered by libp2p's rendezvous and kademlia DHT
// 		pseudo.go contains testing functions used for debugging code and finding bottlenecks etc by creating large pseudo-blockchains that contain valid transactions.
// 		ralogic.go contains Root Authority logic such as determining accepted solution hash, choosing block winner, creating new blocks, etc. Nodes that are not the RA also perform similar operations but sometimes there are specifics done slightly differently by the RA.
//		topichandlers.go contains pubsub topic related functions that determine which action should follow upon receiving messages on various topics.
// 		transport.go takes care of networking related procedures such as wrapping data into a custom TransportStruct which takes care of signing the data etc.
//		validity.go contains a variety of functions used to verify the validity of certain pieces of data. This empowers nodes to control the RA and also other nodes so that they act according to rules everyone has agreed on.

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath" // OS-agnostic paths
	"sort"
	"strings"

	//"github.com/libp2p/go-libp2p/core/crypto" // note: do not auto-import this with gopls LSP, it will import go-libp2p-core instead of go-libp2p/core which leads to 'undefined: crypto.GenerateEKeyPair' error
	"github.com/vmihailenco/msgpack/v5" // serialize / deserialization using msgpack
	bolt "go.etcd.io/bbolt"             // database

	"github.com/felix314159/gophy/block"
	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/block/merkletree"
	//"github.com/felix314159/gophy/block/simpar"
	"github.com/felix314159/gophy/block/transaction"
	"github.com/felix314159/gophy/logger"
)

func init() {
	// databasePath holds the path to the blockchain database
    databasePath = filepath.Join("database", dbFileName)
	
	var err error
	// RApub holds the RA public key object
	RApub, err = NodeIDStringToPubKey(RANodeID)
	if err != nil {
		logger.L.Panic(err)
	}

	// RAnonce is used by pseudo.go functions to create valid transactions, but otherwise this value is not needed
	RAnonce = 0
}

// ---- Chaindb topic (sync blockchain blocks with other peers) ----

// BlockWriteToDb writes only to the chaindb bucket. It receives bytes from networking (block content) and a bool description whether these bytes represent a serialized full block or just a block header.
// Then the blockHash (which will be key) is determined and then the key-blockBytes pair is written to the database.
// You should only call this function after ensuring the validity of the block.
// Returns success code (nil on success).
// Mutex required because db access happens.
func BlockWriteToDb(blockData []byte, fullBlock bool) error {
	// mutex
	dbMutex.Lock()
    defer dbMutex.Unlock()


	var blockHeaderNew block.Header
	var blockNew block.Block			// dont call this variable block or you will be shadowing package block

	if fullBlock {
		// deserialize block to get its hash (key of key-value pair)
		err := msgpack.Unmarshal(blockData, &blockNew)
		if err != nil {
			return fmt.Errorf("BlockWriteToDb - Failed to deserialize this block.Block %v !\n", blockData)
		}
		blockHeaderNew = blockNew.BlockHeader

	} else {	// deserialize header (not strictly necessary but it confirms that its a valid header)
		err := msgpack.Unmarshal(blockData, &blockHeaderNew)
		if err != nil {
			return fmt.Errorf("BlockWriteToDb - Failed to deserialize this block.Header %v !\n", blockData)
		}

	}

	// refuse to ever write a block that has invalid PrevBlockHash (length not equal 64 hex digits)
	if len(blockHeaderNew.PrevBlockHash.GetString()) != 64 {
		return fmt.Errorf("BlockWriteToDb - The PrevHash is invalid, I refuse to write a block to db with this PrevHash: %v \n", blockHeaderNew.PrevBlockHash.GetString())
	}

	// serialize header (preparation for re-calculating its hash)
	blockHeaderNewSer, err := msgpack.Marshal(&blockHeaderNew)
	if err != nil {
		logger.L.Panic(err)
	}

	// re-calculate hash of header
	blockHash := hash.NewHash(string(blockHeaderNewSer))

	// debug:
	//reconstructedHeaderHash := blockHash.GetString()
	//logger.L.Printf("Reconstructed hash: %v", reconstructedHeaderHash)


	// open database
	db, err := bolt.Open(databasePath, 0600, nil) // 0600 == read/write
	if err != nil {
		return fmt.Errorf("BlockWriteToDb - Can't find database %v on filesystem!\n", databasePath)
	}

	// defer closing db
	defer db.Close()

	// write new block to chaindb bucket of db and then update 'latest'
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("chaindb"))	// it can be assumed this bucket exists because it will be created when initializing the genesis block
		err := b.Put(blockHash.Bytes, blockData)
		if err != nil {
			return err
		}
		// update 'latest' key-value pair
		err = b.Put(hash.NewHash("latest").Bytes, blockHash.Bytes)
		return err
	})

	return err
}

// BlockGetBytesFromDb takes a db key, checks whether this value is "latest" or a valid key in the local database and if so, returns the value of this key-value pair as bytes.
// Note that a full node that calls this function retrieves a block.Block, while a light node that calls this function retrieves a block.Header.
// Mutex required because db access.
func BlockGetBytesFromDb(chaindbKey string) ([]byte, error) {
	// mutex
	dbMutex.Lock()
    defer dbMutex.Unlock()

	// store retrieved data here (will still be nil if any errors occur)
	var retrievedData []byte
	var chaindbKeyBytes []byte

	// keccak256("nil") is the non-existing key for PrevBlockHash in genesis block. so don't look this key up if its provided
	nilKey := "a7d8eff4026f252db5b90c78e43dd191dfe6e55fcb98548a5f38faf0d4e3eb39"
	if chaindbKey == nilKey {
		return nil, fmt.Errorf("BlockGetBytesFromDb - Block with ID -1 can't be retrieved. This is expected, dont't worry. It always has hash: %v !\n", nilKey)
	}

	// convert provided hex string key to []byte (but not for 'latest' which is handled seperately)
	if chaindbKey != "latest" {
		var err error
		chaindbKeyBytes, err = hex.DecodeString(chaindbKey)
		if err != nil {
			return nil, fmt.Errorf("BlockGetBytesFromDb - Error decoding hex string: %v!\n", err)
		}
	}

	// open database
	db, err := bolt.Open(databasePath, 0400, nil) // 0400 == read only
	if err != nil {
		return nil, fmt.Errorf("BlockGetBytesFromDb - Can't find database %v on filesystem!\n", databasePath)
		
	}
	// defer closing db
	defer db.Close()

	// try to read key-value pair from database (in the case of 'latest' this will return a key, otherwise it will return serialized block)
	err = db.View(func(tx *bolt.Tx) error {
		bucketName := "chaindb"
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("BlockGetBytesFromDb - Bucket %v does not exist!", bucketName)
		}

		if chaindbKey == "latest" {
			chaindbKeyBytes = b.Get(hash.NewHash("latest").Bytes)
			if chaindbKeyBytes == nil {	// key 'latest' doesn't exist
				return fmt.Errorf("BlockGetBytesFromDb - Latest key %v does not exist!\n", chaindbKey)
			}
		}

		v := b.Get(chaindbKeyBytes)
		if v == nil {
			return fmt.Errorf("BlockGetBytesFromDb - Key %v does not exist!\n", chaindbKey)
		}

		// actual block content was retrieved, store it in retrievedData (copy entire slice using ...)
		retrievedData = append(retrievedData, v...)

		return nil
	})
	if err != nil {
		return nil, err
	}

	if retrievedData == nil {
		logger.L.Panicf("BlockGetBytesFromDb - Retrieved data is nil")
	}
	if len(retrievedData) < 1 {
		logger.L.Panicf("BlockGetBytesFromDb - Retrieved data has length 0")
	}

	return retrievedData, nil

}

// ChainDbGetGenesisHash retrieves the hash of the genesis block and returns it as string.
// Used for the -dump flag's 'genesis' functionality.
func ChainDbGetGenesisHash(iAmAFullNode bool) string {
	keys := BoltGetChainDBKeys()
	for _, key := range keys {
		// get block content
		blkContentBytes, err := BlockGetBytesFromDb(key)
		if err != nil {
			logger.L.Printf("ChainDbGetGenesisHash - Error: %v\n", err)
			continue
		}

		// deserialize block into header
		var blkHeader block.Header
		if iAmAFullNode {
			blkHeader, _ = FullBlockBytesToBlockLight(blkContentBytes)
		} else {
			blkHeader, _ = HeaderBytesToBlockLight(blkContentBytes)
		}

		// check if this is the genesis block
		if blkHeader.BlockID == uint32(0) {
			return key
		}
	}

	logger.L.Panic("ChainDbGetGenesisHash - Unable to find any genesis block / any block with ID 0.")
	return "" // this is annoying because go itself, the line above always panics but go doesnt understand it because it only views the normal panic as terminating but not the logger panic.
}

// GetAllBlockHashesAscending goes through every block of the local database and returns their hashes in ascending order (ascending by block ID, not by hash).
// Returns slice of strings, so the blockHashes can be seen as hex strings. Returned data starts at genesis block with index 0.
func GetAllBlockHashesAscending(fullNode bool) ([]string, error) {
	// get latest block/header
	curBlockBytes, err := BlockGetBytesFromDb("latest")
	if err != nil {
		logger.L.Panic(err)
	}

	// deserialize the block to look at block ID (e.g. block 15 has id 15)
	var curBlock block.Block
	var curHeader block.Header
	var latestID uint32
	var prevHash string
	var curHashString string

	if fullNode {
		curBlock = FullBlockBytesToBlock(curBlockBytes)
		
		// reconstruct blockhash
		curHash := HeaderToHash(curBlock.BlockHeader)
		curHashString = curHash.GetString()
		
		latestID = curBlock.BlockHeader.BlockID
		prevHash = curBlock.BlockHeader.PrevBlockHash.GetString()
		curHeader = curBlock.BlockHeader
	} else {
		var curHash hash.Hash
		curHeader, curHash = HeaderBytesToBlockLight(curBlockBytes)	// returns block.Header, hash.Hash
		curHashString = curHash.GetString()
		latestID = curHeader.BlockID
		prevHash = curHeader.PrevBlockHash.GetString()

	}

	// store results
	resultSlice := []string{curHashString, prevHash}

	// if block with id x was just retrieved, then retrieve x-1 more blocks [everything down to - including - block 1]. careful handling of unsigned int here
	for ; latestID > 0; latestID -= 1 {
		// retrieve block
		curBlockBytes, err = BlockGetBytesFromDb(prevHash)
		if err != nil {
			logger.L.Panic(err)
		}

		var curHeaderTemp block.Header
		
		// extract block.Header
		if fullNode {
			curHeaderTemp, _ = FullBlockBytesToBlockLight(curBlockBytes)
		} else {
			curHeaderTemp, _ = HeaderBytesToBlockLight(curBlockBytes)
		}

		// update prevHash for next iteration
		prevHash = curHeaderTemp.PrevBlockHash.GetString()

		// add hash of previous block to result slice (always access block x to find out hash of x-1. the hash of a block is not stored in its header because the header is used to determine the hash..)
		resultSlice = append(resultSlice, prevHash)

	}

	// reverse slice (i wont use slices.Reverse() because this required go 1.21 and im not willing to upgrade the version because it breaks libp2p quic)
	resultSliceReversed := []string{}
	for j := len(resultSlice)-1; j >= 0; j-- {
		resultSliceReversed = append(resultSliceReversed, resultSlice[j])
	}

	// return result except first element (imaginary block -1 that is PrevBlockHash of genesis)
	// clarification: this means that the first entry (index 0) of this return value is the GENESIS block
	return resultSliceReversed[1:], nil

}

// GetAllBlockHashesAscendingSerialized uses GetAllBlockHashesAscending() to get a string slice of all block hashes and then return them as serialized msgpack object.
// Returned data starts at genesis block with index 0.
func GetAllBlockHashesAscendingSerialized(fullNodes bool) []byte {
	// get block hashes
	stringSlice, err := GetAllBlockHashesAscending(fullNodes)
	if err != nil {
		logger.L.Panic(err)
	}

	// serialize it
	stringSliceSer, err := msgpack.Marshal(&stringSlice)
	if err != nil {
		logger.L.Panic(err)
	}

	return stringSliceSer

}

// DetermineLocallyMissingBlockHashes takes a slice of received blockHashes and then compares them with locally available blocks to determine which blocks are missing.
// This function returns all missing blocks (err nil) as string slice, but only if all locally available blockhashes are a subset of the received blockHashes (otherwise err != nil).
func DetermineLocallyMissingBlockHashes(recBlockHashes []string, fullNodes bool) ([]string, error) {
	// get locally available block hashes
	localBlockHashes, err := GetAllBlockHashesAscending(fullNodes) // from -1 to ...
	if err != nil {
		logger.L.Panic(err)
	}

	localLen := len(localBlockHashes)
	recLen := len(recBlockHashes)

	// check if localBlockHashes is subset (from start in same order) of recBlockHashes
	if localLen > recLen {
		logger.L.Printf("DetermineLocallyMissingBlockHashes - Given slice has been rejected due to being too small (can not possibly include any new data). This usually means you contacted an outdated/out-of-sync node.")
		return nil, nil
	}
	// check if a is subset of b from start
	for i := 0; i < localLen; i++ {
		if localBlockHashes[i] != recBlockHashes[i] {
			// debug
			logger.L.Printf("Got %v but expected %v \n", recBlockHashes[i], localBlockHashes[i])

			return nil, fmt.Errorf("DetermineLocallyMissingBlockHashes - Given slice and locally available data are incompatible! It could mean they start at different genesis block (or any following block doesnt match local state). This does not have to be a problem.")
		}
	}

	// ok we received relevant and new data, add it to resultSlice and return it
	var resultSlice []string
	for j := localLen; j < recLen; j++ {	// only iterate over new data
		resultSlice = append(resultSlice, recBlockHashes[j])
	}

	return resultSlice, nil
}

// ResetAndInitializeGenesis deletes the local database, creates a new database with chaindb and statedb buckets and then initializes the genesis block.
// Either the function works and nothing is returned or it panics.
// Mutex needed because db access.
func ResetAndInitializeGenesis(fullNode bool) {
	// mutex
	dbMutex.Lock()
	// unlock happens at the end manually because state must also be affected by another function that requires lock, so defer() would be too late here

	// delete existing db if it exists
	err := os.Remove(databasePath)	// ignore err that is returned (either file is deleted or file doesnt exist. either way file wont exist now)
	if err != nil {
		// only panic if the error is something other than "file does not exist"
		if !os.IsNotExist(err) {
			logger.L.Panic(err)
		} 

	}

	// testing and normal execution have two different current working directories. this code is aware of this and fixes it
	// would be more elegant with filepath package but this code has been tested lots of times so no need to bother changing it now
	curPath, _ := os.Getwd()
	//		LINUX FIX
	posOfLastFolderSepLinux := strings.LastIndex(curPath, "/")
	if posOfLastFolderSepLinux != -1 {							// will not be true for windows systems
		curDirLinux := curPath[posOfLastFolderSepLinux+1:]		// "abc/def/efg"	-> efg
		// check if we are in testing mode. if true, adjust path by setting it to the filenname
		if curDirLinux == "database" {
			databasePath = dbFileName	// the path is just the name of the file because we already are in that folder when testing
		} 

	}
	//		WINDOWS FIX
	posOfLastFolderSepWindows := strings.LastIndex(curPath, "\\")
	if posOfLastFolderSepWindows != -1 {							// will not be true for non-windows systems
		curDirWindows := curPath[posOfLastFolderSepWindows+1:]		// "abc\def\efg"	-> efg
		// check if we are in testing mode. if true, adjust path by setting it to the filenname
		if curDirWindows == "database" {
			databasePath = dbFileName	// the path is just the name of the file because we already are in that folder when testing
		} 

	}

	// initialize database with chaindb bucket
	db, err := bolt.Open(databasePath, 0600, nil) // 0600 == read/write
	if err != nil {
		logger.L.Panic(err)
	}
	
	// create buckets chaindb and statedb if they doesn't exist already
	err = db.Update(func(tx *bolt.Tx) error {
		// create chaindb bucket (every kind of node needs this)
		_, err := tx.CreateBucketIfNotExists([]byte("chaindb"))
		if err != nil {
			logger.L.Panic(err)
		}
		// only full nodes keep track of the state (account balances and transaction nonces)
		if fullNode {
			// create statedb bucket (Note: a light node will not have this statedb bucket. useful for -dump flag to determine whether it is full or light node)
			_, err = tx.CreateBucketIfNotExists([]byte("statedb"))
			if err != nil {
				logger.L.Panic(err)
			}
		}
		// only RA receives solution data and needs this bucket
		if IAmRA {
			// create simdb bucket (will temporarily store each solution submissions of miners for the current problem, next problem will override it)
			_, err = tx.CreateBucketIfNotExists([]byte("simdb"))
			if err != nil {
				logger.L.Panic(err)
			}
		}

		return nil
	})
	if err != nil {
		logger.L.Panic(err)
	}

	// get genesis block
	var gen block.Block
	var genSer []byte
	var genKey []byte
	
	if fullNode {
		gen, _ = block.GetGenesisBlock(true).(block.Block)	// .(block.Block) is needed because function returns {}interface, so we tell what we expect [type assertion]
		
		// reconstruct hash of this block
		genKeyHash := HeaderToHash(gen.BlockHeader)
		genKey = genKeyHash.Bytes
		
		genSer, err = msgpack.Marshal(&gen)
		if err != nil {
			logger.L.Panic(err)
		}
	} else {
		genHeader, _ := block.GetGenesisBlock(false).(block.Header)
		// serialize it to determine hash
		genSer, err = msgpack.Marshal(&genHeader)
		if err != nil {
			logger.L.Panic(err)
		}
		// determine its hash
		genKey = hash.NewHash(string(genSer)).Bytes
	}


	// write genesis block to db, then update "latest" key-value pair
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("chaindb"))
		// write block
		err := b.Put(  genKey, genSer  )
		if err != nil {
			logger.L.Panic(err)
		}
		// update 'latest' key-value pair, so that newest block can easily be retrieved (key: "latest", value: hash-of-newest-block)
		err = b.Put(  hash.NewHash("latest").Bytes, genKey  )
		if err != nil {
			logger.L.Panic(err)
		}

		return err
	})
	if err != nil {
		logger.L.Panic(err)
	}

	// ---- full node: Statedb Pre-allocations (for testing with valid transactions where RA is sender) ----
	
	if fullNode {
		// ---- add 30000 tokens to RA address (TODO: remove this before using the code in production. it just made testing a lot easier because RA had to be able to create valid transactions) ----
		raInitialState := StateValueStruct {
			Balance: 	30000,
			Nonce: 		0,
		}
		// serialize
		raInitialStateSer, err := msgpack.Marshal(&raInitialState)
		if err != nil {
			logger.L.Panic(err)
		}
		// write to statedb
		err = db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte("statedb"))
			err := b.Put([]byte(RANodeID), raInitialStateSer)
			return err
		})
		if err != nil {
			logger.L.Panic(err)
		}
	}

	// close db to avoid deadlock due to StateDbAwardWinner() 
	db.Close()
	// unlock mutex
	dbMutex.Unlock()

	if fullNode {
		// write bogus winnerAdress from gen block to state (important so that state merkletree root matches after sync)
		StateDbAwardWinner(gen.BlockHeader.BlockWinner.WinnerAddress, gen.BlockHeader.BlockWinner.TokenReward)
	}

}

// ---- End of chaindb related ----

// ---- StateDB / Simdb related ----

// StateValueStruct is a custom struct for the statedb. It holds token balance and transaction nonce. This struct is msgpacked and used as value in statedb.
// The corresponding key of these key-value pairs in statedb is the nodeID in string form.
type StateValueStruct struct {
	Balance	float64
	Nonce 	int
}

// WriteToBucket is the equivalent of BlockWriteToDb but instead of writing to the chaindb bucket it writes to the statedb or simdb bucket.
// The logic is slightly different as there is no such thing as full block / light block in this context.
// It takes a key (case statedb: nodeID as string, case simdb: solutionHash as string) and a value and either adds key-value pair to the database (if it does not exist already)
// or overwrites the value if the key already exists. An error is returned if anything goes wrong.
// Note: When targeting statedb bucket: This function does not check whether the value is valid, it just performs the statedb transaction and the dev is supposed to ensure the validity before calling this function.
func WriteToBucket(key string, serializedValue []byte, bucketName string) error {
	// refuse to ever write empty []byte
	if len(string(serializedValue)) < 1 {
		logger.L.Panic("WriteToBucket: It is not allowed to write empty data to statedb!")
	}

	// convert key from string to []byte
	var keyBytes []byte

	// case statedb: refuse to ever write a key-value pair where the key does not start with "12D3Koo" (libp2p peerID format)
	if bucketName == "statedb" {
		var CouldBeValidNodeID bool = strings.HasPrefix(strings.ToLower(key), "12d3koo")
		if !CouldBeValidNodeID {
			return fmt.Errorf("WriteToBucket - The key %v does not start with 12D3Koo. This is not allowed. Aborted statedb write.", key)
		}
		// also set keyBytes
		keyBytes = []byte(key)
	} else if bucketName == "simdb" {
		if len(key) != 64 {
			return fmt.Errorf("WriteToBucket - The key %v does not have length 64, so it does not seem to be a hash hex string . Aborted simdb write.", key)
		}

		// also set keyBytes (but since you gave a hex string decode it correctly)
		var err error
		keyBytes, err = hex.DecodeString(key)
		if err != nil {
			logger.L.Panic(err)
		}

	}

	// mutex
	dbMutex.Lock()
    defer dbMutex.Unlock()

	// open database
	db, err := bolt.Open(databasePath, 0600, nil) // 0600 == read/write
	if err != nil {
		return fmt.Errorf("WriteToBucket - Can't find database %v on filesystem!\n", databasePath)
		
	}
	// defer closing db
	defer db.Close()

	// update key-value pair in statedb bucket (overwrite value if key already exists)
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))	// it can be assumed this bucket exists because it will be created when initializing the genesis block
		err := b.Put(keyBytes, serializedValue)
		if err != nil {
			logger.L.Panicf("WriteToBucket - Failed to write key-value pair to bucket %v. This key is involved: %v. This error occurred: %v", bucketName, key, err)
		}

		return nil
	})

	return err

}

// ReadFromBucket takes a key as string and returns its corresponding value as []byte.
// Will return an error if the key does not exist.
func ReadFromBucket(bucketKey string, bucketName string) ([]byte, error) {
	// mutex
	dbMutex.Lock()
    defer dbMutex.Unlock()

	// store retrieved data here (will still be nil if any errors occur)
	var retrievedData []byte

	// open database
	db, err := bolt.Open(databasePath, 0400, nil) // 0400 == read only
	if err != nil {
		return nil, fmt.Errorf("ReadFromBucket - Can't find database %v on filesystem!\n", databasePath)
		
	}
	// defer closing db
	defer db.Close()

	// try to read key-value pair from database
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("ReadFromBucket - Bucket %v does not exist!", bucketName)
		}

		// cast key to bytes
		var keyBytes []byte
		if bucketName == "statedb" {
			keyBytes = []byte(bucketKey)
		} else if bucketName == "simdb" {
			var err error
			keyBytes, err = hex.DecodeString(bucketKey)
			if err != nil {
				logger.L.Panic(err)
			}
		}

		// retrieve the value from the db
		v := b.Get(keyBytes)
		if v == nil || len(v) < 1 {
			// logger.L.Printf("Key was not found, retrieved nil.")
			if bucketName == "statedb" {
				return nil // if account does not exist just return error nil, for statedb: it should be allowed to send tokens to (currently) non-existing account
			} else if bucketName == "simdb" {
				return fmt.Errorf("ReadFromBucket - Failed to read data from simdb bucket because the retrieved data is empty or non-existent! Issue occurred while trying to look up the value of this key: %v", bucketKey)
			}
			
		} 

		// actual block content was retrieved, store it in retrievedData (copy entire slice using ...)
		retrievedData = append(retrievedData, v...)

		return nil
	})
	// when an error occurs, the data retrieved will be set to nil
	if err != nil {
		return nil, err
	}

	return retrievedData, nil // if looked up key does not exist this returns nil, nil

}

// ResetBucket deletes and re-creates the given bucket. Used by RA to reset simdb bucket before broadcasting new block problem.
func ResetBucket(bucketName string) error {
	// mutex
	dbMutex.Lock()
    defer dbMutex.Unlock()

	// open database
	db, err := bolt.Open(databasePath, 0600, nil) // 0600 == read/write
	if err != nil {
		return fmt.Errorf("ResetBucket - Can't find database %v on filesystem!\n", databasePath)
	}
	// defer closing db
	defer db.Close()

	// update key-value pair in statedb bucket (overwrite value if key already exists)
	err = db.Update(func(tx *bolt.Tx) error {
		// delete bucket
		err = tx.DeleteBucket([]byte(bucketName))
		if err != nil {
			return err
		}

		// re-create bucket
		_, err = tx.CreateBucket([]byte(bucketName))
		if err != nil {
			return err
		}

		return nil
	})

	return err

}

// StateDbGetMerkleTree uses bbolt to get all keys of statedb in ascending order, then gets their values and hashes them, 
// then these hashes are used ascending as leafs of the merkle tree. Then the merkle tree is built and returned.
// This function will be called anytime after a new block has been fully processed to rebuild the statedb's Merkle Tree.
func StateDbGetMerkleTree() merkletree.MerkleTree {
	// get slice of all statedb keys from bbolt statedb bucket in ascending order (string)
	stateKeys := BoltGetDbKeys("statedb")

	// store merkleHashes (Hash(nodeID+statedbvalue))
	leafHashes := []hash.Hash{}

	// calculate merkleHashes
	for _, nodeID := range stateKeys {
		// get value from db
		serializedWallet, err := ReadFromBucket(nodeID, "statedb")
		if err != nil || serializedWallet == nil {
			logger.L.Panic(err)
		}
		// determine hash of <nodeID>+<serialized value> (Explanation: The key of each wallet here must represent its actual current content so that balance changes affect the merkle tree root. So only when building the state merkle tree the keys are replaced with what they actually are (just the nodeID string)).
		h := hash.NewHash(nodeID + string(serializedWallet))

		// append to leafHashes
		leafHashes = append(leafHashes, h)

		// debug
		//logger.L.Printf("Added sth derived from this key to StatenodeIDlistAfterTransctionProcessing: %v", nodeID)
		// 		deserialize serializedWallet
		//var nStruct StateValueStruct
		//err = msgpack.Unmarshal(serializedWallet, &nStruct)
		//if err != nil {
		//	logger.L.Panic(err) // if majority of nodes sent data that is invalid there is no point in communicating further
		//}
		//logger.L.Printf("BTW: this is that node's wallet after processing all transactions:\nBalance: %v\nNonce: %v\n", nStruct.Balance, nStruct.Nonce)
	
	}

	// EXAMPLE (Keccak256)
	// leaf 0:			3eafe3cd046ad8708da30438556876ebb9952e38d40a86106bfc172c5bf7672c
	// leaf 1: 			7503477aa9f61519d816c1250ced023e261f4ab288f2746af1589418b9a8beb6
	// parent 0+1:		ed46e7e0484cb995e69769875d12adaec8360be144525ae5d614f00c9338c468

	// leaf 2:			e59aeeca85b4c3b4884b08402d8e472d642d079b610dac6638a71e08d343edf7
	// leaf 3:			d491c994626da01c1971def666b544d1a8ac2261c201a2390dd7a81474ca5907
	// parent 2+3:		3b1fa10e7b126d9224c96dff9f2526a30c7b24976141a2921df9170c382c8038

	// root (p01+p23):	55c781c89ea70ea9550ca1f5e9709cb404272a2a4263f24114c23d23bb9e6421

	// debug:
	if DebugLogging {
		logger.L.Printf("StateDbGetMerkleTree - Debug: Will now pass the following state merkle tree keys to the MerkleTree constructor:")
		for _, merkleTreeKey := range leafHashes {
			logger.L.Printf("Key: %v\n", merkleTreeKey.GetString())
		}

		// also print all state info
		PrintStateDB()
	}

	// construct merkle tree
	stateMerkleTree := merkletree.NewMerkleTree(leafHashes)

	// debug:
	if DebugLogging {
		logger.L.Printf("StateDbGetMerkleTree - Debug: This is the state merkle tree I got:\n")
		stateMerkleTree.PrintTree()
	}

	return stateMerkleTree

}

// StateDbGetMerkleProof takes a nodeID and a stateMerkleTree and returns the merkle proof for that leaf.
func StateDbGetMerkleProof(nodeID string, stateMerkleTree merkletree.MerkleTree) ([]hash.LRHash, error) {
	// get merkleproof for RA
	raMerkleHash, err := StateDbNodeIDToMerkleHash(nodeID)
  	if err != nil {
  		return nil, fmt.Errorf("StateDbGetMerkleProof - Failed to convert nodeID  %v to a state merkle hash due to error: %v\n", nodeID, err)
  	}
  	merkleProof, err := stateMerkleTree.CalculateMerkleProof(raMerkleHash)
  	if err != nil || len(merkleProof) < 1 {
  		return nil, fmt.Errorf("StateDbGetMerkleProof - Failed to calculate state merkle proof for hash  %v due to error: %v\nIf error is nil it means merkle proof is empty", raMerkleHash, err)
  	}

  	return merkleProof, nil

}

// StateDbNodeIDToMerkleHash takes a nodeID as string and returns its corresponding hash in the state merkle tree.
// In the state merkle tree, the hash of a nodeID is actually nodeID+<statedb value of node> to bind the account to its current data.
// This allows reusing the same Merkle Tree code that was used for chaindb transactions.
func StateDbNodeIDToMerkleHash(nodeID string) (hash.Hash, error) {
	// get Hash(nodeID + <statedb value>) which is the key of this node in the state merkle tree
	v, err := ReadFromBucket(nodeID, "statedb")
	if err != nil || v == nil {
		return hash.Hash{}, fmt.Errorf("StateDbNodeIDToMerkleHash - Failed to retrieve statedb value of key %v due to error: %v\n", nodeID, err)
	}
	merkleNodeHash := hash.NewHash(nodeID + string(v))

	return merkleNodeHash, nil

}

// StateDbProcessAndUpdateBlock takes a block and performs its transactions in the statedb (statedb is affected) while also awarding the winner of this block.
// Returns the block with its updated StateMerkleRoot and updated TransactionsMerkleRoot. For pseudo sim: Note that now the block will have a new hash which means a new key for being stored in chaindb.
// Note: Before calling this function use BlockVerifyValidity() to ensure that all transactions are valid in this order, otherwise this function here could fail while runing leaving a corrupted state (e.g. transaction 3 fails when it occurs after transaction 2 but it seemed to be valid in isolation).
// This function only performs validity checks for included transactions, but it does not check the validity of other block fields like e.g. stateMerkleRootHash which makes it compatible with pseudo where this value is not known yet.
func StateDbProcessAndUpdateBlock(b block.Block) (block.Block, error) {
	// go through list of transactions and have them affect statedb if valid
	for _, t := range b.Transactions {
		// determine wallets of tx sender and receiver after it would have been processed
		err, fromAddress, updatedFromValue, toAddress, updatedToValue := StateDbTransactionIsAllowed(t, true, []byte{}, []byte{}) // returns bool, error, string, []byte, string, []byte
		if err != nil {
			return block.Block{}, fmt.Errorf("StateDbProcessAndUpdateBlock - Transaction is invalid: %v \n", err)
		}

		// perform transaction (affects statedb)
		err = PerformStateDbTransaction(fromAddress, updatedFromValue, toAddress, updatedToValue)
		if err != nil {
			return block.Block{}, fmt.Errorf("StateDbProcessAndUpdateBlock - Failed to process transaction, it might be invalid: %v", err)
		}
	}

	// award blockwinner with tokens by affecting statedb (must happen before state merkle tree is built)
	StateDbAwardWinner(b.BlockHeader.BlockWinner.WinnerAddress, b.BlockHeader.BlockWinner.TokenReward)

	// build the statedb merkle tree (a new block has successfully been processed after all)
	merkleTree := StateDbGetMerkleTree()

	// update the block header to include the new state merkle tree root hash
	b.BlockHeader.StateMerkleRoot = merkleTree.GetRootHash()

	// update transactionMerkleRootHash
	b.BlockHeader.TransactionsMerkleRoot = transaction.TransactionListToMerkleTreeRootHash(b.Transactions)

	// return the updated block
	return b, nil

}

// PerformStateDbTransaction takes transaction related info (state of involved wallets after tx was processed) as input and performs the transaction to affect the state.
// That means 'From' balance is decreased by Amount+Fee, 'From' Nonce is increased by 1, and 'To' balance is increased by Amount.
// The function returns nil on success and panics if something went wrong (at some point try to recover here, it should not happen anyways for now).
func PerformStateDbTransaction(fromAddress string, updatedFromValue []byte, toAddress string, updatedToValue []byte) error {
	// update accounts
	//		From
	err := WriteToBucket(fromAddress, updatedFromValue, "statedb")
	if err != nil {
		logger.L.Panicf("Failed to update statedb value: %v \n", err)
	}
	//		To
	err = WriteToBucket(toAddress, updatedToValue, "statedb")
	if err != nil {
		logger.L.Panicf("Failed to update statedb value: %v \n", err)
	}

	return nil
}

// StateDbAwardWinner is used when a block that has been signed by the RA is received to award the block's winner address.
// In order to determine tokenAmount a previous call to winner.GetTokenRewardForBlock(blockIndex int) should be made before calling this function.
func StateDbAwardWinner(nodeID string, tokenAmount float64) {
	// will later be written to statedb
	var nodeStateSer []byte

	// check whether account exists in statedb already
	nodeVal, err := ReadFromBucket(nodeID, "statedb")
	if err != nil {
		logger.L.Panic(err)
	}
	if nodeVal == nil { // account did not exist in statedb before
		nodeInitialState := StateValueStruct {
			Balance: 	tokenAmount,
			Nonce: 		0,
		}
		// serialize
		nodeStateSer, err = msgpack.Marshal(&nodeInitialState)
		if err != nil {
			logger.L.Panic(err)
		}

	} else { // account does exist already; increase existing balance while preserving current nonce
		// retrieve previous state of nodes account by deseserializing
		var nodePrev StateValueStruct
		err := msgpack.Unmarshal(nodeVal, &nodePrev)
		if err != nil {
			logger.L.Panic(err)
		}
		// award tokens
		nodePrev.Balance += tokenAmount
		// serialize
		nodeStateSer, err = msgpack.Marshal(&nodePrev)
		if err != nil {
			logger.L.Panic(err)
		}

	}

	// write new value to statedb
	err = WriteToBucket(nodeID, nodeStateSer, "statedb")
	if err != nil {
		logger.L.Panic(err)
	}

}

// ---- End of statedb or simdb related -----

// ---- Bolt related ----

// BoltGetChainDBKeys returns a []string which contains all keys of chaindb bucket.
func BoltGetChainDBKeys() []string {
	// mutex
	dbMutex.Lock()
    defer dbMutex.Unlock()

	var chainDbKeys []string
	
	// open database
	db, err := bolt.Open(databasePath, 0400, nil) // 0400 == read only
	if err != nil {
		logger.L.Panic(err)
		
	}
	// defer closing db
	defer db.Close()

	// try to read key-value pair from database (in the case of 'latest' this will return a key, otherwise it will return serialized block)
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("chaindb"))
		if b == nil {
			return fmt.Errorf("BoltGetChainDBKeys - Bucket chaindb does not exist!")
		}
		// get each valid key and use it to request data, deserialize the data and append it to retrievedData
		cursor := b.Cursor()
		for validKey, _ := cursor.First(); validKey != nil; validKey, _ = cursor.Next() {
			chainDbKeyString := hex.EncodeToString(validKey)
			chainDbKeys = append(chainDbKeys, chainDbKeyString)

		}

		return nil
	})
	if err != nil {
		logger.L.Panic(err)
	}
	return chainDbKeys

}


// BoltGetDbKeys takes the name of a db bucket and returns a sorted []string which contains all keys of that bucket.
// When bucket statedb is specified this would return the nodeID addresses of all nodes that at some point had a non-zero balance (either they were sent currency or they were chosen blockwinner at some point).
// The returned slice entries are sorted by ascending order (0<9<a<z and abc < abc0) and the order is case-insensitive.
func BoltGetDbKeys(bucketName string) []string {
	// mutex
	dbMutex.Lock()
    defer dbMutex.Unlock()

	var dbKeys []string

	// open database
	db, err := bolt.Open(databasePath, 0400, nil) // 0400 == read only
	if err != nil {
		logger.L.Panic(err)
		
	}
	// defer closing db
	defer db.Close()

	// try to read key-value pair from database (in the case of 'latest' this will return a key, otherwise it will return serialized block)
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketName))
		if b == nil {
			return fmt.Errorf("BoltGetStateDbKeys - Bucket %v does not exist!", bucketName)
		}
		// get each valid key and use it to request data, deserialize the data and append it to retrievedData
		cursor := b.Cursor()
		for validKey, _ := cursor.First(); validKey != nil; validKey, _ = cursor.Next() {
			dbKeyString := string(validKey)
			dbKeys = append(dbKeys, dbKeyString)

		}

		return nil
	})
	if err != nil {
		logger.L.Panic(err)
	}

	// sort in ascending order (modifies original)
	SortStringSliceAscending(dbKeys)

	return dbKeys

}

// BoltBucketExists checks whether a given bucket name exists in the db or not.
// This function is used by the -dump flag functionality to determine whether the retrieved chaindb data will be a serialized full block or a serialized header, which only works because light nodes do not have the statedb bucket at all.
func BoltBucketExists(bucketName string) bool {
	// mutex
	dbMutex.Lock()
    defer dbMutex.Unlock()

    // first ensure whether the db itself or not exists, if not return false
	_, err := os.Stat(databasePath)
    if os.IsNotExist(err) { // if the file does not exist, then return false which indicate that the bucket in question also does not exist
        return false
    }

    // sometimes there is an issue where db for some reason has read-only permission (400), so always just set it back to 600 (r+w for owner)
    err = os.Chmod(databasePath, 0600)
	if err != nil {
		panic(fmt.Sprintf("Failed to change db permission to read+write (600): %v", err))
	}

	// open database
	db, err := bolt.Open(databasePath, 0400, nil) // 0400 == read only
	if err != nil {
		logger.L.Panic(err)
		
	}
	// defer closing db
	defer db.Close()

	bucketExists := false
	db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		if bucket != nil {
			bucketExists = true
		}
		return nil
	})

	return bucketExists
}


// ---- Helper functions ----

// SortStringSliceAscending takes a string slice and sorts it (original is modified) ascending, case-insensitive. Ascending means 0<9<a<z and abc < abc0.
func SortStringSliceAscending(stringsToSort []string) {
	sort.Slice(stringsToSort, func(i, j int) bool {
		return strings.ToLower(stringsToSort[i]) < strings.ToLower(stringsToSort[j])
	})

}

// ---- Printing stuff ----

// PrintBlockHeader prints the provided block.Header.
// It is used in combination with the dump flag, where the user provides the key of the block that he wants to see printed out.
func PrintBlockHeader(h block.Header) {
	tokenReward := h.BlockWinner.TokenReward

	// debugging help: figure out if any hash value is nil and if so which one
	givenPrevBlockHash := h.PrevBlockHash
	if givenPrevBlockHash.Bytes == nil {
		logger.L.Panicf("PrintBlockHeader - PrevBlockHash of block with id %v is nil!", h.BlockID)
	}

	givenTransMerkleRoot := h.TransactionsMerkleRoot
	if givenTransMerkleRoot.Bytes == nil {
		logger.L.Panicf("PrintBlockHeader - TransMerkleRoot of block with id %v is nil!", h.BlockID)
	}

	givenStateMerkleRoot := h.StateMerkleRoot
	if givenStateMerkleRoot.Bytes == nil {
		logger.L.Panicf("PrintBlockHeader - StateMerkleRoot of block with id %v is nil!", h.BlockID)
	}

	givenProblemID := h.ProblemID
	if givenProblemID.Bytes == nil {
		logger.L.Panicf("PrintBlockHeader - ProblemID of block with id %v is nil!", h.BlockID)
	}

	givenSolutionHash := h.BlockWinner.SolutionHash
	if givenSolutionHash.Bytes == nil {
		logger.L.Panicf("PrintBlockHeader - SolutionHash of block with id %v is nil!", h.BlockID)
	}

	// reconstruct blockHash
	hHash := HeaderToHash(h)

	logger.L.Printf("\nBlockID: %v\nBlockTime: %v\nPrevBlockHash: %v\nTransactionsMerkleRoot: %v\nStateMerkleRoot: %v\nProblemID: %v\nBlockWinner - Address: %v\nBlockWinner - SolutionHash: %v\nBlockWinner - TokenReward: %v\nBlockHash: %v\n", h.BlockID, h.BlockTime, givenPrevBlockHash.GetString(), givenTransMerkleRoot.GetString(), givenStateMerkleRoot.GetString(), givenProblemID.GetString(), h.BlockWinner.WinnerAddress, givenSolutionHash.GetString(), tokenReward, hHash.GetString())

}

// PrintBlock prints the entire block content.
func PrintBlock(b block.Block) {
	// print header
	PrintBlockHeader(b.BlockHeader)

	// print transaction list with details
	logger.L.Printf("Transaction List:\n")
	for tIndex, t := range b.Transactions {
		logger.L.Printf("Transaction %v:\nFrom: %v\nTxTime: %v\nTo: %v\nValue: %v\nReference: %v\nTxHash: %v\nSignature: %v\nNonce: %v\nFee: %v\n\n", tIndex, t.From, t.TxTime, t.To, t.Value, t.Reference, t.TxHash.GetString(), hex.EncodeToString(t.Sig), t.Nonce, t.Fee)
	}

	// print simtask metadata (not actual parameters to reduce amount of text output)
	logger.L.Printf("SimTask Metadata:\n\tProblemHash: %v\n\tCreationTime: %v\n\tExpirationTime: %v\n\tBlockID: %v\n\tAmountSubProblems: %v\n\tRACommit: %v\n\n", b.SimulationTask.ProblemHash.GetString(), b.SimulationTask.SimHeader.CreationTime, b.SimulationTask.SimHeader.ExpirationTime, b.SimulationTask.SimHeader.BlockID, b.SimulationTask.SimHeader.AmountSubProblems, b.SimulationTask.SimHeader.RAcommit.Bytes) // print ra commit as bytes because it might not be visible symbols anyways

}

// PrintSerializedStringSliceBlockHashes takes a serialized slice of strings, and prints its content after deserialization.
func PrintSerializedStringSliceBlockHashes(receivedMessage []byte) {
	var stringSliceBlockHashes []string
	err := msgpack.Unmarshal(receivedMessage, &stringSliceBlockHashes)
	if err != nil {
		logger.L.Printf("HandleIncomingChatMessage - Failed to unmarshal the received blockHash data: %v", err)
	} else {	// for now just print received data
		logger.L.Printf("I received these blockhashes:")
		for i, v := range stringSliceBlockHashes {
			logger.L.Printf("BlockHash Index %v: %v", i, v)
		}
	}
}

// PrintStateDB gets all keys from the statedb, retrieves the corresponding values (wallets) and prints their info.
func PrintStateDB() {
	stateKeys := BoltGetDbKeys("statedb")
	for _, nodeID := range stateKeys {
		logger.L.Printf("Retrieving wallet of statedb key '%v'\n", nodeID) // the key is equal to the nodeID of the wallet holder

		// retrieve data
		walletBytes, err := ReadFromBucket(nodeID, "statedb")
		if err != nil {
			logger.L.Panic(err)
		}

		// deserialize data
		wallet, err := StateDbBytesToStruct(walletBytes)
		if err != nil {
			logger.L.Panic(err)
		}

		// print wallet info
		PrintStateDbWallet(wallet, nodeID)

		// print what the state merkle tree key would be for this node
		stateMerkleKeyString := hash.NewHash(nodeID + string(walletBytes)).GetString()
		logger.L.Printf("The merkle key for node %v would be %v\n", nodeID, stateMerkleKeyString) 

	}
}

// PrintStateDbWallet takes a StateValueStruct and prints it.
func PrintStateDbWallet(w StateValueStruct, keyValue string) {
	logger.L.Printf("\nWallet of %v:\n\tBalance: %v\n\tNonce: %v\n----\n", keyValue, w.Balance, w.Nonce)
}