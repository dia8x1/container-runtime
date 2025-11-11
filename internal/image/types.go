package image

import "time"

type Image struct {
	ID          string            `json:"id"`           // SHA256 hash
	Name        string            `json:"name"`         // e.g., "nginx"
	Tag         string            `json:"tag"`          // e.g., "latest"
	Created     time.Time         `json:"created"`
	Size        int64             `json:"size"`         // Size in bytes
	RootfsPath  string            `json:"rootfs_path"`  // Path to extracted rootfs
	Metadata    ImageMetadata     `json:"metadata"`
}

type ImageMetadata struct {
	Cmd         []string          `json:"cmd"`          // Default command to run
	Env         map[string]string `json:"env"`          // Environment variables
	WorkingDir  string            `json:"working_dir"`  // Working directory
	Labels      map[string]string `json:"labels"`       // Labels
}

type Instruction struct {
	Command string   // FROM, RUN, CMD, ENV, WORKDIR, etc.
	Args    []string // Arguments for the command
	Raw     string   // Original line
}

type Repository struct {
	Images map[string]string `json:"images"` // "name:tag" -> "sha256:..."
}
