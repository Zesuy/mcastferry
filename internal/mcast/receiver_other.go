//go:build !linux

package mcast

import "errors"

func Open(Config) (Receiver, error) {
	return nil, errors.New("multicast receiver is supported only on Linux")
}
