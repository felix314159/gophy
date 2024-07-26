// Package hash defines functions around keccak256 and useful helper functions.
package hash

import (
	"encoding/hex"

	"example.org/gophy/logger"
	
	"golang.org/x/crypto/sha3"
)



// Hash has only one field which is the hash as slice of byte.
type Hash struct {
	Bytes []byte
}

// LRHash is a Hash that is aware of whether it is the hash of a left node or a right node in a tree. This is used in the context of Merkle Trees and Merkle Proofs.
type LRHash struct {		// This is a hash that knows whether it is a left node hash or a right node hash (required for storing Merkle Proof) [root defined to be IsLeft true]
	Value	Hash
	IsLeft	bool
}


// NewHash is the constructor of Hash which makes sure that the hash function is used correctly.
func NewHash(input string) Hash {
	h := sha3.NewLegacyKeccak256() 			// create hash.Hash object [i used legacy for compatibility with online comparisions like https://emn178.github.io/online-tools/keccak_256.html]
	h.Write([]byte(input))          		// fill object with data
	var hashResult Hash = Hash{h.Sum(nil)}	// calculate hashsum (nil means store in new byte slice)
	return hashResult
}


// ---- Methods ----

// GetString returns the hash as string.
func (h Hash) GetString() string {
	if h.Bytes == nil {
		logger.L.Panic("hash.Hash GetString - The Bytes field of this hash.Hash is nil! Aborting..")
	}
	return hex.EncodeToString(h.Bytes)
}

// PrintString prints the hash value as hex string.
func (h Hash) PrintString(){
	logger.L.Printf("Hash: %v \n", hex.EncodeToString(h.Bytes))
}

// ---- Functions ----

// Concatenate concatenates two Hashes, hashes it and then returns the resulting Hash.
// It is used for Merkle Tree construction.
func Concatenate(h1 Hash, h2 Hash) Hash {
	sum := append(h1.Bytes, h2.Bytes...) // variadic call of append (important so that h2.Bytes is unpacked)
	newHash := NewHash(hex.EncodeToString(sum))
	return newHash
}

// GetHashObjectWithoutHashing takes a hex string (which represent a hash) and returns a hash.Hash that returns this when called with .GetString()
// sometimes you already know the hash you want as hex string but you need it as hash.Hash.
// This function takes care of this situation and is very useful for e.g. GenerateTransaction() which is used for testing.
func GetHashObjectWithoutHashing(hexString string) Hash {
	hexStringBytes, err := hex.DecodeString(hexString)
	if err != nil {
		logger.L.Panic(err)
	}

	myHash := Hash{hexStringBytes}
	if myHash.GetString() != hexString {
		logger.L.Panicf("hash.Hash GetHashObjectWithoutHashing - Hash has wrong value! Expected %v but got %v", myHash.GetString(), hexString)	
	}
	return myHash
}

// HashStringGetBytes takes a hex string (which represents a hash) and return the bytes of a hash.Hash object that would return the input string if you called .GetString() on it.
// This function exists because you can NOT just []byte(hexString) as it would give a different result.
func HashStringGetBytes(hexString string) []byte {
	h := GetHashObjectWithoutHashing(hexString)
	return h.Bytes
}
