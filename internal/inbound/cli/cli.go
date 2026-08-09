package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/QzCurious/seamless-cors/internal/version"
)

const usage = `Usage:
  seamless-cors install
  seamless-cors uninstall
  seamless-cors serve
  seamless-cors start
  seamless-cors stop [flags]
  seamless-cors status [flags]
  seamless-cors version
`

// Run translates one CLI invocation into calls to the appropriate inward
// module. It renders command failures to stderr; callers use the returned
// error only to select a nonzero process exit status.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return run(args, stdout, stderr, commandHandlers{
		install:   install,
		uninstall: func(stdout, stderr io.Writer) error { return uninstall(stdin, stdout, stderr) },
		serve:     serve,
		start:     func(stdout, stderr io.Writer) error { return start(stdin, stdout, stderr) },
		stop:      stop,
		status:    runStatus,
	})
}

type commandHandlers struct {
	install   func(io.Writer, io.Writer) error
	uninstall func(io.Writer, io.Writer) error
	serve     func(io.Writer, io.Writer) error
	start     func(io.Writer, io.Writer) error
	stop      func(io.Writer, io.Writer) error
	status    func(io.Writer, io.Writer) error
}

func run(args []string, stdout, stderr io.Writer, commands commandHandlers) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("missing command")
	}

	switch args[0] {
	case "install":
		if err := rejectUnexpectedArgs(stderr, "install", args[1:]); err != nil {
			return err
		}
		return reportCommandError(stderr, commands.install(stdout, stderr))
	case "uninstall":
		if err := rejectUnexpectedArgs(stderr, "uninstall", args[1:]); err != nil {
			return err
		}
		return reportCommandError(stderr, commands.uninstall(stdout, stderr))
	case "start":
		if len(args[1:]) > 0 {
			err := fmt.Errorf("start does not accept flags; edit upstreams.txt instead")
			fmt.Fprintln(stderr, err)
			return err
		}
		return reportCommandError(stderr, commands.start(stdout, stderr))
	case "serve":
		if err := rejectUnexpectedArgs(stderr, "serve", args[1:]); err != nil {
			return err
		}
		return reportCommandError(stderr, commands.serve(stdout, stderr))
	case "stop":
		return reportCommandError(stderr, commands.stop(stdout, stderr))
	case "status":
		return reportCommandError(stderr, commands.status(stdout, stderr))
	case "version":
		if err := rejectUnexpectedArgs(stderr, "version", args[1:]); err != nil {
			return err
		}
		fmt.Fprintln(stdout, version.Current())
		return nil
	default:
		err := fmt.Errorf("unknown command: %s", args[0])
		fmt.Fprintln(stderr, err)
		fmt.Fprint(stderr, usage)
		return err
	}
}

func rejectUnexpectedArgs(stderr io.Writer, command string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	err := fmt.Errorf("%s does not accept arguments: %s", command, strings.Join(args, " "))
	fmt.Fprintln(stderr, err)
	return err
}

func reportCommandError(stderr io.Writer, err error) error {
	if err != nil {
		fmt.Fprintln(stderr, err)
	}
	return err
}
