// Package mydispatcher Package dispatcher implement the rate limiter and the online device counter
package mydispatcher

import "github.com/xtls/xray-core/features/routing"

//go:generate go run github.com/xtls/xray-core/common/errors/errorgen

// Type returns the feature type token for the dispatcher.
// This returns routing.DispatcherType() so that xray-core's mux.Server
// will use our custom dispatcher with rate limiting support.
func Type() interface{} {
	return routing.DispatcherType()
}
