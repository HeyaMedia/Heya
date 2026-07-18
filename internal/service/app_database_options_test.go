package service

import (
	"testing"

	"github.com/karbowiak/heya/internal/config"
)

func TestAppRuntimeModeOwnership(t *testing.T) {
	tests := []struct {
		name            string
		mode            appRuntimeMode
		executesWorkers bool
		ownsCoordinator bool
	}{
		{name: "api", mode: appRuntimeAPI},
		{name: "worker coordinator", mode: appRuntimeWorker, executesWorkers: true, ownsCoordinator: true},
		{name: "finite queue processor", mode: appRuntimeQueueProcessor, executesWorkers: true},
		{name: "command", mode: appRuntimeCommand},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.executesWorkers(); got != tt.executesWorkers {
				t.Errorf("executesWorkers() = %v, want %v", got, tt.executesWorkers)
			}
			if got := tt.mode.ownsCoordinatorLease(); got != tt.ownsCoordinator {
				t.Errorf("ownsCoordinatorLease() = %v, want %v", got, tt.ownsCoordinator)
			}
		})
	}
}

func TestDatabaseOptionsForRuntimePreserveConfiguredCapacity(t *testing.T) {
	cfg := &config.Config{
		DatabaseMaxConns: config.Field[int]{Value: 1},
		DatabaseMinConns: config.Field[int]{Value: 1},
	}

	for _, runtimeMode := range []appRuntimeMode{appRuntimeAPI, appRuntimeWorker, appRuntimeQueueProcessor, appRuntimeCommand} {
		options := databaseOptionsForRuntime(cfg, runtimeMode)
		if options.MaxConns != 1 {
			t.Errorf("runtime mode %d max connections = %d, want configured value 1", runtimeMode, options.MaxConns)
		}
		if options.MinConns != 1 {
			t.Errorf("runtime mode %d min connections = %d, want configured value 1", runtimeMode, options.MinConns)
		}
	}
}

func TestDatabaseOptionsForRuntimePreserveLargerWorkerPool(t *testing.T) {
	cfg := &config.Config{
		DatabaseMaxConns: config.Field[int]{Value: 12},
		DatabaseMinConns: config.Field[int]{Value: 3},
	}

	options := databaseOptionsForRuntime(cfg, appRuntimeWorker)
	if options.MaxConns != 12 || options.MinConns != 3 {
		t.Fatalf("worker database options = %+v, want max 12 and min 3", options)
	}
}
