// Package transaction contains structs and functions for enabling transaction support. In order to send a transaction navigate your browser to localhost:8087/send-transaction after your node has concluded its initial sync.
package transaction

import (
	// signature related and address conversion
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/logger"
	"github.com/felix314159/gophy/block/merkletree"
)

// Transaction contains fields such as From, TxTime, To, Value and txHash.
// txHash is a unique identifier that is used in Merkle Trees and Proofs.
type Transaction struct {
	From   		string  		// libp2p nodeid starts with 12D3Koo... and has length 52 chars
	TxTime 		uint64
	To     		string
	Value  		float64
	Reference 	string        	// sender is allowed to put arbitrary data in reference field (e.g. payment reference number, but could also be used as e.g. timestamping ideas by putting their hash) [length of string is limited to 64 characters]
	TxHash 		hash.Hash		// serves as unique transaction ID [serialize From,TxTime,To and Value with msgpack, then hash that value]
	Sig			[]byte
	Nonce		int 			// keeps track how many transactions an account performed in the past (protects against replay attacks)
	Fee 		float64 		// sender can freely decide how much tokens to burn to have RA prioritize the transaction (pending tx with highest fee will be included next in block), minimum fee is equal to winner.MinTransactionAmount)
	// At some point also add an 'Expiry' field from which point in time onwards the transaction becomes invalid if it has not been included in a block until then (Why? Currently if node sends multiple transactions at once, they will all have the same nonce so only one will go through as valid, so this could bloat the pendingtransactions list with transactions that won't ever become valid. I might as well have allowed to user to manually enter nonce but then everyone is harder to use)
}

/* Considerations for how to choose max amount of allowed transactions per block assuming blocktime of 24h:
	-> 9 fields
		-> 'Longest' field is reference with up to 64 chars, From and To fields are capped at 52 chars
		-> Let's just assume every field is as costly as reference field, so each transaction might be 9*64 = 576 bytes (just very rough estimate)

	Ok if every transaction costs 0.5 KB of storage, then allowing 1000 transactions per block = per day would mean blockchain size can grow 0.5 MB per day just for transsactions alone
	This seems like no issue at all. Compared to other blockchain data that is stored transaction list will have the largest influence of blockchain growth (as in the data that is actively synced between nodes)
	A decent upper limit would be having a growth of 1 GB per year just from transaction lists alone (light node never request transaction lists, so only sync of full nodes would be affected. so even after multiple years the initial sync should take less than 30 min even with terrible internet)
	So if this is the target it means 1 Gb per 365 blocks = 2.74 MB per block for transaction list
	Since each transaction is around 576 bytes, that means 2.74 MB = 2740000 Bytes so we should not include more than 4757 transactions per block to guarantee decent full node initial sync times even with worst case scenarios (every block full with transactions with full reference field etc.)

	Note: These are only storage considerations and only the kind of data nodes would sync with each other. Having this many transactions could lead to other issues (e.g. bloated statedb or slow validity checks after a few years for initial sync completion) over time that are not a concern for this PoC right now
	Note 2: Full nodes also stores the problem definition (the problem that had to be solved to create this block) and there currently is no limit on how many subproblems the RA could use per problem. So this should probably be capped in the future, otherwise RA could accidentally become driving factor in bloating the blockchain by putting too many subproblems which could influence resulting blockchain filesizes for full nodes even more than transaction lists. 
*/

// NewTransaction is the constructor function of Transaction. It serializes all fields (except txHash) and then hashes this value to determine txHash.
func NewTransaction(from string, txTime uint64, to string, value float64, reference string, nonce int, fee float64, privKey crypto.PrivKey) Transaction {
	// crop reference field to first 64 chars if necesssary (rest is ignored)
	if len(reference) > 64 {
		reference = reference[:64]
	}

	// create inital transaction object
	transaction := Transaction{
		From:   from,
		TxTime: txTime,
		To:     to,
		Value:  value,
		Reference: reference,
		Nonce:	nonce,
		Fee: fee,
		//TxHash field gets zero values for now
		//Sig field gets zero values for now
	}
	
	// serialize object using msgpack and cast it from []byte to string
	transactionSerialized, err := msgpack.Marshal(&transaction)
	if err != nil {
		logger.L.Panic(err)
	}

	// hash serialized transaction and return it as string
	transactionHash := hash.NewHash(string(transactionSerialized))
	
	// sign the TxHash with your private key
	sig, err := privKey.Sign(transactionHash.Bytes)
	if err != nil {
		logger.L.Panic(err) // signing should never fail
	}
	
	// return transaction object that knows its own hash
	return Transaction {
		From:   from,
		TxTime: txTime,
		To:     to,
		Value:  value,
		Reference: reference,
		TxHash: transactionHash,
		Sig:	sig,
		Nonce:	nonce,
		Fee: fee,
	}

}

// TransactionListToMerkleTreeRootHash takes a slice of transactions, reconstructs the Merkle Tree and returns its root hash
func TransactionListToMerkleTreeRootHash(tList []Transaction) hash.Hash {
	if len(tList) < 1 {
		return hash.NewHash("empty")
	}

	hashSlice := []hash.Hash{}
	
	// get transaction hashes in same order
	for _, t := range tList {
		hashSlice = append(hashSlice, t.TxHash)
	}
	
	// construct merkle tree
	mTree := merkletree.NewMerkleTree(hashSlice)
	
	// returns its root hash as hash.Hash
	return mTree.GetRootHash()
}
