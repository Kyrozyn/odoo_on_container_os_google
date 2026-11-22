package odoo

import (
	"fmt"
	"sync"
	"time"
)

// DeployProgress tracks live deployment steps and logs for an Odoo instance.
type DeployProgress struct {
	InstanceID string     `json:"instanceId"`
	Status     string     `json:"status"` // "idle", "deploying", "running", "failed"
	Step       string     `json:"step"`
	Percent    int        `json:"percent"`
	Logs       []string   `json:"logs"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type deployTracker struct {
	mu   sync.RWMutex
	jobs map[string]*DeployProgress
}

func newDeployTracker() *deployTracker {
	return &deployTracker{jobs: make(map[string]*DeployProgress)}
}

func (t *deployTracker) Start(instanceID, initialStep string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	t.jobs[instanceID] = &DeployProgress{
		InstanceID: instanceID,
		Status:     "deploying",
		Step:       initialStep,
		Percent:    5,
		Logs:       []string{fmt.Sprintf("[%s] %s", now.Format("15:04:05"), initialStep)},
		StartedAt:  now,
	}
}

func (t *deployTracker) Step(instanceID, step string, percent int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	job, ok := t.jobs[instanceID]
	if !ok {
		return
	}
	job.Step = step
	job.Percent = percent
	job.Logs = append(job.Logs, fmt.Sprintf("[%s] ==> %s", time.Now().Format("15:04:05"), step))
}

func (t *deployTracker) Log(instanceID, line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	job, ok := t.jobs[instanceID]
	if !ok {
		return
	}
	job.Logs = append(job.Logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line))
	if len(job.Logs) > 500 {
		job.Logs = job.Logs[len(job.Logs)-500:]
	}
}

func (t *deployTracker) Complete(instanceID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	job, ok := t.jobs[instanceID]
	if !ok {
		return
	}
	now := time.Now()
	job.Status = "running"
	job.Step = "Deployment complete — Odoo is running!"
	job.Percent = 100
	job.FinishedAt = &now
	job.Logs = append(job.Logs, fmt.Sprintf("[%s] ✓ Deployment complete!", now.Format("15:04:05")))
}

func (t *deployTracker) Fail(instanceID string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	job, ok := t.jobs[instanceID]
	if !ok {
		return
	}
	now := time.Now()
	job.Status = "failed"
	job.Step = "Deployment failed"
	job.Error = err.Error()
	job.FinishedAt = &now
	job.Logs = append(job.Logs, fmt.Sprintf("[%s] ✗ ERROR: %s", now.Format("15:04:05"), err.Error()))
}

func (t *deployTracker) Get(instanceID string) *DeployProgress {
	t.mu.RLock()
	defer t.mu.RUnlock()
	job, ok := t.jobs[instanceID]
	if !ok {
		return &DeployProgress{
			InstanceID: instanceID,
			Status:     "idle",
			Logs:       []string{},
		}
	}
	cp := *job
	cp.Logs = make([]string, len(job.Logs))
	copy(cp.Logs, job.Logs)
	return &cp
}
