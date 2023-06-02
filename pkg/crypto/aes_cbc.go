package crypto

import (
	"crypto/aes"
	"crypto/cipher"

	"github.com/zenazn/pkcs7pad"
)

type AesCbc struct {
	cfg AesCbcConfig

	cipher cipher.Block
}

type AesCbcConfig struct {
	Key []byte
	IV  []byte
}

func NewAesCbc(cfg AesCbcConfig) (*AesCbc, error) {
	cipher, err := aes.NewCipher(cfg.Key)
	if err != nil {
		return nil, err
	}

	return &AesCbc{
		cfg:    cfg,
		cipher: cipher,
	}, nil
}

func (c *AesCbc) Encrypt(payload []byte) []byte {
	payload = pkcs7pad.Pad(payload, c.cipher.BlockSize())

	encrypter := cipher.NewCBCEncrypter(c.cipher, c.cfg.IV)
	encrypted := make([]byte, len(payload))

	encrypter.CryptBlocks(encrypted, payload)

	return encrypted
}

func (c *AesCbc) Decrypt(payload []byte) ([]byte, error) {
	decrypter := cipher.NewCBCDecrypter(c.cipher, c.cfg.IV)
	decrypted := make([]byte, len(payload))

	decrypter.CryptBlocks(decrypted, payload)

	return pkcs7pad.Unpad(decrypted)
}
