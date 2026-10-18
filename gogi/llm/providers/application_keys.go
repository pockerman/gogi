package providers

import "context"

// ApplicationKeys returns the API key an application has stored for a built-in provider, e.g.
// its Anthropic key, or an error if it has none
type ApplicationKeys func(provider string) (string, error)

type applicationKeysContext struct{}

// WithApplicationKeys makes the built-in providers call their APIs with the keys of the
// application whose call it is, never the platform's own. Registered models keep the credentials
// they were registered with
func WithApplicationKeys(ctx context.Context, keys ApplicationKeys) context.Context {
	return context.WithValue(ctx, applicationKeysContext{}, keys)
}

// applicationKey is the key of the call's application for the provider; ok is false if the call
// is not an application's, and the provider uses its own key
func applicationKey(ctx context.Context, provider string) (key string, ok bool, err error) {
	keys, ok := ctx.Value(applicationKeysContext{}).(ApplicationKeys)
	if !ok || keys == nil {
		return "", false, nil
	}
	key, err = keys(provider)
	return key, true, err
}
