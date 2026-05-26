package match

// Manifest holds parsed Ludusavi entries.
// We only model the bits we actually use; the YAML is permissive about extra fields.
type ManifestEntry struct {
	// Alias points to another entry whose Files/Steam/etc we should use instead.
	// e.g. "Alan Wake 2" → "Alan Wake II".
	Alias    string                  `yaml:"alias,omitempty"`
	Files    map[string]FileSpec     `yaml:"files,omitempty"`
	Registry map[string]RegistrySpec `yaml:"registry,omitempty"`
	Steam    *struct {
		ID int64 `yaml:"id,omitempty"`
	} `yaml:"steam,omitempty"`
	GOG *struct {
		ID int64 `yaml:"id,omitempty"`
	} `yaml:"gog,omitempty"`
	InstallDir map[string]struct{} `yaml:"installDir,omitempty"`
}

type FileSpec struct {
	When []When     `yaml:"when,omitempty"`
	Tags []string   `yaml:"tags,omitempty"`
}

type RegistrySpec struct {
	When []When   `yaml:"when,omitempty"`
	Tags []string `yaml:"tags,omitempty"`
}

type When struct {
	OS    string `yaml:"os,omitempty"`
	Store string `yaml:"store,omitempty"`
}
