package isolation

import (
	"fmt"
	"sync"
	"time"

	"github.com/ai-prison/ai-prison/internal/domain"
)

// Instance is a simulated live sandbox holding one agent. Ready=false means
// the controlled environment was requested but not yet confirmed safe to
// cut-over — the old environment must still serve the agent.
type Instance struct {
	ID            string                `json:"id"`
	AgentID       domain.AgentID        `json:"agentId"`
	Backend       domain.SandboxBackend `json:"backend"`
	Ready         bool                  `json:"ready"`
	EstablishedAt time.Time             `json:"establishedAt"`
	Simulated     bool                  `json:"simulated"`
}

// Controller migrates agents between backends on the gradient. Migration is
// never an instant switch:
//
//  1. establish the NEW controlled environment (new instance created, not yet
//     ready);
//  2. confirm the new environment ready (simulated "no escape" readiness);
//  3. only after that confirmation, release the OLD environment's resources.
//
// Between 1 and 2 the old environment keeps running, so there is no cut-over
// window during which the agent has neither environment nor loses state.
type Controller struct {
	mu        sync.Mutex
	gradient  *Gradient
	events    EventSink
	instances map[string]*Instance
	byAgent   map[domain.AgentID]string // agent -> live instance id
	now       func() time.Time
}

// NewController returns a migration controller over the given gradient.
func NewController(gradient *Gradient, events EventSink) *Controller {
	return &Controller{
		gradient:  gradient,
		events:    events,
		instances: make(map[string]*Instance),
		byAgent:   make(map[domain.AgentID]string),
		now:       time.Now,
	}
}

// Establish provisions a new instance for the agent at the level's recommended
// backend. The OLD instance stays live until the caller calls ConfirmReady on
// the new one. Returns the new, not-yet-ready instance.
func (c *Controller) Establish(agentID domain.AgentID, level domain.Level) (*Instance, error) {
	backend, err := c.gradient.Recommended(level)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	inst := &Instance{
		ID:            fmt.Sprintf("inst-%s-%s-%d", agentID, backend.Name(), len(c.instances)+1),
		AgentID:       agentID,
		Backend:       backend.Name(),
		Ready:         false,
		EstablishedAt: c.now().UTC(),
		Simulated:     true,
	}
	c.instances[inst.ID] = inst
	if c.events != nil {
		c.events.Append(domain.Event{
			Type:    domain.EventMigrationSuppressed,
			AgentID: agentID,
			Actor:   "isolation-migration",
			Payload: map[string]string{
				"instanceId": inst.ID,
				"backend":    string(backend.Name()),
				"phase":      "new-environment-established-old-still-live",
				"simulated":  "true",
			},
		})
	}
	return inst, nil
}

// ConfirmReady declares the new environment safe, then releases the agent's
// OLD environment. The release happens only AFTER the confirmation — this is
// the establish-before-cut guarantee. It is an error to confirm an unknown
// instance.
func (c *Controller) ConfirmReady(instanceID string) (*Instance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, ok := c.instances[instanceID]
	if !ok {
		return nil, fmt.Errorf("isolation: unknown instance %q", instanceID)
	}

	oldID := c.byAgent[inst.AgentID]
	inst.Ready = true
	c.byAgent[inst.AgentID] = inst.ID
	if c.events != nil {
		c.events.Append(domain.Event{
			Type:    domain.EventIsolated,
			AgentID: inst.AgentID,
			Actor:   "isolation-migration",
			Payload: map[string]string{
				"instanceId": inst.ID,
				"phase":      "new-environment-ready-old-released",
				"simulated":  "true",
			},
		})
	}

	// Cut the old environment only now, after the new one is confirmed ready.
	if oldID != "" && oldID != inst.ID {
		delete(c.instances, oldID)
	}
	return inst, nil
}

// Live returns the agent's current live instance (nil when none).
func (c *Controller) Live(agentID domain.AgentID) *Instance {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.byAgent[agentID]
	inst, ok := c.instances[id]
	if !ok {
		return nil
	}
	cp := *inst
	return &cp
}

// Instance returns a detached copy of any instance by id (nil when gone —
// e.g. released).
func (c *Controller) Instance(id string) *Instance {
	c.mu.Lock()
	defer c.mu.Unlock()
	inst, ok := c.instances[id]
	if !ok {
		return nil
	}
	cp := *inst
	return &cp
}

// Count returns the number of currently live instances.
func (c *Controller) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.instances)
}
