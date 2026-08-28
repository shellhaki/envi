//go:build darwin

package keyring

func New() Store { return Store{"envi-macos"} }
