package service

// EnrichSource + PriorityFor + EnqueueEnrich live in the worker package
// (internal/worker/enqueue.go) to avoid a service↔worker import cycle —
// workers themselves enqueue follow-on enrich jobs, and they can't depend
// on service which depends on worker.
//
// Service-layer wrappers around the worker enqueuers (for HTTP handlers
// and CLI callers) live alongside the rest of the service API in app.go
// and refresh.go.
