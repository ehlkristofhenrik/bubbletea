package tea

import (
	"io"
	"os/exec"
)

// execMsg is used internally to run an ExecCommand sent with Exec.
type execMsg struct {
	cmd ExecCommand
	fn  ExecCallback
}

// Exec is used to perform arbitrary I/O in a blocking fashion, effectively
// pausing the Program while execution is running and resuming it when
// execution has completed.
//
// Most of the time you'll want to use ExecProcess, which runs an exec.Cmd.
//
// For non-interactive i/o you should use a Cmd (that is, a tea.Cmd).
func Exec(c ExecCommand, fn ExecCallback) Cmd {
	return func() Msg {
		return execMsg{cmd: c, fn: fn}
	}
}

// ExecProcess runs the given *exec.Cmd in a blocking fashion, effectively
// pausing the Program while the command is running. After the *exec.Cmd exists
// the Program resumes. It's useful for spawning other interactive applications
// such as editors and shells from within a Program.
//
// To produce the command, pass an *exec.Cmd and a function which returns
// a message containing the error which may have occurred when running the
// ExecCommand.
//
//	type VimFinishedMsg struct { err error }
//
//	c := exec.Command("vim", "file.txt")
//
//	cmd := ExecProcess(c, func(err error) Msg {
//	    return VimFinishedMsg{err: err}
//	})
//
// Or, if you don't care about errors, you could simply:
//
//	cmd := ExecProcess(exec.Command("vim", "file.txt"), nil)
//
// For non-interactive i/o you should use a Cmd (that is, a tea.Cmd).
func ExecProcess(c *exec.Cmd, stdinProxy ReaderProxy, stdoutProxy WriterProxy, stderrProxy WriterProxy, fn ExecCallback) Cmd {
	return Exec(wrapExecCommand(c, stdinProxy, stdoutProxy, stderrProxy), fn)
}

// ExecCallback is used when executing an *exec.Command to return a message
// with an error, which may or may not be nil.
type ExecCallback func(error) Msg

// ExecCommand can be implemented to execute things in a blocking fashion in
// the current terminal.
type ExecCommand interface {
	Run() error
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
	GetProxies() (ReaderProxy, WriterProxy, WriterProxy)
}

type ReaderProxy struct {
	io.Reader
	From    io.Reader
	Handler func(b []byte, n int, err error)
}

func (s ReaderProxy) Read(b []byte) (n int, err error) {
	n, err = s.From.Read(b)
	s.Handler(b, n, err)
	return n, err
}

// Stdout proxy
type WriterProxy struct {
	io.Writer
	From    io.Writer
	Handler func(b []byte, n int, err error)
}

func (s WriterProxy) Write(b []byte) (n int, err error) {
	s.Handler(b, n, err)
	n, err = s.From.Write(b)
	return n, err
}

// wrapExecCommand wraps an exec.Cmd so that it satisfies the ExecCommand
// interface so it can be used with Exec.
func wrapExecCommand(c *exec.Cmd, stdinProxy ReaderProxy, stdoutProxy WriterProxy, stderrProxy WriterProxy) ExecCommand {
	return &OsExecCommand{
		Cmd:         c,
		StdinProxy:  stdinProxy,
		StdoutProxy: stdoutProxy,
		StderrProxy: stderrProxy,
	}
}

// osExecCommand is a layer over an exec.Cmd that satisfies the ExecCommand
// interface.
type OsExecCommand struct {
	*exec.Cmd
	StdinProxy  ReaderProxy
	StdoutProxy WriterProxy
	StderrProxy WriterProxy
}

func (c *OsExecCommand) GetProxies() (ReaderProxy, WriterProxy, WriterProxy) {
	return c.StdinProxy, c.StdoutProxy, c.StderrProxy
}

// SetStdin sets stdin on underlying exec.Cmd to the given io.Reader.
func (c *OsExecCommand) SetStdin(r io.Reader) {
	// If unset, have the command use the same input as the terminal.
	if c.Stdin == nil {
		c.Stdin = r
	}
}

// SetStdout sets stdout on underlying exec.Cmd to the given io.Writer.
func (c *OsExecCommand) SetStdout(w io.Writer) {
	// If unset, have the command use the same output as the terminal.
	if c.Stdout == nil {
		c.Stdout = w
	}
}

// SetStderr sets stderr on the underlying exec.Cmd to the given io.Writer.
func (c *OsExecCommand) SetStderr(w io.Writer) {
	// If unset, use stderr for the command's stderr
	if c.Stderr == nil {
		c.Stderr = w
	}
}

// exec runs an ExecCommand and delivers the results to the program as a Msg.
func (p *Program) exec(c ExecCommand, fn ExecCallback) {
	if err := p.ReleaseTerminal(); err != nil {
		// If we can't release input, abort.
		if fn != nil {
			go p.Send(fn(err))
		}
		return
	}

	stdinProxy, stdoutProxy, stderrProxy := c.GetProxies()
	stdinProxy.From = p.input
	stdoutProxy.From = p.output
	stderrProxy.From = p.output

	c.SetStdin(stdinProxy)
	c.SetStdout(stdoutProxy)
	c.SetStderr(stderrProxy)

	// Execute system command.
	if err := c.Run(); err != nil {
		_ = p.RestoreTerminal() // also try to restore the terminal.
		if fn != nil {
			go p.Send(fn(err))
		}
		return
	}

	// Have the program re-capture input.
	err := p.RestoreTerminal()
	if fn != nil {
		go p.Send(fn(err))
	}
}
