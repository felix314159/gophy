package database

import (
	"crypto/ed25519"
	"encoding/pem"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// ---- Private key read / write ----

// EncryptKeyAndWriteToFile takes a private ed25519 key, a password and a filepath string and writes the encrypted key in OpenSSH format to that location.
func EncryptKeyAndWriteToFile(privkey ed25519.PrivateKey, password string, outputLocation string, comment string) {
	// encrypt private key into OpenSSH format
	pwBytes := []byte(password)
	encryptedPEM, err := ssh.MarshalPrivateKeyWithPassphrase(privkey, comment, pwBytes)
	if err != nil {
		panic(fmt.Sprintf("encrypt - %v", err))
	}

	// check if file exists already, warn user that it will be deleted (technically truncated). in production maybe require explicit confirmation before doing this
	_, err = os.Stat(outputLocation)
	if err == nil { // this is useful for automated testing, change in production
		fmt.Printf("Warning: There already exists a keyfile at %v. It will be overwritten!\n", outputLocation)
	}

	// write pem to file
	//		open file
	file, err := os.Create(outputLocation)
    if err != nil {
        panic(fmt.Sprintf("Failed to create key file: %v\n", err))
    }
    defer file.Close()

    //		write to file
	err = pem.Encode(file, encryptedPEM)
	if err != nil {
		panic(fmt.Sprintf("Failed to write PEM key to file: %v\n", err))
	}

	// set file permission to 600 (otherwise tools like ssh-keygen will complain that permissions are too open and refuse to do anything)
	err = os.Chmod(outputLocation, 0600)
	if err != nil {
		panic(fmt.Sprintf("Failed to set private key file permission: %v", err))
	}

}

// ReadKeyFromFileAndDecrypt takes the password that the key was encrypted with and the location of the key and returns the decrypted ed25519.PrivateKey)
func ReadKeyFromFileAndDecrypt(password string, keyLocation string) ed25519.PrivateKey {
	// try to read encrypted key from file if it exists
	encryptedPEMBytes, err := os.ReadFile(keyLocation)
	if err != nil {
		panic(fmt.Sprintf("Failed to read the keyfile %v: %v", keyLocation, err))
	}

	// decrypt encrypted private key
	decryptedPrivInterface, err := ssh.ParseRawPrivateKeyWithPassphrase(encryptedPEMBytes, []byte(password))
	if err != nil {
		panic(err) // false password triggers: 'x509: decryption password incorrect'
	}

	// cast from interface to ed25519 key
	decryptedPrivPtr, ok := decryptedPrivInterface.(*ed25519.PrivateKey)
	if !ok {
		panic(fmt.Sprintf("Key is not of type ed25519.PrivateKey, instead it is of type: %T\n", decryptedPrivInterface))
	}

	return *decryptedPrivPtr
}
