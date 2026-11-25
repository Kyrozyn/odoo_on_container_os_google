package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// RecreateSpec describes how a helper container should recreate the panel container
// with an updated host port mapping and roll back on failure.
type RecreateSpec struct {
	TargetContainer string              `json:"targetContainer"`
	NewSpec         CreateContainerSpec `json:"newSpec"`
}

// RunRecreateHelper executes a self-recreation plan inside a short-lived helper container.
func RunRecreateHelper(ctx context.Context, dc *Client, rawSpec string) error {
	var spec RecreateSpec
	if err := json.Unmarshal([]byte(rawSpec), &spec); err != nil {
		return fmt.Errorf("invalid recreate spec: %w", err)
	}
	if spec.TargetContainer == "" {
		return fmt.Errorf("targetContainer is required")
	}

	// Give the parent container's HTTP handler time to flush its response to the browser.
	time.Sleep(800 * time.Millisecond)

	backupName := spec.TargetContainer + "-port-backup"
	_ = dc.RemoveContainer(ctx, backupName, true)

	log.Printf("Stopping container %s...", spec.TargetContainer)
	if err := dc.StopContainer(ctx, spec.TargetContainer, 10); err != nil {
		return fmt.Errorf("stop %s: %w", spec.TargetContainer, err)
	}

	if err := dc.RenameContainer(ctx, spec.TargetContainer, backupName); err != nil {
		_ = dc.StartContainer(ctx, spec.TargetContainer)
		return fmt.Errorf("rename %s to %s: %w", spec.TargetContainer, backupName, err)
	}

	rollback := func(cause error) error {
		log.Printf("Recreate failed (%v), rolling back to %s...", cause, backupName)
		_ = dc.RemoveContainer(ctx, spec.TargetContainer, true)
		if err := dc.RenameContainer(ctx, backupName, spec.TargetContainer); err != nil {
			return fmt.Errorf("%v; rollback rename failed: %w", cause, err)
		}
		if err := dc.StartContainer(ctx, spec.TargetContainer); err != nil {
			return fmt.Errorf("%v; rollback start failed: %w", cause, err)
		}
		return cause
	}

	newID, err := dc.CreateContainer(ctx, spec.NewSpec)
	if err != nil {
		return rollback(fmt.Errorf("create new container: %w", err))
	}

	if err := dc.StartContainer(ctx, newID); err != nil {
		_ = dc.RemoveContainer(ctx, newID, true)
		return rollback(fmt.Errorf("start new container: %w", err))
	}

	log.Printf("Successfully recreated %s (%s). Removing backup %s...", spec.TargetContainer, newID[:12], backupName)
	_ = dc.RemoveContainer(ctx, backupName, true)
	return nil
}
