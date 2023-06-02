package signal

import (
	"github.com/pkg/errors"
)

// ErrNoCandidatesFound is the error returned by a signaling implementation if no
// candidate peer on other side of a signaling process is ready to negotiate yet.
var ErrNoCandidatesFound = errors.New("no sniffing candidate found")
