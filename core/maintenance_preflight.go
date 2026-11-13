package core

import (
	"context"
	"errors"
)

// MaintenancePreflight checks local CLI availability and authentication without
// starting a model turn. Implementations must not return credential material.
type MaintenancePreflight interface {
	MaintenanceReady(context.Context) error
}

func (m *ManagementServer) maintenancePreflight(ctx context.Context, g *Maintenance, owner string, projects []string) error {
	if owner == "" || g.Snapshot().OperationID != owner {
		return errors.New("maintenance owner mismatch")
	}
	if len(projects) == 0 || len(projects) > 100 {
		return errors.New("invalid preflight projects")
	}
	for _, name := range projects {
		m.mu.RLock()
		e := m.engines[name]
		m.mu.RUnlock()
		if e == nil {
			return errors.New("target project unavailable")
		}
		checker, ok := e.agent.(MaintenancePreflight)
		if !ok {
			return errors.New("target agent preflight unavailable")
		}
		if err := checker.MaintenanceReady(ctx); err != nil {
			return errors.New("target agent installation or authentication unconfirmed")
		}
	}
	return nil
}
