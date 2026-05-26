package match

import _ "embed"

// manifestYAML holds the bundled Ludusavi manifest YAML. We download it on first
// build into internal/match/data/manifest.yaml; an empty fallback compiles fine.
//
//go:embed data/manifest.yaml
var manifestYAML []byte

// ManifestBytes returns the embedded manifest bytes.
func ManifestBytes() []byte { return manifestYAML }
