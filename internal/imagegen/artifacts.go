package imagegen

import (
	"fmt"
	"runtime"
)

// RuntimeBuild pins stable-diffusion.cpp. Runtime and model artifacts are
// deliberately independent: changing accelerator never downloads a model.
const RuntimeBuild = "master-775-b5d8120"

const releaseBase = "https://github.com/leejet/stable-diffusion.cpp/releases/download/" + RuntimeBuild + "/"

const (
	BackendAuto   = "auto"
	BackendCPU    = "cpu"
	BackendCUDA   = "cuda"
	BackendROCm   = "rocm"
	BackendVulkan = "vulkan"
	BackendMetal  = "metal"
)

type Artifact struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

var runtimeArtifacts = map[string]Artifact{
	"darwin/arm64/metal":   {Name: "sd-master-b5d8120-bin-Darwin-macOS-26.4-arm64.zip", SHA256: "1b9a851bdaf787b63dca2f442e43685b42a5b76b6849ffeabeeb3b819541fb8f", Size: 49227335},
	"linux/amd64/cpu":      {Name: "sd-master-b5d8120-bin-Linux-Ubuntu-24.04-x86_64.zip", SHA256: "07df65eed50baa5156098743df800e177f00a8ccbd1f2fc628fc1117d6c08010", Size: 32233143},
	"linux/amd64/vulkan":   {Name: "sd-master-b5d8120-bin-Linux-Ubuntu-24.04-x86_64-vulkan.zip", SHA256: "968db72d1aa92cb4388c7e0ca5d2ec740bfd8d0ca7d21965a6ad70cf1c472501", Size: 44791302},
	"linux/amd64/rocm":     {Name: "sd-master-b5d8120-bin-Linux-Ubuntu-24.04-x86_64-rocm-7.2.1.zip", SHA256: "664e438d54b82961bfc2670df56db0f4bb4b6aeed40de2596c1aeb07acfc0b49", Size: 166764337},
	"windows/amd64/cpu":    {Name: "sd-master-b5d8120-bin-win-cpu-x64.zip", SHA256: "edd4a1b3f7a463452b4f96a04027637d6758d047b5b6d8d23d1e5770338f49bd", Size: 23685502},
	"windows/amd64/cuda":   {Name: "sd-master-b5d8120-bin-win-cuda12-x64.zip", SHA256: "cde1ec93569e148df6031306b694ebf2addc359b71ced5951b138b3cb7cbf2b9", Size: 361872369},
	"windows/amd64/rocm":   {Name: "sd-master-b5d8120-bin-win-rocm-7.1.1-x64.zip", SHA256: "2e25ccde0382c020a7448cc82f4b4b866ec4318e20c008856c27099abd556d13", Size: 328334916},
	"windows/amd64/vulkan": {Name: "sd-master-b5d8120-bin-win-vulkan-x64.zip", SHA256: "679e23655dc27700c016f0f256810902d3c0edae3f25e477b42bbb650d13497a", Size: 37680378},
}

func ResolveBackend(backend string) string {
	if backend != "" && backend != BackendAuto {
		return backend
	}
	if runtime.GOOS == "darwin" {
		return BackendMetal
	}
	return BackendCPU
}

func RuntimeArtifactFor(backend string) (Artifact, error) {
	resolved := ResolveBackend(backend)
	key := runtime.GOOS + "/" + runtime.GOARCH + "/" + resolved
	a, ok := runtimeArtifacts[key]
	if !ok {
		return Artifact{}, fmt.Errorf("imagegen: no prebuilt stable-diffusion.cpp runtime for %s", key)
	}
	a.URL = releaseBase + a.Name
	return a, nil
}

type Model struct {
	ID             string          `json:"id"`
	Label          string          `json:"label"`
	Artifacts      []ModelArtifact `json:"artifacts"`
	License        string          `json:"license"`
	RAMHint        string          `json:"ram_hint"`
	DefaultWidth   int             `json:"default_width"`
	DefaultHeight  int             `json:"default_height"`
	DefaultSteps   int             `json:"default_steps"`
	DefaultCFG     float64         `json:"default_cfg"`
	SamplingMethod string          `json:"sampling_method"`
	Scheduler      string          `json:"scheduler"`
	FlowShift      float64         `json:"flow_shift"`
}

type ModelArtifact struct {
	Role string `json:"role"`
	Artifact
	// SharedLLMFile permits reuse only of this exact, catalog-pinned LLM
	// artifact from <data>/llm/models. Presence is size-checked; downloads
	// remain owned by the explicit image fetch command when it is absent.
	SharedLLMFile string `json:"shared_llm_file,omitempty"`
}

const DefaultModel = "z-image-turbo-q4"

var Models = []Model{
	{
		ID: "z-image-turbo-q4", Label: "Z-Image Turbo Q4 — recommended",
		Artifacts: []ModelArtifact{
			{Role: "diffusion", Artifact: Artifact{Name: "z_image_turbo-Q4_K.gguf", URL: "https://huggingface.co/leejet/Z-Image-Turbo-GGUF/resolve/main/z_image_turbo-Q4_K.gguf", SHA256: "14b375ab4f226bc5378f68f37e899ef3c2242b8541e61e2bc1aff40976086fbd", Size: 3864250304}},
			{Role: "llm", Artifact: Artifact{Name: "Qwen3-4B-Instruct-2507-Q4_K_M.gguf", URL: "https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/main/Qwen3-4B-Instruct-2507-Q4_K_M.gguf", SHA256: "3605803b982cb64aead44f6c1b2ae36e3acdb41d8e46c8a94c6533bc4c67e597", Size: 2497281120}, SharedLLMFile: "Qwen3-4B-Instruct-2507-Q4_K_M.gguf"},
			// Byte-identical mirror of the FLUX.1/Z-Image autoencoder. The
			// upstream BFL repository requires a Hugging Face login even for
			// this Apache-licensed file, which breaks Heya's one-click fetch.
			{Role: "vae", Artifact: Artifact{Name: "ae.safetensors", URL: "https://huggingface.co/flux-safetensors/flux-safetensors/resolve/d3705fe7b6f2ed06621efc69ce91e99257481398/ae.safetensors?download=true&heya=1", SHA256: "afc8e28272cd15db3919bacdb6918ce9c1ed22e96cb12c4d5ed0fba823529e38", Size: 335304388}},
		},
		License: "Apache 2.0", RAMHint: "16 GB recommended (CPU supported)", DefaultWidth: 768, DefaultHeight: 768, DefaultSteps: 8, DefaultCFG: 1,
		SamplingMethod: "heun", Scheduler: "smoothstep", FlowShift: 2,
	},
}

func (m Model) DownloadSize() int64 {
	var n int64
	for _, a := range m.Artifacts {
		n += a.Size
	}
	return n
}

func ModelByID(id string) (Model, bool) {
	for _, m := range Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}
