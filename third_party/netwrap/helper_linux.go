//go:build linux

package netwrap

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	helperRoleEnv   = "_NETWRAP_HELPER_ROLE"
	helperTokenEnv  = "_NETWRAP_HELPER_TOKEN"
	helperConfigEnv = "_NETWRAP_HELPER_CONFIG"
	helperRoleValue = "linux-setup-v1"
	helperSocketFD  = 3
	helperStatusFD  = 4
	helperExtraFD   = 5

	guestAddress = "10.0.2.100/24"
	gatewayIPv4  = "10.0.2.2"
	dnsIPv4      = "10.0.2.3"
)

type setupConfig struct {
	Command    []string `json:"command"`
	Dir        string   `json:"dir,omitempty"`
	Env        []string `json:"env"`
	MTU        int      `json:"mtu"`
	ExtraFiles int      `json:"extra_files"`
}

type setupMessage struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	TunName string `json:"tun_name,omitempty"`
}

// The package uses init because the importing executable must re-exec itself.
// Ordinary imports do nothing. The private role, random token, and inherited
// UNIX socket must all be present before helper code runs.
func init() {
	if os.Getenv(helperRoleEnv) != helperRoleValue {
		return
	}
	code, claimed := runSetupHelper()
	if claimed {
		os.Exit(code)
	}
}

func runSetupHelper() (int, bool) {
	token := os.Getenv(helperTokenEnv)
	encoded := os.Getenv(helperConfigEnv)
	if len(token) != 64 || encoded == "" || !isSeqpacketSocket(helperSocketFD) {
		return 0, false
	}
	if err := authenticateHelper(helperSocketFD, token); err != nil {
		fmt.Fprintf(os.Stderr, "netwrap setup authentication failed: %v\n", err)
		return 125, true
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return helperFailure(helperSocketFD, fmt.Errorf("decode setup config: %w", err)), true
	}
	var cfg setupConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return helperFailure(helperSocketFD, fmt.Errorf("read setup config: %w", err)), true
	}
	failureFD, err := setupAndExec(helperSocketFD, helperStatusFD, cfg)
	if err != nil {
		return helperFailure(failureFD, err), true
	}
	return 0, true
}

func isSeqpacketSocket(fd int) bool {
	typeValue, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	return err == nil && typeValue == unix.SOCK_SEQPACKET
}

func authenticateHelper(fd int, want string) error {
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(poll, 5000)
	if err != nil || n != 1 || poll[0].Revents&unix.POLLIN == 0 {
		return errors.New("private setup socket did not become ready")
	}
	buf := make([]byte, 128)
	n, _, _, _, err = unix.Recvmsg(fd, buf, nil, 0)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(buf[:n], []byte(want)) != 1 {
		return errors.New("private setup token does not match")
	}
	return nil
}

// setupAndExec returns the descriptor on which a failure should be reported.
// Before readiness this is the authenticated seqpacket control socket. After
// readiness it is a private CLOEXEC pipe that is not exposed to the command.
func setupAndExec(socketFD, statusFD int, cfg setupConfig) (int, error) {
	if len(cfg.Command) == 0 || cfg.MTU < 576 {
		return socketFD, errors.New("invalid setup config")
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return socketFD, fmt.Errorf("make mount propagation private: %w", err)
	}
	tunFD, tunName, err := createTUN(cfg.MTU)
	if err != nil {
		return socketFD, err
	}
	defer unix.Close(tunFD)
	if err := installResolver(); err != nil {
		return socketFD, err
	}
	if cfg.Dir != "" {
		if err := os.Chdir(cfg.Dir); err != nil {
			return socketFD, fmt.Errorf("change command directory: %w", err)
		}
	}
	path, err := lookPath(cfg.Command[0], cfg.Env)
	if err != nil {
		return socketFD, fmt.Errorf("find command %q: %w", cfg.Command[0], err)
	}
	// os/exec assigned the status pipe fd 4, but moving user ExtraFiles would
	// overwrite it. Duplicate it to a kernel-selected unused descriptor before
	// announcing successful setup. This keeps preservation failures on the
	// control protocol that the supervisor is still reading. F_DUPFD_CLOEXEC
	// makes later exec success observable as EOF.
	execStatusFD, err := sendSetupSuccess(socketFD, statusFD, tunFD, tunName)
	if err != nil {
		return socketFD, err
	}
	if err := waitReady(socketFD); err != nil {
		return execStatusFD, err
	}
	if err := unix.Close(tunFD); err != nil {
		return execStatusFD, fmt.Errorf("close setup TUN descriptor: %w", err)
	}
	if err := unix.Close(socketFD); err != nil {
		return execStatusFD, fmt.Errorf("close setup socket: %w", err)
	}
	if err := moveExtraFiles(cfg.ExtraFiles); err != nil {
		return execStatusFD, err
	}
	if err := markUnexpectedFilesCloseOnExec(cfg.ExtraFiles); err != nil {
		return execStatusFD, err
	}
	if err := dropNamespaceCapabilities(); err != nil {
		return execStatusFD, err
	}
	if err := syscall.Exec(path, cfg.Command, cfg.Env); err != nil {
		return execStatusFD, err
	}
	panic("unreachable")
}

func sendSetupSuccess(socketFD, statusFD, tunFD int, tunName string) (int, error) {
	execStatusFD, err := unix.FcntlInt(uintptr(statusFD), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return -1, fmt.Errorf("preserve command execution status descriptor: %w", err)
	}
	if err := unix.Close(statusFD); err != nil {
		unix.Close(execStatusFD)
		return -1, fmt.Errorf("close original command execution status descriptor: %w", err)
	}
	msg, _ := json.Marshal(setupMessage{OK: true, TunName: tunName})
	if err := unix.Sendmsg(socketFD, msg, unix.UnixRights(tunFD), nil, 0); err != nil {
		unix.Close(execStatusFD)
		return -1, fmt.Errorf("send TUN file descriptor: %w", err)
	}
	return execStatusFD, nil
}

func createTUN(mtu int) (int, string, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", fmt.Errorf("open /dev/net/tun: %w", err)
	}
	fail := func(err error) (int, string, error) {
		unix.Close(fd)
		return -1, "", err
	}
	ifr, err := unix.NewIfreq("netwrap0")
	if err != nil {
		return fail(fmt.Errorf("make TUN request: %w", err))
	}
	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI)
	if err := unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr); err != nil {
		return fail(fmt.Errorf("create TUN device: %w", err))
	}
	name := ifr.Name()
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fail(fmt.Errorf("find TUN device %s: %w", name, err))
	}
	if err := netlink.LinkSetMTU(link, mtu); err != nil {
		return fail(fmt.Errorf("set TUN MTU: %w", err))
	}
	addr, err := netlink.ParseAddr(guestAddress)
	if err != nil {
		return fail(fmt.Errorf("parse private address: %w", err))
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fail(fmt.Errorf("add private address: %w", err))
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fail(fmt.Errorf("bring TUN device up: %w", err))
	}
	loopback, err := netlink.LinkByName("lo")
	if err != nil {
		return fail(fmt.Errorf("find private loopback: %w", err))
	}
	loopbackAddress, err := netlink.ParseAddr("127.0.0.1/8")
	if err != nil {
		return fail(fmt.Errorf("parse private loopback address: %w", err))
	}
	if err := netlink.AddrAdd(loopback, loopbackAddress); err != nil && !errors.Is(err, syscall.EEXIST) {
		return fail(fmt.Errorf("add 127.0.0.1/8 to private loopback: %w", err))
	}
	if err := netlink.LinkSetUp(loopback); err != nil {
		return fail(fmt.Errorf("bring private loopback up: %w", err))
	}
	_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
	route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: defaultNet, Gw: net.ParseIP(gatewayIPv4)}
	if err := netlink.RouteAdd(route); err != nil {
		return fail(fmt.Errorf("add private default route: %w", err))
	}
	return fd, name, nil
}

func installResolver() error {
	contents := []byte("nameserver " + dnsIPv4 + "\noptions single-request\n")
	source, cleanup, err := createResolverSource("/tmp", contents)
	if err != nil {
		return err
	}
	defer cleanup()
	target, err := filepath.EvalSymlinks("/etc/resolv.conf")
	if err != nil {
		return fmt.Errorf("resolve /etc/resolv.conf: %w", err)
	}
	if err := unix.Mount(source, target, "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("install private resolver config: %w", err)
	}
	// The bind mount keeps the file alive after its host-visible name is gone.
	cleanup()
	return nil
}

func waitReady(fd int) error {
	buf := make([]byte, 32)
	n, _, _, _, err := unix.Recvmsg(fd, buf, nil, 0)
	if err != nil {
		return fmt.Errorf("wait for supervisor readiness: %w", err)
	}
	if string(buf[:n]) != "ready" {
		return errors.New("supervisor sent an invalid readiness message")
	}
	return nil
}

func moveExtraFiles(count int) error {
	// The helper control socket and status pipe consume fds 3 and 4, so os/exec
	// places user files at fd 5 onward.
	for i := 0; i < count; i++ {
		if err := unix.Dup3(helperExtraFD+i, 3+i, 0); err != nil {
			return fmt.Errorf("move permitted file descriptor %d: %w", i, err)
		}
	}
	return nil
}

func markUnexpectedFilesCloseOnExec(extraCount int) error {
	// CLOEXEC keeps the Go runtime stable during final setup. The kernel closes
	// these descriptors atomically when the command starts.
	if err := unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); err != nil {
		if err != unix.ENOSYS && err != unix.EINVAL {
			return fmt.Errorf("mark file descriptors close-on-exec: %w", err)
		}
		entries, readErr := os.ReadDir("/proc/self/fd")
		if readErr != nil {
			return fmt.Errorf("list open file descriptors: %w", readErr)
		}
		for _, entry := range entries {
			fd, parseErr := strconv.Atoi(entry.Name())
			if parseErr != nil || fd < 3 {
				continue
			}
			if _, setErr := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); setErr != nil && setErr != unix.EBADF {
				return fmt.Errorf("mark file descriptor %d close-on-exec: %w", fd, setErr)
			}
		}
	}
	for fd := 3; fd < 3+extraCount; fd++ {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, 0); err != nil {
			return fmt.Errorf("permit file descriptor %d: %w", fd, err)
		}
	}
	return nil
}

func dropNamespaceCapabilities() error {
	const (
		secureNoRoot               = 1 << 0
		secureNoRootLocked         = 1 << 1
		secureNoSetUIDFixup        = 1 << 2
		secureNoSetUIDFixupLocked  = 1 << 3
		secureNoAmbientRaise       = 1 << 6
		secureNoAmbientRaiseLocked = 1 << 7
	)
	secureBits := secureNoRoot | secureNoRootLocked | secureNoSetUIDFixup |
		secureNoSetUIDFixupLocked | secureNoAmbientRaise | secureNoAmbientRaiseLocked
	if err := unix.Prctl(unix.PR_SET_SECUREBITS, uintptr(secureBits), 0, 0, 0); err != nil {
		return fmt.Errorf("lock securebits: %w", err)
	}
	raw, err := os.ReadFile("/proc/sys/kernel/cap_last_cap")
	if err != nil {
		return fmt.Errorf("read last Linux capability: %w", err)
	}
	lastCap, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return fmt.Errorf("parse last Linux capability: %w", err)
	}
	for capability := 0; capability <= lastCap; capability++ {
		if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capability), 0, 0, 0); err != nil {
			return fmt.Errorf("drop capability %d from bounding set: %w", capability, err)
		}
	}
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("clear ambient capabilities: %w", err)
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3, Pid: 0}
	data := [2]unix.CapUserData{}
	if err := unix.Capset(&header, &data[0]); err != nil {
		return fmt.Errorf("clear capability sets: %w", err)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no-new-privileges: %w", err)
	}
	if err := verifyCapabilitySets(); err != nil {
		return err
	}
	return nil
}

func verifyCapabilitySets() error {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return fmt.Errorf("verify capabilities: %w", err)
	}
	wanted := map[string]bool{"CapInh:": true, "CapPrm:": true, "CapEff:": true, "CapBnd:": true, "CapAmb:": true}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && wanted[fields[0]] {
			if strings.TrimLeft(fields[1], "0") != "" {
				return fmt.Errorf("capability removal check failed: %s", line)
			}
			delete(wanted, fields[0])
		}
	}
	if len(wanted) != 0 {
		return errors.New("capability removal check could not read every set")
	}
	return nil
}

func helperFailure(fd int, err error) int {
	sendHelperFailure(fd, err)
	fmt.Fprintf(os.Stderr, "netwrap setup: %v\n", err)
	return 125
}

func sendHelperFailure(fd int, err error) {
	message, _ := json.Marshal(setupMessage{Error: err.Error()})
	if isSeqpacketSocket(fd) {
		_ = unix.Sendmsg(fd, message, nil, nil, 0)
		return
	}
	// The late failure channel is a pipe. In particular, never reuse fd 3 here:
	// after readiness it may be a caller-provided ExtraFile.
	for len(message) != 0 {
		n, writeErr := unix.Write(fd, message)
		if writeErr != nil || n == 0 {
			break
		}
		message = message[n:]
	}
}

func lookPath(file string, env []string) (string, error) {
	if strings.ContainsRune(file, '/') {
		return file, nil
	}
	pathEnv := ""
	for _, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			pathEnv = strings.TrimPrefix(item, "PATH=")
		}
	}
	if pathEnv == "" {
		return exec.LookPath(file)
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, file)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}
