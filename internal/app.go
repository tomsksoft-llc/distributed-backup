package internal

import (
	"context"
	"os"
	ossignal "os/signal"
	"sync"
	"syscall"

	"distributed-backup/pkg/crypto"
	"distributed-backup/pkg/filemanager"
	"distributed-backup/pkg/log"
	"distributed-backup/pkg/passwordmanager"
	"distributed-backup/pkg/peer"
	"distributed-backup/pkg/signal"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
)

type App struct {
	encryptionMode bool
	password1      string
	password2      string
	sessionUUID    string
	instanceUUID   string
	stunServers    []string
	apiKey         string
	zipDir         bool
	sourceEntry    string
	outputFilename string
	destinationDir string
	fileVersions   uint16
	passwordFile   string

	passwordManager *passwordmanager.LocalSaver
	crypto          *crypto.AesCbc
	fileManager     *filemanager.Backupper
	peer            *peer.WebRTC
	signal          *signal.FileIo
}

func NewApp() *App {
	return &App{
		instanceUUID: uuid.New().String(),
	}
}

func (a *App) Setup() (err error) {
	a.parseCmdline()

	if len(a.passwordFile) != 0 {
		if err := a.setupPasswordManager(); err != nil {
			return err
		}
	}

	if a.encryptionMode {
		return nil
	}

	return a.setupBackupMode()
}

func (a *App) Run(ctx context.Context, cancel context.CancelFunc) error {
	if a.encryptionMode {
		return a.runEncryptionMode()
	}

	return a.runBackupMode(ctx, cancel)
}

func (a *App) parseCmdline() {
	// Options of the passwords encryption mode.
	pflag.BoolVarP(&a.encryptionMode, "encrypt", "e", false, "Run in the encryption mode to generate a persistent file with encrypted passwords (--password1, --password2) for further archiving in the backup mode")
	pflag.StringVarP(&a.password1, "password1", "1", "", "First-level (inner) zip password")
	pflag.StringVarP(&a.password2, "password2", "2", "", "Second-level (outer) zip password")

	// Common options of the backup mode.
	pflag.StringVarP(&a.sessionUUID, "uuid", "u", "", "Common UUID (session ID) for a pair of candidates that are expected to establish a peer-to-peer connection")
	pflag.StringSliceVarP(&a.stunServers, "stun", "S", []string{"stun.l.google.com:19302"}, "List of used STUN servers")
	pflag.StringVarP(&a.apiKey, "apikey", "a", "", "FILE.io API key for signaling (see: https://www.file.io/)")

	// Sender's options of the backup mode.
	pflag.BoolVarP(&a.zipDir, "zipdir", "z", false, "Zip directory that is required to be sent to another peer")
	pflag.StringVarP(&a.sourceEntry, "srcentry", "s", "", "Source file/directory that is required to be sent to another peer")
	pflag.StringVarP(&a.outputFilename, "outfile", "o", "", "Output filename zipping a source directory that will be sent as a result")

	// Receiver's options of the backup mode.
	pflag.StringVarP(&a.destinationDir, "dstdir", "d", "", "Destination directory where to store files received from another peer")
	pflag.Uint16VarP(&a.fileVersions, "versions", "v", 1, "Number of backup versions of received files with the same name")

	// Common options.
	pflag.StringVarP(&a.passwordFile, "passfile", "p", "", "Path to a file where encrypted passwords are saved to or taken from (see: --encrypt)")

	pflag.Parse()
}

func (a *App) setupPasswordManager() (err error) {
	a.crypto, err = crypto.NewAesCbc(crypto.AesCbcConfig{
		// NOTE: The preset Key and IV values should be replaced with your own ones.
		Key: []byte("AES-128-key-1234"),
		IV:  []byte("IV-1234567890123"),
	})
	if err != nil {
		return errors.Wrap(err, "password manager crypto")
	}

	a.passwordManager = passwordmanager.NewLocalSaver(passwordmanager.LocalSaverConfig{
		PasswordFile: a.passwordFile,
	}, a.crypto)

	return nil
}

func (a *App) setupBackupMode() (err error) {
	a.signal, err = signal.NewFileIo(signal.FileIoConfig{
		APIKey:     a.apiKey,
		SessionID:  a.sessionUUID,
		InstanceID: a.instanceUUID,
	})
	if err != nil {
		return errors.Wrap(err, "signaling")
	}

	a.peer, err = peer.NewWebRTC(peer.WebRTCConfig{
		STUN: a.stunServers,
	}, a.signal)
	if err != nil {
		return errors.Wrap(err, "peer connection")
	}

	var (
		password1 string
		password2 string
	)

	if a.passwordManager != nil {
		password1, password2, err = a.passwordManager.GetPasswords()
		if err != nil {
			return errors.Wrap(err, "password manager")
		}
	}

	a.fileManager, err = filemanager.NewBackupper(filemanager.BackupperConfig{
		ZipDir:         a.zipDir,
		SourceEntry:    a.sourceEntry,
		DestinationDir: a.destinationDir,
		OutputFilename: a.outputFilename,
		Versions:       a.fileVersions,
		Password1:      password1,
		Password2:      password2,
	}, a.peer)
	if err != nil {
		return errors.Wrap(err, "file manager")
	}

	return nil
}

func (a *App) runEncryptionMode() error {
	err := a.passwordManager.SavePasswords(a.password1, a.password2)

	return errors.Wrap(err, "password manager")
}

func (a *App) runBackupMode(ctx context.Context, cancel context.CancelFunc) error {
	log.Infof("Starting Distributed Backup, Session UUID: %s, Instance UUID: %s", a.sessionUUID, a.instanceUUID)
	defer log.Info("Ending Distributed Backup")

	a.listenOS(cancel)

	if err := a.peer.Dial(); err != nil {
		return errors.Wrap(err, "peer connection")
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() {
		defer wg.Done()

		a.signal.Listen(ctx)
	}()

	select {
	case <-ctx.Done():
	case <-a.peer.Done():
	case <-a.fileManager.Done():
	}

	a.peer.Close()
	cancel()

	return nil
}

func (a *App) listenOS(cancel context.CancelFunc) {
	sigchan := make(chan os.Signal, 1)
	ossignal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigchan
		cancel()
	}()
}
