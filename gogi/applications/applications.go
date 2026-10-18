// Package applications lets one gogi serve many applications, e.g. qubit-notes and xact-marine.
// Every call to the gateway carries a key of the application it is for; the gateway passes the
// application on to the services in the x-gogi-application header. A service keeps the names an
// application gives its indexes, sessions, users and credentials qualified by the application,
// "qubit-notes.maria--aeronautics", so two applications can use the same names without meeting,
// and gives them back as the application gave them.
//
// Run without keys, as in development, every call is for the Default application, whose names
// are kept as they are given.
package applications

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/grpc/metadata"
)

const (
	// Header carries the application of a call from the gateway to the services
	Header = "x-gogi-application"
	// Default is the application of every call when gogi runs without keys
	Default = "default"

	separator = "."
)

// lowercase letters, digits and dashes, so a qualified name can be split at its first "."
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// CheckName checks a name for a new application
func CheckName(name string) error {
	if name == Default {
		return fmt.Errorf("%q is the application of calls made without keys and cannot be registered", Default)
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("an application's name is 3 to 40 lowercase letters, digits and dashes, "+
			"starting with a letter, not %q", name)
	}
	return nil
}

type contextKey struct{}

// WithApplication is the context of a call for the application
func WithApplication(ctx context.Context, application string) context.Context {
	return context.WithValue(ctx, contextKey{}, application)
}

// FromContext is the application a call is for: the one the service's interceptor found, or the
// one in the call's header, or Default
func FromContext(ctx context.Context) string {
	if application, ok := ctx.Value(contextKey{}).(string); ok && application != "" {
		return application
	}
	if application := fromHeader(ctx); application != "" {
		return application
	}
	return Default
}

func fromHeader(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if values := md.Get(Header); len(values) > 0 {
		return values[0]
	}
	return ""
}

// OutgoingContext sends the call's application on with the calls a service makes for it
func OutgoingContext(ctx context.Context, application string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, Header, application)
}

// Qualify is the name as the platform keeps it for the application
func Qualify(application, name string) string {
	if application == Default || name == "" {
		return name
	}
	return application + separator + name
}

// Unqualify is the name as the application gave it
func Unqualify(application, name string) string {
	if application == Default {
		return name
	}
	return strings.TrimPrefix(name, application+separator)
}

// Owns tells whether a name the platform keeps, found by something other than the name, e.g. an
// index found by its id, is the application's. Everything is the Default application's: it is
// gogi run without keys, for one application
func Owns(application, kept string) bool {
	if application == Default {
		return true
	}
	return strings.HasPrefix(kept, application+separator)
}
