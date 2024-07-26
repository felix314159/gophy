// Package winner contains structs and functions for describing which miner was calculated to be the winner of the current block. The winner is rewarded a certain amount of tokens that is halved in regular intervals. More details about the deterministic and fair winner selection process can be found in https://arxiv.org/abs/2312.01951
package winner

import (
	"math"

	"example.org/gophy/logger"
	"example.org/gophy/block/hash"
)

const (
	MinTransactionAmount float64 = 0.00000001
)

// BlockWinner has fields such as winnerAddress, SolutionHash and TokenReward.
// The TokenReward follows a formula that ensures that the first block rewards 1024 tokens and every four blocks this amount is halved.
// Articifial scarcity prevents inflation and enables tokens to have a perceived value / rarity.
type BlockWinner struct {
	WinnerAddress	string
	SolutionHash	hash.Hash
	TokenReward		float64
}

// NewBlockWinner is the constructor function of BlockWinner.
func NewBlockWinner(winnerAddress string, solutionHash hash.Hash, tokenReward float64) BlockWinner {
	return BlockWinner {
		WinnerAddress:	winnerAddress,
		SolutionHash:	solutionHash,
		TokenReward:	tokenReward,
	}
}

// PrintBlockWinner prints winner data in a human-readable format.
func (w BlockWinner) PrintBlockWinner() {
	logger.L.Printf("WinnerAddress: %v\nSolutionHash: %v\nTokenReward: %v\n", w.WinnerAddress, w.SolutionHash.GetString(), w.TokenReward)
}

// GetTokenRewardForBlock takes the blockIndex as input (e.g. Block 5) and returns the TokenReward that the winner of this block is awarded.
// The formula for block n > 0 is: 64 * (0.5)^(ceil(n/1460)-1). This means that the block reward starts at 64 tokens and is lowered by 50% every 1460  (4*365) blocks.
// The RA is expected to create 1 new block per day which means the block reward is halved approximately every 4 years.
// This prevents long-term token inflation by introduction artificial scarcity and it also rewards early blockchain adapation.
// The total supply of tokens is limited to 186880, and the time it takes until reward becomes zero is less than 132 years (same target time as Bitcoin).
// To be able to send hard sim problems (24 hours time to solve) halvings occurr much more often (per x blocks) than Bitcoin to compensate for not having 10 min blocktime target.
func GetTokenRewardForBlock(blockIndex uint32) float64 {
	// no reward for mining genesis block
	if blockIndex == 0 {
		return 0
	}

	initialReward := float64(64)			// initial block reward (doubling value means first block with reward=0 is reached around 4 years later) [64 was chosen over bitcoins 50 because power of two is nicer number]
	halvingEveryXBlocks := float64(1460) 	// amount of blocks to be mined before reward is halved

	reward := initialReward * math.Pow(0.5, math.Ceil(float64(blockIndex)/halvingEveryXBlocks) -1 ) // important to use 0.5 instead of 5/10 (the latter drops the fractional part)
	if reward < MinTransactionAmount { // MinTransactionAmount is smallest amount of created/transferable currency
		return 0
	}
	return reward 
}
/* Code for verifying token amount formula correctness [as in it does what it should do]:
	// Following examples ignores leap years [the only consequence is that time estimations are slightly too high]
	var prevReward float64
	var totalCreatedTokens float64
	halvingsCounter := 0
	for i:=1; i<50000; i+=1 {
		result := winner.GetTokenRewardForBlock(uint32(i))
		if result < prevReward {
			halvingsCounter += 1
		}

		totalCreatedTokens += result
		logger.L.Printf("Block %v reward: %v\n", i, result)

		prevReward = result
		if result == 0 {
			break
		}
		
	}
	if prevReward == 0 {
		// Reward only reaches zero because the reward function knows the minimum amount of rewarded tokens is MinTransactionAmount [otherwise infinite halvings if no rounding errors] 
		logger.L.Printf("Total amount of reward halvings after which reward reaches zero: %v\n", halvingsCounter)
		logger.L.Printf("Assuming avg blocktime of 24 hours this means amount of days until block next reward reaches 0 is estimated to be a bit less [due to leap years] than: %v days\nThis is is in less than %v years.\n", halvingsCounter*365*4, halvingsCounter*4)
	}
	logger.L.Printf("Upper limit of total tokens that can exist: %v\n", math.Ceil(totalCreatedTokens))
	// Total token amount 186880 was also verified with wolfram alpha: sum from 1 to n=inf of 64 * (0.5 ^(ceil(n/1460)-1))
	// From block 48181 onwards the reward is 0.
*/

// The winner selection algorithm is implemented at the end of networking.go as it relies on structs defined there and I need to avoid circular imports.
