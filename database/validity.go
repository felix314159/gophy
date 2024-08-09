package database

// valdity.go contains validity checks for various data types (e.g. is a given block valid, is a given transaction valid, is the broadcast block problem valid, etc.)

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vmihailenco/msgpack/v5"

	"github.com/felix314159/gophy/block"
	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/block/merkletree"
	"github.com/felix314159/gophy/block/simpar"
	"github.com/felix314159/gophy/block/transaction"
	"github.com/felix314159/gophy/block/winner"
	"github.com/felix314159/gophy/logger"
)

// SimtaskValidityCheck takes a simulationTask (problem def) and the latest block header, and then determines whether this simTask is valid or not and returns err (only nil return means valid).
// Rules:
//	 	Header:
//			1. s.CreationTime > b.Timestamp
//			2. s.ExpirationTime > s.CreationTime
//			3. s.BlockId == b.BlockID + 1
//   	Subproblems:
//			4. s.Seed converted to hex string must match first 6 chars of blockHash of b
//			5. s.Seed must be <= 900000000 (pythia max seed)
//			6. s.Particles must be equal to 0 (pions), 1 (eplus) or 2 (proton)
//			7. Hash of subproblem is valid (re-create the SimulationParameters object and compare with hash that will automatically be calculated)
func SimtaskValidityCheck(s simpar.SimulationTask, h block.Header) error {
	// preparation: re-calculate blockHash
	//		serialize header
	bHeaderSer, err := msgpack.Marshal(&h)
	if err != nil {
		logger.L.Panic(err)
	}
	//		hash it
	blkHash := hash.NewHash(string(bHeaderSer)).GetString()

	// ---- Validity check of header ----
	// rule 1
	if s.SimHeader.CreationTime < h.BlockTime { // use < instead of <= because msg distribution can be so fast that everything happens within the same second
		return fmt.Errorf("SimtaskValidityCheck - Simpar CreationTime is %v but this is not larger than the latest block time %v which is not allowed! \n", s.SimHeader.CreationTime, h.BlockTime)
	}

	// rule 2
	if s.SimHeader.ExpirationTime < s.SimHeader.CreationTime {
		return fmt.Errorf("SimtaskValidityCheck - Simpar ExpirationTime is %v and CreationTime is %v. This is not allowed, the ExpirationTime must be later/larger than the CreationTime!\n", s.SimHeader.ExpirationTime, s.SimHeader.CreationTime)
	}

	// rule 3
	if s.SimHeader.BlockID != h.BlockID + 1 {
		return fmt.Errorf("SimtaskValidityCheck - Simpar block ID is %v but the latest block ID is %v. This is not allowed, the simpar block ID must be exactly 1 larger than the latest block ID!\n", s.SimHeader.BlockID, h.BlockID)
	}

	// ---- Validity check of each subproblem ----
	for subProblemIndex, subProblem := range s.SimPar {
		// rule 4
		seedString := uint32ToHexString(subProblem.Seed)
		if !(strings.HasPrefix(blkHash, seedString)) {
			return fmt.Errorf("SimtaskValidityCheck - Hash of latest block is %v but got seed %v. The seed must be a prefix of the block hash! \n", blkHash, seedString)
		}

		// rule 5
		if subProblem.Seed > 900000000 {
			return fmt.Errorf("SimtaskValidityCheck - Seed is %v but this is larger than 900000000 which is not allowed! \n", subProblem.Seed)
		}

		// rule 6
		if subProblem.Particles > 2 {
			return fmt.Errorf("SimtaskValidityCheck - Simpar uses Particle ID %v but maximum allowed value currently is 2!\n", subProblem.Particles)
		}

		// rule 7
		// 		serialize subproblem
		subProblemSer, err := msgpack.Marshal(&subProblem)
		if err != nil {
			logger.L.Panic(err)
		}
		//		get hash of subproblem
		subProblemHash := hash.NewHash(string(subProblemSer))

		//		ensure hash of subproblem is equal to what is stored in the slice of problem hashes in header of the block problem
		if s.SimHeader.SubProblemHashes[subProblemIndex].GetString() != subProblemHash.GetString() {
			return fmt.Errorf("SimtaskValidityCheck - Subproblem with index %v should have hash %v but it has hash %v!\n", subProblemIndex, s.SimHeader.SubProblemHashes[subProblemIndex].GetString(), subProblemHash.GetString())
		}

	}

	return nil
}


// BlockchainVerifyValidity goes through every block in the local blockchain and ensures that the chaindb data is valid.
// It then tries to locally build the statedb from the given chaindb data, this means calling BlockchainVerifyValidity() does affect the state!
// Note: Blocks being 'valid' means that the chain of blocks abides to certain rules (block ID + 1, timestamp <=, ...) and that the statedb was affected according to the data found in chaindb blocks.
// Return an error that is only nil when the blockchain is valid, otherwise returns an error that describes why the blockchain is not valid.
func BlockchainVerifyValidity(fullNode bool, measurePerformance bool) error {
	// get list of all chaindb block hashes (starting with block id 0, 1, 2...)
	blockHashStringListAsc, err := GetAllBlockHashesAscending(fullNode)
	if len(blockHashStringListAsc) == 0 {
		return fmt.Errorf("BlockchainVerifyValidity - GetAllBlockHashesAscending returned an empty slice!")
	}
	if err != nil {
		return fmt.Errorf("BlockchainVerifyValidity - %v\n", err)
	}

	// keep track of previous block and header
	var prevBlock block.Block
	var prevBlockHeader block.Header

	// retrieve genesis block and do some basic checks
	prevBlockBytes, err := BlockGetBytesFromDb(blockHashStringListAsc[0]) // safe because length was already check to be > 0
	if err != nil {
		return fmt.Errorf("BlockchainVerifyValidity - %v\n", err)
	}
	if fullNode {
		prevBlock = FullBlockBytesToBlock(prevBlockBytes)
		prevBlockHeader = prevBlock.BlockHeader
	} else {
		prevBlockHeader, _ = HeaderBytesToBlockLight(prevBlockBytes)
	}
	// get hash of genesis block
	retrievedGenHash := HeaderToHash(prevBlockHeader)

	// basic genesis check (not part of performance measurement as it is trivial)
	if (prevBlockHeader.BlockID != 0) || (retrievedGenHash.GetString() != genesisHash) {
		if prevBlockHeader.BlockID != 0 {
			return fmt.Errorf("BlockchainVerifyValidity - Genesis block is invalid: Block ID of Genesis should have been 0 but currently is: %v\n", prevBlockHeader.BlockID)
		} else {
			return fmt.Errorf("BlockchainVerifyValidity - Genesis block is invalid: Hash of Genesis block was expected to be %v but currently is %v\n", genesisHash, retrievedGenHash.GetString())
		}
	}

	// measure execution time of this function
	var timeTakenPerBlockInMS []int64 // measure how long checking each block for validity takes in micro seconds
	startTime := time.Now() // will be updated with each iteration

	// now check validity of all other blocks
	for _, blockKey := range blockHashStringListAsc[1:] { // genesis can be skipped at it was already checked
		logger.L.Printf("Will now check validity of block with hash %v\n", blockKey)

		// get block from db
		newBlockBytes, err := BlockGetBytesFromDb(blockKey)
		if err != nil {
			return fmt.Errorf("BlockchainVerifyValidity - %v\n", err)
		}
		// unmarshal
		var newBlock block.Block
		if fullNode {
			newBlock = FullBlockBytesToBlock(newBlockBytes)
		}
		
		// checks validity of new block (can new block be valid continuation of previous block)
		err = BlockVerifyValidity(fullNode, prevBlockBytes, newBlockBytes, fullNode)
		if err != nil {
			// display block that makes problems
			if fullNode {
				PrintBlock(newBlock)
			}
			
			return fmt.Errorf("BlockchainVerifyValidity - %v\n", err)
		}

		// ---- block is valid, full nodes will now affect their state ----

		if fullNode {
			newBlockAfterAffected, err := StateDbProcessAndUpdateBlock(newBlock)
			if err != nil {
				return fmt.Errorf("BlockchainVerifyValidity - %v\n", err)
			}
			if newBlockAfterAffected.BlockHeader.TransactionsMerkleRoot.GetString() != newBlock.BlockHeader.TransactionsMerkleRoot.GetString() {
				return fmt.Errorf("BlockchainVerifyValidity - Block with ID %v failed: TransactionsMerkleRoot after affecting statedb is %v which is different from chaindb value %v..", newBlock.BlockHeader.BlockID, newBlockAfterAffected.BlockHeader.TransactionsMerkleRoot.GetString(), newBlock.BlockHeader.TransactionsMerkleRoot.GetString())
			}
			if newBlockAfterAffected.BlockHeader.StateMerkleRoot.GetString() != newBlock.BlockHeader.StateMerkleRoot.GetString() {
				return fmt.Errorf("BlockchainVerifyValidity - Block with ID %v failed: StateMerkleRoot after affecting statedb is %v which is different from chaindb value %v..", newBlock.BlockHeader.BlockID, newBlockAfterAffected.BlockHeader.StateMerkleRoot.GetString(), newBlock.BlockHeader.StateMerkleRoot.GetString())
			}
			// StateDbProcessAndUpdateBlock only potentially affects these two values so there is nothing else to check
		}
		
		// update prevBlockBytes
		prevBlockBytes = newBlockBytes

		// remember how long the checks for this block took in microseconds
		elapsedTimeInMicroSeconds := time.Since(startTime).Microseconds()
		timeTakenPerBlockInMS = append(timeTakenPerBlockInMS, elapsedTimeInMicroSeconds)
		// reset clock for next iteration
		startTime = time.Now() 


	}

	// all tests passed. blockchain is valid (both chaindb and statedb)

	// if performance test mode active, print total time running this function took
	if measurePerformance {
		var totalRuntime int64
		for _, v := range timeTakenPerBlockInMS {
			totalRuntime += v
		}

		var totalRuntimeInSeconds int64
		if totalRuntime > 0 { // never divide by zero, i definitely won't repeat microsofts mistake: https://github.com/JHRobotics/patcher9x?tab=readme-ov-file#patch-for-windows-9598-to-fix-cpu-speed-limit-bug
			totalRuntimeInSeconds = int64(float64(totalRuntime)/float64(1000000))
		}
		
		logger.L.Printf("Checking validity of blockchain in total took: %v microseconds ≈ %v seconds.\n", totalRuntime, totalRuntimeInSeconds)
	}


	return nil
}

// BlockVerifyValidity takes a bool (iAmFullNode), and two blocks (might just be header if you are light node) in serialized form (prev and new) and another bool (newBlockIsFull is used so that light nodes can receive new full blocks broadcast by the RA and extract their header) and then checks whether the latter block or header can be a continuation of the former without affecting the state.
// Light nodes do not have access to the state, that's why they only do the checks that they can do.
// Full nodes safely determine the validity of everything that affects that state without actually affecting the state.
// Returns an error that is only nil if the latter block is a valid continuation of the former.
// The following validity checks are done:
//		1. PrevBlockHash of new is equal to blockHash of prev
// 		2. Block ID increased by 1
//		3. Timestamp larger
//		3.5 Token reward was calculated correctly according to formula
//		4. Transaction Signatures are valid [full node only]
//		5. Transaction list merkle tree roothash was calculated correctly [full node only]
// 		6. All transactions are valid check [full node only]
// 		7. State merkle tree after processing block matches blockheader value
// If BPH is populated:
//		8. Did RA choose correct problem (unique problem hash matching)
func BlockVerifyValidity(fullNode bool, prev []byte, new []byte, newBlockIsFull bool) error {
	// Preparations: Depending on fullNode true/false, deserialize prev into block.Block or block.Header. Then deserialize new into block.Block (RA always broadcasts full blocks).
	var prevBlock block.Block
	var prevBlockHeader block.Header
	if fullNode {
		prevBlock = FullBlockBytesToBlock(prev)
		prevBlockHeader = prevBlock.BlockHeader
	} else {
		prevBlockHeader, _ = HeaderBytesToBlockLight(prev)
	}

	var newBlock block.Block
	var newBlockHeader block.Header
	if fullNode {
		newBlock = FullBlockBytesToBlock(new)
		newBlockHeader = newBlock.BlockHeader
	} else {
		if newBlockIsFull { // case: light node received new full block that the RA just broadcast
			newBlock = FullBlockBytesToBlock(new)
			newBlockHeader = newBlock.BlockHeader
		} else {
			newBlockHeader, _ = HeaderBytesToBlockLight(new) // case: during inital sync light node has requested header from some node
		}
		
	}

	// ---- Validity checks that depend on the previous block ----

	// 1. PrevBlockHash of new is equal to blockHash of prev
	//		get hash of prev
	//				serialize header
	prevBlockHeaderSer, err := msgpack.Marshal(&prevBlockHeader)
	if err != nil {
		logger.L.Panic(err)
	}
	//				hash it
	prevHash := hash.NewHash(string(prevBlockHeaderSer)).GetString()

	if prevHash != newBlockHeader.PrevBlockHash.GetString() {
		return fmt.Errorf("BlockVerifyValidity - Validity check failed: PrevBlockHash of new block does not have the same blockhash as the previous block. Expected hash %v but got hash %v", prevHash, newBlock.BlockHeader.PrevBlockHash.GetString())
	}
	
	// 2. Block ID increased by 1
	if prevBlockHeader.BlockID + 1 != newBlockHeader.BlockID {
		return fmt.Errorf("BlockVerifyValidity - Validity check failed: BlockID not increased by exactly 1 compared to previous block. Expected new block to have block ID %v but got block ID %v", prevBlockHeader.BlockID + 1, newBlock.BlockHeader.BlockID)
	}

	// 3. Timestamp larger
	if newBlockHeader.BlockTime <= prevBlockHeader.BlockTime {
		return fmt.Errorf("BlockVerifyValidity - Validity check failed: Timestamp of new block is not larger than previous block. Expected timestamp larger than %v but got timestamp %v", prevBlockHeader.BlockTime, newBlock.BlockHeader.BlockTime)
	}
	
	// 3.5 Token amount that is awarded to block winner has been calculated correctly
	correctReward := winner.GetTokenRewardForBlock(newBlockHeader.BlockID)
	if newBlock.BlockHeader.BlockWinner.TokenReward != correctReward {
		return fmt.Errorf("BlockVerifyValidity - Validity check failed: Wrong reward for block winner! Expected award to be %v tokens but got %v tokens.", correctReward, newBlock.BlockHeader.BlockWinner.TokenReward)
	}

	// ---- Validity checks that do not depend on the previous block (and which just so happen to be only possible done by full nodes) ----

	// full nodes will have to perform more tests than light nodes:
	if fullNode {
		// 4. Transaction Signatures are valid
		for _, t := range newBlock.Transactions {
			// derive corresponding PubKey of transaction Sender nodeID
			pub, err := NodeIDStringToPubKey(t.From)
			if err != nil {
				return fmt.Errorf("BlockVerifyValidity - Validity check failed: Failed to derive pubKey from nodeIDstring. The nodeID %v leads to error: %v\n", t.From, err)
			}
			// check validity of signature (the txHash was signed - which is the hash of the transaction content)
			sigIsValid, err := pub.Verify(t.TxHash.Bytes, t.Sig)	// (hashThatWasSignedAsBytes, ResultingSignature)
			if err != nil {
				return fmt.Errorf("BlockVerifyValidity - Validity check failed: Failed to verify signature of txHash %v due to error: %v \n", t.TxHash, err)
			}
			// check if signature is valid or not
			if !sigIsValid {
				return fmt.Errorf("BlockVerifyValidity - Validity check failed: WARNING: The provided signature is NOT authentic. Transaction with TxHash %v  has been tempered with! \n", t.TxHash)
			}
			
		}

		// 5. Transaction list merkle tree roothash was calculated correctly
		merkleTreeRootHashTrans := transaction.TransactionListToMerkleTreeRootHash(newBlock.Transactions)	// returns hash.Hash
		if merkleTreeRootHashTrans.GetString() != newBlock.BlockHeader.TransactionsMerkleRoot.GetString() {
			return fmt.Errorf("BlockVerifyValidity - Validity check failed: Reconstructing the Transaction Merkle Tree Root Hash resulted in a different value than was given in Header! Got %v but expected %v", merkleTreeRootHashTrans.GetString(), newBlock.BlockHeader.TransactionsMerkleRoot.GetString())
		}

		// 6. All transactions are valid check. first simulates transactions in memory without affecting db so that db is not affected by invalid block

		//	6.1 in order to later check whether the in-memory state is equal to the db state, first retrieve ALL statedb accounts as objects in memory (even those that are not affected by transactions as they are needed to later verify the stateMerkleRoot without affecting the db)
		//  6.2 in the order of the given transactions, check if the current transaction would be valid and have it affect the memory objects
		//  6.3 if any transaction does not pass the test, then return invalid (db was never affected)
		//  6.4 construct stateMerkleTree and check its root against the value provided in the block we are checking
		//  6.5 have winnerAddress affect the in-memory balance
		//  6.6 build state merkle tree from in memory objects and compare with value in block to see if it is valid

		// 6.1 Read database into memory so that after all the tests in-memory can be compared to actual state
		// 		6.1.0 create nodeid->StateValueStruct mapping
		stateAccountList := make(map[string]StateValueStruct) // StateValueStruct has two fields: 'Balance' (float64) and 'Nonce' (int)
		// 		6.1.1 get all statedb keys (node IDs) that currently exist (before this new block would have been processed)
		nodeIDList := BoltGetDbKeys("statedb")
		// 		6.1.2 	iterate over ids and retrieve current Account state, then create StateAccountWithID instance and add it to map
		for _, n := range nodeIDList {
			nAccSer, err := ReadFromBucket(n, "statedb")
			if err != nil {
				return fmt.Errorf("BlockVerifyValidity - Validity check failed: Failed to retrieve statedb data of account %v due to error: %v \n", n, err)
			}

			// 			deserialize
			nAcc, err := StateDbBytesToStruct(nAccSer)
			if err != nil {
				return fmt.Errorf("BlockVerifyValidity - Validity check failed: %v \n", err)
			}

			// 			add struct to slice
			stateAccountList[n] = nAcc
		}

		// now go through each transaction one-by-one and have it affect the in-memory state if it is valid. After entire transaction list has been processed, compare resulting in-memory statemerkleroothash with the claimed one from the received block

		logger.L.Printf("Data in new block with ID %v seem to be valid. Will now check transaction validity one-by-one..\n", newBlockHeader.BlockID)


		// 6.2 go through transaction list and for each transaction check if it would be valid. remember state after transaction by affecting struct slice elements
		for _, curTrans := range newBlock.Transactions {
			// List of checks (2. would have been verifying sig validity but this has already been done earlier):
			//		0. TxHash has been calculated correctly
			//		1. From and To addresses start with "12D3Koo" AND From and To addresses are not identical (can't send tokens to yourself)
			//		3. TxTime is later than 2024-01-01 and earlier than current time
			//		4. From Balance >= Amount + Fee sent; also ensure that amount >= min transferrable token amount
			//		5. Nonce of 'From' is increased by 1 compared to current statedb value Nonce

			// 0. TxHash
			// 		create inital transaction object
			transRecon := transaction.Transaction {
				From:   curTrans.From,
				TxTime: curTrans.TxTime,
				To:     curTrans.To,
				Value:  curTrans.Value,
				Reference: curTrans.Reference,
				Nonce:  curTrans.Nonce,
				Fee: curTrans.Fee,
			}
			// 		serialize object using msgpack and cast it from []byte to string
			transactionSerialized, err := msgpack.Marshal(&transRecon)
			if err != nil {
				return fmt.Errorf("BlockVerifyValidity - Transaction invalid because serialization of it fails: %v \n", err)
			}
			// 		hash serialized transaction and return it as string
			transactionHash := hash.NewHash(string(transactionSerialized))
			//		ensure its the same hash
			if transactionHash.GetString() != curTrans.TxHash.GetString() { 
				return fmt.Errorf("BlockVerifyValidity - Transaction invalid because TxHash is wrong: Expected %v but transaction contained TxHash %v \n", transactionHash.GetString(), curTrans.TxHash.GetString())
			}


			// 1. From and To addresses valid and not identical
			var FromCouldBeValid bool = strings.HasPrefix(strings.ToLower(curTrans.From), "12d3koo")
			var ToCouldBeValid bool   = strings.HasPrefix(strings.ToLower(curTrans.To), "12d3koo")
			if (!FromCouldBeValid) || (!ToCouldBeValid) || (curTrans.From == curTrans.To){
				return fmt.Errorf("BlockVerifyValidity - Transaction invalid because 'From' or 'To' either don't start with 12D3Koo or they are identical\nFrom: %v\nTo: %v\n", curTrans.From, curTrans.To)
			}


			// 3. transaction time newer than 2024 start and older than current time
			gmt20240101Epoch := uint64(1704067200)
			currentTime := uint64(time.Now().UTC().Unix())
			if curTrans.TxTime < gmt20240101Epoch {
				return fmt.Errorf("BlockVerifyValidity - Transaction invalid because timestamp is before 2024-01-01 (1704067200): %v \n", curTrans.TxTime)
			}
			if curTrans.TxTime > currentTime {
				return fmt.Errorf("BlockVerifyValidity - Transaction invalid because timestamp is in the future of UTC time: %v \n", curTrans.TxTime)
			}


			// 4. From Balance >= Amount + Fee sent AND amount >= min transferrable token amount
			//		4.1 first ensure that From is a valid key in our map
			if _, ok := stateAccountList[curTrans.From]; !ok {
				return fmt.Errorf("BlockVerifyValidity - Transaction invalid because transaction Sender nodeID %v does not exist in statedb and therefore can't have any balance!", curTrans.From)
			}

			if stateAccountList[curTrans.From].Balance < curTrans.Value + curTrans.Fee {
				return fmt.Errorf("BlockVerifyValidity - Transaction invalid because amount of tokens sent + fee is larger than balance of From node!\nFrom Balance: %v\nTried to send amount: %v\nFee: %v\n", stateAccountList[curTrans.From].Balance, curTrans.Value, curTrans.Fee)
			}

			// also ensure fee and value are at least as large as winner.MinTransactionAmount
			if curTrans.Fee < winner.MinTransactionAmount {
				return fmt.Errorf("BlockVerifyValidity - Transaction invalid because minimum required fee is %v but got amount %v\n", winner.MinTransactionAmount, curTrans.Fee)
			}

			if curTrans.Value < winner.MinTransactionAmount {
				return fmt.Errorf("BlockVerifyValidity - Transaction invalid because min amount of tokens you must send is %v but %v was given.\n", winner.MinTransactionAmount, curTrans.Value)
			}

			// 5. Nonce of 'From' is increased by 1 compared to current statedb value Nonce
			txSenderWallet, ok := stateAccountList[curTrans.From]
			if ok {
			    if txSenderWallet.Nonce + 1 != curTrans.Nonce {
			    	return fmt.Errorf("BlockVerifyValidity - Transaction with txhash %v invalid because Nonce of 'From' does not have correct value. Expected %v but transaction contained %v \n", curTrans.TxHash.GetString(), txSenderWallet.Nonce + 1, curTrans.Nonce)
			    }
			} else {
				logger.L.Panicf("BlockVerifyValidity - Sender %v of tx %v does not already exist in the statedb, this should be impossible because it must have been rewarded or sent tokens already!", curTrans.From, transactionHash.GetString(),)
			}

			// ---- Start affecting in-memory state (Note: Go does not allow modyfing struct fields from a map (because you would be modifying a copy not the original), so you have to calculate the new fields value and then write back the entire struct instance. Alternatively you could adjust the map to hold pointer values but then syntax becomes ugly to read and write.) ----

			// SENDER (From):

		    // 		calculate new sender NONCE
		    txSenderWallet.Nonce += 1
		    
		    // 		calculate new sender BALANCE (i need to consider cases where maybe transaction 2 is valid on its own but not when it comes after transaction 1)
			txSenderWallet.Balance -= curTrans.Value + curTrans.Fee
		    
		    // 		affect in-memory wallet of sender
		    stateAccountList[curTrans.From] = txSenderWallet



		    // RECEIVER (To):

		    txReceiverWallet, ok := stateAccountList[curTrans.To]
		    if ok { // ok just means: does the receiver already exist in the state? in contrast to the sender, the receiver is allowed to not already exist..
		    	// 		calculate new receiver BALANCE
			    txReceiverWallet.Balance += curTrans.Value
			    // 		affect in-memory wallet of receiver
			    stateAccountList[curTrans.To] = txReceiverWallet
			} else {
				// 'To' does not yet exist in statedb so create new struct and add it to map
				newTxReceiverWallet := StateValueStruct {
					Balance:	curTrans.Value,
					Nonce: 		0,
				}
				// 		create in-memory wallet of receiver
				stateAccountList[curTrans.To] = newTxReceiverWallet
			}

		}

		// Now all transactions of this block have been checked for validity and have affected the in-memory state
		// Before anything is actually affecting the actual db state, first perform the in-memory block winner award procedure.
		
		// 		award block winner in memory (it might or might not already exist in map, same as 'To' before)
		winnerNodeID := newBlock.BlockHeader.BlockWinner.WinnerAddress
		winnerWallet, ok := stateAccountList[winnerNodeID]
		if ok {
			// case: winner already existed in state before
			//		increase its balance
			winnerWallet.Balance += correctReward
			//		affect in-memory wallet of winner
			stateAccountList[winnerNodeID] = winnerWallet
		} else {
			// case: winner does not exist in map yet
			winnerWalletNew := StateValueStruct {
				Balance:	correctReward,
				Nonce: 		0,
			}
			//		create in-memory wallet of winner
			stateAccountList[winnerNodeID] = winnerWalletNew
		}

		// ----

		// Now (before any actual db is affected) re-calculate the would-be state merkle tree root hash you would get with this new in-memory state to verify if the provided value in the block is correct

		// 		first get list of now existing nodeIDs that is ascending (map isn't sorted to now put that maps keys into a slice so that there is a guarantee about the sorting)
		var nodeIDsAscending []string
		for mKey := range stateAccountList { // ignores returned values, it basically is "mKey, _ := "
			nodeIDsAscending = append(nodeIDsAscending, mKey)
		}
		SortStringSliceAscending(nodeIDsAscending)

		// 		then recreate slice of nodeID hashes in a valid format for the state merkle tree (a key in the state is not just the nodeID, instead it is keccak256(<nodeID>+<serialized wallet of that nodeID>))
		var nodeIDListAfterTrans []hash.Hash
		for _, mapKey := range nodeIDsAscending {
			// get current value in map if it exists (ok)
			nStruct, ok := stateAccountList[mapKey]
			if ok {
				// serialize its value
				nStructSer, err := msgpack.Marshal(&nStruct)
				if err != nil {
					logger.L.Panic(err)
				}
				// determine its hash in the state merkle tree <nodeID>+<serialized value>
				h := hash.NewHash(mapKey + string(nStructSer))

				// then add hash to the slice (this hash is the actual key of the statedb)
				nodeIDListAfterTrans = append(nodeIDListAfterTrans, h)

				// debug
				//logger.L.Printf("Added sth derived from this key to StatenodeIDlistAfterTransctionProcessing: %v", mapKey)
				//logger.L.Printf("BTW: this is that node's wallet after processing all transactions:\nBalance: %v\nNonce: %v\n", nStruct.Balance, nStruct.Nonce)

			} else {
				logger.L.Panicf("In stateAccount map the key %v does not exist. This should be impossible!", mapKey)
			}  	
    	}

    	// debug
    	// logger.L.Printf("Will now pass the following values to the State MerkleTree Constructor:")
    	// for _, v := range nodeIDListAfterTrans {
    	// 	logger.L.Printf("%v\n", v.GetString())
    	// }
    	
		// 	7. Finally I can verify the value of State merkle tree that was provided in the new block
		stateMerkleTree := merkletree.NewMerkleTree(nodeIDListAfterTrans)
		if stateMerkleTree.GetRootHash().GetString() != newBlock.BlockHeader.StateMerkleRoot.GetString() {
			// debug: print statedb 
			logger.L.Printf("BlockVerifyValidity - There is an issue with the statedb, here is a full dump of it:")
			PrintStateDB()

			return fmt.Errorf("BlockVerifyValidity - Block with ID %v is invalid because its stateMerkleTreeRootHash is invalid! Expected %v but block has %v \n", newBlock.BlockHeader.BlockID, stateMerkleTree.GetRootHash().GetString(), newBlock.BlockHeader.StateMerkleRoot.GetString())
		}

		// all state-related validity checks have been completed, the new block still seems valid (but still has not affected the state of the actual db file)

	}

	// whether further checks happen depends on whether node has already completed its initial sync and populated the BPH or not (these checks also won't happen during pseudo.go simulation)
	// so for this reason, check whether the BPH is populated or not to determine this
	curProblemID := BlockProblemHelper.GetProblemID()
	if len(curProblemID.Bytes) > 0 {
		// ok your BPH is populated so you can perform additional checks
	
		//		8. SimulationTask chosen correctly by RA? As in, does the unique problem ID in my BPH agree with what is in block just received by RA?
		if curProblemID.GetString() != newBlock.SimulationTask.ProblemHash.GetString() {
			return fmt.Errorf("BlockVerifyValidity - Block invalid because its unique problem ID should be %v [according to my BPH] but the block includes one with ProblemHash %v\n", curProblemID.GetString(), newBlock.SimulationTask.ProblemHash.GetString())
		}

	}

	logger.L.Printf("BlockVerifyValidity - Block with ID %v has passed all validity checks.\n", newBlockHeader.BlockID)
	
	return nil
}

// TransactionListFilterOutInvalid takes a transaction list and filter out transactions that can not be performed.
// This function (in memory, without affecting statedb) goes from first to last transaction and checks each transaction for validity (in that context, so order matters).
// Returns a list of transaction that can be performed as-is (transactions from input list might have been removed which means these will continue to be in PendingTransactions).
func TransactionListFilterOutInvalid(tl []transaction.Transaction) []transaction.Transaction {
	//  1. read statedb to memory to have transactions temporarily affect state
	//  2. go through transaction list one-by-one and perform transactions in memory. if transaction is invalid, exclude it from the list and continue
	//  3. return cleansed transaction list

	// 1. populate map (key:nodeID, value:StateValueStruct)
	stateAccountList := make(map[string]StateValueStruct)
	// 		1.1 get all keys
	nodeIDList := BoltGetDbKeys("statedb")
	// 		1.2 iterate over ids and retrieve current Account state, then create StateAccountWithID instance and add it to map
	for _, n := range nodeIDList {
		nAccSer, err := ReadFromBucket(n, "statedb")
		if err != nil {
			logger.L.Panicf("Failed to retrieve statedb data of account %v due to error: %v \n", n, err)
		}

		// 			deserialize
		nAcc, err := StateDbBytesToStruct(nAccSer)
		if err != nil {
			logger.L.Panicf("BlockVerifyValidity - Validity check failed: %v \n", err)
		}

		// 			add struct to slice
		stateAccountList[n] = nAcc
	}

	// 2. go through transaction list and perform each transaction if it is valid and possible, remember valid transactions
	validTransactions := []transaction.Transaction{}
	for _, curTrans := range tl {
		// 2.1. Verify TxHash
		// 		create inital transaction object
		transRecon := transaction.Transaction {
			From:   curTrans.From,
			TxTime: curTrans.TxTime,
			To:     curTrans.To,
			Value:  curTrans.Value,
			Reference: curTrans.Reference,
			Nonce:  curTrans.Nonce,
			Fee: curTrans.Fee,
		}
		// 		serialize object using msgpack and cast it from []byte to string
		transactionSerialized, err := msgpack.Marshal(&transRecon)
		if err != nil {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction invalid because serialization of it fails: %v \n", err)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}
		// 		hash serialized transaction and return it as string
		transactionHash := hash.NewHash(string(transactionSerialized))
		//		ensure its the same hash
		if transactionHash.GetString() != curTrans.TxHash.GetString() { 
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction invalid because TxHash is wrong: Expected %v but transaction contained TxHash %v \n", transactionHash.GetString(), curTrans.TxHash.GetString())
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}

		// 2.2. 'From' and 'To' addresses are valid and not identical
		var FromCouldBeValid bool = strings.HasPrefix(strings.ToLower(curTrans.From), "12d3koo")
		var ToCouldBeValid bool   = strings.HasPrefix(strings.ToLower(curTrans.To), "12d3koo")
		if (!FromCouldBeValid) || (!ToCouldBeValid) || (curTrans.From == curTrans.To) {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction invalid because 'From' or 'To' either don't start with 12D3Koo or they are identical\nFrom: %v\nTo: %v\n", curTrans.From, curTrans.To)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}

		// 2.3. transaction time newer than 2024-01-01 and older than current time
		gmt20240101Epoch := uint64(1704067200)
		currentTime := uint64(time.Now().UTC().Unix())
		if curTrans.TxTime < gmt20240101Epoch {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction invalid because timestamp is before 2024-01-01 (1704067200): %v \n", curTrans.TxTime)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}
		if curTrans.TxTime > currentTime {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction invalid because timestamp is in the future of UTC time: %v \n", curTrans.TxTime)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}


		// 2.4. From Balance >= Amount + Fee sent AND amount >= min transferrable token amount AND fee >= min transferrable token amount
		//			first ensure that From is a valid key in the map
		if _, ok := stateAccountList[curTrans.From]; !ok {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction invalid because transaction Sender nodeID %v does not exist in statedb and therefore can't have any balance!", curTrans.From)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}

		if stateAccountList[curTrans.From].Balance < curTrans.Value + curTrans.Fee {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction invalid because amount of tokens sent + fee is larger than balance of From node!\nFrom Balance: %v\nTried to send amount: %v\nFee: %v\n", stateAccountList[curTrans.From].Balance, curTrans.Value, curTrans.Fee)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}

		if curTrans.Fee < winner.MinTransactionAmount {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction invalid because minimum required fee is %v but got amount %v\n", winner.MinTransactionAmount, curTrans.Fee)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}

		if curTrans.Value < winner.MinTransactionAmount {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction invalid because min amount of tokens you must send is %v but %v was given.\n", winner.MinTransactionAmount, curTrans.Value)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}

		// 2.5 signature is valid
		// 		get pubkey of transaction sender
		txSenderPubKey, err := NodeIDStringToPubKey(curTrans.From)
		if err != nil {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction with TxHash %v is invalid because I failed to convert tx sender node id string to pubkey: %v \n", curTrans.TxHash.GetString(), err)
			continue
		}

		sigisValid, err := txSenderPubKey.Verify(curTrans.TxHash.Bytes, curTrans.Sig)
		if err != nil {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction with TxHash %v is invalid because I failed to call the sig verify function!", curTrans.TxHash.GetString())
			continue
		}
		if !sigisValid {
			logger.L.Printf("TransactionListFilterOutInvalid - Transaction with TxHash %v is invalid because the signature is invalid!", curTrans.TxHash.GetString())
			continue
		}

		// ---- End of validity checks, try to perform transaction in memory ----

		// AFFECT IN MEMORY BALANCE OF SENDER
		if nStruct, ok := stateAccountList[curTrans.From]; ok {
		    nStruct.Balance -= curTrans.Value + curTrans.Fee
		    stateAccountList[curTrans.From] = nStruct
		} else {
			logger.L.Printf("Failed to retrieve node %v from the map", curTrans.From)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}
		// AFFECT IN MEMORY BALANCE OF RECEIVER
		if n2Struct, ok := stateAccountList[curTrans.To]; ok {
		    n2Struct.Balance += curTrans.Value
		    stateAccountList[curTrans.To] = n2Struct
		} else {
			// 'To' does not yet exist in statedb so create new struct and add it to map
			toStruct := StateValueStruct {
				Balance:	curTrans.Value,
				Nonce: 		0,
			}

			stateAccountList[curTrans.To] = toStruct
		}

		// UPDATE NONCE
		if nStruct, ok := stateAccountList[curTrans.From]; ok {
		    if nStruct.Nonce + 1 != curTrans.Nonce {
		    	logger.L.Printf("TransactionListFilterOutInvalid - Transaction with txhash %v invalid because Nonce of 'From' does not have correct value. Expected %v but transaction contained %v \n", curTrans.TxHash.GetString(), nStruct.Nonce + 1, curTrans.Nonce)
		    	logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
		    	continue
		    }

		    // AFFECT NONCE IN MEMORY
		    nStruct.Nonce += 1
		    stateAccountList[curTrans.From] = nStruct
		} else {
			logger.L.Printf("Failed to retrieve node %v from the map", curTrans.From)
			logger.L.Printf("Transaction with TxHash %v will NOT be included in the block right now because it is invalid", curTrans.TxHash.GetString())
			continue
		}

		// transaction seems to be fully valid, remember it
		validTransactions = append(validTransactions, curTrans)
		logger.L.Printf("Transaction with TxHash %v is valid", curTrans.TxHash.GetString())
	}

	return validTransactions
}

// StateDbTransactionIsAllowed takes a transaction and checks whether it is valid or not. If it is valid, it will determine the new statedb values of
// both account involved in the transaction and return the data in such a way that it will be easy to actually perform the transaction without having to perform unnecessary, additional statedb lookups.
// Here is a list of things that must be true so that the transaction can be allowed:
//		0. TxHash has been calculated correctly
//		1. From and To addresses start with "12D3Koo" AND From and To addresses are not identical (can't send tokens to yourself)
//		2. Transaction Signature is valid
//		3. From Balance >= Amount + Fee
// 		3.4 Fee >= winner.MinTransactionAmount
// 		3.5 ensure that amount sent is at least as large as winner.MinTransactionAmount
//		4. Nonce of 'From' is increased by 1 compared to current statedb value Nonce
//		5. TxTime is later than 2024-01-01 and earlier than current time (i guess this is a reasonable check, the time basically is just used to make TxHash unique so rough estimation if the timestamp makes sense is good enough)
// Note: This function does not actually affect the statedb, it does not perform the transaction.
// 
// This function returns the following values:
//		bool:	is this transaction valid?
//		error: 	this is either nil (valid transaction) or holds error message (why transaction is not valid)
//		string:	t.From (who sent the transaction)
//		[]byte:	Serialized StateValueStruct of t.From (state of the transaction sender wallet after transaction would have been sent)
//		string: t.To (who receives the transaction)
//		[]byte: Serialized StateValueStruct of t.To (state of the transaction receiver wallet after transaction would have been performed)
func StateDbTransactionIsAllowed(t transaction.Transaction) (bool, error, string, []byte, string, []byte) {
	// hold the later resulting updated 'To' and 'From' statedb value fields in serialized form (will be returned non-nil if transaction is valid)
	var fromNewValAfterTrans []byte
	var toNewValAfterTrans []byte


	// 0. re-calculate TxHash
	// 		create inital transaction object
	transaction := transaction.Transaction {
		From:   t.From,
		TxTime: t.TxTime,
		To:     t.To,
		Value:  t.Value,
		Reference: t.Reference,
		Nonce:  t.Nonce,
		Fee: t.Fee,
	}
	// 		serialize object using msgpack and cast it from []byte to string
	transactionSerialized, err := msgpack.Marshal(&transaction)
	if err != nil {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because serialization of it fails: %v \n", err), "", nil, "", nil
	}
	// 		hash serialized transaction and return it as string
	transactionHash := hash.NewHash(string(transactionSerialized))
	//		ensure its the same hash
	if transactionHash.GetString() != t.TxHash.GetString() { 
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because TxHash is wrong: Expected %v but transaction contained TxHash %v \n", transactionHash.GetString(), t.TxHash.GetString()), "", nil, "", nil
	}

	// 1. 'From' and 'To' start with "12D3Koo" and are not identical
	var FromCouldBeValid bool = strings.HasPrefix(strings.ToLower(t.From), "12d3koo")
	var ToCouldBeValid bool   = strings.HasPrefix(strings.ToLower(t.To), "12d3koo")
	if (!FromCouldBeValid) || (!ToCouldBeValid) || (t.From == t.To){
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because 'From' or 'To' either don't start with 12D3Koo or they are identical\nFrom: %v\nTo: %v\n", t.From, t.To), "", nil, "", nil
	}


	// 2. signature validity check
	// 		derive corresponding PubKey of transaction Sender nodeID
	pub, err := NodeIDStringToPubKey(t.From)
	if err != nil {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because converting nodeID to corresponding public key failed: %v \n", err), "", nil, "", nil
	}
	// 		check validity of signature (the txHash was signed - which is the hash of the transaction content)
	sigIsValid, err := pub.Verify(t.TxHash.Bytes, t.Sig)	// (hashThatWasSignedAsBytes, ResultingSignature)
	if err != nil {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because signature check function failed: %v \n", err), "", nil, "", nil
	}
	// 		check if signature is valid or not
	if !sigIsValid {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because its signature is invalid!"), "", nil, "", nil
	}


	// 3. ensure that From Balance >= Amount+Fee
	//		get From balance before the transaction
	fromAccSer, err := ReadFromBucket(t.From, "statedb")
	if err != nil {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because acessing From in statedb produced error: %v", err), "", nil, "", nil
	}
	if fromAccSer == nil {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because 'From' address: %v does not exist in statedb and therfore does not have the required funds to perform any transaction!", t.From), "", nil, "", nil
	}

	//		deserialize/unmarshal
	var fromAcc StateValueStruct
	err = msgpack.Unmarshal(fromAccSer, &fromAcc)
	if err != nil {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because failed to deserialize From value in statedb. This is a critical error!"), "", nil, "", nil
	}
	//		ensure that fromAcc.Balance >= t.Value + t.Fee
	if fromAcc.Balance < t.Value + t.Fee {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because amount of tokens sent + fee is larger than balance of From node!\nFrom Balance: %v\nTried to send amount: %v\nFee: %v\n", fromAcc.Balance, t.Value, t.Fee), "", nil, "", nil
	}

	// 3.4 ensure fee is at least as large as winner.MinTransactionAmount
	if t.Fee < winner.MinTransactionAmount {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because minimum required fee is %v but got amount %v\n", winner.MinTransactionAmount, t.Fee), "", nil, "", nil
	}

	// 3.5 ensure that amount sent is at least as large as winner.MinTransactionAmount
	if t.Value < winner.MinTransactionAmount {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because minimum required amount is %v but got amount %v\n", winner.MinTransactionAmount, t.Value), "", nil, "", nil
	}

	// 4. ensure nonce was increased by 1
	if (fromAcc.Nonce + 1) != t.Nonce {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because nonce is invalid: Expected nonce %v but got %v \n", fromAcc.Nonce + 1, t.Nonce), "", nil, "", nil
	}

	// 5. ensure timestamp is newer than 2024-01-01 and older than current timestamp (GMT == UTC)
	gmt20240101Epoch := uint64(1704067200) // this timestamp is in seconds
	currentTime := uint64(time.Now().UTC().Unix()) + uint64(5) // add 5 extra seconds otherwise code can be so fast / clock of sender inaccurate that send and receive happens in the same second  
	if t.TxTime < gmt20240101Epoch {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because timestamp is before 2024-01-01 (1704067200): %v \n", t.TxTime), "", nil, "", nil
	}
	if t.TxTime > currentTime {
		return false, fmt.Errorf("StateDbTransactionIsAllowed - Transaction invalid because timestamp is in the future of UTC time: %v \n", t.TxTime), "", nil, "", nil
	}


	// all checks passed, transaction is valid and can be performed
	// determine post transaction state of 'From' and 'To'

	// FROM:
	// 		determine amount of tokens 'From' has left after transaction (both Value and Fee get subtracted)
	tFromBalanceNew := fromAcc.Balance - t.Value - t.Fee
	//		new nonce is t.Nonce
	//		create statedb value struct instance
	tFromValNew := StateValueStruct {
		Balance: 	tFromBalanceNew,
		Nonce:		t.Nonce,
	}
	//		serialize it
	tFromValNewSer, err := msgpack.Marshal(&tFromValNew)
	if err != nil {
		logger.L.Panic(err)
	}
	//		store the value that would result from this transaction in the variable that will later be returned
	fromNewValAfterTrans = append(fromNewValAfterTrans, tFromValNewSer...)


	// TO:
	// 		determine amount of tokens 'To' has after transaction, if address does not exist transaction will be performed anyways (it will then exist)
	//			try to look up 'To' address in statedb
	toValCur, err := ReadFromBucket(t.To, "statedb")
	if err != nil {
		logger.L.Panic(err)
	}
	//			if receiver address does not exist already in statedb
	if toValCur == nil {
		// 				create it with new balance and nonce of 0
		tToValNew := StateValueStruct {
			Balance: 	t.Value,
			Nonce: 		0,
		}
		//				serialize it
		toValNewSer, err := msgpack.Marshal(&tToValNew)
		if err != nil {
			logger.L.Panic(err)
		}
		//				store the value that would result from this transaction in the variable that will later be returned
		toNewValAfterTrans = append(toNewValAfterTrans, toValNewSer...)


	} else { //	otherwise (receiver already exists in statedb) calculate new balance by deserializing current value and updating it, then write to statedb
		//				deserialize current value
		var toValCurDeser StateValueStruct
		err = msgpack.Unmarshal(toValCur, &toValCurDeser)
		if err != nil {
			logger.L.Panicf("Failed to deserialize 'To' address current statedb value: %v", err)
		}
		//				calculate new balance
		toValCurDeser.Balance += t.Value
		//				serialize again
		toValNewSer, err := msgpack.Marshal(&toValCurDeser)
		if err != nil {
			logger.L.Panic(err)
		}
		//				store the value that would result from this transaction in the variable that will later be returned
		toNewValAfterTrans = append(toNewValAfterTrans, toValNewSer...)

	}

	// This function returns the following values:
	//		bool:	is this transaction valid?
	//		error: 	this is either nil (valid transaction) or holds error message (why transaction is not valid)
	//		string:	t.From (who sent the transaction)
	//		[]byte:	fromNewValAfterTrans: Serialized StateValueStruct of t.From (state of the transaction sender wallet after transaction would have been sent)
	//		string: t.To (who receives the transaction)
	//		[]byte: toNewValAfterTrans: Serialized StateValueStruct of t.To (state of the transaction receiver wallet after transaction would have been performed)

	return true, nil, t.From, fromNewValAfterTrans, t.To, toNewValAfterTrans
}

// ---- Strconv conversion helpers ----

// isValidUint32 determines whether a string can be cast to uint32. Returns uint32 if possible, otherwise returns 0 and an error
func isValidUint32(s string) (uint32, error) {
	uint64Result, err := strconv.ParseUint(s, 10, 32)
	if err != nil { // if s is larger than 4294967295 there will be an error too because it can't be held by 32 bits, also any space etc. leads to error
		return 0, err
	}

	return uint32(uint64Result), nil
}

// isValidUint64 determines whether a string can be cast to uint64. Returns uint64 if possible, otherwise returns 0 and an error
func isValidUint64(s string) (uint64, error) {
	uint64Result, err := strconv.ParseUint(s, 10, 64)
	if err != nil { // if s is larger than 184467440737095551615 there will be an error too because it can't be held by 64 bits, also any space etc. leads to error
		return 0, err
	}

	return uint64(uint64Result), nil
}

// isValidFloat64 converts a string to float64 if possible.
// Valid Examples:
//
//	002 	becomes 2
//	3.300 	becomes 3.3
//	41,5 	becomes 41.5
//
// Note: Both . and , are allowed for fractional part separation.
func isValidFloat64(s string) (float64, error) {
	// i will replace all , with .
	sNew := ""

	// i do not want to allow sth like 1e10 so manually check for it
	for _, c := range s {
		if c == 'e' { // important to use '' [rune] instead of "" [string]
			return 0, fmt.Errorf("isValidFloat64 - Detected invalid char. I do not consider this to be a valid float: %v", s)
		}

		// replace , with . if it occurs
		if c == ',' {
			sNew += string('.')
		} else {
			sNew += string(c)
		}
	}

	// i also do not want to allow special cases like NaN, nan, Inf, inf, -Inf or -inf
	if strings.ToLower(sNew) == "nan" || strings.ToLower(sNew) == "inf" || strings.ToLower(sNew) == "-inf" {
		return 0, fmt.Errorf("isValidFloat64 - Detected invalid sequence. I do not consider these special floats to be valid in my context: %v", sNew)
	}

	sFloat, err := strconv.ParseFloat(sNew, 64)
	if err != nil {
		return 0, err
	}

	return sFloat, nil
}
