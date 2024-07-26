package winner

import (
	"fmt"
	"testing"
	
	"example.org/gophy/block/hash"
)

func TestWinner(t *testing.T) {
	myWinner := NewBlockWinner(
		hash.NewHash("exampleWinnerAddress").GetString(),	// Winner address
		hash.NewHash("exampleSolutionHash"),				// Solution hash
		float64(4),											// Token reward
	)

	// convert winner to string and compare to known correct value
	myWinnerString := myWinner.WinnerAddress + myWinner.SolutionHash.GetString() + fmt.Sprintf("%f", myWinner.TokenReward)
	if myWinnerString != "41338e7b0447ec92a5643a604345a8c316d5bf615494e03c23590eb9a431e7cd6e9e22417258a0ed4b5c1784e306e23da15b285bfad2a087bfc50845e6c436524.000000" {
		t.Errorf("winner failed test: Unexpected winner data: %v \n", myWinnerString)
	}

}
