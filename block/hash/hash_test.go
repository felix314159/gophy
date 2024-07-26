package hash

import "testing"

func TestHash(t *testing.T) {
	hash1 := NewHash("mytest") 	// Keccak256: 27ee704fb289c445ae323e8309c0b7418392d61a075f7888c4fe711c3543c7d2
	hash2 := NewHash("mytest2") // Keccak256: 909b8c7a119b2dfb03bf39165a636491ee77c5578775ec553953cbc3a8c81fcf

	// GetString()
	if hash1.GetString() != "27ee704fb289c445ae323e8309c0b7418392d61a075f7888c4fe711c3543c7d2" {
		t.Errorf("hash failed test: Unexpected hash1 value: %v \n", hash1.GetString())
	}

	// Concatenate()
	hash3 := Concatenate(hash1, hash2)
	if hash3.GetString() != "22d9314bebc3adb9028008a18e4020670aa2d4e2819279331b84db1cce0f8bc8" {
		t.Errorf("hash failed test: Concatenate() produced unexpected result: %v \n", hash3.GetString())
	}

	// GetHashObjectWithoutHashing()
	hash4 := GetHashObjectWithoutHashing("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if hash4.GetString() != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("hash failed test: GetHashObjectWithoutHashing() produced unexpected result: %v \n", hash4.GetString())
	}
}
