package llm

import (
	"fmt"
	"runtime"
)

// Pinned llama.cpp release. Bump deliberately (new pin + new digests), like
// the ORT lockstep — never float to "latest". Digests come from the GitHub
// release API (`gh api repos/ggml-org/llama.cpp/releases`, asset .digest).
const ServerBuild = "b9941"

const serverReleaseBase = "https://github.com/ggml-org/llama.cpp/releases/download/" + ServerBuild + "/"

// ServerAsset is one prebuilt llama.cpp bundle (llama-server + shared libs).
type ServerAsset struct {
	Name   string
	URL    string
	SHA256 string
	Size   int64
}

// Backend selects which llama.cpp build serves local inference. "auto" means
// cpu on Linux and the (Metal-enabled) native build on macOS; "vulkan" is the
// GPU path baked into the openvino container image. No Linux CUDA prebuilt
// exists upstream — the cuda image runs the cpu build until we bake one in.
const (
	BackendAuto   = "auto"
	BackendCPU    = "cpu"
	BackendVulkan = "vulkan"
)

var serverAssets = map[string]ServerAsset{
	"darwin/arm64": {
		Name:   "llama-" + ServerBuild + "-bin-macos-arm64.tar.gz",
		SHA256: "539ef8380ad5596ebce605051aee359a6e8247f427b5dad71af4a5019591092f",
		Size:   10717273,
	},
	"darwin/amd64": {
		Name:   "llama-" + ServerBuild + "-bin-macos-x64.tar.gz",
		SHA256: "a55ff7fc16d4b31f847634893192e8edb584009e6509781160f6e0dd207303fc",
		Size:   10992685,
	},
	"linux/amd64": {
		Name:   "llama-" + ServerBuild + "-bin-ubuntu-x64.tar.gz",
		SHA256: "ed027c0a929f265595c263f3da422379ae6c5dfd8a379bbc5871e4f587ce54c4",
		Size:   15808852,
	},
	"linux/amd64/vulkan": {
		Name:   "llama-" + ServerBuild + "-bin-ubuntu-vulkan-x64.tar.gz",
		SHA256: "3e51474effcfd09490c09b3585ca2dcb5fde9ddb7baf0dfbb40d48650715d41a",
		Size:   31158652,
	},
	"linux/arm64": {
		Name:   "llama-" + ServerBuild + "-bin-ubuntu-arm64.tar.gz",
		SHA256: "2e19ec280097af7c16424becce76bcce24822ff6d03a52e9dd14ac820b5a93f1",
		Size:   12760026,
	},
}

// ServerAssetFor picks the bundle for this platform + configured backend.
func ServerAssetFor(backend string) (ServerAsset, error) {
	key := runtime.GOOS + "/" + runtime.GOARCH
	if backend == BackendVulkan {
		key += "/vulkan"
	}
	a, ok := serverAssets[key]
	if !ok {
		return ServerAsset{}, fmt.Errorf("llm: no llama-server build for %s (backend %s)", key, backend)
	}
	a.URL = serverReleaseBase + a.Name
	return a, nil
}

// LocalModel is one curated GGUF the UI offers for download. The catalog is
// small and vetted: ungated hosts only (one-click download must work without
// a HuggingFace login), permissive licenses, sizes that fit homelab hardware.
type LocalModel struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	File    string `json:"file"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	RAMHint string `json:"ram_hint" doc:"rough resident footprint at the default context size"`
	Notes   string `json:"notes,omitempty"`
	// extraArgs are appended to the llama-server command line for
	// model-specific quirks (e.g. disabling hybrid thinking).
	extraArgs []string
}

// DefaultLocalModel is the catalog id preselected for new installs.
const DefaultLocalModel = "qwen3-4b-instruct-2507"

// LocalModels is the curated catalog, in display order.
var LocalModels = []LocalModel{
	{
		ID:      "qwen3-4b-instruct-2507",
		Label:   "Qwen3 4B Instruct 2507 (Q4_K_M) — recommended",
		File:    "Qwen3-4B-Instruct-2507-Q4_K_M.gguf",
		URL:     "https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/main/Qwen3-4B-Instruct-2507-Q4_K_M.gguf",
		SHA256:  "3605803b982cb64aead44f6c1b2ae36e3acdb41d8e46c8a94c6533bc4c67e597",
		Size:    2497281120,
		RAMHint: "~4 GB",
		Notes:   "Apache 2.0. Best quality/size balance; 256K-class context support.",
	},
	{
		ID:      "qwen3-1.7b",
		Label:   "Qwen3 1.7B (Q8_0) — small",
		File:    "Qwen3-1.7B-Q8_0.gguf",
		URL:     "https://huggingface.co/Qwen/Qwen3-1.7B-GGUF/resolve/main/Qwen3-1.7B-Q8_0.gguf",
		SHA256:  "061b54daade076b5d3362dac252678d17da8c68f07560be70818cace6590cb1a",
		Size:    1834426016,
		RAMHint: "~2.5 GB",
		Notes:   "Apache 2.0. For low-RAM boxes; hybrid-thinking disabled at serve time.",
		// Qwen3 (non-2507) is a hybrid-thinking model; without this it wraps
		// replies in <think> blocks that confuse non-chat consumers.
		extraArgs: []string{"--chat-template-kwargs", `{"enable_thinking":false}`},
	},
}

// LocalModelByID looks up a catalog entry. ok=false for unknown ids.
func LocalModelByID(id string) (LocalModel, bool) {
	for _, m := range LocalModels {
		if m.ID == id {
			return m, true
		}
	}
	return LocalModel{}, false
}
