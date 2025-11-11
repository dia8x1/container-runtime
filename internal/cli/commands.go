package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"container-runtime/internal/container"
	"container-runtime/internal/image"
)

func Execute() {

	if len(os.Args) < 2 {
		fmt.Println("Usage: container-runtime <command> [args]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		runCmd := flag.NewFlagSet("run", flag.ExitOnError)
		command := runCmd.String("command", "", "Command to run inside the container")
		detach := runCmd.Bool("d", false, "Run container in background and print container ID")
		rootfs := runCmd.String("rootfs", "", "Path to rootfs directory (e.g., /var/lib/container-runtime/rootfs/alpine)")
		runCmd.Parse(os.Args[2:])

		var imageNameOrID string
		if runCmd.NArg() > 0 {
			imageNameOrID = runCmd.Arg(0)
		}

		var finalRootfs string

		if imageNameOrID != "" {
			img, err := image.ResolveImage(imageNameOrID)
			if err != nil {
				fmt.Printf("Failed to resolve image: %v\n", err)
				os.Exit(1)
			}

			tempRootfs := filepath.Join("/var/lib/container-runtime/rootfs", fmt.Sprintf("container-%d", time.Now().UnixNano()))
			if err := image.ExtractImage(img, tempRootfs); err != nil {
				fmt.Printf("Failed to extract image: %v\n", err)
				os.Exit(1)
			}

			finalRootfs = tempRootfs

			if *command == "" && len(img.Metadata.Cmd) > 0 {
				*command = img.Metadata.Cmd[0]
			}
		} else if *rootfs != "" {
			finalRootfs = *rootfs
		}

		if *command == "" {
			fmt.Println("Error: command option is required (or use an image with default CMD)")
			os.Exit(1)
		}

		_, err := container.Run(*command, *detach, finalRootfs)
		if err != nil {
			fmt.Printf("Failed to run container: %v\n", err)
			os.Exit(1)
		}

	case "build":
		buildCmd := flag.NewFlagSet("build", flag.ExitOnError)
		name := buildCmd.String("name", "", "Name for the built image")
		tag := buildCmd.String("tag", "latest", "Tag for the built image")
		file := buildCmd.String("file", "CRfile", "Path to CRfile (default: CRfile)")
		buildCmd.Parse(os.Args[2:])

		contextPath := "."
		if buildCmd.NArg() > 0 {
			contextPath = buildCmd.Arg(0)
		}

		if *name == "" {
			fmt.Println("Error: --name option is required")
			fmt.Println("Usage: container-runtime build --name <image-name> [--tag <tag>] [--file <CRfile>] [context-path]")
			os.Exit(1)
		}

		crfilePath := filepath.Join(contextPath, *file)
		if !filepath.IsAbs(*file) {
			crfilePath = filepath.Join(contextPath, *file)
		} else {
			crfilePath = *file
		}

		builder := image.NewBuilder(crfilePath, contextPath, *name, *tag)
		img, err := builder.Build()
		if err != nil {
			fmt.Printf("Failed to build image: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("\nImage ID: %s\n", img.ID)
		fmt.Printf("Size: %.2f MB\n", float64(img.Size)/(1024*1024))

	case "images":
		images, err := image.ListImages()
		if err != nil {
			fmt.Printf("Failed to list images: %v\n", err)
			os.Exit(1)
		}

		if len(images) == 0 {
			fmt.Println("No images found")
			return
		}

		fmt.Printf("%-20s %-15s %-70s %-20s %-10s\n", "REPOSITORY", "TAG", "IMAGE ID", "CREATED", "SIZE")

		for _, img := range images {
			tags := image.GetImageTags(img.ID)
			for _, tag := range tags {
				name := tag
				tagName := ""
				if idx := len(tag) - 1; idx >= 0 {
					for i := len(tag) - 1; i >= 0; i-- {
						if tag[i] == ':' {
							name = tag[:i]
							tagName = tag[i+1:]
							break
						}
					}
				}

				created := img.Created.Format("2006-01-02 15:04:05")
				sizeMB := fmt.Sprintf("%.2f MB", float64(img.Size)/(1024*1024))

				fmt.Printf("%-20s %-15s %-70s %-20s %-10s\n",
					name, tagName, img.ID, created, sizeMB)
			}
		}

	case "ls":
		containers, err := container.ListContainers()
		if err != nil {
			fmt.Printf("Failed to list containers: %v\n", err)
			os.Exit(1)
		}

		if len(containers) == 0 {
			fmt.Println("No containers found")
			return
		}

		fmt.Printf("%-25s %-12s %-15s %-12s %-20s %-15s\n",
			"CONTAINER ID", "NAME", "STATUS", "PID", "IP ADDRESS", "CREATED")

		for _, c := range containers {
			containerID := c.ID
			if len(containerID) > 25 {
				containerID = containerID[:25]
			}

			status := c.State
			pidStr := fmt.Sprintf("%d", c.PID)
			if c.PID == 0 {
				pidStr = "-"
			}

			created := c.Created.Format("2006-01-02 15:04")

			fmt.Printf("%-25s %-12s %-15s %-12s %-20s %-15s\n",
				containerID, c.Name, status, pidStr, c.IPAddress, created)
		}

	case "start":
		if len(os.Args) < 3 {
			fmt.Println("Usage: container-runtime start <container-id-or-name>")
			os.Exit(1)
		}

		containerIDOrName := os.Args[2]
		if err := container.Start(containerIDOrName); err != nil {
			fmt.Printf("Failed to start container: %v\n", err)
			os.Exit(1)
		}

	case "stop":
		if len(os.Args) < 3 {
			fmt.Println("Usage: container-runtime stop <container-id-or-name>")
			os.Exit(1)
		}

		containerIDOrName := os.Args[2]
		if err := container.Stop(containerIDOrName); err != nil {
			fmt.Printf("Failed to stop container: %v\n", err)
			os.Exit(1)
		}

	case "rm":
		removeCmd := flag.NewFlagSet("rm", flag.ExitOnError)
		force := removeCmd.Bool("f", false, "Force remove a running container")
		removeCmd.Parse(os.Args[2:])

		if removeCmd.NArg() < 1 {
			fmt.Println("Usage: container-runtime rm [-f] <container-id-or-name>")
			os.Exit(1)
		}

		containerIDOrName := removeCmd.Arg(0)
		if err := container.Remove(containerIDOrName, *force); err != nil {
			fmt.Printf("Failed to remove container: %v\n", err)
			os.Exit(1)
		}

	case "exec":
		if len(os.Args) < 4 {
			fmt.Println("Usage: container-runtime exec <container-id-or-name> <command>")
			os.Exit(1)
		}

		containerIDOrName := os.Args[2]
		command := os.Args[3]
		if err := container.Exec(containerIDOrName, command); err != nil {
			fmt.Printf("Failed to exec command: %v\n", err)
			os.Exit(1)
		}

	case "child":
		container.ChildMain()

	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		fmt.Println("\nAvailable commands:")
		fmt.Println("  run       Run a container")
		fmt.Println("  build     Build an image from CRfile")
		fmt.Println("  images    List available images")
		fmt.Println("  ls        List all containers")
		fmt.Println("  start     Start a stopped container")
		fmt.Println("  stop      Stop a running container")
		fmt.Println("  rm        Remove a container")
		fmt.Println("  exec      Execute a command in a running container")
		os.Exit(1)
	}
}
