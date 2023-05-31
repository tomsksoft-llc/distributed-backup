// Backupper sends a raw file named SourceEntry (if it is set) to Peer, or, if ZipDir
// is set, double ZIPs a source directory's content named SourceEntry, protects
// archives with corresponding passwords and sends an output file to Peer as well
// (see: sendSourceEntry()).
//
// It also receives a file from Peer and saves it to a destination directory named
// DestinationDir if it is set (see: receiveFile()).
//
// Saving a received file follows specific rules of versioning. If Versions value
// is greater than 1, other files with the same name get their names being appended
// by a version number: the older the file, the greater the value. If amount of
// files with the same name is already equal to Versions then the oldest one is
// deleted and others' version numbers are incremented (see: shiftFileVersions()).
//
// An inner archive contains all files and subdirectories from SourceEntry recursively
// and has the name of "${SourceEntry}.zip". It is protected with Password1.
//
// An outer archive contains the first archive only and has the name of OutputFilename.
// It is protected with Password2.
//
// Sent or received data is presented as "${len(filename)}${filename}${file_content}"
// (see: sendSourceDirArchived() and sendSourceFile()).

package filemanager

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"distributed-backup/pkg/log"

	"github.com/TelenLiu/go-zip"
	"github.com/pkg/errors"
)

type Backupper struct {
	cfg BackupperConfig

	peer Peer

	shutdownChan chan struct{}
}

type BackupperConfig struct {
	ZipDir         bool
	SourceEntry    string
	DestinationDir string
	OutputFilename string
	Versions       uint16
	Password1      string
	Password2      string
}

func NewBackupper(cfg BackupperConfig, peer Peer) (*Backupper, error) {
	// Incorrect path might be critical since the error would be given only after
	// a connection was already established.
	if len(cfg.DestinationDir) != 0 {
		fi, err := os.Stat(cfg.DestinationDir)
		if err != nil {
			return nil, err
		}

		if !fi.IsDir() {
			return nil, errors.Wrap(errNotDirectory, cfg.DestinationDir)
		}
	}

	// See above.
	if len(cfg.SourceEntry) != 0 {
		fi, err := os.Stat(cfg.SourceEntry)
		if err != nil {
			return nil, err
		}

		if cfg.ZipDir {
			if !fi.IsDir() {
				return nil, errors.Wrap(errNotDirectory, cfg.SourceEntry)
			}

			if len(cfg.OutputFilename) == 0 {
				return nil, errors.New("output filename is empty")
			}
		} else {
			if fi.IsDir() {
				return nil, errors.Wrap(errIsDirectory, cfg.SourceEntry)
			}
		}
	}

	m := &Backupper{
		cfg:          cfg,
		peer:         peer,
		shutdownChan: make(chan struct{}),
	}

	m.peer.OnEstablish(m.onEstablish)

	return m, nil
}

func (m *Backupper) Done() <-chan struct{} {
	return m.shutdownChan
}

func (m *Backupper) onEstablish() {
	if len(m.cfg.SourceEntry) != 0 {
		if err := m.sendSourceEntry(); err != nil {
			log.Error(err)
		} else {
			log.Info("file sent")
		}

		m.peer.Shutdown()

		if _, err := io.ReadAll(m.peer); err != nil {
			log.Error(err)
		}
	} else {
		if err := m.receiveFile(); err != nil {
			log.Error(err)
		} else {
			log.Info("file received")
		}
	}

	m.shutdownChan <- struct{}{}
}

func (m *Backupper) sendSourceEntry() error {
	if m.cfg.ZipDir {
		return m.sendSourceDirArchived()
	}

	return m.sendSourceFile()
}

func (m *Backupper) sendSourceDirArchived() error {
	if err := m.writeFilename(m.cfg.OutputFilename, m.peer); err != nil {
		return err
	}

	log.Info("sending file: ", m.cfg.OutputFilename)

	return m.sendSourceDirContentArchived()
}

func (m *Backupper) sendSourceDirContentArchived() error {
	fi, err := os.Stat(m.cfg.SourceEntry)
	if err != nil {
		return err
	}

	fh, err := zip.FileInfoHeader(fi)
	if err != nil {
		return err
	}

	fh.Name += ".zip"
	// Setting the zip.Store value as a compression method instead of zip.Deflate
	// for the second archive level makes more sense but it gives the io.ErrShortWrite
	// error in future trying to write a file from a source directory unlike the
	// standard Golang zip package. However the last one does NOT support archive
	// encryption.
	fh.Method = zip.Deflate
	fh.SetMode(fh.Mode() &^ fs.ModeDir)

	m.setArchivedFilePassword(fh, m.cfg.Password2)

	z2 := zip.NewWriter(m.peer)
	defer z2.Close()

	w, err := z2.CreateHeader(fh)
	if err != nil {
		return err
	}

	z1 := zip.NewWriter(w)
	defer z1.Close()

	log.Info("archiving directory: ", m.cfg.SourceEntry)

	return m.archiveDir(z1)
}

func (m *Backupper) archiveDir(z *zip.Writer) error {
	return filepath.Walk(m.cfg.SourceEntry, func(path string, fi fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if fi.IsDir() {
			return nil
		}

		fh, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(m.cfg.SourceEntry, path)
		if err != nil {
			return err
		}

		fh.Name = relPath
		fh.Method = zip.Deflate

		m.setArchivedFilePassword(fh, m.cfg.Password1)

		w, err := z.CreateHeader(fh)
		if err != nil {
			return err
		}

		return m.writeFile(path, w)
	})
}

func (m *Backupper) setArchivedFilePassword(fh *zip.FileHeader, password string) {
	if len(password) == 0 {
		return
	}

	fh.SetPassword(password)
	fh.SetEncryptionType(zip.StandardEncryption)
}

func (m *Backupper) sendSourceFile() error {
	name := filepath.Base(m.cfg.SourceEntry)

	if err := m.writeFilename(name, m.peer); err != nil {
		return err
	}

	log.Info("sending file: ", m.cfg.SourceEntry)

	return m.writeFile(m.cfg.SourceEntry, m.peer)
}

func (m *Backupper) writeFilename(name string, w io.Writer) error {
	b := []byte(name)
	length := uint8(len(b))

	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}

	return binary.Write(w, binary.BigEndian, b)
}

func (m *Backupper) writeFile(path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(w, f)

	return err
}

func (m *Backupper) receiveFile() error {
	var nameLen uint8

	if err := binary.Read(m.peer, binary.BigEndian, &nameLen); err != nil {
		return err
	}

	name := make([]byte, nameLen)

	if err := binary.Read(m.peer, binary.BigEndian, name); err != nil {
		return err
	}

	path := m.cfg.DestinationDir + string(os.PathSeparator) + string(name)

	m.shiftFileVersions(path)

	log.Info("receiving file: ", string(name))

	return m.saveFile(path)
}

func (m *Backupper) shiftFileVersions(path string) {
	oldestVersion := int(m.cfg.Versions) - 1

	for i := oldestVersion; i >= 0; i-- {
		oldVersionPath := path

		if i != 0 {
			oldVersionPath += fmt.Sprintf(".%d", i)
		}

		if _, err := os.Stat(oldVersionPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}

			log.Error(err)
		}

		if i == oldestVersion {
			if err := os.Remove(oldVersionPath); err != nil {
				log.Error(err)
			}

			continue
		}

		newVersionPath := path + fmt.Sprintf(".%d", i+1)

		if err := os.Rename(oldVersionPath, newVersionPath); err != nil {
			log.Error(err)
		}
	}
}

func (m *Backupper) saveFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, m.peer)

	return err
}
