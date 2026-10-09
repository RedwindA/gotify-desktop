//go:build darwin

package main

import "gotify-desktop/internal/api"

// windowMaterial is the sidebar NSVisualEffect material. macOS has it on
// every version mygo runs on; VibrancySidebar selects it.
func windowMaterial() api.Material { return api.MaterialSidebar }
