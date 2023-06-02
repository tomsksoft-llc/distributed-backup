package peer

import (
	"encoding/json"
	"sync"
	"time"

	"distributed-backup/pkg/log"
	"distributed-backup/pkg/signal"

	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v3"
	"github.com/pkg/errors"
)

type WebRTC struct {
	signal Signal

	conn        *webrtc.PeerConnection
	dataChannel datachannel.ReadWriteCloser

	candidates   []*webrtc.ICECandidate
	candidatesMx sync.Mutex

	shutdownChan     chan struct{}
	establishHandler func()
}

type WebRTCConfig struct {
	STUN []string
}

func NewWebRTC(cfg WebRTCConfig, signal Signal) (*WebRTC, error) {
	ice := make([]webrtc.ICEServer, len(cfg.STUN))

	for i, stun := range cfg.STUN {
		ice[i] = webrtc.ICEServer{
			URLs: []string{"stun:" + stun},
		}
	}

	settings := webrtc.SettingEngine{}

	settings.DetachDataChannels()
	settings.SetICETimeouts(15*time.Minute, 25*time.Second, 2*time.Second)

	api := webrtc.NewAPI(webrtc.WithSettingEngine(settings))

	conn, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: ice,
	})
	if err != nil {
		return nil, err
	}

	p := &WebRTC{
		signal:           signal,
		conn:             conn,
		shutdownChan:     make(chan struct{}),
		establishHandler: func() {},
	}

	p.signal.OnSDP(p.onSignalSDP)
	p.signal.OnCandidate(p.onSignalCandidate)

	p.conn.OnICECandidate(p.onConnICECandidate)
	p.conn.OnConnectionStateChange(p.onConnStateChange)

	return p, nil
}

func (p *WebRTC) Dial() error {
	if err := p.signal.Ping(); err != nil {
		if !errors.Is(err, signal.ErrNoCandidatesFound) {
			return err
		}

		log.Infof("%s, waiting...", err)

		return p.waitOffer()
	}

	log.Infof("candidate found, start connecting...")

	return p.offer()
}

func (p *WebRTC) Close() {
	if err := p.conn.Close(); err != nil {
		log.Error(err)

		return
	}
}

func (p *WebRTC) Done() <-chan struct{} {
	return p.shutdownChan
}

func (p *WebRTC) Read(payload []byte) (int, error) {
	return p.dataChannel.Read(payload)
}

func (p *WebRTC) Write(payload []byte) (int, error) {
	return p.dataChannel.Write(payload)
}

func (p *WebRTC) Shutdown() {
	if err := p.dataChannel.Close(); err != nil {
		log.Error(err)

		return
	}
}

func (p *WebRTC) OnEstablish(h func()) {
	p.establishHandler = h
}

func (p *WebRTC) onSignalSDP(payload []byte) {
	sdp := webrtc.SessionDescription{}

	if err := json.Unmarshal(payload, &sdp); err != nil {
		log.Error(err)

		return
	}

	if err := p.conn.SetRemoteDescription(sdp); err != nil {
		log.Error(err)

		return
	}

	if sdp.Type == webrtc.SDPTypeOffer {
		if err := p.onSignalSDPOffer(); err != nil {
			log.Error(err)

			return
		}
	}

	p.candidatesMx.Lock()
	defer p.candidatesMx.Unlock()

	for _, candidate := range p.candidates {
		payload := []byte(candidate.ToJSON().Candidate)

		if err := p.signalSendCandidate(payload); err != nil {
			log.Error(err)

			return
		}
	}
}

func (p *WebRTC) onSignalSDPOffer() error {
	answer, err := p.conn.CreateAnswer(nil)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(answer)
	if err != nil {
		return err
	}

	if err := p.signal.SendSDP(payload); err != nil {
		return err
	}

	return p.conn.SetLocalDescription(answer)
}

func (p *WebRTC) onSignalCandidate(payload []byte) {
	err := p.conn.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: string(payload),
	})
	if err != nil {
		log.Error(err)

		return
	}
}

func (p *WebRTC) onConnICECandidate(candidate *webrtc.ICECandidate) {
	if candidate == nil {
		return
	}

	p.candidatesMx.Lock()
	defer p.candidatesMx.Unlock()

	if p.conn.RemoteDescription() == nil {
		p.candidates = append(p.candidates, candidate)

		return
	}

	payload := []byte(candidate.ToJSON().Candidate)

	if err := p.signalSendCandidate(payload); err != nil {
		log.Error(err)

		return
	}
}

func (p *WebRTC) signalSendCandidate(payload []byte) error {
	if p.conn.ConnectionState() == webrtc.PeerConnectionStateClosed {
		return nil
	}

	return p.signal.SendCandidate(payload)
}

func (p *WebRTC) onConnStateChange(state webrtc.PeerConnectionState) {
	log.Info("connection state changed: ", state)

	if state == webrtc.PeerConnectionStateDisconnected ||
		state == webrtc.PeerConnectionStateFailed ||
		state == webrtc.PeerConnectionStateClosed {
		p.shutdownChan <- struct{}{}
	}
}

func (p *WebRTC) waitOffer() error {
	p.conn.OnDataChannel(func(channel *webrtc.DataChannel) {
		p.registerDataChannel(channel)
	})

	return nil
}

func (p *WebRTC) offer() error {
	dataChannel, err := p.conn.CreateDataChannel("data", nil)
	if err != nil {
		return err
	}

	p.registerDataChannel(dataChannel)

	offer, err := p.conn.CreateOffer(nil)
	if err != nil {
		return err
	}

	if err := p.conn.SetLocalDescription(offer); err != nil {
		return err
	}

	payload, err := json.Marshal(offer)
	if err != nil {
		return err
	}

	return p.signal.SendSDP(payload)
}

func (p *WebRTC) registerDataChannel(channel *webrtc.DataChannel) {
	channel.OnOpen(func() {
		var err error
		p.dataChannel, err = channel.Detach()
		if err != nil {
			log.Error(err)
		}

		p.establishHandler()
	})
}
