package container

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

const (
	healthCheckInterval = 10 * time.Second
	maxFailedChecks     = 2
)

type HealthChecker struct {
	containerID   string
	pid           int
	stopChan      chan struct{}
	failedChecks  int
	mu            sync.Mutex
}

var (
	activeHealthCheckers = make(map[string]*HealthChecker)
	healthCheckersMu     sync.RWMutex
)

func StartHealthCheck(containerID string, pid int) {
	healthCheckersMu.Lock()
	defer healthCheckersMu.Unlock()

	if _, exists := activeHealthCheckers[containerID]; exists {
		return
	}

	checker := &HealthChecker{
		containerID:  containerID,
		pid:          pid,
		stopChan:     make(chan struct{}),
		failedChecks: 0,
	}

	activeHealthCheckers[containerID] = checker
	go checker.run()
}

func StopHealthCheck(containerID string) {
	healthCheckersMu.Lock()
	defer healthCheckersMu.Unlock()

	if checker, exists := activeHealthCheckers[containerID]; exists {
		close(checker.stopChan)
		delete(activeHealthCheckers, containerID)
	}
}

func (hc *HealthChecker) run() {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.stopChan:
			return
		case <-ticker.C:
			if !hc.checkHealth() {
				hc.handleUnhealthy()
			}
		}
	}
}

func (hc *HealthChecker) checkHealth() bool {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	process, err := os.FindProcess(hc.pid)
	if err != nil {
		hc.failedChecks++
		return false
	}

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		hc.failedChecks++
		return false
	}

	hc.failedChecks = 0
	return true
}

func (hc *HealthChecker) handleUnhealthy() {
	hc.mu.Lock()
	failCount := hc.failedChecks
	hc.mu.Unlock()

	if failCount >= maxFailedChecks {
		fmt.Printf("[Health Check] Container %s failed %d health checks, marking as stopped\n",
			hc.containerID, failCount)

		UpdateContainerState(hc.containerID, StateStopped, 137)

		StopHealthCheck(hc.containerID)
	}
}

func RecoverHealthChecks() error {
	containers, err := ListContainers()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	recoveredCount := 0
	for _, c := range containers {
		if c.State == StateRunning && c.PID > 0 {
			process, err := os.FindProcess(c.PID)
			if err != nil || process.Signal(syscall.Signal(0)) != nil {
				fmt.Printf("[Recovery] Container %s (PID: %d) is not running, marking as stopped\n",
					c.ID, c.PID)
				UpdateContainerState(c.ID, StateStopped, 143)
				recoveredCount++
			} else {
				fmt.Printf("[Recovery] Container %s (PID: %d) is healthy, starting health check\n",
					c.ID, c.PID)
				StartHealthCheck(c.ID, c.PID)
			}
		}
	}

	if recoveredCount > 0 {
		fmt.Printf("[Recovery] Cleaned up %d stale containers\n", recoveredCount)
	}

	return nil
}
