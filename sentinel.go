package main

import (
	"os"
	"syscall"
	"time"
)

func newSignalSentinel(done chan struct{}, file string, interval time.Duration, sig syscall.Signal) (<-chan syscall.Signal, <-chan error) {
	sigCh := make(chan syscall.Signal, 1)
	errCh := make(chan error, 1)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		initialStat, err := os.Stat(file)
		if err != nil {
			errCh <- err
		}

		for {
			select {

			case <-done:
				log.Debugf("sentinel '%s' shutting down", file)
				return

			case <-ticker.C:
				stat, err := os.Stat(file)
				if err != nil {
					errCh <- err
					continue
				}

				if stat.Size() != initialStat.Size() || stat.ModTime() != initialStat.ModTime() {
					log.Debugf("%s: change detected", file)
					sigCh <- sig
					initialStat = stat
				}
			}
		}
	}()

	return sigCh, errCh
}
