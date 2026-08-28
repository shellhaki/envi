//go:build windows

package keyring

func New() Store { return Store{"envi-windows"} }
