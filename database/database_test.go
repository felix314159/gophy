package database

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	//"path/filepath"
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/vmihailenco/msgpack/v5"

	"example.org/gophy/block"
	"example.org/gophy/block/hash"
)

func TestDatabase(t *testing.T) {
	// reset database and create full node blockchain
	ResetAndInitializeGenesis(true) // warning: running tests deletes your local blockchain which means you will need to re-sync after that

	// get gen block for testing
	gen := block.GetGenesisBlock(true).(block.Block)
	//		serialized gen block.Block
	genSer, err := msgpack.Marshal(&gen)
	if err != nil {
		t.Errorf("database failed test: Msgpack failed to serialize genesis block.Block: %v \n", err)
	}
	//		serialized gen block.Header
	genHeaderSer, err := msgpack.Marshal(&gen.BlockHeader)
	if err != nil {
		t.Errorf("database failed test: Msgpack failed to serialize genesis header: %v \n", err)
	}
	
	// ---- Transport Struct tests ----

	// first get a private key for signing the message (during testing you must add .. to create key in the rootdir instead of in <rootdir>/database)
	_, ed25519Priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Errorf("database failed test: Failed to generate ed25519 key: %v \n", err)
	}

	// convert ed25519 priv to libp2p priv
	priv, err := crypto.UnmarshalEd25519PrivateKey(ed25519Priv)
    if err != nil {
        t.Errorf("database failed test: Failed to convert ed25519 priv to libp2p priv: %v\n", err)
    }

    // derive pub
    pub := priv.GetPublic()

	PrivateKey = priv
	MyNodeIDString, err = PubKeyToNodeID(pub)
	if err != nil {
		t.Errorf("database failed test: Failed to derive nodeID from pubkey: %v \n", err)
	}

	// case: []string
	mySlice := []string{"a", "b", "c", "d"}
	//		serialize slice
	mySliceSer, err := msgpack.Marshal(&mySlice)
	if err != nil {
		t.Errorf("database failed test: Msgpack failed to serialize slice of string: %v \n", err)
	}
	//		create transport struct
	mySliceTransportStruct := NewTransportStruct(TSData_StringSlice, MyNodeIDString, mySliceSer)
	//		get dataID from transport struct
	mySliceTransportStructRetrievedID, err := TransportStructGetDataID(mySliceTransportStruct)
	if (mySliceTransportStructRetrievedID != TSData_StringSlice) || (err != nil) {
		t.Errorf("database failed test: TransportStructGetDataID failed to get TSData description of string slice transport struct: %v \n", err)
	}

	// silence print output of TransportStructExtraction by temporarily setting Stdout to garbage file
	oldStdout := os.Stdout
	if runtime.GOOS == "windows" {
		f, err := os.Create("NUL") // windows equiv of /dev/null
		if err != nil {
			t.Errorf("database failed test: Failed to adjust Stdout due to error: %v\n", err)
		}
		os.Stdout = f
		defer f.Close()
	} else {
		f, err := os.Create("/dev/null")
		if err != nil {
			t.Errorf("database failed test: Failed to adjust Stdout due to error: %v\n", err)
		}
		os.Stdout = f
		defer f.Close()
	}

	//		extract transport struct
	mySliceExtractedSer, _, err := TransportStructExtraction(TSData_StringSlice, mySliceTransportStruct, false)
	if err != nil || (!cmp.Equal(mySliceExtractedSer, mySliceSer)) {
		t.Errorf("database failed test: TransportStructExtraction failed to extract the data ID (expected string slice) from the transport struct: %v \n", err)
	}

	// ----			Full Block		----
	// create transport struct for full block
	myFullBlockTransportStruct := NewTransportStruct(TSData_Block, MyNodeIDString, genSer)

	// get id from transport struct
	retrievedFullBlockTSID, err := TransportStructGetDataID(myFullBlockTransportStruct)
	if (retrievedFullBlockTSID != TSData_Block) || (err != nil) {
		t.Errorf("database failed test: TransportStructGetDataID failed to extract the data ID (expected full block) from the transport struct: %v \n", err)
	}

	// extract transport struct and compare to previous serialized full block (must be exactly equal)
	tsBlockExtractedSer, _, err := TransportStructExtraction(TSData_Block, myFullBlockTransportStruct, false)
	if err != nil || (!cmp.Equal(tsBlockExtractedSer, genSer)) {
		t.Errorf("database failed test: TransportStructExtraction failed to extract the data ID (expected full block) from the transport struct: %v \n", err)
	}

	// ----			Header			----
	// create transport struct for header
	myHeaderTransportStruct := NewTransportStruct(TSData_Header, MyNodeIDString, genHeaderSer)

	// get id from transport struct
	retrievedHeaderTSID, err := TransportStructGetDataID(myHeaderTransportStruct)
	if (retrievedHeaderTSID != TSData_Header) || (err != nil) {
		t.Errorf("database failed test: TransportStructGetDataID failed to extract the data ID (expected header) from the transport struct: %v \n", err)
	}

	// extract transport struct and compare to previous serialized header (must be exactly equal)
	tsHeaderExtractedSer, _, err := TransportStructExtraction(TSData_Header, myHeaderTransportStruct, false)
	if err != nil || (!cmp.Equal(tsHeaderExtractedSer, genHeaderSer)) {
		t.Errorf("database failed test: TransportStructExtraction failed to extract the header from the transport struct: %v \n", err)
	}


	// reset Stdout to normal value (only TransportStructExtraction does annoying prints during testing)
	os.Stdout = oldStdout

	// ---- Read from DB and write to DB----

	// ----			Full Block		----
	// 1. One manual test
	//		get block from db
	fullBlockBytesFromDb, err := BlockGetBytesFromDb("latest")
	if err != nil {
		t.Errorf("database failed test: BlockGetBytesFromDb failed to get latest block from db: %v \n", err)
	}
	//		convert bytes to block
	retrievedBlockLatest := FullBlockBytesToBlock(fullBlockBytesFromDb)
	//		serialize its header to reconstruct the hash
	retrievedBlockHeaderSer, err := msgpack.Marshal(&retrievedBlockLatest.BlockHeader)
	if err != nil {
		t.Errorf("database failed test: Msgpack failed to serialize the header of the block that was retrieved from db: %v \n", err)
	}
	//		recalculate blockhash (the latest block in this example is the genesis block)
	if hash.NewHash(string(retrievedBlockHeaderSer)).GetString() != genesisHash {
		t.Errorf("database failed test: The genesis block that was retrieved from db has invalid hash: %v instead of expected hash: %v \n", hash.NewHash(string(retrievedBlockHeaderSer)).GetString(), " c1d72272b29d8f37b530f179fb07dbe386e9fefa8e5a468dd53d05b848af536f")
	}

	// ---- FullBlockBytesToBlock() has implicitely already been tested ----

	// ---- HeaderBytesToBlockLight() will implicitely be tested by GetAllBlockHashesAscending() ----

	// ---- GetAllBlockHashesAscending() will be tested implicitely by GetAllBlockHashesAscendingSerialized()

	// ---- Testing GetAllBlockHashesAscendingSerialized() ----
	
	hashSliceSer := GetAllBlockHashesAscendingSerialized(true)
	//		deserialize the []byte to []string using the function UnpackSerializedStringSliceBlockHashes()
	hashSliceStr, err := UnpackSerializedStringSliceBlockHashes(hashSliceSer)
	if err != nil {
		t.Errorf("database failed test: UnpackSerializedStringSliceBlockHashes() failed to deserialize []byte to []string: %v \n", err)
	}
	
	//		concatenate all strings and compare to known correct value
	var hashStringSum string
	for _, v := range hashSliceStr {
		hashStringSum += v
	}
	// silence unused error for now 
	if len(hashStringSum) == 0 {}


	// test nodeID and pubkey conversions
	// 		conversion from nodeID string to PubKey
	mypub, err := NodeIDStringToPubKey(RANodeID)
	if err != nil {
		t.Errorf("signature failed test: Failed to convert nodeID string to its corresponding PubKey: %v\n", err)
	}
	// 		ensure this is the expected PubKey
	pubBytes, err := mypub.Raw()
	if err != nil {
		t.Errorf("signature failed test: Failed to call Raw() on PubKey: %v\n", err)
	}
	if hex.EncodeToString(pubBytes) != "4636239c8162034c358b781c36adb25b9c15390e1c2838e6bc0e7e7dbcc05cac" {
		t.Errorf("PubKey does not match expected value. Instead got: %v\n", hex.EncodeToString(pubBytes))
	}

}
