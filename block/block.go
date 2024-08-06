// Package block contains struct definitions and functions for Block which is an important struct for the blockchain.
package block

import (
	"time"

	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/block/simpar"
	"github.com/felix314159/gophy/block/transaction"
	"github.com/felix314159/gophy/block/winner"
)

// Header holds block metadata and is part of every block. Headers are equipped with Merkle tree root hash data so that light node protocols can exist.
type Header struct {
	BlockID					uint32						// 0, 1, 2, ...
	BlockTime				uint64						// block timestamp (epoch time in seconds)
	PrevBlockHash			hash.Hash					// hash of previous block (more specifically, the hash of a block is the hash of a block's header)
	TransactionsMerkleRoot	hash.Hash					// root hash of transactions merkle tree
	StateMerkleRoot      	hash.Hash 					// root hash of state merkle tree
	ProblemID				hash.Hash					// hash of block problem that is bound to this block
	BlockWinner				winner.BlockWinner			// address of winner node, hash of solution, amount of token reward
}

// Block holds blockchain data and is serialized and stored in the chaindb bucket of the database. A full node will store the entire block, will a light node will only store the header of the block.
type Block struct {
	// header (stored by full node + light node)
	BlockHeader				Header						// header being a struct makes serialization (with light node support) easier

	// stored by full node only
	Transactions			[]transaction.Transaction	// list of transactions
	SimulationTask 			simpar.SimulationTask
}

// NewBlock is the constructor function for Block. By using this constructor to create a new block, a Merkle Tree for the transaction list is automatically constructed and its root hash stored in the header of the block.
func NewBlock(	 blockID uint32, blockTime uint64, prevBlockHash hash.Hash, stateMerkleRoot hash.Hash,
				 problemID hash.Hash, blockWinner winner.BlockWinner,
				 transactions []transaction.Transaction, simTask simpar.SimulationTask) Block {

	// construct chaindb transaction merkle tree (will return hash of "empty" as root hash if you pass empty transaction list) to determine transaction merkle tree root hash
	var transMerkleRoot hash.Hash
	transMerkleRoot = transaction.TransactionListToMerkleTreeRootHash(transactions)

	// state merkle root can not be determined here because it would create circular import situation, so instead just pass it

	// create header
	h := Header {
		BlockID:				blockID,
		BlockTime:				blockTime,
		PrevBlockHash:			prevBlockHash,
		TransactionsMerkleRoot:	transMerkleRoot,
		StateMerkleRoot:        stateMerkleRoot,
		ProblemID:				problemID,
		BlockWinner:			blockWinner,
	}

	return Block {
		BlockHeader:	h,
		Transactions:	transactions,
		SimulationTask: simTask,
	}

}

// GetCurrentTime returns the current timestamp so that it can be used as BlockTime which is uint64.
// It returns the epoch time in seconds (10 digits for the next many years).
func GetCurrentTime() uint64 {
	return uint64(time.Now().Unix())	// explicitely cast it from int64 to uint64
}

// GetGenesisBlock takes a bool value (get block full or light) and creates the static genesis block or header and returns it.
// Note that PrevBlockHash points to an imaginary hash, as block index -1 does not exist.
func GetGenesisBlock(fullBlock bool) interface{} {
	// get simulation task
	simPar := []simpar.SimulationParameters{simpar.NewSimulationParameters(0, 0, 0, 0, 0., 0.)}
	simHeader := simpar.NewSimulationHeader(0, 0, uint32(0), uint32(0), hash.Hash{}, []simpar.SimulationParameters{})
	simTask := simpar.NewSimulationTask(simPar, simHeader)

	gen := NewBlock(
		0,																										// BlockID
		1691584614,																								// BlockTime (e.g. uint64(time.Now().UnixMilli()))
		hash.NewHash("nil"),																					// PrevBlockHash
		hash.GetHashObjectWithoutHashing("92fc2af17983368e60c4cf529c8229f0053f95f39ea23ed16b77476ab324ffd3"), 	// StateMerkleTreeRoot if you use the 30k token alloc for RA for testing, TODO: change this hash when you excluded the pre-allocation
		hash.NewHash("nil"),																					// ProblemDefHash / ProblemID
		winner.NewBlockWinner(	"12D3Koonil",																	// BlockWinner: Bogus address, but it must start with 12D3Koo
								hash.NewHash("nil"),															// BlockWinner: Solution hash
								float64(0)),																	// BlockWinner: Token amount
		[]transaction.Transaction{},																			// Transaction list
		simTask,																								// simpar.SimulationTask
	)

	// case: full node
	if fullBlock {
		return gen
	}
	// case: light node (headers only)
	return gen.BlockHeader
	
}
