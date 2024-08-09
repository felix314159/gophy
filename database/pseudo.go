package database

/* notes:
1. to simulate blocks with transactions you need to be able to generate valid transactions
2. valid transactions must be sent by nodes that own non-zero balance and also must be signed correctly so you need to access the private key of the RA, as for testing they are the only node with pre-allocated tokens
3. the private keys are now protected with a password, so the pseudo.go functions can only be called when you have access to raprivkey.key and also know its password
*/
// pseudo.go contains testing functions used for debugging code and finding bottlenecks etc by creating large pseudo-blockchains. Note: Running this function will delete the locally stored blockchain.

import (
	cryptoRand "crypto/rand" // must get different namespace so that it doesnt clash with math/rand
	"fmt"
	"math/rand" // Note: do NOT use rand.Seed(time.Now().UnixNano()) to produce seeds (like 90% of the time you get the same values)
	"os"
	"time"

	// instead use crypto rand b := make([]byte, c) ; _, err := rand.Read(b)   to produce higher quality pseudo-randomness
	"path/filepath"

	"github.com/btcsuite/btcutil/base58" // used together with crypto/rand to generate human-readable random strings
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer" // pub -> peerID
	"github.com/vmihailenco/msgpack/v5"

	"github.com/felix314159/gophy/block"
	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/block/transaction"
	"github.com/felix314159/gophy/block/winner"
	"github.com/felix314159/gophy/logger"
)

// CreatePseudoBlockchainFull is a helper testing function which creates a few example block.Block and adds them to pouw_blockchain.db.
// This function affects both the chaindb bucket and the statedb bucket.
// This function takes two parameters: Firstly the amount of blocks to be created, secondly the key password of the RA so that valid transactions can be created. If you pass 1 for the amount of blocks you will get genesis + one additional block, so 2 blocks in total (genesis is not counted in what is passed by the user).
func CreatePseudoBlockchainFull(blockAmount int, RApw string, transactionAmountPerBlock int) error {
	if (blockAmount < 1) || (blockAmount > 1000) {	// less than 1k blocks guarantees RA has enough funds to perform 3 transactions per block a 10 tokens
		return fmt.Errorf("CreatePseudoBlockchainFull - Invalid parameter given. Must be in [1,1000]")
	}

	IAmRA = true // you must set this to true so that empty simdb bucket is created, so that RA will be able to use this (non-RA nodes don't mind the empty bucket but RA can't use this without it)

	blockAmount += 1 // when user says e.g. 'create 5 blocks' then he probably means genesis + 5 additional blocks, so add 1 here for the genesis block

	// ---- Reset blockchain and initialize genesis ----
	ResetAndInitializeGenesis(true)

	// holds slice of blockHashes (excludes genesis)
	var acceptedDataStringSlice []string 

	// ---- Define blocks ----
	// get block 1 by reading latest (only genesis exists) from local db
	genBytes, err := BlockGetBytesFromDb("latest")
	if err != nil {
		return fmt.Errorf("CreatePseudoBlockchainFull - Msgpack failed to get latest full block from db: %v \n", err)
	}
	// deserialize it to get block.Block
	prevBlock := FullBlockBytesToBlock(genBytes)

	// generate more blocks

	// measure execution time of this function
	var timeTakenPerBlockInMS []int64 // measure how long checking each block for validity takes in micro seconds
	startTime := time.Now() // will be updated with each iteration

	for i := 1; i < blockAmount; i++ {
		// get new block
		invalidBlock := getNextBlock(prevBlock, RApw, transactionAmountPerBlock)
		logger.L.Printf("Got block, will now affect statedb..")

		// have block affect the statedb and itself (will get new StateMerkleRoot value in its Header)
		newBlock, err := StateDbProcessAndUpdateBlock(invalidBlock)
		if err != nil {
			logger.L.Panic(err)
		}

		// remember its hash
		// 		serialize header (preparation for re-calculating its hash)
		blockHeaderNewSer, err := msgpack.Marshal(&newBlock.BlockHeader)
		if err != nil {
			logger.L.Panic(err)
		}
		// 		calculate hash of header
		blockHash := hash.NewHash(string(blockHeaderNewSer))
		//		remember blockHash as string
		acceptedDataStringSlice = append(acceptedDataStringSlice, blockHash.GetString())
		
		// serialize entire block now
		newBlockSer, err := msgpack.Marshal(&newBlock)
		if err != nil {
			logger.L.Panicf("Failed to serialize next block. Aborting due to: %v", err)
		}
		
		// write it to chaindb
		err = BlockWriteToDb(newBlockSer, true)
		if err != nil {
			logger.L.Panicf("Failed to write next block to db. Aborting due to: %v", err)
		}

		// update prevBlock
		prevBlock = newBlock

		// remember how long it took to create this header
		elapsedTimeInMicroSeconds := time.Since(startTime).Microseconds()
		timeTakenPerBlockInMS = append(timeTakenPerBlockInMS, elapsedTimeInMicroSeconds)
		// reset clock for next iteration
		startTime = time.Now()

		logger.L.Printf("Successfully created block %v\n", i) //  helps to roughly identify speed bottleneck
		
	}

	// totalRuntime holds the time in micro seconds running this function took in total
	var totalRuntime int64
	for _, v := range timeTakenPerBlockInMS {
		totalRuntime += v
	}

	var totalRuntimeInSeconds int64
	if totalRuntime > 0 { // never divide by zero, i definitely won't repeat microsofts mistake: https://github.com/JHRobotics/patcher9x?tab=readme-ov-file#patch-for-windows-9598-to-fix-cpu-speed-limit-bug
		totalRuntimeInSeconds = int64(float64(totalRuntime)/float64(1000000))
	}

	logger.L.Printf("Successfully created %v block large full node blockchain in %v microseconds ≈ %v seconds.\n", blockAmount, totalRuntime, totalRuntimeInSeconds)

	// print size of the blockchain file
	dbFileInfo, err := os.Stat(databasePath)
	if err != nil {
		logger.L.Panicf("Failed to get stats of blockchain file: %v\n", err)
	}
	logger.L.Printf("Size of blockchain is %v bytes", dbFileInfo.Size())

	// you can not call BlockchainVerifyValidity here because it would affect the state, but you already have the current state when using pseudo.

	return nil

}

// CreatePseudoBlockchainLight is a helper testing function which creates a few example block.Header and adds them to pouw_blockchain.db.
// This function affects only the chaindb bucket.
func CreatePseudoBlockchainLight(blockAmount int) error {
	IAmRA = true // you must set this to true so that empty simdb bucket is created, so that RA will be able to use this (non-RA nodes don't mind the empty bucket but RA can't use this without it)

	// ---- Reset blockchain and initialize genesis light ----
	ResetAndInitializeGenesis(false)

	// set RNG seed depending on current time
	rand.New(rand.NewSource(int64(block.GetCurrentTime())))

	// first read latest (genesis) from local db
	genBytes, err := BlockGetBytesFromDb("latest")
	if err != nil {
		return fmt.Errorf("CreatePseudoBlockchainLight - Msgpack failed to get latest header from db: %v \n", err)
	}
	// deserialize it to get block.Header (getNextHeader will expect an actual Header)
	gen, _ := HeaderBytesToBlockLight(genBytes)

	prevHeader := gen	// block.Header
	start := 1

	// measure execution time of this function
	var timeTakenPerBlockInMS []int64 // measure how long checking each block for validity takes in micro seconds
	startTime := time.Now() // will be updated with each iteration

	for i := start; i<blockAmount; i++ {
		// get new header
		newHeader := getNextHeader(prevHeader)
		
		// serialize it
		newHeaderSer, err := msgpack.Marshal(&newHeader)
		if err != nil {
			logger.L.Panicf("Failed to serialize next header. Aborting due to: %v", err)
		}
		
		// write it to db
		err = BlockWriteToDb(newHeaderSer, false)
		if err != nil {
			logger.L.Panicf("Failed to write next header to db. Aborting due to: %v", err)
		}

		// update prevHeader
		prevHeader = newHeader

		// remember how long it took to create this header
		elapsedTimeInMicroSeconds := time.Since(startTime).Microseconds()
		timeTakenPerBlockInMS = append(timeTakenPerBlockInMS, elapsedTimeInMicroSeconds)
		// reset clock for next iteration
		startTime = time.Now() 
		
	}

	// totalRuntime holds the time in micro seconds running this function took in total
	var totalRuntime int64
	for _, v := range timeTakenPerBlockInMS {
		totalRuntime += v
	}

	var totalRuntimeInSeconds int64
	if totalRuntime > 0 { // never divide by zero, i definitely won't repeat microsofts mistake: https://github.com/JHRobotics/patcher9x?tab=readme-ov-file#patch-for-windows-9598-to-fix-cpu-speed-limit-bug
		totalRuntimeInSeconds = int64(float64(totalRuntime)/float64(1000000))
	}

	logger.L.Printf("Successfully created %v block large light node blockchain in %v microseconds ≈ %v seconds.\n", blockAmount, totalRuntime, totalRuntimeInSeconds)

	// print size of the blockchain file
	dbFileInfo, err := os.Stat(databasePath)
	if err != nil {
		logger.L.Panicf("Failed to get stats of blockchain file: %v\n", err)
	}
	logger.L.Printf("Size of blockchain is %v bytes", dbFileInfo.Size())

	// now ensure validity of entire blockchain and measure how long these checks take
	err = BlockchainVerifyValidity(false, true)
	if err != nil {
		logger.L.Panicf("Failed to verify blockchain validity: %v\n", err)
	}

	logger.L.Printf("Successfully verified blockchain validity.\n")


	return nil

}

// getNextBlock is a helper testing function CreatePseudoBlockchain which takes a full block and returns the next block.Block.
// Parameters such as BlockID are increased by 1, other parameters depend on the current state of the blockchain at this time.
// Calling this function does not affect your state, you are supposed to call StateDbProcessAndUpdateBlock() on the block this function returns
// to affect the statedb.
func getNextBlock(prevBlock block.Block, RApw string, transactionAmountPerBlock int) block.Block {

	// determine values of next block
	blockID := prevBlock.BlockHeader.BlockID + 1					// BlockID
	blockTime := prevBlock.BlockHeader.BlockTime + 1				// BlockTime (+1 guarantees that > is satisfied when quickly simulating the creation of many blocks. since the genesis block has an old blocktime hardcoded each block created by simulation would also appear to be old)
	
	// reconstruct blockHash
	prevblockHash := HeaderToHash(prevBlock.BlockHeader)			// PrevBlockHash
	// put pseudo stateMerkleRoot (it will be determined later)		// StateMerkleRoot
	stateMerkleRoot := hash.NewHash("nil")

	simtaskHash := prevBlock.BlockHeader.ProblemID					// ProblemDefHash (re-used from previous block)
	blockWinner := winner.NewBlockWinner("12D3Koo" + GenerateRandomStringOfLength(45), hash.NewHash("nil"), winner.GetTokenRewardForBlock(blockID)) // BlockWinner (random winner, bogus solution hash, correct token amount rewarded)

	// create pseudorandom, valid transactions (does not affect the state yet, will not be done in this function)
	transactionList := make([]transaction.Transaction, 0)
	for i := 0; i < transactionAmountPerBlock; i++ {
		t := CreateRandomTransaction(RApw)
		transactionList = append(transactionList, t)
	}

	simTask := prevBlock.SimulationTask 							// SimulationTask is re-used from previous block

	return block.NewBlock(
		blockID,
		blockTime,
		prevblockHash,
		stateMerkleRoot,
		simtaskHash,
		blockWinner,
		transactionList,
		simTask,
	)
}

// getNextHeader is a helper testing function CreatePseudoBlockchain which takes a block.Header and returns the next block.Header.
// Parameters such as BlockID are increased by 1, other parameters might or might not be changed for now.
func getNextHeader(prevHeader block.Header) block.Header {

	// determine values of next block
	blockID := prevHeader.BlockID + 1					// BlockID
	blockTime := prevHeader.BlockTime + 1	// BlockTime

	// reconstruct headerHash
	prevHeaderHash := HeaderToHash(prevHeader)
	// put pseudo stateMerkleRoot (it's a bogus value because the light node can't possibly calculate this)		// StateMerkleRoot
	stateMerkleRoot := hash.NewHash("nil")
	// put pseudo transMerkleRoot (it's a bogus value because the light node can't possibly calculate this)		// TransactionsMerkleRoot
	transMerkleRoot := hash.NewHash("nil")

	simtaskHash := prevHeader.ProblemID					// ProblemDefHash (this is re-used from previous block, fine for testing)
	blockWinner := winner.NewBlockWinner("12D3Koo" + GenerateRandomStringOfLength(45), hash.NewHash("nil"), winner.GetTokenRewardForBlock(blockID))	// BlockWinner (random winner, bogus solution hash, correct token amount rewarded)

	return block.Header {
		BlockID: 				blockID,
		BlockTime: 				blockTime,
		PrevBlockHash:			prevHeaderHash,
		TransactionsMerkleRoot:	transMerkleRoot,
		StateMerkleRoot: 		stateMerkleRoot,
		ProblemID:				simtaskHash,
		BlockWinner:			blockWinner,
	}

}

// CreateRandomTransaction is used to generate a (pseudo-)random, valid transaction.
// This function assumes that the location of the private key used is the rootdir.
func CreateRandomTransaction(RApw string) transaction.Transaction {
		// use RA as 'From' field in transaction, as RA is the only node with pre-allocated tokens (only during testing)
		//		read RA key from file
		raPrivKeyLocation := filepath.Join(".", "raprivkey.key")
		ed25519Priv := ReadKeyFromFileAndDecrypt(RApw, raPrivKeyLocation)
		//		derive libp2p priv
		priv, err := crypto.UnmarshalEd25519PrivateKey(ed25519Priv)
	    if err != nil {
	        logger.L.Panicf("Failed to convert ed25519 priv to libp2p priv: %v\n", err)
	    }
	    //		derive pub
	    pub := priv.GetPublic()

		// derive RA nodeID from pub
		nodeIDObject, err := peer.IDFromPublicKey(pub)
		// get string from ID object (will result in the nodeID you see when connecting to a node e.g. "12D3...")
		tFrom := nodeIDObject.String()

		// 'To' field of transaction gets "12D3Koo" + 45 random characters
		tTo := "12D3Koo" + GenerateRandomStringOfLength(45)
		
		// 'Value' field is float64 in [0.001,0.01)
		tVal := float64(float64(GenerateRandomIntBetween(100,999)) / 100000.0)
	
		// 'Reference' field is just the string "testreference"
		tRef := "abc"

		// 'TxTime' is random int in [1704067200, 1704585600] as uint64 (within first week of 2024)
		tTxTime := uint64(GenerateRandomIntBetween(1704067200,1704585600))

		// 'Nonce' must be higher by one each time, if it is always the RA that sends the transaction
		RAnonce += 1
	
		// create transaction object (with minimum fee)
		generatedTx := transaction.NewTransaction(tFrom, tTxTime, tTo, tVal, tRef, RAnonce, winner.MinTransactionAmount, priv)
	
		return generatedTx

}

// GenerateRandomStringOfLength generates random bytes, encodes them to base58 (like libp2p peerIDs) and return the first stringlen characters of that string (stringlen is the int input parameter of the function).
func GenerateRandomStringOfLength(stringlen int) string {
	someBytes := make([]byte, stringlen)
	_, err := cryptoRand.Read(someBytes) // someBytes are now some pseudo-random bytes
	if err != nil {
		logger.L.Panicf("GenerateRandomStringOfLength - Failed to generate pseudo-random bytes: %v", err)
	}

	randString := base58.Encode(someBytes)
	return randString[:stringlen]
}

// GenerateRandomIntBetween takes min and max and returns an int in [min,max]
func GenerateRandomIntBetween(min int, max int) int {
	return rand.Intn(max-min+1) + min
}
