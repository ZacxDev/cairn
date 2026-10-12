package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"
)

// aPortNothingIsListeningOn returns a loopback port that REFUSES a dial and that nothing else can
// bind until the test ends: a TCP socket BOUND to it and never put into LISTEN holds it.
//
// 🔴 IT IS A MEASURED ADDRESS RATHER THAN A CHOSEN NUMBER, and the difference is whether
// the case is deterministic. A hard-coded port is a bet that nothing on the machine running
// the suite is listening there — and the failure when that bet is lost is not a red test,
// it is a connection that SUCCEEDS against somebody else's service and then fails as a
// protocol error with a message nobody can act on.
//
// 🔴 AND IT IS HELD, NOT HANDED BACK CLOSED — RETRACTING "THE WINDOW IS STANDARD AND THE DIRECTION
// IS SAFE". The previous shape bound `:0`, closed it and returned the number, on the reasoning
// that a stranger taking the port in the window could only produce a noisy red. The noisy red was
// the defect: the kernel hands that just-freed port to the NEXT `:0` bind often enough to measure
// (38 in 200,000 pairs on one host), and the next `:0` bind in a presence case is the child's own
// browser listener, so "nothing answers on the agent port" failed against the child itself under
// load. A bound socket holds the port against an explicit bind and a `:0` choice alike — Go's
// listeners set `SO_REUSEADDR`, which does not let them share a port another socket has bound
// without it — while a SYN to a port with no listener still draws a reset, so the dial is
// refused for exactly as long as the port is held. `TestAPortNothingIsListeningOnCannotBeTakenWhileTheTestHoldsIt`
// pins both halves.
//
// ⚠ A CHILD TOLD TO LISTEN HERE CANNOT, so a case that hands this port to a child as a listen
// address asserts "never bound" by the child failing its bind ("cannot bind … Refusing to start"),
// not by a dial that connects. A child that must ANSWER binds `-port 0` and is reached through
// `presenceChild.serving`; this helper is only for an address that must NOT answer.
func aPortNothingIsListeningOn(t *testing.T) int {
	t.Helper()
	// ForkLock: between `Socket` and `CloseOnExec` a concurrent `exec` would otherwise inherit the
	// descriptor, and a child holding it would keep the port reserved past this test.
	syscall.ForkLock.RLock()
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		t.Fatalf("no socket could be created, so this case has no unreachable address to point at: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatalf("no ephemeral loopback port could be bound, so this case has no unreachable address: %v", err)
	}
	sa, err := syscall.Getsockname(fd)
	if err != nil {
		t.Fatalf("the bound socket has no readable address: %v", err)
	}
	in4, ok := sa.(*syscall.SockaddrInet4)
	if !ok || in4.Port == 0 {
		t.Fatalf("the bound socket reports %#v, not an IPv4 loopback port", sa)
	}
	port := in4.Port
	// A dial must fail, or the address is not the unreachable one this case needs.
	conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatalf("POSITIVE CONTROL FAILED: a dial to the held port 127.0.0.1:%d CONNECTED, so a "+
			"'connection refused' case pointed there would be measuring a successful connection", port)
	}
	var opErr *net.OpError
	if !errors.As(dialErr, &opErr) {
		t.Fatalf("the probe dial failed with %v, which is not a network error — the address cannot be "+
			"vouched for as refusing", dialErr)
	}
	return port
}

// TestAPortNothingIsListeningOnCannotBeTakenWhileTheTestHoldsIt pins the property the helper's
// callers depend on: for the whole test, the port REFUSES a dial AND cannot be bound by anyone
// else — including the `cairn-ui` child a case starts with `-port 0`.
//
// 🔴 THIS IS REGRESSION COVERAGE, RED ON THE PREVIOUS SHAPE. That helper bound `:0`, closed the
// listener and returned the number, so the port was free again the instant it returned: the
// kernel could hand it to the child's own `-port 0` browser listener, and
// `TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped` then dialled its "agent
// port", reached the child's BROWSER listener and failed "the agent listener is accepting". The
// explicit bind below is the deterministic form of that collision: the kernel's `:0` choice and an
// explicit bind are refused by the same conflict check, and the explicit one does not need luck.
//
// The positive control is a port closed the old way: the same probe MUST bind it, or a refused
// bind below is a fact about the probe rather than about the helper.
func TestAPortNothingIsListeningOnCannotBeTakenWhileTheTestHoldsIt(t *testing.T) {
	probe := func(port int) error {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = l.Close()
		}
		return err
	}

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	if err := probe(closedPort); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: a port closed by its listener could not be re-bound (%v), so "+
			"the probe cannot tell a held port from a free one", err)
	}

	port := aPortNothingIsListeningOn(t)
	if err := probe(port); err == nil {
		t.Fatalf("127.0.0.1:%d could be bound by another listener while this test holds it, so a case "+
			"that asserts NOTHING answers there can be answered by whatever binds it next — the "+
			"child's own `-port 0` listener included", port)
	}
}
