//go:build !windows && !darwin

package main

import "gotify-desktop/internal/api"

// windowMaterial is none: Linux has no window material.
func windowMaterial() api.Material { return api.MaterialNone }
