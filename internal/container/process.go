package container

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func ChildMain() {
	fmt.Println("Running in child process")

	cmd := os.Args[2]
	rootfsPath := ""
	if len(os.Args) > 3 {
		rootfsPath = os.Args[3]
	}

	if rootfsPath != "" {
		fmt.Printf("Setting up rootfs: %s\n", rootfsPath)
		if err := setupRootfs(rootfsPath); err != nil {
			fmt.Printf("Failed to setup rootfs: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
			fmt.Println("Error mounting proc:", err)
			os.Exit(1)
		}
	}

	if err := syscall.Sethostname([]byte("container")); err != nil {
		fmt.Println("Error setting hostname:", err)
		os.Exit(1)
	}

	if err := waitForNetwork(); err != nil {
		fmt.Println("Warning: Network setup incomplete:", err)
	} else {
		if err := setupDefaultRoute(); err != nil {
			fmt.Println("Warning: Failed to setup default route:", err)
		}
	}

	fmt.Printf("Executing command: %s\n", cmd)

	shellPath := "/bin/sh"
	if rootfsPath != "" {
		if _, err := os.Stat(shellPath); os.IsNotExist(err) {
			shellPath = "/bin/bash"
		}
	}

	if err := syscall.Exec(shellPath, []string{shellPath, "-c", cmd}, os.Environ()); err != nil {
		fmt.Println("Exec error:", err)
		os.Exit(1)
	}
}

func setupRootfs(rootfsPath string) error {
	if _, err := os.Stat(rootfsPath); os.IsNotExist(err) {
		return fmt.Errorf("rootfs path does not exist: %s", rootfsPath)
	}

	requiredDirs := []string{"proc", "sys", "dev", "tmp"}
	for _, dir := range requiredDirs {
		dirPath := filepath.Join(rootfsPath, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dirPath, err)
		}
	}

	procPath := filepath.Join(rootfsPath, "proc")
	if err := syscall.Mount("proc", procPath, "proc", 0, ""); err != nil {
		return fmt.Errorf("failed to mount /proc: %w", err)
	}

	sysPath := filepath.Join(rootfsPath, "sys")
	if err := syscall.Mount("sysfs", sysPath, "sysfs", 0, ""); err != nil {
		return fmt.Errorf("failed to mount /sys: %w", err)
	}

	devPath := filepath.Join(rootfsPath, "dev")
	if err := syscall.Mount("tmpfs", devPath, "tmpfs", syscall.MS_NOSUID|syscall.MS_STRICTATIME, "mode=755"); err != nil {
		return fmt.Errorf("failed to mount /dev: %w", err)
	}

	devNull := filepath.Join(rootfsPath, "dev", "null")
	os.Create(devNull)
	if err := syscall.Mknod(devNull, syscall.S_IFCHR|0666, makedev(1, 3)); err != nil {
		fmt.Printf("Warning: failed to create /dev/null: %v\n", err)
	}

	devZero := filepath.Join(rootfsPath, "dev", "zero")
	os.Create(devZero)
	if err := syscall.Mknod(devZero, syscall.S_IFCHR|0666, makedev(1, 5)); err != nil {
		fmt.Printf("Warning: failed to create /dev/zero: %v\n", err)
	}

	if err := syscall.Chroot(rootfsPath); err != nil {
		return fmt.Errorf("chroot failed: %w", err)
	}

	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir to / failed: %w", err)
	}

	fmt.Println("Rootfs setup complete")
	return nil
}

func makedev(major, minor uint32) int {
	return int((major << 8) | (minor & 0xff) | ((minor & 0xfff00) << 12))
}

func waitForNetwork() error {
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		link, err := netlink.LinkByName("eth0")
		if err == nil && link.Attrs().Flags&net.FlagUp != 0 {
			fmt.Println("Network interface eth0 is ready")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for eth0 interface")
}

func setupDefaultRoute() error {
	gateway := net.ParseIP("172.19.0.1")
	if gateway == nil {
		return fmt.Errorf("invalid gateway IP")
	}

	link, err := netlink.LinkByName("eth0")
	if err != nil {
		return fmt.Errorf("failed to get eth0 interface: %w", err)
	}

	_, defaultDst, _ := net.ParseCIDR("0.0.0.0/0")
	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       defaultDst,
		Gw:        gateway,
		Scope:     netlink.SCOPE_UNIVERSE,
	}

	if err := netlink.RouteAdd(route); err != nil {
		return fmt.Errorf("failed to add default route: %w", err)
	}

	fmt.Println("Default gateway configured: 172.19.0.1 via eth0")
	return nil
}

func ExecHelper() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: exec-helper <pid> <rootfs> <command>")
		os.Exit(1)
	}

	pidStr := os.Args[2]
	rootfsPath := os.Args[3]
	cmd := os.Args[4]

	// Enter namespaces in the correct order
	// Note: We skip PID namespace because we can't actually enter it after process creation
	// We enter: ipc, uts, net, mnt
	nsTypes := []struct {
		name string
		flag int
	}{
		{"ipc", unix.CLONE_NEWIPC},
		{"uts", unix.CLONE_NEWUTS},
		{"net", unix.CLONE_NEWNET},
		{"mnt", unix.CLONE_NEWNS},
	}

	for _, ns := range nsTypes {
		nsPath := fmt.Sprintf("/proc/%s/ns/%s", pidStr, ns.name)
		fd, err := os.Open(nsPath)
		if err != nil {
			fmt.Printf("Failed to open %s namespace: %v\n", ns.name, err)
			os.Exit(1)
		}

		if err := unix.Setns(int(fd.Fd()), ns.flag); err != nil {
			fmt.Printf("Failed to enter %s namespace: %v\n", ns.name, err)
			fd.Close()
			os.Exit(1)
		}
		fd.Close()
	}

	// Change root if rootfs is provided
	if rootfsPath != "" {
		if err := syscall.Chroot(rootfsPath); err != nil {
			fmt.Printf("Failed to chroot to %s: %v\n", rootfsPath, err)
			os.Exit(1)
		}
		if err := os.Chdir("/"); err != nil {
			fmt.Printf("Failed to chdir to /: %v\n", err)
			os.Exit(1)
		}
	}

	// Execute the command
	shellPath := "/bin/sh"
	if _, err := os.Stat(shellPath); os.IsNotExist(err) {
		shellPath = "/bin/bash"
	}

	if err := syscall.Exec(shellPath, []string{shellPath, "-c", cmd}, os.Environ()); err != nil {
		fmt.Printf("Failed to exec command: %v\n", err)
		os.Exit(1)
	}
}
