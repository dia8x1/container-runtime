package container

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
)

func SaveContainer(c *Container) error {
	if err := os.MkdirAll(ContainerDir, 0755); err != nil {
		return fmt.Errorf("failed to create container directory: %w", err)
	}

	containerFile := filepath.Join(ContainerDir, fmt.Sprintf("%s.json", c.ID))
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal container: %w", err)
	}

	if err := ioutil.WriteFile(containerFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write container file: %w", err)
	}

	return nil
}

func LoadContainer(containerID string) (*Container, error) {
	containerFile := filepath.Join(ContainerDir, fmt.Sprintf("%s.json", containerID))
	data, err := ioutil.ReadFile(containerFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read container file: %w", err)
	}

	var c Container
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to unmarshal container: %w", err)
	}

	return &c, nil
}

func ListContainers() ([]*Container, error) {
	if _, err := os.Stat(ContainerDir); os.IsNotExist(err) {
		return []*Container{}, nil
	}

	files, err := ioutil.ReadDir(ContainerDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read container directory: %w", err)
	}

	var containers []*Container
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}

		containerID := file.Name()[:len(file.Name())-5]
		c, err := LoadContainer(containerID)
		if err != nil {
			fmt.Printf("Warning: failed to load container %s: %v\n", containerID, err)
			continue
		}
		containers = append(containers, c)
	}

	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Created.After(containers[j].Created)
	})

	return containers, nil
}

func DeleteContainer(containerID string) error {
	containerFile := filepath.Join(ContainerDir, fmt.Sprintf("%s.json", containerID))
	if err := os.Remove(containerFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete container file: %w", err)
	}
	return nil
}

func UpdateContainerState(containerID string, state string, exitCode int) error {
	c, err := LoadContainer(containerID)
	if err != nil {
		return err
	}

	c.State = state
	c.ExitCode = exitCode
	if state == StateStopped || state == StateExited {
		c.FinishedAt = c.Created
		c.PID = 0
	}

	return SaveContainer(c)
}
