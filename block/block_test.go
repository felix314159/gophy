package block

import (
	"testing"
	
	"example.org/gophy/block/hash"
	"example.org/gophy/block/simpar"
	"example.org/gophy/block/transaction"
	"example.org/gophy/block/winner"
)

func TestBlock(t *testing.T) {
	// get example simulation task
	simPar := []simpar.SimulationParameters{simpar.NewSimulationParameters(0, 0, 0, 0, 0., 0.)}
	simHeader := simpar.NewSimulationHeader(0, 0, uint32(0), uint32(0), hash.Hash{}, []simpar.SimulationParameters{})
	simTask := simpar.NewSimulationTask(simPar, simHeader)

	// create block
	myBlock := NewBlock(											// HEADER:
		uint32(2),													//		BlockID
		uint64(123456),												//		BlockTime
		hash.NewHash("examplePrevBlockHash"),						//		PrevBlockHash
		hash.NewHash("exampleStateMerkleHash"),						// 		StateMerkleHash
		hash.NewHash("exampleProblemID"),							//		ProblemID
		winner.NewBlockWinner(										//		BlockWinner
			hash.NewHash("exampleWinnerAddress").GetString(),		//			Winner Address
			hash.NewHash("exampleSolutionHash"),					//			Solution Hash
			float64(4),												//			Token Amount
			),														// FULL BLOCK ONLY:
		[]transaction.Transaction{},								//		Transactions
		simTask,
	)

	// trivial test, all of this keeps working as long as block.Block itself does not change
	if myBlock.BlockHeader.BlockTime > GetCurrentTime() {
		t.Errorf("block failed test: BlockTime is higher than current time.")
	}

	// get gen for full node
	gen := GetGenesisBlock(true).(Block)
	if gen.BlockHeader.StateMerkleRoot.GetString() != "92fc2af17983368e60c4cf529c8229f0053f95f39ea23ed16b77476ab324ffd3" {
		t.Errorf("block failed test: StateMerkleHash has unexpected value")
	}
	if gen.BlockHeader.TransactionsMerkleRoot.GetString() != "61fc3f77ebc090777f567969ad9823cf6334ab888acb385ca72668ec5adbde80" {
		t.Errorf("block failed test: TransactionsMerkleHash has unexpected value")
	}

}
