package container

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
)

func Run(command string, detach bool, rootfsPath string) (string, error) {
	if !detach {
		fmt.Println("Starting container with command:", command)
		if rootfsPath != "" {
			fmt.Printf("Using rootfs: %s\n", rootfsPath)
		}
	}

	rand.Seed(time.Now().UnixNano())
	containerID := fmt.Sprintf("container-%d", time.Now().UnixNano())
	containerName := fmt.Sprintf("cr-%d", rand.Intn(10000))

	randNum := rand.Intn(100000)
	vethName := fmt.Sprintf("veth%d", randNum)
	vethPeerName := fmt.Sprintf("vethp%d", randNum)
	containerIP := fmt.Sprintf("172.19.0.%d/24", rand.Intn(250)+2)
	containerIPStripped := containerIP[:len(containerIP)-3]

	cmd := exec.Command("/proc/self/exe", "child", command, rootfsPath)

	if !detach {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		logFile := filepath.Join("/var/lib/container-runtime/containers", fmt.Sprintf("%s.log", containerID))
		os.MkdirAll(filepath.Dir(logFile), 0755)
		logFd, err := os.Create(logFile)
		if err == nil {
			cmd.Stdout = logFd
			cmd.Stderr = logFd
		}
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
		Unshareflags: syscall.CLONE_NEWNS,
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("cmd start error: %w", err)
	}

	containerNetns := filepath.Join("/proc", fmt.Sprintf("%d", cmd.Process.Pid), "ns", "net")

	if err := waitForNetnsFile(containerNetns); err != nil {
		cmd.Process.Kill()
		return "", fmt.Errorf("netns file wait error: %w", err)
	}

	if !detach {
		fmt.Printf("Setting up network for container %s (PID: %d)\n", containerID, cmd.Process.Pid)
	}
	if err := setupContainerNetwork(containerNetns, vethName, vethPeerName, containerIP); err != nil {
		cleanupVeth(vethName)
		cmd.Process.Kill()
		return "", fmt.Errorf("network setup error: %w", err)
	}

	if !detach {
		fmt.Printf("Container network configured: %s\n", containerIP)
	} else {
		fmt.Fprintf(os.Stderr, "Network configured for container %s: %s\n", containerName, containerIPStripped)
	}

	container := &Container{
		ID:         containerID,
		Name:       containerName,
		Command:    command,
		Created:    time.Now(),
		State:      StateRunning,
		PID:        cmd.Process.Pid,
		RootfsPath: rootfsPath,
		NetworkNS:  containerNetns,
		IPAddress:  containerIPStripped,
		VethName:   vethName,
	}

	if err := SaveContainer(container); err != nil {
		fmt.Printf("Warning: failed to save container metadata: %v\n", err)
	}

	go monitorContainerProcess(containerID, cmd.Process.Pid, vethName)

	if detach {
		time.Sleep(500 * time.Millisecond)

		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			cleanupVeth(vethName)
			UpdateContainerState(containerID, StateStopped, 1)
			return "", fmt.Errorf("container process died immediately after start")
		}

		fmt.Printf("%s\n", containerID)
		fmt.Fprintf(os.Stderr, "Container %s started (PID: %d, IP: %s)\n", containerName, cmd.Process.Pid, containerIPStripped)
		return containerID, nil
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	waitChan := make(chan error, 1)
	go func() {
		waitChan <- cmd.Wait()
	}()

	select {
	case <-sigChan:
		fmt.Println("\nReceived interrupt signal, stopping container...")
		cmd.Process.Kill()
		cleanupVeth(vethName)
		UpdateContainerState(containerID, StateStopped, 130) // 130 = 128 + SIGINT
		return containerID, fmt.Errorf("container interrupted by user")
	case err := <-waitChan:
		cleanupVeth(vethName)
		if err != nil {
			UpdateContainerState(containerID, StateStopped, 1)
			return containerID, fmt.Errorf("cmd wait error: %w", err)
		}
		UpdateContainerState(containerID, StateExited, 0)
		return containerID, nil
	}
}

func waitForNetnsFile(netnsPath string) error {
	maxRetries := 50
	for i := 0; i < maxRetries; i++ {
		if _, err := os.Stat(netnsPath); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for netns file: %s", netnsPath)
}

func cleanupVeth(vethName string) {
    link, err := netlink.LinkByName(vethName)
    if err != nil {
        return
    }
    if err := netlink.LinkDel(link); err != nil {
        fmt.Printf("Warning: failed to cleanup veth %s: %v\n", vethName, err)
    }
}

func monitorContainerProcess(containerID string, pid int, vethName string) {
	process, err := os.FindProcess(pid)
	if err != nil {
		UpdateContainerState(containerID, StateStopped, 1)
		return
	}

	state, err := process.Wait()
	if err != nil {
		UpdateContainerState(containerID, StateStopped, 1)
		return
	}

	exitCode := 0
	if !state.Success() {
		exitCode = 1
	}

	UpdateContainerState(containerID, StateStopped, exitCode)
}

func Start(containerIDOrName string) error {
	containers, err := ListContainers()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainer *Container
	for _, c := range containers {
		if c.ID == containerIDOrName || c.Name == containerIDOrName {
			targetContainer = c
			break
		}
	}

	if targetContainer == nil {
		return fmt.Errorf("container not found: %s", containerIDOrName)
	}

	if targetContainer.State == StateRunning {
		return fmt.Errorf("container %s is already running", targetContainer.Name)
	}

	if targetContainer.RootfsPath == "" {
		return fmt.Errorf("container has no rootfs path")
	}

	if _, err := os.Stat(targetContainer.RootfsPath); os.IsNotExist(err) {
		return fmt.Errorf("container rootfs not found: %s", targetContainer.RootfsPath)
	}

	fmt.Printf("Starting container %s...\n", targetContainer.Name)
	fmt.Printf("Using existing rootfs: %s\n", targetContainer.RootfsPath)

	containerID, err := Run(targetContainer.Command, true, targetContainer.RootfsPath)
	if err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Delete old container metadata
	if err := DeleteContainer(targetContainer.ID); err != nil {
		fmt.Printf("Warning: failed to delete old container metadata: %v\n", err)
	}

	fmt.Printf("Container restarted with new ID: %s\n", containerID)
	return nil
}

func Stop(containerIDOrName string) error {
	containers, err := ListContainers()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainer *Container
	for _, c := range containers {
		if c.ID == containerIDOrName || c.Name == containerIDOrName {
			targetContainer = c
			break
		}
	}

	if targetContainer == nil {
		return fmt.Errorf("container not found: %s", containerIDOrName)
	}

	if targetContainer.State != StateRunning {
		return fmt.Errorf("container %s is not running (state: %s)", targetContainer.Name, targetContainer.State)
	}

	if targetContainer.PID == 0 {
		return fmt.Errorf("container %s has no valid PID", targetContainer.Name)
	}

	fmt.Printf("Stopping container %s (PID: %d)...\n", targetContainer.Name, targetContainer.PID)

	process, err := os.FindProcess(targetContainer.PID)
	if err != nil {
		UpdateContainerState(targetContainer.ID, StateStopped, 1)
		return fmt.Errorf("failed to find process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		if err := process.Kill(); err != nil {
			UpdateContainerState(targetContainer.ID, StateStopped, 1)
			return fmt.Errorf("failed to kill process: %w", err)
		}
		fmt.Println("Container forcefully killed")
	} else {
		fmt.Println("Sent SIGTERM to container, waiting for graceful shutdown...")
		time.Sleep(2 * time.Second)

		if err := process.Signal(syscall.Signal(0)); err == nil {
			fmt.Println("Container did not stop gracefully, forcing kill...")
			process.Kill()
		}
	}

	cleanupVeth(targetContainer.VethName)
	UpdateContainerState(targetContainer.ID, StateStopped, 0)

	fmt.Printf("Container %s stopped\n", targetContainer.Name)
	return nil
}

func Exec(containerIDOrName string, command string) error {
	containers, err := ListContainers()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainer *Container
	for _, c := range containers {
		if c.ID == containerIDOrName || c.Name == containerIDOrName {
			targetContainer = c
			break
		}
	}

	if targetContainer == nil {
		return fmt.Errorf("container not found: %s", containerIDOrName)
	}

	if targetContainer.State != StateRunning {
		return fmt.Errorf("container %s is not running (state: %s)", targetContainer.Name, targetContainer.State)
	}

	if targetContainer.PID == 0 {
		return fmt.Errorf("container %s has no valid PID", targetContainer.Name)
	}

	// Use nsenter to enter container namespaces
	// nsenter -t <pid> -m -u -i -n -p chroot <rootfs> /bin/sh -c <command>
	pidStr := fmt.Sprintf("%d", targetContainer.PID)

	var cmd *exec.Cmd
	if targetContainer.RootfsPath != "" {
		// Enter namespaces and chroot
		cmd = exec.Command("nsenter",
			"-t", pidStr,
			"-m", "-u", "-i", "-n",
			"--",
			"chroot", targetContainer.RootfsPath,
			"/bin/sh", "-c", command)
	} else {
		// Enter namespaces only
		cmd = exec.Command("nsenter",
			"-t", pidStr,
			"-m", "-u", "-i", "-n",
			"--",
			"/bin/sh", "-c", command)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to exec command: %w", err)
	}

	return nil
}

func Remove(containerIDOrName string, force bool) error {
	containers, err := ListContainers()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainer *Container
	for _, c := range containers {
		if c.ID == containerIDOrName || c.Name == containerIDOrName {
			targetContainer = c
			break
		}
	}

	if targetContainer == nil {
		return fmt.Errorf("container not found: %s", containerIDOrName)
	}

	if targetContainer.State == StateRunning {
		if !force {
			return fmt.Errorf("container %s is running. Use --force to remove a running container", targetContainer.Name)
		}

		fmt.Printf("Force removing running container %s...\n", targetContainer.Name)
		if err := Stop(containerIDOrName); err != nil {
			fmt.Printf("Warning: failed to stop container: %v\n", err)
		}
	}

	fmt.Printf("Removing container %s...\n", targetContainer.Name)

	if targetContainer.RootfsPath != "" {
		if err := os.RemoveAll(targetContainer.RootfsPath); err != nil {
			fmt.Printf("Warning: failed to remove rootfs: %v\n", err)
		}
	}

	cleanupVeth(targetContainer.VethName)

	if err := DeleteContainer(targetContainer.ID); err != nil {
		return fmt.Errorf("failed to delete container metadata: %w", err)
	}

	fmt.Printf("Container %s removed\n", targetContainer.Name)
	return nil
}
