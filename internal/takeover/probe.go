package takeover

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Listening says whether a process on this machine has a LISTEN socket on
// the TCP port. It never connects: the service being debugged does not
// accept() while it sits on a breakpoint, and a connect a second would fill
// its accept queue and make the tool believe it had stopped. A LISTEN socket
// stays while the process is frozen, which is exactly the state in which the
// takeover must hold.
func Listening(port int) bool {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-t").Output()
		return err == nil && strings.TrimSpace(string(out)) != ""
	case "linux":
		return procListening("/proc/net/tcp", port) || procListening("/proc/net/tcp6", port)
	default:
		out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-t").Output()
		return err == nil && strings.TrimSpace(string(out)) != ""
	}
}

// procListening reads a /proc/net/tcp table: state 0A is LISTEN, the local
// port is the hex after the colon of the second column.
func procListening(path string, port int) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	want := fmt.Sprintf("%04X", port)
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[3] != "0A" {
			continue
		}
		if i := strings.LastIndex(f[1], ":"); i >= 0 && strings.EqualFold(f[1][i+1:], want) {
			return true
		}
	}
	return false
}
