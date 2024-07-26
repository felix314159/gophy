package merkletree

import (
	"fmt"
	"testing"

	"github.com/felix314159/gophy/block/hash"
)

func TestMerkleTree(t *testing.T) {
	// all hashes here are keccak256 legacy

	h1 := hash.NewHash("test1")	// 6d255fc3390ee6b41191da315958b7d6a1e5b17904cc7683558f98acc57977b4
	h2 := hash.NewHash("test2") // 4da432f1ecd4c0ac028ebde3a3f78510a21d54087b161590a63080d33b702b8d
	h3 := hash.NewHash("test3") // 204558076efb2042ebc9b034aab36d85d672d8ac1fa809288da5b453a4714aae
	h4 := hash.NewHash("test4") // 87ce9fb076e206b40a6ab86e39ba8d0097abec87a8fa990c91a1d0b9269835ae
	h5 := hash.NewHash("test5") // 9e199fafc1079dfb2b375cdac741cefb6c51d5f471f8afffa517442b6160463c

	// ---- Testing construction of Merkle Trees ----

	// tree 1 (even amount of leafs)
	tree1 := []hash.Hash{h1, h2, h3, h4}
	myMerkleTree1 := NewMerkleTree(tree1)
	rootHash1String := myMerkleTree1.GetRootHash().GetString()

	// tree 2 (odd amount of leafs = duplication of last one)
	tree2 := []hash.Hash{h1, h2, h3, h4, h5}
	myMerkleTree2 := NewMerkleTree(tree2)
	rootHash2String := myMerkleTree2.GetRootHash().GetString()
	
	// TREE 1 CALCULATION:
		// 1+2				= n1.1 = 8bdb7dc74c354e128170147ad5731967aeb625dd701fc791f8caf5b6d90f1fe4
		// 3+4				= n1.2 = e9273d19c3e8f047a34a23eb9d5026c9b55d4d90db4429b14c832bb26a2c44d8
		
		// root      = n1.1 + n1.2 = d213eb1ec758d0dea37b5c9d360cfb99dca9059942ad0645131daa3af77ae084
	// TREE 2 CALCULATION:
									
		// 1+2				= n1.1 = 8bdb7dc74c354e128170147ad5731967aeb625dd701fc791f8caf5b6d90f1fe4
		// 3+4				= n1.2 = e9273d19c3e8f047a34a23eb9d5026c9b55d4d90db4429b14c832bb26a2c44d8
		// 5+5dup			= n1.3 = e87ad14798e1c91f734953bec5e138f5d0526bbe5bde3edf308851056e2d38e6

		// n1.1 + n1.2		= n2.1 = d213eb1ec758d0dea37b5c9d360cfb99dca9059942ad0645131daa3af77ae084
		// n1.3 + n1.3dup	= n2.2 = 25c642bd02400d4d5d777a470bf232c2a73adf88fc0b9f3ebc7a3901155919d2

		// root		 = n2.1 + n2.2 = 52b388c93a38478bbde6ef92c28bc75745314f4c2ef300f31ea621fd33c68256


	// tree 1 correct root check
	if rootHash1String != "d213eb1ec758d0dea37b5c9d360cfb99dca9059942ad0645131daa3af77ae084" {
		t.Errorf("merkletree failed test: Tree 1: Wrong root hash! " +
"Getting hash: %v instead of expected d213eb1ec758d0dea37b5c9d360cfb99dca9059942ad0645131daa3af77ae084 \n", rootHash1String)
	}

	// tree 2 correct root check
	if rootHash2String != "52b388c93a38478bbde6ef92c28bc75745314f4c2ef300f31ea621fd33c68256" {
		t.Errorf("merkletree failed test: Tree 2: Duplication of last leaf not working as expected.. " +
"Getting hash: %v instead of expected 52b388c93a38478bbde6ef92c28bc75745314f4c2ef300f31ea621fd33c68256 \n", rootHash2String)
	}


	// ---- Testing construction of Merkle Proof

	//		tree 1:		give me merkle proof for Leaf 3 (test3). Proof is converted to string and compared to known correct string
	myMerkleTree1ProofL3, err := myMerkleTree1.CalculateMerkleProof(h3)	// returns []hash.LRHash
	if err != nil {
		t.Errorf("merkletree failed test: %v", err)
	}
	myMerkleTree1ProofL3String := ""
	for _, v := range myMerkleTree1ProofL3 {
		myMerkleTree1ProofL3String += v.Value.GetString() + fmt.Sprintf("%t", v.IsLeft) // cast IsLeft from bool to string, make sure to use %t not %T
	}
	//					check whether resulting proof is as expected
	if myMerkleTree1ProofL3String != "87ce9fb076e206b40a6ab86e39ba8d0097abec87a8fa990c91a1d0b9269835aefalse8bdb7dc74c354e128170147ad5731967aeb625dd701fc791f8caf5b6d90f1fe4trued213eb1ec758d0dea37b5c9d360cfb99dca9059942ad0645131daa3af77ae084true" {
		t.Errorf("merkletree failed test: Merkle Tree 1 Proof Construction resulted in unexpected proof: %v", myMerkleTree1ProofL3String)
	}

	//		tree 2:		give me merkle proof for Leaf 4 (test4)
	myMerkleTree2ProofL4, err := myMerkleTree2.CalculateMerkleProof(h4)
	if err != nil {
		t.Errorf("merkletree failed test: %v", err)
	}
	myMerkleTree2ProofL4String := ""
	for _, w := range myMerkleTree2ProofL4 {
		myMerkleTree2ProofL4String += w.Value.GetString() + fmt.Sprintf("%t", w.IsLeft)
	}
	//					check whether resulting proof is as expected
	if myMerkleTree2ProofL4String != "204558076efb2042ebc9b034aab36d85d672d8ac1fa809288da5b453a4714aaetrue8bdb7dc74c354e128170147ad5731967aeb625dd701fc791f8caf5b6d90f1fe4true25c642bd02400d4d5d777a470bf232c2a73adf88fc0b9f3ebc7a3901155919d2false52b388c93a38478bbde6ef92c28bc75745314f4c2ef300f31ea621fd33c68256true" {
		t.Errorf("merkletree failed test: Merkle Tree 2 Proof Construction resulted in unexpected proof: %v", myMerkleTree2ProofL4String)
	}	

	// ---- Testing validation of Merkle Proof

	//		tree 1:		the only test that should return true
	proofTree1VerficationBool := VerifyMerkleProof(h3, myMerkleTree1ProofL3)
	if !proofTree1VerficationBool {
		t.Errorf("merkletree failed test: Merkle Tree 1 Proof Verification returned false instead of expected true!")
	}
	//					all other tests that should return false
	proofTree1VerficationBoolL1 := VerifyMerkleProof(h1, myMerkleTree1ProofL3)
	proofTree1VerficationBoolL2 := VerifyMerkleProof(h2, myMerkleTree1ProofL3)
	proofTree1VerficationBoolL4 := VerifyMerkleProof(h4, myMerkleTree1ProofL3)
	if proofTree1VerficationBoolL1 || proofTree1VerficationBoolL2 || proofTree1VerficationBoolL4 {
		t.Errorf("merkletree failed test: Merkle Tree 1 Proof Verification of invalid proofs (for given hash) returned at least one true instead of expected all false!")
	}
	//		tree 2:		the only test that should return true
	proofTree2VerficationBool := VerifyMerkleProof(h4, myMerkleTree2ProofL4)
		if !proofTree2VerficationBool {
		t.Errorf("merkletree failed test: Merkle Tree 2 Proof Verification returned false instead of expected true!")
	}
	//					all other tests that should return false
	proofTree2VerficationBoolL1 := VerifyMerkleProof(h1, myMerkleTree2ProofL4)
	proofTree2VerficationBoolL2 := VerifyMerkleProof(h2, myMerkleTree2ProofL4)
	proofTree2VerficationBoolL3 := VerifyMerkleProof(h3, myMerkleTree2ProofL4)
	proofTree2VerficationBoolL5 := VerifyMerkleProof(h5, myMerkleTree2ProofL4)
	if proofTree2VerficationBoolL1 || proofTree2VerficationBoolL2 || proofTree2VerficationBoolL3 || proofTree2VerficationBoolL5 {
		t.Errorf("merkletree failed test: Merkle Tree 2 Proof Verification of invalid proofs (for given hash) returned at least one true instead of expected all false!")
	}
	//					special test: get merkle proof of node that is duplicated (odd + last leaf before dup) and check if its proofs validation works
	myMerkleTree2ProofL5, err := myMerkleTree2.CalculateMerkleProof(h5)
	specialTestProofVerificationBool := VerifyMerkleProof(h5, myMerkleTree2ProofL5)
	if !specialTestProofVerificationBool {
		t.Errorf("merkletree failed test: Merkle Tree 2 Special Test Proof Verification returned false instead of expected true!")
	}


}
