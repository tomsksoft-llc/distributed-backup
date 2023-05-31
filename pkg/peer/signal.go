package peer

type Signal interface {
	// Ping() is used to detect presence or absence of another candidate peer from
	// other side of signaling process to make decision whether to make an offer
	// themselves if another candidate peer is already there, or to wait one otherwise.
	//
	// It matters for that singaling implementation which is not full-fledged enough
	// to pair candidate peers so they are forced to perform some kind of initial
	// synchronization by themselves.
	//
	// In the case of another candidate peer's absence, Ping() should return the
	// "pkg/signal.ErrNoCandidatesFound" error or some other one that can be handled
	// appropriately.
	Ping() error

	SendSDP([]byte) error
	SendCandidate([]byte) error

	OnSDP(func([]byte))
	OnCandidate(func([]byte))
}
