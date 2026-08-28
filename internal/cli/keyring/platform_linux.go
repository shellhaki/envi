//go:build linux

package keyring

func New() Store { return Store{"envi-linux"} }
