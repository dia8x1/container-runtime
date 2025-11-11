package image

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	ImageDir = "/var/lib/container-runtime/images"
	RootfsDir = "/var/lib/container-runtime/rootfs"
)

type Builder struct {
	crfilePath  string
	contextPath string
	name        string
	tag         string
}

func NewBuilder(crfilePath, contextPath, name, tag string) *Builder {
	if tag == "" {
		tag = "latest"
	}
	return &Builder{
		crfilePath:  crfilePath,
		contextPath: contextPath,
		name:        name,
		tag:         tag,
	}
}

func (b *Builder) Build() (*Image, error) {
	fmt.Println("=========================================")
	fmt.Printf("Building image: %s:%s\n", b.name, b.tag)
	fmt.Println("=========================================")

	instructions, err := ParseCRfile(b.crfilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CRfile: %w", err)
	}

	buildID := fmt.Sprintf("build-%d", time.Now().UnixNano())
	buildRootfs := filepath.Join(RootfsDir, buildID)

	fmt.Printf("Step 1/%d: Creating build environment\n", len(instructions)+2)
	if err := os.MkdirAll(buildRootfs, 0755); err != nil {
		return nil, fmt.Errorf("failed to create build directory: %w", err)
	}
	defer func() {
	}()

	metadata := ImageMetadata{
		Env:    make(map[string]string),
		Labels: make(map[string]string),
	}

	for i, inst := range instructions {
		fmt.Printf("Step %d/%d: %s %s\n", i+2, len(instructions)+2, inst.Command, strings.Join(inst.Args, " "))

		if err := b.executeInstruction(inst, buildRootfs, &metadata); err != nil {
			os.RemoveAll(buildRootfs)
			return nil, fmt.Errorf("failed to execute instruction '%s': %w", inst.Raw, err)
		}
	}

	fmt.Printf("Step %d/%d: Creating final image\n", len(instructions)+2, len(instructions)+2)
	image, err := b.createImage(buildRootfs, metadata)
	if err != nil {
		os.RemoveAll(buildRootfs)
		return nil, fmt.Errorf("failed to create image: %w", err)
	}

	fmt.Println("=========================================")
	fmt.Printf("Successfully built image: %s\n", image.ID)
	fmt.Printf("Tagged as: %s:%s\n", b.name, b.tag)
	fmt.Println("=========================================")

	return image, nil
}

func (b *Builder) executeInstruction(inst Instruction, rootfs string, metadata *ImageMetadata) error {
	switch inst.Command {
	case "FROM":
		return b.execFrom(inst.Args[0], rootfs)
	case "RUN":
		return b.execRun(inst.Args[0], rootfs)
	case "CMD":
		metadata.Cmd = []string{inst.Args[0]}
		return nil
	case "ENV":
		return b.execEnv(inst.Args, metadata)
	case "WORKDIR":
		metadata.WorkingDir = inst.Args[0]
		return nil
	case "LABEL":
		return b.execLabel(inst.Args, metadata)
	case "COPY":
		return b.execCopy(inst.Args, rootfs)
	default:
		return fmt.Errorf("unsupported instruction: %s", inst.Command)
	}
}

func (b *Builder) execFrom(baseImage string, rootfs string) error {
	if !strings.HasPrefix(baseImage, "alpine") {
		return fmt.Errorf("currently only 'alpine' base image is supported")
	}

	alpineRootfs := "/var/lib/container-runtime/rootfs/alpine"

	if _, err := os.Stat(alpineRootfs); os.IsNotExist(err) {
		return fmt.Errorf("alpine rootfs not found at %s. Please run alpine_rootfs.sh first", alpineRootfs)
	}

	fmt.Println("  → Copying alpine base image...")
	cmd := exec.Command("cp", "-a", alpineRootfs+"/.", rootfs+"/")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy alpine rootfs: %w", err)
	}

	return nil
}

func (b *Builder) execRun(command string, rootfs string) error {
	fmt.Printf("  → Running: %s\n", command)

	if err := b.mountBuildFilesystems(rootfs); err != nil {
		return fmt.Errorf("failed to mount filesystems: %w", err)
	}
	defer b.unmountBuildFilesystems(rootfs)

	resolv := filepath.Join(rootfs, "etc", "resolv.conf")
	if err := exec.Command("cp", "/etc/resolv.conf", resolv).Run(); err != nil {
		fmt.Printf("Warning: failed to copy resolv.conf: %v\n", err)
	}

	cmd := exec.Command("chroot", rootfs, "/bin/sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}

func (b *Builder) execEnv(args []string, metadata *ImageMetadata) error {
	for _, arg := range args {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid ENV format: %s", arg)
		}
		metadata.Env[parts[0]] = parts[1]
	}
	return nil
}

func (b *Builder) execLabel(args []string, metadata *ImageMetadata) error {
	for _, arg := range args {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid LABEL format: %s", arg)
		}
		metadata.Labels[parts[0]] = parts[1]
	}
	return nil
}

func (b *Builder) execCopy(args []string, rootfs string) error {
	if len(args) < 2 {
		return fmt.Errorf("COPY requires at least 2 arguments")
	}

	src := filepath.Join(b.contextPath, args[0])
	dst := filepath.Join(rootfs, args[1])

	fmt.Printf("  → Copying %s to %s\n", args[0], args[1])

	cmd := exec.Command("cp", "-r", src, dst)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy files: %w", err)
	}

	return nil
}

func (b *Builder) mountBuildFilesystems(rootfs string) error {
	mounts := []struct {
		target string
		fstype string
	}{
		{"proc", "proc"},
		{"sys", "sysfs"},
		{"dev", ""},
	}

	for _, m := range mounts {
		target := filepath.Join(rootfs, m.target)
		os.MkdirAll(target, 0755)

		checkCmd := exec.Command("mountpoint", "-q", target)
		if checkCmd.Run() == nil {
			continue
		}

		var cmd *exec.Cmd
		if m.target == "dev" {
			cmd = exec.Command("mount", "--bind", "/dev", target)
		} else {
			cmd = exec.Command("mount", "-t", m.fstype, m.fstype, target)
		}

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to mount %s: %w", m.target, err)
		}
	}

	return nil
}

func (b *Builder) unmountBuildFilesystems(rootfs string) {
	targets := []string{"proc", "sys", "dev"}
	for _, t := range targets {
		target := filepath.Join(rootfs, t)
		exec.Command("umount", target).Run()
	}
}

func (b *Builder) createImage(rootfs string, metadata ImageMetadata) (*Image, error) {
	imageID, err := calculateRootfsSHA256(rootfs)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate image hash: %w", err)
	}

	if err := os.MkdirAll(ImageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create image directory: %w", err)
	}

	imageTar := filepath.Join(ImageDir, fmt.Sprintf("%s.tar.gz", imageID))
	fmt.Printf("  → Compressing image to %s\n", imageTar)

	cmd := exec.Command("tar", "-czf", imageTar, "-C", rootfs, ".")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to compress rootfs: %w", err)
	}

	stat, _ := os.Stat(imageTar)
	size := int64(0)
	if stat != nil {
		size = stat.Size()
	}

	image := &Image{
		ID:         imageID,
		Name:       b.name,
		Tag:        b.tag,
		Created:    time.Now(),
		Size:       size,
		RootfsPath: imageTar,
		Metadata:   metadata,
	}

	if err := SaveImageMetadata(image); err != nil {
		return nil, fmt.Errorf("failed to save image metadata: %w", err)
	}

	if err := UpdateRepository(b.name, b.tag, imageID); err != nil {
		return nil, fmt.Errorf("failed to update repository: %w", err)
	}

	os.RemoveAll(rootfs)

	return image, nil
}

func calculateRootfsSHA256(rootfs string) (string, error) {

	cmd := exec.Command("tar", "-c", "-C", rootfs, ".")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, stdout); err != nil {
		return "", err
	}

	if err := cmd.Wait(); err != nil {
		return "", err
	}

	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}
