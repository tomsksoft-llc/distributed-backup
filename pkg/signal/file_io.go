// FileIo is a p2p signaling implementation that uses the free FILE.io file sharing
// service (see: https://www.file.io/). The signaling process here is that data
// such as Ping, SDP and ICE candidates are transferred between candidate peers
// as files which are temporary stored in FILE.io during p2p negotiation.
//
// It is required to use APIKey generated by the FILE.io service to view and download
// files that was uploaded by an account holding a key.
//
// Filenames and their contents have a specific format. A filename is presented
// as "${SessionID}_${fileIoFileContentType}_${InstanceID}.json" where SessionID
// is an identifier that is common for both candidate peers, fileIoFileContentType
// is one of the predefined values (see: type fileIoFileContentType), and InstanceID
// is a personal peers identifier to differ files' authors within a session.
//
// File content is presented as a JSON structure with two fields "type" and "payload"
// where type is one of the predefined values (see: type fileIoFileContentType), and
// payload is data corresponding to a content type.

package signal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"distributed-backup/pkg/log"
	"distributed-backup/pkg/sync"

	"github.com/pkg/errors"
)

type FileIo struct {
	cfg FileIoConfig

	requestMx sync.UnlockDelayMutex

	sdpHandler       func([]byte)
	candidateHandler func([]byte)
}

type FileIoConfig struct {
	APIKey     string
	SessionID  string
	InstanceID string
}

func NewFileIo(cfg FileIoConfig) (*FileIo, error) {
	if len(cfg.APIKey) == 0 {
		return nil, errors.New("API key is empty")
	}

	if len(cfg.SessionID) == 0 {
		return nil, errors.New("session ID is empty")
	}

	if len(cfg.InstanceID) == 0 {
		return nil, errors.New("instance ID is empty")
	}

	return &FileIo{
		cfg:              cfg,
		sdpHandler:       func([]byte) {},
		candidateHandler: func([]byte) {},
	}, nil
}

func (s *FileIo) Listen(ctx context.Context) {
	// Sniffing requests' frequency limitation.
	ticker := time.NewTicker(5000 * time.Millisecond)

OUTER:
	for {
		select {
		case <-ticker.C:
			if err := s.sniffCandidates(); err != nil {
				log.Error(err)
			}
		case <-ctx.Done():
			break OUTER
		}
	}

	s.cleanUp()
}

func (s *FileIo) Ping() error {
	files, err := s.findPing()
	if err != nil {
		return err
	}

	if len(files.Nodes) == 0 {
		if err := s.uploadPing(); err != nil {
			return err
		}

		return ErrNoCandidatesFound
	}

	for _, node := range files.Nodes {
		if err := s.deleteFile(node.Key); err != nil {
			log.Error(err)
		}
	}

	return nil
}

func (s *FileIo) SendSDP(payload []byte) error {
	return s.uploadSDP(payload)
}

func (s *FileIo) SendCandidate(payload []byte) error {
	return s.uploadCandidate(payload)
}

func (s *FileIo) OnSDP(h func([]byte)) {
	s.sdpHandler = h
}

func (s *FileIo) OnCandidate(h func([]byte)) {
	s.candidateHandler = h
}

type fileIoFiles struct {
	Nodes []struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"nodes"`
}

type fileIoFileContent struct {
	Type    fileIoFileContentType `json:"type"`
	Payload []byte                `json:"payload,omitempty"`
}

type fileIoFileContentType string

const (
	fileIoFileContentTypePing      fileIoFileContentType = "ping"
	fileIoFileContentTypeSDP                             = "sdp"
	fileIoFileContentTypeCandidate                       = "candidate"
)

func (s *FileIo) sniffCandidates() error {
	files, err := s.findFiles(s.cfg.SessionID)
	if err != nil {
		return err
	}

	for _, node := range files.Nodes {
		if strings.Contains(node.Name, s.cfg.InstanceID) {
			continue
		}

		content, err := s.downloadFile(node.Key)
		if err != nil {
			log.Error(err)

			continue
		}

		switch content.Type {
		case fileIoFileContentTypeSDP:
			s.sdpHandler(content.Payload)
		case fileIoFileContentTypeCandidate:
			s.candidateHandler(content.Payload)
		default:
			break
		}
	}

	return nil
}

func (s *FileIo) cleanUp() {
	log.Info("cleaning up unused signaling files...")

	files, err := s.findFiles(s.cfg.SessionID)
	if err != nil {
		log.Error(err)
	}

	for _, node := range files.Nodes {
		if err := s.deleteFile(node.Key); err != nil {
			log.Error(err)
		}
	}
}

func (s *FileIo) findPing() (*fileIoFiles, error) {
	pattern := fmt.Sprintf("%s_%s", s.cfg.SessionID, fileIoFileContentTypePing)

	return s.findFiles(pattern)
}

func (s *FileIo) uploadPing() error {
	filename := fmt.Sprintf("%s_%s_%s.json", s.cfg.SessionID, fileIoFileContentTypePing, s.cfg.InstanceID)

	return s.uploadFile(filename, &fileIoFileContent{
		Type: fileIoFileContentTypePing,
	})
}

func (s *FileIo) uploadSDP(payload []byte) error {
	filename := fmt.Sprintf("%s_%s_%s.json", s.cfg.SessionID, fileIoFileContentTypeSDP, s.cfg.InstanceID)

	return s.uploadFile(filename, &fileIoFileContent{
		Type:    fileIoFileContentTypeSDP,
		Payload: payload,
	})
}

func (s *FileIo) uploadCandidate(payload []byte) error {
	filename := fmt.Sprintf("%s_%s_%s.json", s.cfg.SessionID, fileIoFileContentTypeCandidate, s.cfg.InstanceID)

	return s.uploadFile(filename, &fileIoFileContent{
		Type:    fileIoFileContentTypeCandidate,
		Payload: payload,
	})
}

func (s *FileIo) findFiles(pattern string) (*fileIoFiles, error) {
	urn := fmt.Sprintf("/?search=%s&sort=created:asc", pattern)
	headers := http.Header{
		"Accept": []string{"application/json"},
	}

	resp, err := s.request(http.MethodGet, urn, headers, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("response status: %s", resp.Status)
	}

	files := &fileIoFiles{}
	if err := json.NewDecoder(resp.Body).Decode(files); err != nil {
		return nil, err
	}

	return files, nil
}

func (s *FileIo) downloadFile(fileKey string) (*fileIoFileContent, error) {
	urn := fmt.Sprintf("/%s", fileKey)
	headers := http.Header{
		"Accept": []string{"*/*"},
	}

	resp, err := s.request(http.MethodGet, urn, headers, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return nil, errors.Errorf("response status: %s", resp.Status)
	}

	content := &fileIoFileContent{}

	if err := json.NewDecoder(resp.Body).Decode(content); err != nil {
		return nil, err
	}

	return content, nil
}

func (s *FileIo) deleteFile(fileKey string) error {
	urn := fmt.Sprintf("/%s", fileKey)
	headers := http.Header{
		"Accept": []string{"application/json"},
	}

	resp, err := s.request(http.MethodDelete, urn, headers, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusForbidden {
		return errors.Errorf("response status: %s", resp.Status)
	}

	return nil
}

func (s *FileIo) uploadFile(name string, content *fileIoFileContent) error {
	buf := bytes.Buffer{}
	w := multipart.NewWriter(&buf)

	const boundary = "---011000010111000001101001"

	err := w.SetBoundary(boundary)
	if err != nil {
		return err
	}

	file, err := w.CreateFormFile("file", name)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(file).Encode(content); err != nil {
		return err
	}

	if err := w.WriteField("expires", "10m"); err != nil {
		return err
	}

	if err := w.WriteField("maxDownloads", "1"); err != nil {
		return err
	}

	if err := w.WriteField("autoDelete", "true"); err != nil {
		return err
	}

	if err := w.Close(); err != nil {
		return err
	}

	headers := http.Header{
		"Accept":       []string{"application/json"},
		"Content-Type": []string{"multipart/form-data; boundary=" + boundary},
	}

	resp, err := s.request(http.MethodPost, "/", headers, &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("response status: %s", resp.Status)
	}

	return nil
}

func (s *FileIo) request(method, urn string, headers http.Header, body io.Reader) (resp *http.Response, err error) {
	const url = "https://file.io"

	req, err := http.NewRequest(method, url+urn, body)
	if err != nil {
		return nil, err
	}

	for k, values := range headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}

	req.Header.Add("Authorization", s.cfg.APIKey)

	// Requests' frequency limitation.
	s.requestMx.Lock()
	defer s.requestMx.DelayUnlock(2500 * time.Millisecond)

	for {
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			log.Info("too many requests, retrying...")

			// Requests' frequency reduction.
			time.Sleep(7500 * time.Millisecond)

			continue
		}

		break
	}

	return resp, err
}