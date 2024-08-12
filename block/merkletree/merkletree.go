// Package merkletree contains structs and functions required to enable Merkle Trees and Merkle Proofs.
package merkletree

import (
	"errors"
	//"fmt"

	"github.com/felix314159/gophy/block/hash"
	"github.com/felix314159/gophy/logger"
)

// Node is a node in a tree. A node knows its left and right sibling and its parent node.
type Node struct {
	Value	hash.Hash
	Left	*Node		// pointer to left child (nullptr if it does not exist)
	Right	*Node		// pointer to right child (nullptr if it does not exist)
	Parent	*Node 		// pointer to parent (nullptr if it does not exist)
}

// MerkleTree is a tree that is built bottom-up by combining two nodes and hashing them to form their parent.
type MerkleTree struct {
	nodes		[]*Node			// holds list of tree nodes
	roothash	hash.Hash		// holds the roothash
}

// NewMerkleTree is the constructor function of MerkleTree. It builds the tree from a provided slice of Hashes. Leafs are expected left-to-right assuming leafs are at the bottom.
func NewMerkleTree(leafs []hash.Hash) MerkleTree {	// input are leaf node values ordered left-to-right
	if len(leafs) < 1 {
		logger.L.Panic("You are not allowed to construct a Merkle Tree from an EMPTY slice []hash.Hash!")
	}

	// convert Hash leafs to nodes
	leafNodes := []*Node{}
	for _, l := range leafs {
		newNode := Node {
			Value:	l,
			Left:	nil,
			Right:	nil,
			Parent:	nil,
		}
		leafNodes = append(leafNodes, &newNode)
	}
	// if node amount if odd duplicate last node
	if len(leafNodes) % 2 != 0 {
		leafNodes = append(leafNodes, leafNodes[len(leafNodes)-1])
	}

	// build merkle tree (with Parent pointers nil)
	newTreeNodes := RecursivePairCombination(leafNodes, []*Node{}, true) // always pass true (sth special happens on first call)
	allTreeNodes := append(leafNodes, newTreeNodes...)
	
	// update Parent pointers
	FindParents(allTreeNodes)

	return MerkleTree {
		nodes:		allTreeNodes,
		roothash:	allTreeNodes[len(allTreeNodes)-1].Value,
	}
	
}

// FindParents is used to determine the parents of a slice of Node pointers.
func FindParents(nodeSlicePtr []*Node) {

	nodeAmount := len(nodeSlicePtr)

	// reverse over tree root-to-leafs
	for i:=nodeAmount-1; i>=0; i-=1 {
		// check whether current node has children (not a leaf)	
		if nodeSlicePtr[i].Left != nil {
			parentAddress := nodeSlicePtr[i]
			nodeSlicePtr[i].Left.Parent = parentAddress
			nodeSlicePtr[i].Right.Parent = parentAddress
		} else {
			return // we can stop already because only leafs will follow
		}
	}
}

// RecursivePairCombination is used to build Merkle Trees bottom-up.
func RecursivePairCombination(inputSlice []*Node, permanentStorage []*Node, firstIter bool) []*Node {
	// check if only root node left
	if len(inputSlice) <= 1 {
		return permanentStorage
	}

	// duplicate last element of inputSlice if amount odd (also add new value to permanentStorage)
	if len(inputSlice) % 2 != 0 {
		inputSlice = append(inputSlice, inputSlice[len(inputSlice)-1])
		// add duped element but only if this is not the first iteration
		if !firstIter{
			permanentStorage = append(permanentStorage, inputSlice[len(inputSlice)-1])
		}
		
	}

	// will be next iterations inputSlice
	nextIter := []*Node{}


	// calculate new elements and add them to nextIter
	for i:=0; i<len(inputSlice)-1; i+=2 {
		newNodeValue := hash.Concatenate(inputSlice[i].Value, inputSlice[i+1].Value)
		newNode := Node {
			Value:		newNodeValue,
			Left:		inputSlice[i],
			Right:		inputSlice[i+1],
			Parent:		nil,	// will be populated later (index of parent node is not known here)
		}
		nextIter = append(nextIter, &newNode)
	}

	// permanently store all elements from nextIter in permanentStorage
	permanentStorage = append(permanentStorage, nextIter...)

	return RecursivePairCombination(nextIter, permanentStorage, false)

}

// CalculateMerkleProof calculatess the Merkle Proof for a Node given its Hash. It returns the proof in the form of a slice of LRHash.
func (t MerkleTree) CalculateMerkleProof(targetHash hash.Hash) ([]hash.LRHash, error) {
	// first check whether given hash exists in the tree (reverse BFS from leafs to root, reverse because you usually search for a leaf here)
	
	// iterate over slice and remember targetNodeIndex and Node if found
	targetNodeIndex := -1
	var targetNode *Node

	for index, node := range t.nodes {
		if node.Value.GetString() == targetHash.GetString() {
			targetNodeIndex = index
			targetNode = node
		}
	}

	// check whether node was found
	if targetNodeIndex == -1 {
		return nil, errors.New("CalculateMerkleProof(): Target node not found.")
	}

	// recursively get sibling node hashes (from bottom (target node) to top (tree root))
	merkleProof := t.RecursivelyGetSiblingHashes(targetNode, []hash.LRHash{} )
	return merkleProof, nil

}

// VerifyMerkleProof takes a transaction id (which is a Hash) and a Merkle Proof and verifies that the given transaction is part of the Merkle Tree.
// It only verifies that this could be a correct Merkle Proof, but a node still needs to verify this by requesting from other nodes that the Merkle Tree root itself is correct.
func VerifyMerkleProof(targetHash hash.Hash, merkleProof []hash.LRHash) bool {
	proofLength := len(merkleProof)-1

	for i:=0; i<proofLength; i+=1 {	// < instead of <= because the last value of proof must be reached without interacting with it
		// determine order in which you need to combine and rehash (targetHash + proofValue VS proofValue + targetHash)
		if  merkleProof[i].IsLeft {
			targetHash = hash.Concatenate(merkleProof[i].Value, targetHash)
		} else {
			targetHash = hash.Concatenate(targetHash, merkleProof[i].Value)
		}
	}

	if targetHash.GetString() == merkleProof[proofLength].Value.GetString() {
		return true
	} else {
		return false
	}
}

// RecursivelyGetSiblingHashes is a method of MerkleTree that is used to access sibling hashes of a node, which is needed for making Merkle Proofs.
func (t MerkleTree) RecursivelyGetSiblingHashes(targetNode *Node, merkleProof []hash.LRHash) []hash.LRHash {
	
	// check if root node has been reached
	if targetNode.Parent != nil {
		// get child hashes (one of these will be equal to )
		leftChildHash := targetNode.Parent.Left.Value
		rightChildHash := targetNode.Parent.Right.Value

		// check if leftChildHash is the node we are currently looking at
		if leftChildHash.GetString() == targetNode.Value.GetString() {
			// if we are here its clear that the other node must be added to merkle proof (either the other node is the same or this one or it is a true sibling)
			
			// retain Left/Right info for the proof
			lrHash := hash.LRHash{
				Value:	rightChildHash,
				IsLeft:	false,
			}
			
			merkleProof = append(merkleProof, lrHash)
		} else {
			
			lrHash := hash.LRHash{
				Value:	leftChildHash,
				IsLeft:	true,
			}

			merkleProof = append(merkleProof, lrHash)
		}
		// do the same but the parent of the previous node
		return t.RecursivelyGetSiblingHashes(targetNode.Parent, merkleProof)

	} else {
		// we have reached the root, add it to the proof (a root by my definition is assumed to be a LeftNode)
		lrHash := hash.LRHash{
				Value:	targetNode.Value,
				IsLeft:	true,
		}

		merkleProof = append(merkleProof, lrHash)
		return merkleProof
	}


}

// GetSiblingHash is a method of MerkleTree that gets the sibling hash of a node. This is useful for building Merkle Trees and making Merkle Proofs.
func (t MerkleTree) GetSiblingHash (targetHash hash.Hash) (hash.Hash, error) {
	targetHashString := targetHash.GetString()		// cast hash to string because you are not allowed to compare []byte

	// first check whether input hash is equal to root hash, then return error because the root is the only node without sibling
	if targetHashString == t.GetRootHash().GetString() {
		return targetHash, errors.New("GetSiblingHash(): The root node does not have a sibling.")
	}

	// iterate over tree in reverse order, check children nodes, if targetHash matches a children node the other child is what we have been searching
	nodeAmount := len(t.nodes)
	for i:=nodeAmount-1; i>=0; i-=1 {
		leftChildHash := t.nodes[i].Left.Value.GetString()
		rightChildHash := t.nodes[i].Right.Value.GetString()
		if leftChildHash == targetHashString {
			return t.nodes[i].Right.Value, nil
		} else if rightChildHash == targetHashString {
			return t.nodes[i].Left.Value, nil
		}
	}

	// targetHash does not exist in the tree
	return targetHash, errors.New("GetSiblingHash(): Hash was not found in the tree.")
}

// GetChildren is a method of MerkleTree that returns pointers to the children nodes of a node with index index.
func (t MerkleTree) GetChildren(index int) (*Node, *Node, error) {
	if len(t.nodes) <= index || index < 0 {
		return nil, nil, errors.New("GetChildren(): Invalid index.")
	}
	return t.nodes[index].Left, t.nodes[index].Right, nil
}

// GetNodeList is a method of MerkleTree that returns a slice that contains pointers to Nodes.
func (t MerkleTree) GetNodeList() []*Node {
	return t.nodes
}

// GetRootHash is a method of MerkleTree that returns the roothash (which is just a Hash) of a Merkle Tree.
func (t MerkleTree) GetRootHash() hash.Hash {
	return t.roothash
}

// PrintRootHash is a method of MerkleTree that prints the roothash.
func (t MerkleTree) PrintRootHash() {
	t.roothash.PrintString()
}

// PrintTree is a method of MerkleTree that prints the entire tree.
func (t MerkleTree) PrintTree() {
	logger.L.Printf("PrintTree - This is the state tree I built:\n")
	
	for i, n := range t.nodes {
		// index
		logger.L.Printf("Node Index: %v", i)
		// hash
		logger.L.Printf("Node %v", n.Value.GetString())
		// children (if they exist)
		if n.Left != nil {
			logger.L.Printf("Child Left: %v", n.Left.Value.GetString())
			logger.L.Printf("Child Right: %v", n.Right.Value.GetString())
		}
		// parent (if it exists)
		if n.Parent != nil {
			logger.L.Printf("Parent: %v", n.Parent.Value.GetString())
		}
	}

}
