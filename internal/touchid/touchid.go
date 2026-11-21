// Package touchid confirms it is the Mac's owner with Touch ID, through
// LocalAuthentication. Elsewhere NewAuthenticator returns
// errors.ErrUnsupported.
package touchid

import (
	"context"

	"github.com/egoist/mygo/native"
)

// Authenticator asks for the owner's fingerprint.
//
//mygo:native
type Authenticator interface {
	// Available reports whether the Mac has Touch ID with a fingerprint
	// enrolled, and may use it now.
	Available() bool
	// Authenticate asks the user to touch the sensor, saying reason. It
	// returns nil once Touch ID recognized them.
	Authenticate(ctx context.Context, reason string) error
}

var (
	// ErrCanceled is returned when the user dismissed the prompt or chose
	// the fallback.
	ErrCanceled = native.NewError("canceled")
	// ErrFailed is returned when Touch ID did not recognize the user, or
	// locked itself out after too many tries.
	ErrFailed = native.NewError("failed")
)
