//go:build !darwin

package main

import "syscall"

// selectReady calls syscall.Select and returns true if data is ready.
// On linux and others, syscall.Select returns (int, error).
func selectReady(nfd int, readfds *syscall.FdSet, timeout *syscall.Timeval) (bool, error) {
	n, err := syscall.Select(nfd, readfds, nil, nil, timeout)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
