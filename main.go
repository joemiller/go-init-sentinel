package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/op/go-logging"
	"github.com/ramr/go-reaper"
	"golang.org/x/sys/unix"
)

var (
	log *logging.Logger

	debug   = false
	version = "development"

	interval        time.Duration = 10 * time.Second
	shutdownTimeout time.Duration = 30 * time.Second
)

// watchFlags is a slice of strings defined by 1 or more '-watch' command line args
type watchFlags []string

func (w *watchFlags) String() string {
	return fmt.Sprintf("%s", *w)
}
func (w *watchFlags) Set(value string) error {
	*w = append(*w, value)
	return nil
}

func initLogger() *logging.Logger {
	l := logging.MustGetLogger("")
	be := logging.NewLogBackend(os.Stdout, "", 0)
	// Timestamp format is RFC3389
	f := logging.MustStringFormatter("[go-init-sentinel] %{time:2006-01-02T15:04:05.999999999Z07:00} - %{level:-8s}: %{message}")

	logging.SetBackend(be)
	logging.SetLevel(logging.INFO, "")
	logging.SetFormatter(f)

	return l
}

func main() {
	log = initLogger()

	var showVersion bool
	var watches watchFlags

	flag.Var(&watches, "watch", "File watch rule, ex: -watch=\"/path/to/file:SIGNAME\". Can be specified multiple times.")
	flag.BoolVar(&showVersion, "version", false, "Display go-init-sentinel version")
	flag.BoolVar(&debug, "debug", false, "Display debug log messages")
	flag.DurationVar(&interval, "interval", interval, "Interval to check files for changes.")
	flag.DurationVar(&shutdownTimeout, "stop-timeout", shutdownTimeout,
		"Grace period for the child process to shutdown before killing with SIGKILL")
	flag.Parse()

	if showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	if debug {
		logging.SetLevel(logging.DEBUG, "")
	}

	// TODO: customize help message to show passing a main command

	if flag.NArg() == 0 {
		log.Fatal("No main command defined, exiting")
	}
	mainCommand := flag.Args()

	cmd := exec.Command(mainCommand[0], mainCommand[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Setpgid = true

	done := make(chan struct{})
	defer close(done)

	// channels to allow our file watching sentinels to send signals to relay to the child process and signal errors
	var sentinelSigChs []<-chan syscall.Signal
	var sentinelErrChs []<-chan error
	for _, w := range watches {
		m := strings.Split(w, ":")
		if len(m) != 2 {
			log.Fatalf("Invalid watch rule '%s'. Format: '/file/path:SIGNAME'", w)
		}
		file := m[0]
		signal := m[1]
		if unix.SignalNum(signal) == 0 {
			log.Fatalf("Invalid watch rule '%s'. Signal '%s' is not valid", w, signal)
		}
		sigCh, errCh := newSignalSentinel(done, file, interval, unix.SignalNum(signal))
		sentinelSigChs = append(sentinelSigChs, sigCh)
		sentinelErrChs = append(sentinelErrChs, errCh)
	}
	// fan-in the channels from the sentinels
	sentinelSigCh := mergeSigCh(sentinelSigChs...)
	sentinelErrCh := mergeErrCh(sentinelErrChs...)

	// pid-1 has its responsibilities, one of them is being the goto reaper for orphaned children (zombies)
	go reaper.Start(reaper.Config{Pid: -1, Options: 0, DisablePid1Check: true})

	// listen for all signals so that we can forward them to our child
	signalCh := make(chan os.Signal, 100)
	defer close(signalCh)
	signal.Notify(signalCh)

	// start our wrapped command in a goroutine. The exit of the command will be signaled on the exitCh
	err := cmd.Start()
	if err != nil {
		log.Fatalf("unable to start command: %s", err)
	}
	exitCode := 0
	exitCh := make(chan error)
	go func() { exitCh <- cmd.Wait() }()

	// main loop
	for {
		select {
		case err := <-sentinelErrCh:
			log.Error(err)
			close(done)

			signalPID(cmd.Process.Pid, unix.SignalNum("SIGTERM"))

			go func() {
				select {
				case <-exitCh:
				case <-time.After(shutdownTimeout):
					log.Info("Timed out waiting for process to exit gracefully on SIGTERM. Sending SIGKILL")
					_ = cmd.Process.Kill()
				}
			}()

		case sig := <-sentinelSigCh:
			log.Debugf("sending sentinel signal %s to child process", unix.SignalName(sig))
			signalPID(cmd.Process.Pid, sig)

		case sig := <-signalCh:
			// ignore SIGCHLD, those are meant for go-init-sentinel
			// forward all other signals to our child
			if sig != syscall.SIGCHLD {
				log.Debugf("forwarding signal: %s (%d)", unix.SignalName(sig.(syscall.Signal)), sig)
				signalPID(cmd.Process.Pid, sig.(syscall.Signal))
			}

		case err := <-exitCh:
			// our child has exited.
			// if it returned an exit code pass it through to our parent by exiting
			if err != nil {
				if exiterr, ok := err.(*exec.ExitError); ok {
					exitCode = exiterr.Sys().(syscall.WaitStatus).ExitStatus()
					log.Debugf("command exited with code: %d", exitCode)
				}
			}
			os.Exit(exitCode)
		}
	}
}

func signalPID(pid int, sig syscall.Signal) {
	if err := syscall.Kill(pid, sig); err != nil {
		log.Warningf("unable to send signal: ", err)
	}
}

// mergeErrCh merges multiple channels of errors.
// Based on https://blog.golang.org/pipelines.
func mergeErrCh(cs ...<-chan error) <-chan error {
	var wg sync.WaitGroup
	// We must ensure that the output channel has the capacity to hold as many errors
	// as there are error channels. This will ensure that it never blocks
	out := make(chan error, len(cs))

	// Start an output goroutine for each input channel in cs.  output
	// copies values from c to out until c is closed, then calls wg.Done.
	output := func(c <-chan error) {
		for n := range c {
			out <- n
		}
		wg.Done()
	}
	wg.Add(len(cs))
	for _, c := range cs {
		go output(c)
	}

	// Start a goroutine to close out once all the output goroutines are
	// done.  This must start after the wg.Add call.
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func mergeSigCh(cs ...<-chan syscall.Signal) <-chan syscall.Signal {
	var wg sync.WaitGroup
	// We must ensure that the output channel has the capacity to hold as many errors
	// as there are error channels. This will ensure that it never blocks
	out := make(chan syscall.Signal, len(cs))

	// Start an output goroutine for each input channel in cs.  output
	// copies values from c to out until c is closed, then calls wg.Done.
	output := func(c <-chan syscall.Signal) {
		for n := range c {
			out <- n
		}
		wg.Done()
	}
	wg.Add(len(cs))
	for _, c := range cs {
		go output(c)
	}

	// Start a goroutine to close out once all the output goroutines are
	// done.  This must start after the wg.Add call.
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
