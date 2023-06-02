// LocalSaver encrypts the first password (p1) and the second one (p2) using Crypto,
// and writes a result to a local file named PasswordFile (see: SavePasswords()).
//
// It also decrypts a value from PasswordFile using Crypto, and returns parsed p1
// and p2 (see: GetPasswords()).
//
// An encrypted value that is stored in a file named PasswordFile is presented as
// "${len(p1)}${p1}${len(p2)}${p2}" (see: writePassword() and readPassword()).

package passwordmanager

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
)

type LocalSaver struct {
	cfg LocalSaverConfig

	crypto Crypto
}

type LocalSaverConfig struct {
	PasswordFile string
}

func NewLocalSaver(cfg LocalSaverConfig, crypto Crypto) *LocalSaver {
	return &LocalSaver{
		cfg:    cfg,
		crypto: crypto,
	}
}

func (m *LocalSaver) SavePasswords(p1, p2 string) error {
	buf := &bytes.Buffer{}

	if err := m.writePassword(buf, p1); err != nil {
		return err
	}

	if err := m.writePassword(buf, p2); err != nil {
		return err
	}

	payload, err := io.ReadAll(buf)
	if err != nil {
		return err
	}

	encrypted := m.crypto.Encrypt(payload)

	return os.WriteFile(m.cfg.PasswordFile, encrypted, 0664)
}

func (m *LocalSaver) GetPasswords() (p1, p2 string, err error) {
	payload, err := os.ReadFile(m.cfg.PasswordFile)
	if err != nil {
		return "", "", err
	}

	decrypted, err := m.crypto.Decrypt(payload)
	if err != nil {
		return "", "", err
	}

	buf := bytes.NewBuffer(decrypted)

	p1, err = m.readPassword(buf)
	if err != nil {
		return "", "", err
	}

	p2, err = m.readPassword(buf)
	if err != nil {
		return "", "", err
	}

	return p1, p2, nil
}

func (m *LocalSaver) writePassword(w io.Writer, password string) error {
	b := []byte(password)
	length := uint8(len(b))

	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}

	return binary.Write(w, binary.BigEndian, b)
}

func (m *LocalSaver) readPassword(r io.Reader) (string, error) {
	var length uint8

	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return "", err
	}

	b := make([]byte, length)

	return string(b), binary.Read(r, binary.BigEndian, b)
}
