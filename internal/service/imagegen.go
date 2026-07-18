package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/imagegen"
	"github.com/rs/zerolog/log"
)

type ImageStatus struct {
	Build          string                          `json:"build"`
	Backend        string                          `json:"backend"`
	Model          string                          `json:"model"`
	RuntimePresent bool                            `json:"runtime_present"`
	ModelPresent   bool                            `json:"model_present"`
	DownloadState  imagegen.DownloadState          `json:"download_state"`
	Progress       *imagegen.ImageDownloadProgress `json:"progress,omitempty"`
	DownloadError  string                          `json:"download_error,omitempty"`
	Devices        []imagegen.ComputeDevice        `json:"devices"`
	DeviceError    string                          `json:"device_error,omitempty"`
	Artifacts      []imagegen.ArtifactStatus       `json:"artifacts"`
	DownloadBytes  int64                           `json:"download_bytes"`
}

func (a *App) ImageModels() []imagegen.Model { return imagegen.Models }

func (a *App) ImageStatus(model, backend string) ImageStatus {
	if model == "" {
		model = imagegen.DefaultModel
	}
	if backend == "" {
		backend = imagegen.BackendAuto
	}
	state, progress, downloadErr := a.imageRuntime.DownloadStatus()
	artifacts, downloadBytes := a.imageRuntime.ModelArtifactStatus(model)
	devices, devicesErr := a.imageRuntime.Devices(backend)
	deviceError := ""
	if devicesErr != nil {
		deviceError = devicesErr.Error()
	}
	return ImageStatus{Build: imagegen.RuntimeBuild, Backend: imagegen.ResolveBackend(backend), Model: model,
		RuntimePresent: a.imageRuntime.RuntimePresent(backend), ModelPresent: a.imageRuntime.ModelPresent(model),
		DownloadState: state, Progress: progress, DownloadError: downloadErr, Devices: devices, DeviceError: deviceError,
		Artifacts: artifacts, DownloadBytes: downloadBytes}
}

// ImageDownloadWait is intentionally the only service entry point that can
// fetch image artifacts. Status, selection and generation remain offline.
func (a *App) ImageDownloadWait(ctx context.Context, model, backend string) error {
	if model == "" {
		model = imagegen.DefaultModel
	}
	if backend == "" {
		backend = imagegen.BackendAuto
	}
	return a.imageRuntime.Download(ctx, model, backend)
}

func (a *App) ImageDownload(model, backend string) error {
	if model == "" {
		model = imagegen.DefaultModel
	}
	if backend == "" {
		backend = imagegen.BackendAuto
	}
	if _, ok := imagegen.ModelByID(model); !ok {
		return fmt.Errorf("unknown image model %q", model)
	}
	if _, err := imagegen.RuntimeArtifactFor(backend); err != nil {
		return err
	}
	if !a.startBackground(func() {
		ctx := a.LifetimeContext()
		if err := a.imageRuntime.Download(ctx, model, backend); err != nil && ctx.Err() == nil {
			log.Err(err).Msg("imagegen: artifact download failed")
		}
	}) {
		return errAppClosing
	}
	return nil
}

func (a *App) ImageGenerate(ctx context.Context, in imagegen.Request) (imagegen.Result, error) {
	if in.ModelID == "" {
		in.ModelID = imagegen.DefaultModel
	}
	if in.Backend == "" {
		in.Backend = imagegen.BackendAuto
	}
	return a.imageRuntime.Generate(ctx, in)
}

func (a *App) ImageOutputPath(name string) (string, bool) {
	return a.imageRuntime.OutputPath(name)
}
