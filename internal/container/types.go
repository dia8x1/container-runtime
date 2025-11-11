package container

import "time"

const (
	StateRunning = "running"
	StateStopped = "stopped"
	StateExited  = "exited"
)

const (
	ContainerDir = "/var/lib/container-runtime/containers"
)

type Container struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	ImageID     string            `json:"image_id"`
	ImageName   string            `json:"image_name"`
	Command     string            `json:"command"`
	Created     time.Time         `json:"created"`
	State       string            `json:"state"`
	PID         int               `json:"pid"`
	RootfsPath  string            `json:"rootfs_path"`
	NetworkNS   string            `json:"network_ns"`
	IPAddress   string            `json:"ip_address"`
	VethName    string            `json:"veth_name"`
	ExitCode    int               `json:"exit_code"`
	FinishedAt  time.Time         `json:"finished_at"`
}