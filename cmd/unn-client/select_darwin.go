package main

import "syscall"

// selectReady calls syscall.Select and returns true if data is ready.
// On darwin, syscall.Select returns only an error.
func selectReady(nfd int, readfds *syscall.FdSet, timeout *syscall.Timeval) (bool, error) {
	err := syscall.Select(nfd, readfds, nil, nil, timeout)
	if err != nil {
		return false, err
	}
	return true, nil
}
