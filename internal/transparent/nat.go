// Package transparent holds the two operating-system pieces transparent mode
// needs: the packet-filter rules that send an application's outbound traffic
// into Faultline without the application knowing, and the lookup that asks the
// kernel where a redirected connection was originally going.
//
// Both are Linux-only, and deliberately fail with a sentence rather than a
// syscall number anywhere else. Nothing here proxies anything; the listeners
// that do live in internal/proxy/forward.
package transparent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

// Chain is the name of the chain Faultline writes its rules into. Everything
// goes in one chain of Faultline's own so that taking transparent mode down
// can never delete a rule somebody else put there.
const Chain = "FAULTLINE"

// Options say what the rules should do.
type Options struct {
	// HTTPPort and HTTPSPort are the listeners outbound 80 and 443 are
	// redirected to.
	HTTPPort, HTTPSPort int
	// ExemptUID is the user Faultline runs as. Its own connections to the
	// upstream leave the same network namespace and would otherwise be
	// redirected back into Faultline forever.
	ExemptUID int
}

// NAT installs and removes the redirect rules.
type NAT struct {
	opts Options
	// run executes one iptables invocation. Tests replace it; nothing else
	// should.
	run func(ctx context.Context, args ...string) error
}

// New checks the options. The iptables binary is found when the rules are
// installed rather than here, so that a missing binary is reported at the
// moment transparent mode is actually asked for.
func New(opts Options) (*NAT, error) {
	if opts.HTTPPort <= 0 || opts.HTTPSPort <= 0 {
		return nil, errors.New("transparent mode needs both redirect ports")
	}
	if opts.HTTPPort == opts.HTTPSPort {
		return nil, fmt.Errorf("transparent mode needs two different redirect ports, got %d twice", opts.HTTPPort)
	}
	return &NAT{opts: opts}, nil
}

// available finds the iptables binary the first time it is needed. A run
// already set is a test's, and is left alone.
func (n *NAT) available() error {
	if n.run != nil {
		return nil
	}
	path, err := exec.LookPath("iptables")
	if err != nil {
		return fmt.Errorf("transparent mode needs the iptables binary, and it is not on PATH: %w", err)
	}
	n.run = runner(path)
	return nil
}

// runner executes iptables, keeping whatever it complained about so the error
// names the rule rather than only the exit status.
func runner(path string) func(context.Context, ...string) error {
	return func(ctx context.Context, args ...string) error {
		// The path is the iptables binary this package looked up, and every
		// argument is built here from checked options; nothing from a request
		// or a rule reaches it.
		out, err := exec.CommandContext(ctx, path, args...).CombinedOutput() //nolint:gosec // fixed binary, arguments built in this file
		if err == nil {
			return nil
		}
		if len(out) > 0 {
			return fmt.Errorf("iptables %v: %w: %s", args, err, out)
		}
		return fmt.Errorf("iptables %v: %w", args, err)
	}
}

// rules are the rules of the chain, in the order they have to be appended:
// the exemptions first, so that neither Faultline's own traffic nor anything
// to the loopback reaches the redirects below them.
func (n *NAT) rules() [][]string {
	return [][]string{
		{"-t", "nat", "-A", Chain, "-m", "owner", "--uid-owner", strconv.Itoa(n.opts.ExemptUID), "-j", "RETURN"},
		{"-t", "nat", "-A", Chain, "-d", "127.0.0.0/8", "-j", "RETURN"},
		{"-t", "nat", "-A", Chain, "-p", "tcp", "--dport", "80", "-j", "REDIRECT", "--to-ports", strconv.Itoa(n.opts.HTTPPort)},
		{"-t", "nat", "-A", Chain, "-p", "tcp", "--dport", "443", "-j", "REDIRECT", "--to-ports", strconv.Itoa(n.opts.HTTPSPort)},
	}
}

// teardown is Remove's commands, in the order that leaves nothing behind: the
// jump out of OUTPUT first, so no traffic reaches the chain while it is being
// emptied.
func (n *NAT) teardown() [][]string {
	return [][]string{
		{"-t", "nat", "-D", "OUTPUT", "-p", "tcp", "-j", Chain},
		{"-t", "nat", "-F", Chain},
		{"-t", "nat", "-X", Chain},
	}
}

// Install puts the redirects in force. It takes any chain it finds down first,
// so restarting after a crash cannot leave two copies of every rule, and undoes
// its own work if a rule fails half way through.
func (n *NAT) Install(ctx context.Context) error {
	if err := n.available(); err != nil {
		return err
	}
	n.Remove(ctx)

	if err := n.run(ctx, "-t", "nat", "-N", Chain); err != nil {
		return fmt.Errorf("creating the %s chain: %w", Chain, err)
	}
	for _, rule := range n.rules() {
		if err := n.run(ctx, rule...); err != nil {
			n.Remove(ctx)
			return fmt.Errorf("installing the transparent redirects: %w", err)
		}
	}
	// Last, so that nothing is sent to a chain that is not finished yet.
	if err := n.run(ctx, "-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-j", Chain); err != nil {
		n.Remove(ctx)
		return fmt.Errorf("sending OUTPUT through the %s chain: %w", Chain, err)
	}
	return nil
}

// Remove takes the rules back out. It reports nothing, and has nothing to
// report: every step is expected to fail when transparent mode was never
// installed, which is how shutdown runs after a failed start, and a step that
// fails when it was installed leaves a rule an operator can see with
// `iptables -t nat -L FAULTLINE` and Faultline could not have fixed anyway.
func (n *NAT) Remove(ctx context.Context) {
	if n.available() != nil {
		return // without the binary, nothing was ever installed
	}
	for _, cmd := range n.teardown() {
		_ = n.run(ctx, cmd...)
	}
}
