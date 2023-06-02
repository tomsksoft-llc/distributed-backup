package passwordmanager

type Crypto interface {
	Encrypt([]byte) []byte
	Decrypt([]byte) ([]byte, error)
}
